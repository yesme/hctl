#!/usr/bin/env python3
"""Pure derive/guardrail predicates for corpus (D-34..D-41). No network.

Commands print JSON or plain tokens; exit 0 on success of the check mode.
"""
from __future__ import annotations

import json
import re
import subprocess
import sys
from typing import Any, Optional

_TOKEN = re.compile(r"^[a-z][a-z0-9-]{0,31}$")


def is_ancestor(repo: str, old: str, new: str) -> bool:
    if old == new:
        return True
    r = subprocess.run(
        ["git", "-C", repo, "merge-base", "--is-ancestor", old, new],
        capture_output=True,
    )
    return r.returncode == 0


def classify_progress(
    kind: str,
    old: dict[str, Optional[str]],
    new: dict[str, Optional[str]],
    repo: Optional[str] = None,
) -> str:
    """Return forward | environment_reset | same | non_forward.

    old/new for gate/merge: {base, head}; author: {tip}.
    """
    if kind in ("gate", "merge"):
        ob, oh = old.get("base"), old.get("head")
        nb, nh = new.get("base"), new.get("head")
        if oh == nh and ob == nb:
            return "same"
        if oh == nh and ob != nb:
            return "environment_reset"
        if oh != nh:
            if repo is None:
                raise ValueError("repo required for ancestry")
            if oh and nh and is_ancestor(repo, oh, nh) and oh != nh:
                return "forward"
            return "non_forward"
        return "non_forward"
    if kind == "author":
        ot, nt = old.get("tip"), new.get("tip")
        if ot is None and nt is not None:
            return "forward"
        if ot == nt:
            return "same"
        if ot and nt and repo and is_ancestor(repo, ot, nt) and ot != nt:
            return "forward"
        return "non_forward"
    raise ValueError(kind)


def streak_after_reclaims(
    classifications: list[str],
    *,
    start_escalated: bool = False,
) -> dict[str, Any]:
    """Walk re-claim classifications (not including initial claim).

    classifications[i] describes transition into re-claim i+1 (1-based streak semantics).
    """
    escalated = start_escalated
    streak = 0
    color = "green"
    for c in classifications:
        if escalated:
            # progress does not clear red
            color = "red"
            continue
        if c in ("forward", "environment_reset"):
            streak = 0
            color = "green"
            continue
        # same / non_forward → replacement without progress
        streak += 1
        if streak == 1:
            color = "yellow"
        else:
            color = "red"
            escalated = True
    return {"streak": streak, "color": color, "escalated": escalated}


def claim_stale_gate(revision_at_claim: dict[str, str], current: dict[str, str]) -> bool:
    return (
        revision_at_claim["base"] != current["base"]
        or revision_at_claim["head"] != current["head"]
    )


def push_outcome_delivered(repo: str, pending: str, remote_tip: str) -> str:
    """delivered | lost — D-34 ancestry criterion."""
    if is_ancestor(repo, pending, remote_tip):
        return "delivered"
    # if tips equal without ancestry impossible; if diverge:
    r = subprocess.run(
        ["git", "-C", repo, "merge-base", pending, remote_tip],
        capture_output=True,
        text=True,
    )
    if r.returncode != 0:
        return "lost"
    # common ancestor exists but pending not ancestor of tip → diverged
    if pending != remote_tip and not is_ancestor(repo, pending, remote_tip):
        return "lost"
    return "delivered"



def three_way_event(ev: dict[str, Any], *, chain_seat: str, known_types: set[str]) -> str:
    """Narrow helper ONLY: envelope integrity + unknown type/major (D-39 prefix).

    Support domain (codex-27k §2.1 / terminal packet):
      - CORRUPT_CHAIN: missing/invalid envelope, seat mismatch
      - UNSUPPORTED_FACTS: unknown type or future major *after* valid envelope
      - KNOWN_TYPE_DEFER: known P1 type body is NOT classified here

    Normative OK/CORRUPT for known CLAIM/VERDICT/CANCEL is Go kernel only.
    Corpus must plant events and assert via hctl; never treat Python OK as gate evidence.
    """
    if not isinstance(ev, dict):
        return "CORRUPT_CHAIN"
    required_env = {"schema_version", "type", "actor", "created_at"}
    if not required_env.issubset(ev.keys()):
        return "CORRUPT_CHAIN"
    if "v" in ev:
        return "CORRUPT_CHAIN"
    major = ev["schema_version"]
    if not isinstance(major, int) or isinstance(major, bool) or major < 1:
        return "CORRUPT_CHAIN"
    t = ev["type"]
    if not isinstance(t, str) or not t:
        return "CORRUPT_CHAIN"
    actor = ev["actor"]
    if not isinstance(actor, dict):
        return "CORRUPT_CHAIN"
    if actor.get("seat") != chain_seat:
        return "CORRUPT_CHAIN"
    if not isinstance(actor.get("machine"), str) or not actor.get("machine"):
        return "CORRUPT_CHAIN"
    if "session" not in actor:
        return "CORRUPT_CHAIN"
    created = ev["created_at"]
    if not isinstance(created, str) or not created:
        return "CORRUPT_CHAIN"
    if t not in known_types:
        return "UNSUPPORTED_FACTS"
    if major > 1:
        return "UNSUPPORTED_FACTS"
    return "KNOWN_TYPE_DEFER"


def patterns_disjoint(patterns: list[str]) -> bool:
    """Conservative glob disjointness for refs/heads/... with single *."""

    def expand(p: str) -> tuple[str, str]:
        # return (prefix, suffix) around *
        if p.count("*") > 1:
            raise ValueError("at most one *")
        if "*" not in p:
            return p, ""
        pre, suf = p.split("*", 1)
        return pre, suf

    def overlap(a: str, b: str) -> bool:
        if a == b:
            return True
        # exact vs pattern
        ap, as_ = expand(a)
        bp, bs = expand(b)
        # crude: if both patterns, overlap if prefixes compatible
        # For doctor-level: two patterns overlap if there exists a string matching both.
        # Simplified: if one is prefix-compatible.
        def matches(pat: str, s: str) -> bool:
            pre, suf = expand(pat)
            if "*" not in pat:
                return pat == s
            return s.startswith(pre) and s.endswith(suf) and len(s) >= len(pre) + len(suf)

        # generate candidates from the non-star parts
        candidates = set()
        for p in (a, b):
            pre, suf = expand(p)
            if "*" not in p:
                candidates.add(p)
            else:
                candidates.add(pre + "x" + suf)
                candidates.add(pre + suf)
        for c in list(candidates):
            if matches(a, c) and matches(b, c):
                return True
        # also: if a is prefix of b's fixed form
        if "*" not in a and matches(b, a):
            return True
        if "*" not in b and matches(a, b):
            return True
        # work/claude/* vs memo/claude/* — different prefixes
        if ap != bp and not ap.startswith(bp) and not bp.startswith(ap):
            # still may overlap if one prefix contains the other path segment wrongly
            # e.g. refs/heads/work/* vs refs/heads/work/claude/*
            shorter, longer = (ap, bp) if len(ap) <= len(bp) else (bp, ap)
            if longer.startswith(shorter):
                return True
            return False
        return True

    for i in range(len(patterns)):
        for j in range(i + 1, len(patterns)):
            if overlap(patterns[i], patterns[j]):
                return False
    return True


def seats_patterns_ok(seats: dict[str, Any]) -> list[str]:
    """Return list of violations for cross seat/lane pattern overlap."""
    viol = []
    items: list[tuple[str, str, str]] = []  # seat, lane, pattern
    for seat, cfg in seats.items():
        for p in cfg.get("author_branch_patterns") or []:
            items.append((seat, "author", p))
        for p in cfg.get("memo_branch_patterns") or []:
            items.append((seat, "memo", p))
    for i in range(len(items)):
        for j in range(i + 1, len(items)):
            s1, l1, p1 = items[i]
            s2, l2, p2 = items[j]
            if not patterns_disjoint([p1, p2]):
                viol.append(f"{s1}/{l1}:{p1} overlaps {s2}/{l2}:{p2}")
    return viol


def merge_capacity_allows(active_merge_claims: int, capacity: int = 1) -> bool:
    return active_merge_claims < capacity


def loader_merge_assignee_ok(assignee: str, coordinator: str) -> bool:
    return assignee == coordinator


def reclaim_allowed(*, escalated: bool) -> bool:
    """D-34: escalated ⇒ frozen; re-claim denied until thaw path."""
    return not escalated


def thaw_escalated(path: str) -> bool:
    """D-34 three thaw paths only (no progress-based thaw)."""
    return path in ("cancel_claim", "cancel_obligation", "new_assignment_revision")


def rebind_allowed(*, active_merge_claims_old_coord: int) -> bool:
    """D-37: rebind blocked while old coordinator holds active merge claim."""
    return active_merge_claims_old_coord == 0


def assignment_revision_moved(prev_blob: str, cur_blob: str) -> bool:
    """D-33: any blob change (incl. non-semantic) is a new assignment revision."""
    return prev_blob != cur_blob


def integrity_before_progress(chain_linear: bool) -> Optional[str]:
    """D-34: ref/chain integrity runs before progress classification."""
    if not chain_linear:
        return "CORRUPT_CHAIN"
    return None


def receipt_config_ok(
    claim_config_blob: str,
    hctl_base_config_blob: str,
    *,
    phase: str,
) -> bool:
    """phase=pre-merge uses current; phase=receipt uses Hctl-Base (D-37 F3')."""
    if phase == "receipt":
        return claim_config_blob == hctl_base_config_blob
    if phase == "pre-merge":
        return claim_config_blob == hctl_base_config_blob  # caller passes current as second arg
    raise ValueError(phase)


def quorum_counts(
    verdict: dict[str, Any],
    *,
    required_coverage: str,
    expected_revision: Optional[dict[str, str]] = None,
) -> bool:
    """D-41 four conditions: exact revision + COMPLETE + APPROVE + covering scope.

    expected_revision is required (merge-time {base,head}). Passing None is a
    programming error — raise rather than silently skip freshness (claude F2′ / codex P2).
    """
    if expected_revision is None:
        raise TypeError("quorum_counts requires expected_revision={base,head}")
    rev = verdict.get("revision") or {}
    if rev.get("base") != expected_revision.get("base") or rev.get("head") != expected_revision.get("head"):
        return False
    if verdict.get("completeness") != "COMPLETE":
        return False
    if verdict.get("decision") != "APPROVE":
        return False
    scope = verdict.get("scope") or {}
    kind = scope.get("kind")
    if required_coverage == "full":
        if kind == "full":
            return True
        if kind == "composite":
            parts = scope.get("parts") or []
            kinds = {p.get("kind") for p in parts}
            if kinds == {"delta"}:
                return False
            if "full" in kinds:
                return True
            if kinds == {"fix_verification"}:
                return False  # fix_only is not a required_coverage mode for full surface
            if "fix_verification" in kinds and "delta" in kinds:
                return True
            return False
        return False
    return False


def latest_wins(verdicts: list[dict[str, Any]]) -> dict[str, Any]:
    """Last verdict in list for same (gater, obligation, revision) wins."""
    return verdicts[-1]


def main(argv: list[str]) -> int:
    if len(argv) < 2:
        print("usage: derive_rules.py <cmd> ...", file=sys.stderr)
        return 2
    cmd = argv[1]
    if cmd == "claim-stale":
        old = json.loads(argv[2])
        cur = json.loads(argv[3])
        print("stale" if claim_stale_gate(old, cur) else "fresh")
        return 0
    if cmd == "progress":
        kind = argv[2]
        repo = argv[3]
        old = json.loads(argv[4])
        new = json.loads(argv[5])
        print(classify_progress(kind, old, new, repo if repo != "-" else None))
        return 0
    if cmd == "streak":
        classes = json.loads(argv[2])
        print(json.dumps(streak_after_reclaims(classes)))
        return 0
    if cmd == "delivered":
        print(push_outcome_delivered(argv[2], argv[3], argv[4]))
        return 0
    if cmd == "three-way":
        ev = json.loads(argv[2])
        seat = argv[3]
        known = set(argv[4].split(","))
        print(three_way_event(ev, chain_seat=seat, known_types=known))
        return 0
    if cmd == "patterns":
        pats = json.loads(argv[2])
        print("ok" if patterns_disjoint(pats) else "overlap")
        return 0
    if cmd == "seats-patterns":
        seats = json.loads(sys.stdin.read() if argv[2] == "-" else argv[2])
        v = seats_patterns_ok(seats)
        if v:
            print("\n".join(v))
            return 1
        print("ok")
        return 0
    if cmd == "capacity":
        n = int(argv[2])
        print("allow" if merge_capacity_allows(n) else "deny")
        return 0
    if cmd == "merge-assignee":
        print("ok" if loader_merge_assignee_ok(argv[2], argv[3]) else "deny")
        return 0
    if cmd == "receipt-config":
        print(
            "ok"
            if receipt_config_ok(argv[2], argv[3], phase=argv[4])
            else "deny"
        )
        return 0
    if cmd == "quorum":
        if len(argv) < 5:
            print("usage: derive_rules.py quorum <verdict-json> <coverage> <expected-revision-json>", file=sys.stderr)
            return 2
        v = json.loads(argv[2])
        cov = argv[3]
        rev = json.loads(argv[4])
        print("green" if quorum_counts(v, required_coverage=cov, expected_revision=rev) else "not-green")
        return 0
    if cmd == "latest-wins":
        vs = json.loads(argv[2])
        print(json.dumps(latest_wins(vs)))
        return 0
    print(f"unknown cmd {cmd}", file=sys.stderr)
    return 2


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))

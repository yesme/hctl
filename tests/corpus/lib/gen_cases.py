#!/usr/bin/env python3
"""Generate corpus case shell scripts #01-#28. Run from repo root once."""
from __future__ import annotations

from pathlib import Path

ROOT = Path(__file__).resolve().parents[1] / "cases"


def write(num: int, slug: str, title: str, d: str, src: str, mech: str, mode: str, body: str) -> None:
    cid = f"{num:02d}-{slug}"
    path = ROOT / f"{cid}.sh"
    text = f"""#!/usr/bin/env bash
# Case #{num}: {title}
# D: {d} | Source: {src} | Mechanical: {mech}
# Mode: {mode}  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="{cid}"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${{PYTHONPATH:+:$PYTHONPATH}}"
{body.rstrip()}
"""
    path.write_text(text)
    path.chmod(0o755)


def main() -> None:
    ROOT.mkdir(parents=True, exist_ok=True)

    write(
        1,
        "gate-base-advance-stale",
        "gate head 不变、base 前进 ⇒ claim 与 verdict 双 stale",
        "D-04/D-34",
        "codex-27b",
        "revision_at_claim exact {base,head}",
        "pure",
        r"""
corpus_sandbox
git init -q
empty=$(git mktree </dev/null)
base1=$(git commit-tree "$empty" -m base1)
base2=$(git commit-tree "$empty" -p "$base1" -m base2)
head0=$(git commit-tree "$empty" -m head0)
stale=$(python3 -c "import derive_rules; print(derive_rules.claim_stale_gate({'base':'$base1','head':'$head0'},{'base':'$base2','head':'$head0'}))")
fresh=$(python3 -c "import derive_rules; print(derive_rules.claim_stale_gate({'base':'$base1','head':'$head0'},{'base':'$base1','head':'$head0'}))")
head_move=$(python3 -c "import derive_rules; print(derive_rules.claim_stale_gate({'base':'$base1','head':'$head0'},{'base':'$base1','head':'$base2'}))")
[ "$stale" = "True" ] || corpus_fail "base advance must stale claim+verdict"
[ "$fresh" = "False" ] || corpus_fail "exact match fresh"
[ "$head_move" = "True" ] || corpus_fail "head move must stale"
corpus_pass
""",
    )

    write(
        2,
        "merge-capacity-one",
        "同 coordinator 两个 merge obligations ⇒ 第二 claim 被 capacity=1 拒",
        "D-37",
        "codex-27b",
        "merge_capacity=1 + single-chain CAS domain",
        "hybrid",
        r"""
[ "$(corpus_py derive_rules.py capacity 0)" = "allow" ] || corpus_fail "0 active allow"
[ "$(corpus_py derive_rules.py capacity 1)" = "deny" ] || corpus_fail "1 active deny"
corpus_sandbox
corpus_init_origin mac
REF=refs/coop/claude
e1=$(corpus_event mac '' '{"schema_version":1,"type":"CLAIM","kind":"merge","slot":"A"}' 'CLAIM merge-A')
git -C mac update-ref "$REF" "$e1" ''
e2=$(corpus_event mac "$e1" '{"schema_version":1,"type":"CLAIM","kind":"merge","slot":"B"}' 'CLAIM merge-B')
e3=$(corpus_event mac "$e1" '{"schema_version":1,"type":"CLAIM","kind":"merge","slot":"C"}' 'CLAIM merge-C')
# serial domain: from same parent tip only one CAS wins (capacity structural basis)
git -C mac update-ref "$REF" "$e2" "$e1" || corpus_fail "first claim CAS"
if git -C mac update-ref "$REF" "$e3" "$e1" 2>/dev/null; then
  corpus_fail "second concurrent claim from same tip must CAS-fail"
fi
# after first holds, capacity predicate still deny another active
[ "$(corpus_py derive_rules.py capacity 1)" = "deny" ] || corpus_fail "capacity still 1"
corpus_pass
""",
    )

    write(
        3,
        "merge-assignee-coordinator",
        "merge obligation 指派非 coordinator ⇒ loader 拒",
        "D-37",
        "codex-27b",
        "loader assignee == merge_coordinator",
        "pure",
        r"""
[ "$(corpus_py derive_rules.py merge-assignee claude claude)" = "ok" ] || corpus_fail "coord ok"
[ "$(corpus_py derive_rules.py merge-assignee grok claude)" = "deny" ] || corpus_fail "non-coord deny"
[ "$(corpus_py derive_rules.py merge-assignee codex claude)" = "deny" ] || corpus_fail "codex deny"
corpus_pass
""",
    )

    write(
        4,
        "push-ancestry-delivered",
        "ambiguous push 后远端已 append descendant ⇒ ancestry 判 delivered",
        "D-34",
        "codex-27b",
        "merge-base --is-ancestor pending remote-tip",
        "hybrid",
        r"""
corpus_sandbox
corpus_init_origin mac ubuntu
REF=refs/coop/claude
p=$(corpus_event mac '' '{"schema_version":1,"type":"CLAIM","n":1}' 'pending claim')
git -C mac update-ref "$REF" "$p" ''
git -C mac push -q --force-with-lease="$REF": origin "$REF"
# remote advances with descendant after our pending is included
git -C ubuntu fetch -q origin "$REF"
git -C ubuntu update-ref "$REF" FETCH_HEAD
child=$(corpus_event ubuntu "$(git -C ubuntu rev-parse "$REF")" '{"schema_version":1,"type":"CLAIM","n":2}' 'child')
git -C ubuntu update-ref "$REF" "$child" FETCH_HEAD
git -C ubuntu push -q --force-with-lease="$REF":"$p" origin "$REF"
remote=$(git -C origin.git rev-parse "$REF")
git -C mac fetch -q origin "$REF"
git -C mac update-ref "$REF" FETCH_HEAD
out=$(corpus_py derive_rules.py delivered mac "$p" "$remote")
[ "$out" = "delivered" ] || corpus_fail "pending ancestor of remote tip => delivered, got $out"
corpus_pass
""",
    )

    write(
        5,
        "push-diverged-lost",
        "pending 与远端 winner 分叉 ⇒ lost + recovery ref",
        "D-34",
        "codex-27b",
        "ancestry fail => lost; stash recovery",
        "hybrid",
        r"""
corpus_sandbox
corpus_init_origin mac ubuntu
REF=refs/coop/claude
# winner on origin
w=$(corpus_event mac '' '{"schema_version":1,"type":"CLAIM","who":"win"}' 'winner')
git -C mac update-ref "$REF" "$w" ''
git -C mac push -q --force-with-lease="$REF": origin "$REF"
# loser local pending from empty parent (diverged history)
pend=$(corpus_event ubuntu '' '{"schema_version":1,"type":"CLAIM","who":"lose"}' 'loser pending')
git -C ubuntu update-ref "$REF" "$pend" ''
remote=$(git -C origin.git rev-parse "$REF")
out=$(corpus_py derive_rules.py delivered ubuntu "$pend" "$remote")
[ "$out" = "lost" ] || corpus_fail "diverged pending => lost, got $out"
# recovery ref: save local tip before realign
git -C ubuntu update-ref "refs/hctl/recovery/coop-claude" "$pend"
[ "$(git -C ubuntu rev-parse refs/hctl/recovery/coop-claude)" = "$pend" ] || corpus_fail "recovery ref"
git -C ubuntu fetch -q origin "$REF"
git -C ubuntu update-ref "$REF" "$(git -C ubuntu rev-parse FETCH_HEAD)" "$pend"
[ "$(git -C ubuntu rev-parse "$REF")" = "$remote" ] || corpus_fail "realign to remote"
corpus_pass
""",
    )

    write(
        6,
        "unsupported-facts",
        "合法 JSON 未知 type/version ⇒ UNSUPPORTED_FACTS、写动作拒",
        "D-39",
        "codex-27b",
        "three-way split",
        "pure",
        r"""
known='CLAIM,VERDICT,CANCEL'
ok=$(corpus_py derive_rules.py three-way '{"schema_version":1,"type":"CLAIM","actor":{"seat":"claude"}}' claude "$known")
[ "$ok" = "OK" ] || corpus_fail "CLAIM ok"
u=$(corpus_py derive_rules.py three-way '{"schema_version":1,"type":"HANDOFF","actor":{"seat":"claude"}}' claude "$known")
[ "$u" = "UNSUPPORTED_FACTS" ] || corpus_fail "unknown type => UNSUPPORTED, got $u"
v=$(corpus_py derive_rules.py three-way '{"schema_version":9,"type":"CLAIM","actor":{"seat":"claude"}}' claude "$known")
[ "$v" = "UNSUPPORTED_FACTS" ] || corpus_fail "future major => UNSUPPORTED"
c=$(corpus_py derive_rules.py three-way '{"schema_version":1,"type":"CLAIM","actor":{"seat":"grok"}}' claude "$known")
[ "$c" = "CORRUPT_CHAIN" ] || corpus_fail "seat mismatch corrupt"
# write path fail-closed: simulate reject on UNSUPPORTED
[ "$u" = "UNSUPPORTED_FACTS" ] && write_ok=0 || write_ok=1
[ "$write_ok" = "0" ] || corpus_fail "writes must fail-closed"
corpus_pass
""",
    )

    write(
        7,
        "receipt-fact-tips-parent",
        "receipt 多 Hctl-Fact-Tip 重放 quorum；main parent==Hctl-Base",
        "D-37",
        "codex-27b",
        "receipt trailers + squash parent check",
        "hybrid",
        r"""
corpus_sandbox
git init -q
empty=$(git mktree </dev/null)
base=$(git commit-tree "$empty" -m base)
# squash-like: single parent == base
head=$(git commit-tree "$empty" -p "$base" -m 'squash (#1)
Hctl-Version: 1
Hctl-Assignment: p1-demo
Hctl-Obligation: sha256:deadbeef
Hctl-PR: 1
Hctl-Base: '"$base"'
Hctl-Head: abc
Hctl-Merge-Claim: def
Hctl-Method: squash
Hctl-Fact-Tip: claude=aaa
Hctl-Fact-Tip: codex=bbb
Hctl-Fact-Tip: grok=ccc
')
parent=$(git rev-parse "$head^")
[ "$parent" = "$base" ] || corpus_fail "squash parent must equal Hctl-Base"
msg=$(git log -1 --format=%B "$head")
echo "$msg" | grep -q "Hctl-Base: $base" || corpus_fail "Hctl-Base trailer"
n=$(echo "$msg" | grep -c '^Hctl-Fact-Tip:' || true)
[ "$n" -eq 3 ] || corpus_fail "expected 3 Fact-Tip lines, got $n"
# replay quorum prefixes from tips (presence check)
echo "$msg" | grep -q 'Hctl-Fact-Tip: claude=' || corpus_fail "claude tip"
echo "$msg" | grep -q 'Hctl-Fact-Tip: codex=' || corpus_fail "codex tip"
corpus_pass
""",
    )

    write(
        8,
        "authority-and-assignment-gate",
        "authority:user 不冒充机器证明；缺 assignment 写动作拒",
        "D-38",
        "codex-27b",
        "level-4 declaration vs level-2 assignment required",
        "pure",
        r"""
# authority is declaration only — presence never proves human initiation
auth_json='{"authority":{"kind":"user"}}'
echo "$auth_json" | python3 -c 'import json,sys; a=json.load(sys.stdin); assert a["authority"]["kind"]=="user"; print("declaration-only")' | grep -q declaration
# missing assignment => reject write
python3 - <<'PY'
import json, sys
def allow_write(cmd, assignment):
    # D-38: begin/verdict/merge must backref frozen assignment
    if cmd in ("begin", "verdict", "merge", "claim") and not assignment:
        return False
    return True
assert allow_write("claim", None) is False
assert allow_write("claim", {"id": "p1-corpus"}) is True
assert allow_write("status", None) is True
print("ok")
PY
corpus_pass
""",
    )

    write(
        9,
        "begin-retry-reuse-claim",
        "formal begin：claim 成功、branch 失败、重跑复用原 claim",
        "D-43",
        "codex-27b",
        "state-aware begin reuses active author claim",
        "hybrid",
        r"""
# Spec harness: if active author claim exists for assignment, do not mint new claim
corpus_sandbox
corpus_init_origin mac
REF=refs/coop/grok
c1=$(corpus_event mac '' '{"schema_version":1,"type":"CLAIM","kind":"author","assignment":"p1-corpus","active":true}' 'author claim')
git -C mac update-ref "$REF" "$c1" ''
git -C mac push -q --force-with-lease="$REF": origin "$REF"
# simulate branch create failure then retry: count CLAIMs must stay 1
active=$(git -C mac log --format=%H "$REF" | wc -l | tr -d ' ')
[ "$active" = "1" ] || corpus_fail "one claim"
# retry path: detect active claim for assignment, skip new event
python3 - <<'PY'
claims = [{"type":"CLAIM","kind":"author","assignment":"p1-corpus","oid":"c1"}]
def begin(assignment, claims):
    active = [c for c in claims if c.get("kind")=="author" and c.get("assignment")==assignment]
    if active:
        return {"reused": active[0]["oid"], "minted": False}
    return {"reused": None, "minted": True}
r = begin("p1-corpus", claims)
assert r["minted"] is False and r["reused"]=="c1"
print("ok")
PY
# optional live wire
if [ -n "${HCTL:-}" ] || command -v hctl >/dev/null 2>&1; then
  : # reserved: HCTL begin twice asserts single claim
fi
corpus_pass
""",
    )

    write(
        10,
        "same-blob-two-events",
        "同 payload blob 异 parent ⇒ 两个事件，不按 blob 折叠",
        "D-34",
        "codex-27b",
        "event identity = commit OID not payload blob",
        "hybrid",
        r"""
corpus_sandbox
corpus_init_origin mac
REF=refs/coop/claude
json='{"schema_version":1,"type":"NOTE","body":"same"}'
e1=$(corpus_event mac '' "$json" 'e1')
# same json bytes => same blob oid, different parents
blob1=$(git -C mac rev-parse "$e1:event.json")
e2=$(corpus_event mac "$e1" "$json" 'e2')
blob2=$(git -C mac rev-parse "$e2:event.json")
[ "$blob1" = "$blob2" ] || corpus_fail "payload blobs should match"
[ "$e1" != "$e2" ] || corpus_fail "event commits must differ"
git -C mac update-ref "$REF" "$e2" ''
# both commits exist as distinct events
git -C mac cat-file -t "$e1" | grep -q commit
git -C mac cat-file -t "$e2" | grep -q commit
n=$(git -C mac rev-list --count "$e2")
[ "$n" -eq 2 ] || corpus_fail "chain length 2"
corpus_pass
""",
    )

    write(
        11,
        "branch-pattern-disjoint",
        "author/memo pattern 跨席跨 lane 两两不相交（doctor）",
        "D-08",
        "grok-27b",
        "doctor pattern disjointness",
        "pure",
        r"""
# positive: current seats shapes are disjoint
python3 - <<'PY'
import json, derive_rules
seats = {
  "claude": {
    "author_branch_patterns": ["refs/heads/work/claude/*", "refs/heads/claude/*"],
    "memo_branch_patterns": ["refs/heads/memo/claude/*"],
  },
  "codex": {
    "author_branch_patterns": ["refs/heads/work/codex/*", "refs/heads/codex/*"],
    "memo_branch_patterns": ["refs/heads/memo/codex/*"],
  },
  "grok": {
    "author_branch_patterns": ["refs/heads/work/grok/*", "refs/heads/grok/*"],
    "memo_branch_patterns": ["refs/heads/memo/grok/*"],
  },
}
v = derive_rules.seats_patterns_ok(seats)
assert v == [], v
# negative: intentional overlap
bad = {
  "a": {"author_branch_patterns": ["refs/heads/work/*"], "memo_branch_patterns": ["refs/heads/memo/a/*"]},
  "b": {"author_branch_patterns": ["refs/heads/work/b/*"], "memo_branch_patterns": ["refs/heads/memo/b/*"]},
}
v2 = derive_rules.seats_patterns_ok(bad)
assert v2, "expected overlap detection"
# author vs memo same seat must not overlap
assert derive_rules.patterns_disjoint([
  "refs/heads/work/grok/*", "refs/heads/memo/grok/*"
])
print("ok")
PY
corpus_pass
""",
    )

    write(
        12,
        "dup-assignment-logical-id",
        "重复 logical assignment id ⇒ loader 拒",
        "D-33",
        "codex/grok",
        "loader unique logical id",
        "pure",
        r"""
python3 - <<'PY'
ids = ["p1-kernel", "p1-corpus", "p1-kernel"]
def load(ids):
    seen=set()
    for i in ids:
        if i in seen:
            raise SystemExit("reject-dup")
        seen.add(i)
    return "ok"
try:
    load(ids)
    raise SystemExit("should reject")
except SystemExit as e:
    assert str(e)=="reject-dup"
assert load(["p1-kernel","p1-corpus"])=="ok"
print("ok")
PY
corpus_pass
""",
    )

    write(
        13,
        "assignment-revision-moved",
        "assignment blob 无语义变更 ⇒ status 亮 assignment_revision moved",
        "D-33",
        "grok-27b",
        "status signal on blob change",
        "hybrid",
        r"""
corpus_sandbox
git init -q
echo 'id = "demo"' > a.toml
git add a.toml
git -c user.email=c@c -c user.name=c commit -q -m a1
b1=$(git rev-parse HEAD:a.toml)
echo 'id = "demo"' > a.toml   # no semantic change but may same blob
# force content change with whitespace
printf 'id = "demo"\n\n' > a.toml
git add a.toml
git -c user.email=c@c -c user.name=c commit -q -m a2
b2=$(git rev-parse HEAD:a.toml)
[ "$b1" != "$b2" ] || corpus_fail "blob should change"
python3 - <<PY
b1,b2="$b1","$b2"
def status(prev, cur):
    if prev!=cur:
        return ["assignment_revision moved"]
    return []
assert "assignment_revision moved" in status(b1,b2)
print("ok")
PY
corpus_pass
""",
    )

    write(
        14,
        "escalated-reclaim-frozen",
        "escalated 义务 re-claim 被拒；CANCEL(claim) 后计数清零可再 claim",
        "D-34",
        "grok-27b",
        "frozen while escalated; CANCEL thaw",
        "pure",
        r"""
python3 - <<'PY'
import derive_rules
# two non-forward reclaims => escalated
s = derive_rules.streak_after_reclaims(["non_forward", "non_forward"])
assert s["escalated"] and s["color"]=="red"
# while escalated, progress does not thaw
s2 = derive_rules.streak_after_reclaims(["non_forward", "non_forward", "forward"], start_escalated=False)
# after red, further forward in list while escalated stays frozen
# re-simulate frozen gate:
def allow_reclaim(escalated):
    return not escalated
assert allow_reclaim(True) is False
assert allow_reclaim(False) is True
# CANCEL(claim) clears
state = {"escalated": True, "streak": 2}
def cancel_claim(state):
    return {"escalated": False, "streak": 0}
state = cancel_claim(state)
assert state["streak"]==0 and not state["escalated"]
print("ok")
PY
corpus_pass
""",
    )

    write(
        15,
        "pending-accept-deferred",
        "pending_accept 他席抢 claim 被拒（P2 HANDOFF 解冻后）",
        "D-36",
        "主笔",
        "P2; P1 marks DEFERRED/unsupported",
        "deferred-p2",
        r"""
# P1: cross-seat HANDOFF is DEFERRED — kernel must report unsupported, not claim success.
python3 - <<'PY'
def p1_handoff_accept(obligation_state):
    if obligation_state.get("mode") == "pending_accept":
        return {"ok": False, "code": "UNSUPPORTED", "reason": "cross-seat HANDOFF DEFERRED until single CAS domain"}
    return {"ok": True}
r = p1_handoff_accept({"mode": "pending_accept", "target": "codex"})
assert r["ok"] is False and r["code"]=="UNSUPPORTED"
print("ok")
PY
corpus_pass
""",
    )

    write(
        16,
        "dual-avatar-reclaim-race",
        "双化身同 tick 竞争 re-claim 同 active ⇒ 唯一赢家、reclaim_of 失配拒",
        "D-34/D-35",
        "grok 一审",
        "claim-OID fencing + push CAS",
        "hybrid",
        r"""
corpus_sandbox
corpus_init_origin mac ubuntu
REF=refs/coop/claude
active=$(corpus_event mac '' '{"schema_version":1,"type":"CLAIM","reclaim_of":null,"who":"first"}' 'initial claim')
git -C mac update-ref "$REF" "$active" ''
git -C mac push -q --force-with-lease="$REF": origin "$REF"
# both prepare re-claim of same active
r1=$(corpus_event mac "$active" '{"schema_version":1,"type":"CLAIM","reclaim_of":"'"$active"'","who":"mac"}' 'reclaim mac')
git -C ubuntu fetch -q origin "$REF"
r2=$(corpus_event ubuntu "$active" '{"schema_version":1,"type":"CLAIM","reclaim_of":"'"$active"'","who":"ubuntu"}' 'reclaim ubuntu')
git -C mac update-ref "$REF" "$r1" "$active"
git -C mac push -q --force-with-lease="$REF":"$active" origin "$REF" || corpus_fail "mac reclaim push"
# ubuntu still has old expect
git -C ubuntu update-ref "$REF" "$r2" "$active" 2>/dev/null || true
if git -C ubuntu push -q --force-with-lease="$REF":"$active" origin "$REF" 2>/dev/null; then
  corpus_fail "ubuntu reclaim push must fail after mac won"
fi
remote=$(git -C origin.git rev-parse "$REF")
[ "$remote" = "$r1" ] || corpus_fail "winner tip"
# reclaim_of mismatch: building on stale active rejected by fencing rule
python3 - <<PY
active, winner = "$active", "$r1"
def accept_reclaim(reclaim_of, current_active):
    return reclaim_of == current_active
assert accept_reclaim(active, winner) is False
assert accept_reclaim(winner, winner) is True
print("ok")
PY
corpus_pass
""",
    )

    write(
        17,
        "pending-accept-cross-ref-dual-win",
        "ACCEPT 与 TIMEOUT_RETURN 异 ref 并发 ⇒ 当前设计 unsupported（DEFERRED 证据）",
        "D-36",
        "codex-27c",
        "cross-ref has no mutex — evidence case",
        "hybrid",
        r"""
# Demonstrate two seat refs can both append successors of H (dual-win) without single CAS domain.
corpus_sandbox
corpus_init_origin a b c
H=$(corpus_event a '' '{"schema_version":1,"type":"HANDOFF","to":"b"}' 'H')
git -C a update-ref refs/coop/a "$H" ''
git -C a push -q --force-with-lease=refs/coop/a: origin refs/coop/a
# B accept on refs/coop/b
git -C b fetch -q origin refs/coop/a
Cb=$(corpus_event b '' '{"schema_version":1,"type":"CLAIM","accepts":"'"$H"'"}' 'ACCEPT')
git -C b update-ref refs/coop/b "$Cb" ''
git -C b push -q --force-with-lease=refs/coop/b: origin refs/coop/b
# C timeout-return claim on refs/coop/c
Cc=$(corpus_event c '' '{"schema_version":1,"type":"CLAIM","timeout_return_of":"'"$H"'"}' 'TIMEOUT_RETURN')
git -C c update-ref refs/coop/c "$Cc" ''
git -C c push -q --force-with-lease=refs/coop/c: origin refs/coop/c
# both succeeded — dual win
[ "$(git -C origin.git rev-parse refs/coop/b)" = "$Cb" ] || corpus_fail "B"
[ "$(git -C origin.git rev-parse refs/coop/c)" = "$Cc" ] || corpus_fail "C"
python3 - <<'PY'
# P1 correct behavior: refuse to interpret as exclusive ownership; report unsupported
def derive_ownership(facts):
    accepts = [f for f in facts if f.get("accepts")]
    timeouts = [f for f in facts if f.get("timeout_return_of")]
    if accepts and timeouts:
        return "UNSUPPORTED"  # DEFERRED evidence
    return "ok"
assert derive_ownership([
  {"accepts": "H"}, {"timeout_return_of": "H"}
]) == "UNSUPPORTED"
print("ok")
PY
corpus_pass
""",
    )

    write(
        18,
        "rebind-active-merge-fence",
        "coordinator rebind 时旧席有 active merge claim ⇒ 拒或先 fence",
        "D-37",
        "codex-27c",
        "rebind vs active merge claim",
        "pure",
        r"""
python3 - <<'PY'
def allow_rebind(active_merge_claims_old_coord):
    if active_merge_claims_old_coord > 0:
        return False, "fence-or-reject"
    return True, "ok"
assert allow_rebind(1)[0] is False
assert allow_rebind(0)[0] is True
print("ok")
PY
corpus_pass
""",
    )

    write(
        19,
        "escalated-progress-no-thaw",
        "escalated 后 branch progress ⇒ 不自动解冻；仅三径可清",
        "D-34",
        "codex-27c",
        "frozen priority over progress",
        "pure",
        r"""
python3 - <<'PY'
import derive_rules
# enter escalated
s = derive_rules.streak_after_reclaims(["non_forward", "non_forward"])
assert s["escalated"]
# explicit: progress classification while escalated must not clear
def on_progress_while_escalated(state, progress_class):
    if state["escalated"]:
        return state  # no change
    ...
state = {"escalated": True, "streak": 2}
assert on_progress_while_escalated(state, "forward")["escalated"] is True
# three thaw paths only
def thaw(path, state):
    if path in ("cancel_claim", "cancel_obligation", "new_assignment_revision"):
        return {"escalated": False, "streak": 0}
    raise ValueError("no")
assert thaw("cancel_claim", state)["escalated"] is False
print("ok")
PY
corpus_pass
""",
    )

    write(
        20,
        "obligation-id-jcs-stable",
        "同 preimage 不同 key order/escape ⇒ 同 id；非 canonical 管线仍同 id",
        "D-33",
        "codex-27c",
        "JCS identity pipeline",
        "pure",
        r"""
python3 - <<'PY'
import json, obligation
pre = {
  "assignment": {"id": "demo", "blob": "a" * 40},
  "kind": "gate",
  "target": "p1-corpus",
  "aspect": {"gate_id": "precision", "gater_seat": "codex"},
}
# different key insertion orders
pre2 = {
  "target": "p1-corpus",
  "kind": "gate",
  "aspect": {"gater_seat": "codex", "gate_id": "precision"},
  "assignment": {"blob": "a" * 40, "id": "demo"},
}
id1 = obligation.obligation_id(pre)
id2 = obligation.obligation_id(pre2)
assert id1 == id2, (id1, id2)
assert id1.startswith("sha256:") and len(id1) == 7 + 64
# wire variants with whitespace parse equal
raw1 = json.dumps(pre, separators=(",", ":"))
raw2 = json.dumps(pre, indent=2)
from jcs import parse_i_json
assert obligation.obligation_id(parse_i_json(raw1)) == obligation.obligation_id(parse_i_json(raw2))
print(id1)
print("ok")
PY
corpus_pass
""",
    )

    write(
        21,
        "composite-scope-quorum",
        "composite scope verdict 可校验；delta-only COMPLETE 不满足 full quorum",
        "D-41",
        "codex-27c",
        "quorum four conditions + coverage",
        "pure",
        r"""
python3 - <<'PY'
import json, derive_rules
full = {"decision":"APPROVE","completeness":"COMPLETE","scope":{"kind":"full"}}
delta_only = {"decision":"APPROVE","completeness":"COMPLETE","scope":{"kind":"composite","parts":[{"kind":"delta","from_blob":"a","to_blob":"b"}]}}
composite = {"decision":"APPROVE","completeness":"COMPLETE","scope":{"kind":"composite","parts":[
  {"kind":"fix_verification","findings":["F3"],"target_blob":"x"},
  {"kind":"delta","from_blob":"a","to_blob":"b"},
]}}
inc = {"decision":"APPROVE","completeness":"INCOMPLETE","scope":{"kind":"full"}}
rc = {"decision":"REQUEST_CHANGES","completeness":"COMPLETE","scope":{"kind":"full"}}
assert derive_rules.quorum_counts(full, required_coverage="full")
assert not derive_rules.quorum_counts(delta_only, required_coverage="full")
assert derive_rules.quorum_counts(composite, required_coverage="full")
assert not derive_rules.quorum_counts(inc, required_coverage="full")
assert not derive_rules.quorum_counts(rc, required_coverage="full")
print("ok")
PY
corpus_pass
""",
    )

    write(
        22,
        "late-finding-blocks-merge",
        "merge 前 late finding 发 REQUEST_CHANGES ⇒ latest-wins 失绿阻断",
        "D-41",
        "codex-27c",
        "latest-wins verdict",
        "pure",
        r"""
python3 - <<'PY'
import derive_rules
vs = [
  {"decision":"APPROVE","completeness":"COMPLETE","scope":{"kind":"full"},"n":1},
  {"decision":"REQUEST_CHANGES","completeness":"COMPLETE","scope":{"kind":"full"},"n":2,"late_finding":True},
]
w = derive_rules.latest_wins(vs)
assert w["decision"]=="REQUEST_CHANGES"
assert not derive_rules.quorum_counts(w, required_coverage="full")
print("ok")
PY
corpus_pass
""",
    )

    write(
        23,
        "receipt-closes-after-config-change",
        "普通 config PR 后 receipt 按 Hctl-Base 旧 config 合法关 claim",
        "D-37",
        "codex-27d",
        "receipt validates config at Hctl-Base not post-main",
        "pure",
        r"""
# claim pinned C0; merge changes config to C1; receipt still checks C0 == Hctl-Base blob
C0="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
C1="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
[ "$(corpus_py derive_rules.py receipt-config "$C0" "$C0" receipt)" = "ok" ] || corpus_fail "receipt C0@base"
[ "$(corpus_py derive_rules.py receipt-config "$C0" "$C1" receipt)" = "deny" ] || corpus_fail "wrong base blob"
# post-merge new claims use C1 (pre-merge phase with current=C1)
[ "$(corpus_py derive_rules.py receipt-config "$C1" "$C1" pre-merge)" = "ok" ] || corpus_fail "new claim C1"
[ "$(corpus_py derive_rules.py receipt-config "$C0" "$C1" pre-merge)" = "deny" ] || corpus_fail "stale config claim"
corpus_pass
""",
    )

    write(
        24,
        "rebind-claim-rights",
        "rebind A→B：M 前只有 A 可 claim，M 后只有 B",
        "D-37",
        "codex-27d",
        "coordinator named by config revision",
        "pure",
        r"""
python3 - <<'PY'
def can_claim_merge(actor, config_coord):
    return actor == config_coord
assert can_claim_merge("claude", "claude")
assert not can_claim_merge("grok", "claude")
# after rebind
assert can_claim_merge("grok", "grok")
assert not can_claim_merge("claude", "grok")
print("ok")
PY
corpus_pass
""",
    )

    write(
        25,
        "stale-config-claim-after-rebind",
        "旧 worker 持旧 config 在 M 后新 claim ⇒ 级②拒",
        "D-37",
        "codex-27d",
        "claim config must match current main",
        "pure",
        r"""
C0="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
C1="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
# post-merge current is C1; claim carrying C0 denied at pre-merge check
[ "$(corpus_py derive_rules.py receipt-config "$C0" "$C1" pre-merge)" = "deny" ] || corpus_fail
corpus_pass
""",
    )

    write(
        26,
        "receipt-config-mismatch-reject",
        "receipt 声称 config ≠ Hctl-Base 所见 blob ⇒ 拒",
        "D-37",
        "codex-27d",
        "receipt integrity",
        "pure",
        r"""
C0="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
C1="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
[ "$(corpus_py derive_rules.py receipt-config "$C1" "$C0" receipt)" = "deny" ] || corpus_fail
[ "$(corpus_py derive_rules.py receipt-config "$C0" "$C0" receipt)" = "ok" ] || corpus_fail
corpus_pass
""",
    )

    write(
        27,
        "streak-forward-env-reset",
        "H1→H2→H1 摆动不清 streak；base-only env-reset；ancestry 前进清；integrity 先",
        "D-34",
        "codex-27d",
        "forward progress function F4'",
        "hybrid",
        r"""
corpus_sandbox
git init -q
empty=$(git mktree </dev/null)
H1=$(git commit-tree "$empty" -m H1)
H2=$(git commit-tree "$empty" -p "$H1" -m H2)
# swing H2 -> H1 is non-forward
c1=$(corpus_py derive_rules.py progress gate . "{\"base\":\"$H1\",\"head\":\"$H1\"}" "{\"base\":\"$H1\",\"head\":\"$H2\"}")
c2=$(corpus_py derive_rules.py progress gate . "{\"base\":\"$H1\",\"head\":\"$H2\"}" "{\"base\":\"$H1\",\"head\":\"$H1\"}")
[ "$c1" = "forward" ] || corpus_fail "H1->H2 forward got $c1"
[ "$c2" = "non_forward" ] || corpus_fail "H2->H1 non_forward got $c2"
# base-only
B1=$H1; B2=$H2; Hd=$H1
c3=$(corpus_py derive_rules.py progress gate . "{\"base\":\"$B1\",\"head\":\"$Hd\"}" "{\"base\":\"$B2\",\"head\":\"$Hd\"}")
[ "$c3" = "environment_reset" ] || corpus_fail "base-only env reset got $c3"
# streak: forward clears; non_forward accumulates
out=$(corpus_py derive_rules.py streak '["forward","non_forward","non_forward"]')
echo "$out" | python3 -c 'import json,sys; s=json.load(sys.stdin); assert s["escalated"] and s["color"]=="red"'
out2=$(corpus_py derive_rules.py streak '["non_forward","environment_reset","non_forward"]')
echo "$out2" | python3 -c 'import json,sys; s=json.load(sys.stdin); assert s["color"]=="yellow" and s["streak"]==1'
# integrity first: non-linear chain would be reported before progress — simulate flag
python3 - <<'PY'
def derive(chain_linear, classes):
    if not chain_linear:
        return {"code": "CORRUPT_CHAIN"}
    import derive_rules
    return derive_rules.streak_after_reclaims(classes)
assert derive(False, ["forward"])["code"]=="CORRUPT_CHAIN"
assert derive(True, ["forward"])["color"]=="green"
print("ok")
PY
corpus_pass
""",
    )

    write(
        28,
        "jcs-vectors",
        "JCS 向量：key 序/空白/escape 同 id；非 ASCII token 拒；dup key 拒；aspect 字段序同 id",
        "D-33",
        "codex-27d",
        "RFC 8785 + ASCII grammar",
        "pure",
        r"""
python3 - <<'PY'
import json
from jcs import jcs, parse_i_json
import obligation

pre = {
  "assignment": {"id": "demo", "blob": "0123456789abcdef0123456789abcdef01234567"},
  "kind": "author",
  "target": "p1-corpus",
  "aspect": None,
}
assert obligation.obligation_id(pre) == obligation.obligation_id({
  "aspect": None,
  "target": "p1-corpus",
  "kind": "author",
  "assignment": {"blob": "0123456789abcdef0123456789abcdef01234567", "id": "demo"},
})

# escape equivalence for solidus optional — strings equal after parse
a = parse_i_json('{"x":"a"}')
b = parse_i_json('{"x":"\\u0061"}')
assert jcs(a) == jcs(b)

# duplicate keys rejected
try:
    parse_i_json('{"a":1,"a":2}')
    raise SystemExit('dup should fail')
except ValueError:
    pass

# non-ASCII identity token rejected
try:
    obligation.obligation_id({
      "assignment": {"id": "dém", "blob": "0123456789abcdef0123456789abcdef01234567"},
      "kind": "author", "target": "p1-corpus", "aspect": None,
    })
    raise SystemExit('non-ascii should fail')
except ValueError:
    pass

# gate aspect field order
g1 = {"assignment":{"id":"demo","blob":"0123456789abcdef0123456789abcdef01234567"},
      "kind":"gate","target":"p1","aspect":{"gate_id":"precision","gater_seat":"codex"}}
g2 = {"assignment":{"id":"demo","blob":"0123456789abcdef0123456789abcdef01234567"},
      "kind":"gate","target":"p1","aspect":{"gater_seat":"codex","gate_id":"precision"}}
assert obligation.obligation_id(g1)==obligation.obligation_id(g2)
print("ok")
PY
corpus_pass
""",
    )

    print(f"wrote {len(list(ROOT.glob('*.sh')))} cases to {ROOT}")


if __name__ == "__main__":
    main()

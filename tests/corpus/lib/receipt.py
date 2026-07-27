#!/usr/bin/env python3
"""Closed-set receipt parse/shape checks (differential oracle for D-37).

Normative authority is Go internal/receipt. This module mirrors the closed field
set, full-length OID rules, and single-parent squash parent==Hctl-Base check so
corpus #7 can assert fixture legality without inventing a second semantics.
"""
from __future__ import annotations

import re
import subprocess
import sys
from typing import Any

_TOKEN = re.compile(r"^[a-z][a-z0-9-]{0,31}$")
_OID = re.compile(r"^([0-9a-f]{40}|[0-9a-f]{64})$")
_OBL = re.compile(r"^sha256:[0-9a-f]{64}$")

ALLOWED_SINGLE = {
    "Hctl-Version",
    "Hctl-Assignment",
    "Hctl-Obligation",
    "Hctl-PR",
    "Hctl-Base",
    "Hctl-Head",
    "Hctl-Merge-Claim",
    "Hctl-Method",
}
REQUIRED = list(ALLOWED_SINGLE)


def parse(message: str) -> dict[str, Any]:
    single: dict[str, str] = {}
    tips: dict[str, str] = {}
    for line in message.splitlines():
        if not line.startswith("Hctl-"):
            continue
        if ": " not in line:
            raise ValueError(f"malformed receipt field {line!r}")
        key, value = line.split(": ", 1)
        if key == "Hctl-Fact-Tip":
            if "=" not in value:
                raise ValueError(f"invalid Hctl-Fact-Tip {value!r}")
            seat, oid = value.split("=", 1)
            if not _TOKEN.match(seat) or not _OID.match(oid):
                raise ValueError(f"invalid Hctl-Fact-Tip {value!r}")
            if seat in tips:
                raise ValueError(f"duplicate Hctl-Fact-Tip seat {seat!r}")
            tips[seat] = oid
            continue
        if key not in ALLOWED_SINGLE:
            raise ValueError(f"unknown receipt field {key}")
        if key in single:
            raise ValueError(f"duplicate receipt field {key}")
        single[key] = value
    for key in REQUIRED:
        if key not in single:
            raise ValueError(f"missing receipt field {key}")
    version = int(single["Hctl-Version"])
    if version != 1:
        raise ValueError("Hctl-Version must be 1")
    pr = int(single["Hctl-PR"])
    if not _TOKEN.match(single["Hctl-Assignment"]):
        raise ValueError("invalid Hctl-Assignment")
    if not _OBL.match(single["Hctl-Obligation"]):
        raise ValueError("invalid Hctl-Obligation")
    for k in ("Hctl-Base", "Hctl-Head", "Hctl-Merge-Claim"):
        if not _OID.match(single[k]):
            raise ValueError(f"invalid {k}")
    if single["Hctl-Method"] != "squash":
        raise ValueError("Hctl-Method must be squash")
    if not tips:
        raise ValueError("at least one Hctl-Fact-Tip required for this corpus case")
    return {
        "version": version,
        "assignment": single["Hctl-Assignment"],
        "obligation": single["Hctl-Obligation"],
        "pr": pr,
        "base": single["Hctl-Base"],
        "head": single["Hctl-Head"],
        "merge_claim": single["Hctl-Merge-Claim"],
        "method": single["Hctl-Method"],
        "fact_tips": tips,
    }


def verify_squash_parent(repo: str, merge_commit: str, expected_base: str) -> None:
    parents = subprocess.check_output(
        ["git", "-C", repo, "rev-list", "--parents", "-n", "1", merge_commit],
        text=True,
    ).strip().split()
    # format: commit parent1 [parent2...]
    if len(parents) != 2:
        raise ValueError(f"squash must have exactly one parent, got {parents}")
    if parents[1] != expected_base:
        raise ValueError(f"parent {parents[1]} != Hctl-Base {expected_base}")


def main(argv: list[str]) -> int:
    if len(argv) < 2:
        print("usage: receipt.py parse <file|-> | verify-parent <repo> <merge> <base>", file=sys.stderr)
        return 2
    cmd = argv[1]
    if cmd == "parse":
        raw = sys.stdin.read() if argv[2] == "-" else open(argv[2], encoding="utf-8").read()
        r = parse(raw)
        print(f"ok tips={len(r['fact_tips'])} assignment={r['assignment']}")
        return 0
    if cmd == "verify-parent":
        verify_squash_parent(argv[2], argv[3], argv[4])
        print("ok")
        return 0
    print(f"unknown cmd {cmd}", file=sys.stderr)
    return 2


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))

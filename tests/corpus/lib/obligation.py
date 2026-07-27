#!/usr/bin/env python3
"""Obligation identity pipeline (D-33): parse → validate → JCS → sha256."""
from __future__ import annotations

import hashlib
import json
import sys
from typing import Any

from jcs import jcs, parse_i_json, validate_identity_token, validate_oid

DOMAIN = b"hctl-obligation-v1\0"


def validate_preimage(pre: dict[str, Any]) -> None:
    required = {"assignment", "kind", "target", "aspect"}
    if set(pre.keys()) != required:
        extra = set(pre.keys()) - required
        missing = required - set(pre.keys())
        raise ValueError(f"preimage keys must be exactly {sorted(required)}; missing={missing} extra={extra}")
    kind = pre["kind"]
    if kind not in ("author", "gate", "merge"):
        raise ValueError(f"kind not in P1 closed set: {kind!r}")
    validate_identity_token(pre["target"])
    asg = pre["assignment"]
    if not isinstance(asg, dict):
        raise ValueError("assignment must be object")
    if "id" in asg and "blob" in asg:
        validate_identity_token(asg["id"])
        validate_oid(asg["blob"])
        if set(asg.keys()) != {"id", "blob"}:
            raise ValueError("static assignment must be {id,blob} only")
    elif "assign_event" in asg:
        if set(asg.keys()) != {"assign_event"}:
            raise ValueError("dynamic assignment must be {assign_event} only (additionalProperties:false)")
        validate_oid(asg["assign_event"])
    else:
        raise ValueError("assignment shape invalid")
    aspect = pre["aspect"]
    if kind == "gate":
        if not isinstance(aspect, dict) or set(aspect.keys()) != {"gate_id", "gater_seat"}:
            raise ValueError("gate aspect must be {gate_id,gater_seat}")
        validate_identity_token(aspect["gate_id"])
        validate_identity_token(aspect["gater_seat"])
    else:
        if aspect is not None:
            raise ValueError("author/merge aspect must be null")


def obligation_id(preimage: dict[str, Any]) -> str:
    validate_preimage(preimage)
    canon = jcs(preimage).encode("utf-8")
    digest = hashlib.sha256(DOMAIN + canon).hexdigest()
    return "sha256:" + digest


def main(argv: list[str]) -> int:
    if len(argv) < 2:
        print("usage: obligation.py id <json-or->", file=sys.stderr)
        return 2
    if argv[1] != "id":
        print("unknown cmd", file=sys.stderr)
        return 2
    raw = sys.stdin.read() if argv[2] == "-" else argv[2]
    # allow full event.obligation or bare preimage
    obj = parse_i_json(raw)
    if "preimage" in obj:
        pre = obj["preimage"]
        declared = obj.get("id")
        computed = obligation_id(pre)
        if declared is not None and declared != computed:
            print(f"MISMATCH declared={declared} computed={computed}", file=sys.stderr)
            return 1
        print(computed)
        return 0
    print(obligation_id(obj))
    return 0


if __name__ == "__main__":
    # allow running as script from lib/
    sys.path.insert(0, sys.path[0] if sys.path else ".")
    raise SystemExit(main(sys.argv))

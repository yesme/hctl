#!/usr/bin/env python3
"""Minimal RFC 8785 JCS for hctl identity preimages (D-33 / corpus #20 #28).

Scope: objects, arrays, strings, ints, bool, null — no floats (identity path never
uses floats). Key sort = lexicographic by UTF-16 code units (ASCII ≡ byte order).
Does NOT perform Unicode normalization (RFC 8785 §3.1).
"""
from __future__ import annotations

import json
import re
import sys
from typing import Any


_TOKEN = re.compile(r"^[a-z][a-z0-9-]{0,31}$")
_OID = re.compile(r"^([0-9a-f]{40}|[0-9a-f]{64})$")


def _escape(s: str) -> str:
    out = ['"']
    for ch in s:
        o = ord(ch)
        if ch == '"':
            out.append('\\"')
        elif ch == "\\":
            out.append("\\\\")
        elif ch == "\b":
            out.append("\\b")
        elif ch == "\f":
            out.append("\\f")
        elif ch == "\n":
            out.append("\\n")
        elif ch == "\r":
            out.append("\\r")
        elif ch == "\t":
            out.append("\\t")
        elif o < 0x20:
            out.append(f"\\u{o:04x}")
        else:
            out.append(ch)
    out.append('"')
    return "".join(out)


def jcs(value: Any) -> str:
    if value is None:
        return "null"
    if value is True:
        return "true"
    if value is False:
        return "false"
    if isinstance(value, int) and not isinstance(value, bool):
        return str(value)
    if isinstance(value, str):
        return _escape(value)
    if isinstance(value, list):
        return "[" + ",".join(jcs(v) for v in value) + "]"
    if isinstance(value, dict):
        # RFC 8785: sort by UTF-16 code units; for BMP strings == ord order
        keys = sorted(value.keys(), key=lambda k: [ord(c) for c in k])
        parts = []
        for k in keys:
            if not isinstance(k, str):
                raise TypeError(f"JCS object key must be str, got {type(k)}")
            parts.append(_escape(k) + ":" + jcs(value[k]))
        return "{" + ",".join(parts) + "}"
    raise TypeError(f"JCS unsupported type: {type(value)}")


def parse_i_json(text: str) -> Any:
    """Parse JSON; reject duplicate keys (no silent last-wins)."""

    def object_pairs(pairs):
        seen = set()
        out = {}
        for k, v in pairs:
            if k in seen:
                raise ValueError(f"duplicate key: {k!r}")
            seen.add(k)
            out[k] = v
        return out

    return json.loads(text, object_pairs_hook=object_pairs)


def validate_identity_token(s: str) -> None:
    if not _TOKEN.match(s):
        raise ValueError(f"identity token fails ASCII grammar: {s!r}")


def validate_oid(s: str) -> None:
    if not _OID.match(s):
        raise ValueError(f"oid not full-length hex: {s!r}")


def main(argv: list[str]) -> int:
    if len(argv) < 2:
        print("usage: jcs.py encode <json-or->  | check-token <s>", file=sys.stderr)
        return 2
    cmd = argv[1]
    if cmd == "encode":
        raw = sys.stdin.read() if argv[2] == "-" else argv[2]
        obj = parse_i_json(raw)
        sys.stdout.write(jcs(obj))
        return 0
    if cmd == "check-token":
        validate_identity_token(argv[2])
        print("ok")
        return 0
    print(f"unknown cmd {cmd}", file=sys.stderr)
    return 2


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))

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


def _reject_lone_surrogates(value: Any, path: str = "$") -> None:
    """I-JSON / structural corruption: lone UTF-16 surrogates are rejected (D-33/D-39)."""
    if isinstance(value, str):
        for i, ch in enumerate(value):
            o = ord(ch)
            if 0xD800 <= o <= 0xDFFF:
                raise ValueError(f"lone surrogate U+{o:04X} at {path}[{i}]")
        return
    if isinstance(value, list):
        for i, item in enumerate(value):
            _reject_lone_surrogates(item, f"{path}[{i}]")
        return
    if isinstance(value, dict):
        for k, v in value.items():
            _reject_lone_surrogates(k, f"{path}.key")
            _reject_lone_surrogates(v, f"{path}.{k}")


def parse_i_json(text: str) -> Any:
    """Parse JSON; reject duplicate keys, lone surrogates, non-I-JSON (no silent last-wins)."""

    def object_pairs(pairs):
        seen = set()
        out = {}
        for k, v in pairs:
            if k in seen:
                raise ValueError(f"duplicate key: {k!r}")
            seen.add(k)
            out[k] = v
        return out

    # Reject raw lone-surrogate escapes before json.loads coerces them.
    if re.search(r"\\u[dD][89a-fA-F][0-9a-fA-F]{2}", text):
        # paired surrogates are also rejected on identity path (ASCII-only tokens);
        # any surrogate escape is structural corruption for hctl identity/events.
        raise ValueError("surrogate escape in JSON text")

    try:
        obj = json.loads(text, object_pairs_hook=object_pairs)
    except json.JSONDecodeError as e:
        raise ValueError(f"non-I-JSON: {e}") from e
    _reject_lone_surrogates(obj)
    return obj


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

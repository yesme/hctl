#!/usr/bin/env bash
# Case #26: receipt 声称 config ≠ Hctl-Base 所见 blob ⇒ 拒
# D: D-37 | Source: codex-27d | Mechanical: receipt integrity
# Mode: pure  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="26-receipt-config-mismatch-reject"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

C0="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
C1="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
[ "$(corpus_py derive_rules.py receipt-config "$C1" "$C0" receipt)" = "deny" ] || corpus_fail
[ "$(corpus_py derive_rules.py receipt-config "$C0" "$C0" receipt)" = "ok" ] || corpus_fail
corpus_pass

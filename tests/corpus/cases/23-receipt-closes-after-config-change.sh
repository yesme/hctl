#!/usr/bin/env bash
# Case #23: 普通 config PR 后 receipt 按 Hctl-Base 旧 config 合法关 claim
# D: D-37 | Source: codex-27d | Mechanical: receipt validates config at Hctl-Base not post-main
# Mode: pure  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="23-receipt-closes-after-config-change"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

# claim pinned C0; merge changes config to C1; receipt still checks C0 == Hctl-Base blob
C0="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
C1="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
[ "$(corpus_py derive_rules.py receipt-config "$C0" "$C0" receipt)" = "ok" ] || corpus_fail "receipt C0@base"
[ "$(corpus_py derive_rules.py receipt-config "$C0" "$C1" receipt)" = "deny" ] || corpus_fail "wrong base blob"
# post-merge new claims use C1 (pre-merge phase with current=C1)
[ "$(corpus_py derive_rules.py receipt-config "$C1" "$C1" pre-merge)" = "ok" ] || corpus_fail "new claim C1"
[ "$(corpus_py derive_rules.py receipt-config "$C0" "$C1" pre-merge)" = "deny" ] || corpus_fail "stale config claim"
corpus_pass

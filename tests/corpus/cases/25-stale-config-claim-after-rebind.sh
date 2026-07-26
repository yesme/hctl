#!/usr/bin/env bash
# Case #25: 旧 worker 持旧 config 在 M 后新 claim ⇒ 级②拒
# D: D-37 | Source: codex-27d | Mechanical: claim config must match current main
# Mode: pure  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="25-stale-config-claim-after-rebind"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

C0="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
C1="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
# post-merge current is C1; claim carrying C0 denied at pre-merge check
[ "$(corpus_py derive_rules.py receipt-config "$C0" "$C1" pre-merge)" = "deny" ] || corpus_fail
corpus_pass

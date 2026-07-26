#!/usr/bin/env bash
# Case #24: rebind A→B：M 前只有 A 可 claim，M 后只有 B
# D: D-37 | Source: codex-27d | Mechanical: coordinator named by config revision
# Mode: pure  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="24-rebind-claim-rights"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

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

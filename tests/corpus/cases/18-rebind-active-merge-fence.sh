#!/usr/bin/env bash
# Case #18: coordinator rebind 时旧席有 active merge claim ⇒ 拒或先 fence
# D: D-37 | Source: codex-27c | Mechanical: rebind vs active merge claim
# Mode: pure  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="18-rebind-active-merge-fence"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

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

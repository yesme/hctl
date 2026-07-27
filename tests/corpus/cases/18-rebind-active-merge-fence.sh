#!/usr/bin/env bash
# Case #18: coordinator rebind 时旧席有 active merge claim ⇒ 拒或先 fence
# D: D-37 | Source: codex-27c | Mechanical: rebind vs active merge claim
# Mode: pure (differential oracle)
set -euo pipefail
CORPUS_CASE_ID="18-rebind-active-merge-fence"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

python3 - <<'PY'
import derive_rules
assert derive_rules.rebind_allowed(active_merge_claims_old_coord=1) is False
assert derive_rules.rebind_allowed(active_merge_claims_old_coord=0) is True
# capacity still 1 for the new coordinator domain
assert derive_rules.merge_capacity_allows(0) is True
assert derive_rules.merge_capacity_allows(1) is False
print("ok")
PY
corpus_pass

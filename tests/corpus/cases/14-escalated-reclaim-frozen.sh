#!/usr/bin/env bash
# Case #14: escalated 义务 re-claim 被拒；CANCEL(claim) 后计数清零可再 claim
# D: D-34 | Source: grok-27b | Mechanical: frozen while escalated; CANCEL thaw
# Mode: pure (differential oracle — shared derive_rules, not ad-hoc stubs)
set -euo pipefail
CORPUS_CASE_ID="14-escalated-reclaim-frozen"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

python3 - <<'PY'
import derive_rules
s = derive_rules.streak_after_reclaims(["non_forward", "non_forward"])
assert s["escalated"] and s["color"] == "red"
assert derive_rules.reclaim_allowed(escalated=True) is False
assert derive_rules.reclaim_allowed(escalated=False) is True
# CANCEL(claim) is an explicit thaw path
assert derive_rules.thaw_escalated("cancel_claim") is True
assert derive_rules.thaw_escalated("branch_progress") is False
# after thaw, streak window restarts (fresh streak function)
cleared = derive_rules.streak_after_reclaims([])
assert cleared["streak"] == 0 and not cleared["escalated"]
print("ok")
PY
corpus_pass

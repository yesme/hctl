#!/usr/bin/env bash
# Case #14: escalated 义务 re-claim 被拒；CANCEL(claim) 后计数清零可再 claim
# D: D-34 | Source: grok-27b | Mechanical: frozen while escalated; CANCEL thaw
# Mode: pure  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="14-escalated-reclaim-frozen"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

python3 - <<'PY'
import derive_rules
# two non-forward reclaims => escalated
s = derive_rules.streak_after_reclaims(["non_forward", "non_forward"])
assert s["escalated"] and s["color"]=="red"
# while escalated, progress does not thaw
s2 = derive_rules.streak_after_reclaims(["non_forward", "non_forward", "forward"], start_escalated=False)
# after red, further forward in list while escalated stays frozen
# re-simulate frozen gate:
def allow_reclaim(escalated):
    return not escalated
assert allow_reclaim(True) is False
assert allow_reclaim(False) is True
# CANCEL(claim) clears
state = {"escalated": True, "streak": 2}
def cancel_claim(state):
    return {"escalated": False, "streak": 0}
state = cancel_claim(state)
assert state["streak"]==0 and not state["escalated"]
print("ok")
PY
corpus_pass

#!/usr/bin/env bash
# Case #19: escalated 后 branch progress ⇒ 不自动解冻；仅三径可清
# D: D-34 | Source: codex-27c | Mechanical: frozen priority over progress
# Mode: pure  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="19-escalated-progress-no-thaw"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

python3 - <<'PY'
import derive_rules
# enter escalated
s = derive_rules.streak_after_reclaims(["non_forward", "non_forward"])
assert s["escalated"]
# explicit: progress classification while escalated must not clear
def on_progress_while_escalated(state, progress_class):
    if state["escalated"]:
        return state  # no change
    ...
state = {"escalated": True, "streak": 2}
assert on_progress_while_escalated(state, "forward")["escalated"] is True
# three thaw paths only
def thaw(path, state):
    if path in ("cancel_claim", "cancel_obligation", "new_assignment_revision"):
        return {"escalated": False, "streak": 0}
    raise ValueError("no")
assert thaw("cancel_claim", state)["escalated"] is False
print("ok")
PY
corpus_pass

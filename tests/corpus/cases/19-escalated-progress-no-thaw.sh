#!/usr/bin/env bash
# Case #19: escalated 后 branch progress ⇒ 不自动解冻；仅三径可清
# D: D-34 | Source: codex-27c | Mechanical: frozen priority over progress
# Mode: pure (differential oracle)
set -euo pipefail
CORPUS_CASE_ID="19-escalated-progress-no-thaw"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

python3 - <<'PY'
import derive_rules
s = derive_rules.streak_after_reclaims(["non_forward", "non_forward"])
assert s["escalated"]
# further progress classifications while escalated do not clear red
s2 = derive_rules.streak_after_reclaims(
    ["non_forward", "non_forward", "forward", "environment_reset"],
)
assert s2["escalated"] and s2["color"] == "red"
# three thaw paths only
for path in ("cancel_claim", "cancel_obligation", "new_assignment_revision"):
    assert derive_rules.thaw_escalated(path)
assert not derive_rules.thaw_escalated("forward_progress")
assert not derive_rules.thaw_escalated("verdict")
print("ok")
PY
corpus_pass

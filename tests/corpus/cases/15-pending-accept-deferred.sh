#!/usr/bin/env bash
# Case #15: pending_accept 他席抢 claim 被拒（P2 HANDOFF 解冻后）
# D: D-36 | Source: 主笔 | Mechanical: P2; P1 marks DEFERRED/unsupported
# Mode: deferred-p2  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="15-pending-accept-deferred"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

# P1: cross-seat HANDOFF is DEFERRED — kernel must report unsupported, not claim success.
python3 - <<'PY'
def p1_handoff_accept(obligation_state):
    if obligation_state.get("mode") == "pending_accept":
        return {"ok": False, "code": "UNSUPPORTED", "reason": "cross-seat HANDOFF DEFERRED until single CAS domain"}
    return {"ok": True}
r = p1_handoff_accept({"mode": "pending_accept", "target": "codex"})
assert r["ok"] is False and r["code"]=="UNSUPPORTED"
print("ok")
PY
corpus_pass

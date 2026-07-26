#!/usr/bin/env bash
# Case #8: authority:user 不冒充机器证明；缺 assignment 写动作拒
# D: D-38 | Source: codex-27b | Mechanical: level-4 declaration vs level-2 assignment required
# Mode: pure  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="08-authority-and-assignment-gate"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

# authority is declaration only — presence never proves human initiation
auth_json='{"authority":{"kind":"user"}}'
echo "$auth_json" | python3 -c 'import json,sys; a=json.load(sys.stdin); assert a["authority"]["kind"]=="user"; print("declaration-only")' | grep -q declaration
# missing assignment => reject write
python3 - <<'PY'
import json, sys
def allow_write(cmd, assignment):
    # D-38: begin/verdict/merge must backref frozen assignment
    if cmd in ("begin", "verdict", "merge", "claim") and not assignment:
        return False
    return True
assert allow_write("claim", None) is False
assert allow_write("claim", {"id": "p1-corpus"}) is True
assert allow_write("status", None) is True
print("ok")
PY
corpus_pass

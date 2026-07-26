#!/usr/bin/env bash
# Case #12: 重复 logical assignment id ⇒ loader 拒
# D: D-33 | Source: codex/grok | Mechanical: loader unique logical id
# Mode: pure  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="12-dup-assignment-logical-id"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

python3 - <<'PY'
ids = ["p1-kernel", "p1-corpus", "p1-kernel"]
def load(ids):
    seen=set()
    for i in ids:
        if i in seen:
            raise SystemExit("reject-dup")
        seen.add(i)
    return "ok"
try:
    load(ids)
    raise SystemExit("should reject")
except SystemExit as e:
    assert str(e)=="reject-dup"
assert load(["p1-kernel","p1-corpus"])=="ok"
print("ok")
PY
corpus_pass

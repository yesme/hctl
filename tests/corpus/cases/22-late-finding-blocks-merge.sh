#!/usr/bin/env bash
# Case #22: merge 前 late finding 发 REQUEST_CHANGES ⇒ latest-wins 失绿阻断
# D: D-41 | Source: codex-27c | Mechanical: latest-wins verdict
# Mode: pure (differential oracle)
set -euo pipefail
CORPUS_CASE_ID="22-late-finding-blocks-merge"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

python3 - <<'PY'
import derive_rules
rev = {"base": "b" * 40, "head": "h" * 40}
vs = [
  {"decision":"APPROVE","completeness":"COMPLETE","scope":{"kind":"full"},"revision":rev,"n":1},
  {"decision":"REQUEST_CHANGES","completeness":"COMPLETE","scope":{"kind":"full"},"revision":rev,"n":2,"late_finding":True},
]
w = derive_rules.latest_wins(vs)
assert w["decision"]=="REQUEST_CHANGES"
assert not derive_rules.quorum_counts(w, required_coverage="full", expected_revision=rev)
print("ok")
PY
corpus_pass

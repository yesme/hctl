#!/usr/bin/env bash
# Case #11: author/memo pattern 跨席跨 lane 两两不相交（doctor）
# D: D-08 | Source: grok-27b | Mechanical: doctor pattern disjointness
# Mode: pure  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="11-branch-pattern-disjoint"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

# positive: current seats shapes are disjoint
python3 - <<'PY'
import json, derive_rules
seats = {
  "claude": {
    "author_branch_patterns": ["refs/heads/work/claude/*", "refs/heads/claude/*"],
    "memo_branch_patterns": ["refs/heads/memo/claude/*"],
  },
  "codex": {
    "author_branch_patterns": ["refs/heads/work/codex/*", "refs/heads/codex/*"],
    "memo_branch_patterns": ["refs/heads/memo/codex/*"],
  },
  "grok": {
    "author_branch_patterns": ["refs/heads/work/grok/*", "refs/heads/grok/*"],
    "memo_branch_patterns": ["refs/heads/memo/grok/*"],
  },
}
v = derive_rules.seats_patterns_ok(seats)
assert v == [], v
# negative: intentional overlap
bad = {
  "a": {"author_branch_patterns": ["refs/heads/work/*"], "memo_branch_patterns": ["refs/heads/memo/a/*"]},
  "b": {"author_branch_patterns": ["refs/heads/work/b/*"], "memo_branch_patterns": ["refs/heads/memo/b/*"]},
}
v2 = derive_rules.seats_patterns_ok(bad)
assert v2, "expected overlap detection"
# author vs memo same seat must not overlap
assert derive_rules.patterns_disjoint([
  "refs/heads/work/grok/*", "refs/heads/memo/grok/*"
])
print("ok")
PY
corpus_pass

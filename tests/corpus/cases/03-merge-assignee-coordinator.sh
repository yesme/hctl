#!/usr/bin/env bash
# Case #3: merge obligation 指派非 coordinator ⇒ loader 拒 / merge holder 仅 coordinator
# D: D-37 | Source: codex-27b | Mechanical: loader assignee == merge_coordinator
# Mode: pure (differential oracle) + hctl-wire
set -euo pipefail
CORPUS_CASE_ID="03-merge-assignee-coordinator"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

# Differential oracle (shared predicate lib — not an ad-hoc stub)
[ "$(corpus_py derive_rules.py merge-assignee claude claude)" = "ok" ] || corpus_fail "coord ok"
[ "$(corpus_py derive_rules.py merge-assignee grok claude)" = "deny" ] || corpus_fail "non-coord deny"
[ "$(corpus_py derive_rules.py merge-assignee codex claude)" = "deny" ] || corpus_fail "codex deny"

# hctl-wire: live status must place merge obligation only on merge_coordinator
if [ -n "${HCTL:-}" ] && [ -x "${HCTL}" ]; then
  corpus_require_hctl
  # shellcheck source=/dev/null
  source "$CORPUS_ROOT/lib/hctl_fixture.sh"
  corpus_sandbox
  corpus_hctl_fixture
  json=$(corpus_hctl claude status --json)
  printf '%s' "$json" | python3 -c '
import json,sys
doc=json.load(sys.stdin)
merges=[o for o in doc.get("obligations",[]) if o.get("kind")=="merge"]
assert merges, "no merge obligation"
for m in merges:
    assert m.get("holder")=="claude", m
print("ok")
'
  # non-coordinator seat cannot claim merge
  merge_id=$(printf '%s' "$json" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(next(o["obligation"]["id"] for o in d["obligations"] if o["kind"]=="merge"))')
  set +e
  err=$(corpus_hctl grok claim "$merge_id" 2>&1)
  code=$?
  set -e
  [ "$code" -ne 0 ] || corpus_fail "non-coordinator claim(merge) must fail"
  echo "$err" | grep -qiE 'holder|seat|claude|grok' || corpus_fail "error should mention seat/holder mismatch"
fi

corpus_pass

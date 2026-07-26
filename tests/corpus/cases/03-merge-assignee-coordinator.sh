#!/usr/bin/env bash
# Case #3: merge obligation 指派非 coordinator ⇒ loader 拒
# D: D-37 | Source: codex-27b | Mechanical: loader assignee == merge_coordinator
# Mode: pure  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="03-merge-assignee-coordinator"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

[ "$(corpus_py derive_rules.py merge-assignee claude claude)" = "ok" ] || corpus_fail "coord ok"
[ "$(corpus_py derive_rules.py merge-assignee grok claude)" = "deny" ] || corpus_fail "non-coord deny"
[ "$(corpus_py derive_rules.py merge-assignee codex claude)" = "deny" ] || corpus_fail "codex deny"
corpus_pass

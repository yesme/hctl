#!/usr/bin/env bash
# Case #24: rebind A→B：M 前只有 A 可 claim，M 后只有 B
# D: D-37 | Source: codex-27d | Mechanical: coordinator named by config revision
# Mode: pure (differential oracle — same predicate as merge-assignee)
set -euo pipefail
CORPUS_CASE_ID="24-rebind-claim-rights"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

[ "$(corpus_py derive_rules.py merge-assignee claude claude)" = "ok" ] || corpus_fail "A before rebind"
[ "$(corpus_py derive_rules.py merge-assignee grok claude)" = "deny" ] || corpus_fail "B denied before rebind"
# after rebind coordinator is grok
[ "$(corpus_py derive_rules.py merge-assignee grok grok)" = "ok" ] || corpus_fail "B after rebind"
[ "$(corpus_py derive_rules.py merge-assignee claude grok)" = "deny" ] || corpus_fail "A denied after rebind"

corpus_pass

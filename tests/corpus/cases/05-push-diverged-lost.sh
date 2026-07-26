#!/usr/bin/env bash
# Case #5: pending 与远端 winner 分叉 ⇒ lost + recovery ref
# D: D-34 | Source: codex-27b | Mechanical: ancestry fail => lost; stash recovery
# Mode: hybrid  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="05-push-diverged-lost"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

corpus_sandbox
corpus_init_origin mac ubuntu
REF=refs/coop/claude
# winner on origin
w=$(corpus_event mac '' '{"schema_version":1,"type":"CLAIM","who":"win"}' 'winner')
git -C mac update-ref "$REF" "$w" ''
git -C mac push -q --force-with-lease="$REF": origin "$REF"
# loser local pending from empty parent (diverged history)
pend=$(corpus_event ubuntu '' '{"schema_version":1,"type":"CLAIM","who":"lose"}' 'loser pending')
git -C ubuntu update-ref "$REF" "$pend" ''
remote=$(git -C origin.git rev-parse "$REF")
out=$(corpus_py derive_rules.py delivered ubuntu "$pend" "$remote")
[ "$out" = "lost" ] || corpus_fail "diverged pending => lost, got $out"
# recovery ref: save local tip before realign
git -C ubuntu update-ref "refs/hctl/recovery/coop-claude" "$pend"
[ "$(git -C ubuntu rev-parse refs/hctl/recovery/coop-claude)" = "$pend" ] || corpus_fail "recovery ref"
git -C ubuntu fetch -q origin "$REF"
git -C ubuntu update-ref "$REF" "$(git -C ubuntu rev-parse FETCH_HEAD)" "$pend"
[ "$(git -C ubuntu rev-parse "$REF")" = "$remote" ] || corpus_fail "realign to remote"
corpus_pass

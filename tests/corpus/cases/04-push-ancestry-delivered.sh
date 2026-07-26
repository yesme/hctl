#!/usr/bin/env bash
# Case #4: ambiguous push 后远端已 append descendant ⇒ ancestry 判 delivered
# D: D-34 | Source: codex-27b | Mechanical: merge-base --is-ancestor pending remote-tip
# Mode: hybrid  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="04-push-ancestry-delivered"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

corpus_sandbox
corpus_init_origin mac ubuntu
REF=refs/coop/claude
p=$(corpus_event mac '' '{"schema_version":1,"type":"CLAIM","n":1}' 'pending claim')
git -C mac update-ref "$REF" "$p" ''
git -C mac push -q --force-with-lease="$REF": origin "$REF"
# remote advances with descendant after our pending is included
git -C ubuntu fetch -q origin "$REF"
git -C ubuntu update-ref "$REF" FETCH_HEAD
child=$(corpus_event ubuntu "$(git -C ubuntu rev-parse "$REF")" '{"schema_version":1,"type":"CLAIM","n":2}' 'child')
git -C ubuntu update-ref "$REF" "$child" FETCH_HEAD
git -C ubuntu push -q --force-with-lease="$REF":"$p" origin "$REF"
remote=$(git -C origin.git rev-parse "$REF")
# ancestry must be evaluated in a repo that has both objects
git -C mac fetch -q origin "$REF"
git -C mac update-ref "$REF" FETCH_HEAD
out=$(corpus_py derive_rules.py delivered mac "$p" "$remote")
[ "$out" = "delivered" ] || corpus_fail "pending ancestor of remote tip => delivered, got $out"
corpus_pass

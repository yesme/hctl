#!/usr/bin/env bash
# Case #10: 同 payload blob 异 parent ⇒ 两个事件，不按 blob 折叠
# D: D-34 | Source: codex-27b | Mechanical: event identity = commit OID not payload blob
# Mode: hybrid  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="10-same-blob-two-events"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

corpus_sandbox
corpus_init_origin mac
REF=refs/coop/claude
json='{"schema_version":1,"type":"NOTE","body":"same"}'
e1=$(corpus_event mac '' "$json" 'e1')
# same json bytes => same blob oid, different parents
blob1=$(git -C mac rev-parse "$e1:event.json")
e2=$(corpus_event mac "$e1" "$json" 'e2')
blob2=$(git -C mac rev-parse "$e2:event.json")
[ "$blob1" = "$blob2" ] || corpus_fail "payload blobs should match"
[ "$e1" != "$e2" ] || corpus_fail "event commits must differ"
git -C mac update-ref "$REF" "$e2" ''
# both commits exist as distinct events
git -C mac cat-file -t "$e1" | grep -q commit
git -C mac cat-file -t "$e2" | grep -q commit
n=$(git -C mac rev-list --count "$e2")
[ "$n" -eq 2 ] || corpus_fail "chain length 2"
corpus_pass

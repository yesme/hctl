#!/usr/bin/env bash
# Case #2: 同 coordinator 两个 merge obligations ⇒ 第二 claim 被 capacity=1 拒
# D: D-37 | Source: codex-27b | Mechanical: merge_capacity=1 + single-chain CAS domain
# Mode: hybrid  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="02-merge-capacity-one"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

[ "$(corpus_py derive_rules.py capacity 0)" = "allow" ] || corpus_fail "0 active allow"
[ "$(corpus_py derive_rules.py capacity 1)" = "deny" ] || corpus_fail "1 active deny"
corpus_sandbox
corpus_init_origin mac
REF=refs/coop/claude
e1=$(corpus_event mac '' '{"schema_version":1,"type":"CLAIM","kind":"merge","slot":"A"}' 'CLAIM merge-A')
git -C mac update-ref "$REF" "$e1" ''
e2=$(corpus_event mac "$e1" '{"schema_version":1,"type":"CLAIM","kind":"merge","slot":"B"}' 'CLAIM merge-B')
e3=$(corpus_event mac "$e1" '{"schema_version":1,"type":"CLAIM","kind":"merge","slot":"C"}' 'CLAIM merge-C')
# serial domain: from same parent tip only one CAS wins (capacity structural basis)
git -C mac update-ref "$REF" "$e2" "$e1" || corpus_fail "first claim CAS"
if git -C mac update-ref "$REF" "$e3" "$e1" 2>/dev/null; then
  corpus_fail "second concurrent claim from same tip must CAS-fail"
fi
# after first holds, capacity predicate still deny another active
[ "$(corpus_py derive_rules.py capacity 1)" = "deny" ] || corpus_fail "capacity still 1"
corpus_pass

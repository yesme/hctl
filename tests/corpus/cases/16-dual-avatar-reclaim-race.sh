#!/usr/bin/env bash
# Case #16: 双化身同 tick 竞争 re-claim 同 active ⇒ 唯一赢家、reclaim_of 失配拒
# D: D-34/D-35 | Source: grok 一审 | Mechanical: claim-OID fencing + push CAS
# Mode: hybrid  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="16-dual-avatar-reclaim-race"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

corpus_sandbox
corpus_init_origin mac ubuntu
REF=refs/coop/claude
active=$(corpus_event mac '' '{"schema_version":1,"type":"CLAIM","reclaim_of":null,"who":"first"}' 'initial claim')
git -C mac update-ref "$REF" "$active" ''
git -C mac push -q --force-with-lease="$REF": origin "$REF"
# both prepare re-claim of same active
r1=$(corpus_event mac "$active" '{"schema_version":1,"type":"CLAIM","reclaim_of":"'"$active"'","who":"mac"}' 'reclaim mac')
git -C ubuntu fetch -q origin "$REF"
r2=$(corpus_event ubuntu "$active" '{"schema_version":1,"type":"CLAIM","reclaim_of":"'"$active"'","who":"ubuntu"}' 'reclaim ubuntu')
git -C mac update-ref "$REF" "$r1" "$active"
git -C mac push -q --force-with-lease="$REF":"$active" origin "$REF" || corpus_fail "mac reclaim push"
# ubuntu still has old expect
git -C ubuntu update-ref "$REF" "$r2" "$active" 2>/dev/null || true
if git -C ubuntu push -q --force-with-lease="$REF":"$active" origin "$REF" 2>/dev/null; then
  corpus_fail "ubuntu reclaim push must fail after mac won"
fi
remote=$(git -C origin.git rev-parse "$REF")
[ "$remote" = "$r1" ] || corpus_fail "winner tip"
# reclaim_of mismatch: building on stale active rejected by fencing rule
python3 - <<PY
active, winner = "$active", "$r1"
def accept_reclaim(reclaim_of, current_active):
    return reclaim_of == current_active
assert accept_reclaim(active, winner) is False
assert accept_reclaim(winner, winner) is True
print("ok")
PY
corpus_pass

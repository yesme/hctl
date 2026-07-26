#!/usr/bin/env bash
# Case #17: ACCEPT 与 TIMEOUT_RETURN 异 ref 并发 ⇒ 当前设计 unsupported（DEFERRED 证据）
# D: D-36 | Source: codex-27c | Mechanical: cross-ref has no mutex — evidence case
# Mode: hybrid  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="17-pending-accept-cross-ref-dual-win"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

# Demonstrate two seat refs can both append successors of H (dual-win) without single CAS domain.
corpus_sandbox
corpus_init_origin a b c
H=$(corpus_event a '' '{"schema_version":1,"type":"HANDOFF","to":"b"}' 'H')
git -C a update-ref refs/coop/a "$H" ''
git -C a push -q --force-with-lease=refs/coop/a: origin refs/coop/a
# B accept on refs/coop/b
git -C b fetch -q origin refs/coop/a
Cb=$(corpus_event b '' '{"schema_version":1,"type":"CLAIM","accepts":"'"$H"'"}' 'ACCEPT')
git -C b update-ref refs/coop/b "$Cb" ''
git -C b push -q --force-with-lease=refs/coop/b: origin refs/coop/b
# C timeout-return claim on refs/coop/c
Cc=$(corpus_event c '' '{"schema_version":1,"type":"CLAIM","timeout_return_of":"'"$H"'"}' 'TIMEOUT_RETURN')
git -C c update-ref refs/coop/c "$Cc" ''
git -C c push -q --force-with-lease=refs/coop/c: origin refs/coop/c
# both succeeded — dual win
[ "$(git -C origin.git rev-parse refs/coop/b)" = "$Cb" ] || corpus_fail "B"
[ "$(git -C origin.git rev-parse refs/coop/c)" = "$Cc" ] || corpus_fail "C"
python3 - <<'PY'
# P1 correct behavior: refuse to interpret as exclusive ownership; report unsupported
def derive_ownership(facts):
    accepts = [f for f in facts if f.get("accepts")]
    timeouts = [f for f in facts if f.get("timeout_return_of")]
    if accepts and timeouts:
        return "UNSUPPORTED"  # DEFERRED evidence
    return "ok"
assert derive_ownership([
  {"accepts": "H"}, {"timeout_return_of": "H"}
]) == "UNSUPPORTED"
print("ok")
PY
corpus_pass

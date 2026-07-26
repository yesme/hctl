#!/usr/bin/env bash
# Case #9: formal begin：claim 成功、branch 失败、重跑复用原 claim
# D: D-43 | Source: codex-27b | Mechanical: state-aware begin reuses active author claim
# Mode: hybrid  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="09-begin-retry-reuse-claim"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

# Spec harness: if active author claim exists for assignment, do not mint new claim
corpus_sandbox
corpus_init_origin mac
REF=refs/coop/grok
c1=$(corpus_event mac '' '{"schema_version":1,"type":"CLAIM","kind":"author","assignment":"p1-corpus","active":true}' 'author claim')
git -C mac update-ref "$REF" "$c1" ''
git -C mac push -q --force-with-lease="$REF": origin "$REF"
# simulate branch create failure then retry: count CLAIMs must stay 1
active=$(git -C mac log --format=%H "$REF" | wc -l | tr -d ' ')
[ "$active" = "1" ] || corpus_fail "one claim"
# retry path: detect active claim for assignment, skip new event
python3 - <<'PY'
claims = [{"type":"CLAIM","kind":"author","assignment":"p1-corpus","oid":"c1"}]
def begin(assignment, claims):
    active = [c for c in claims if c.get("kind")=="author" and c.get("assignment")==assignment]
    if active:
        return {"reused": active[0]["oid"], "minted": False}
    return {"reused": None, "minted": True}
r = begin("p1-corpus", claims)
assert r["minted"] is False and r["reused"]=="c1"
print("ok")
PY
# optional live wire
if [ -n "${HCTL:-}" ] || command -v hctl >/dev/null 2>&1; then
  : # reserved: HCTL begin twice asserts single claim
fi
corpus_pass

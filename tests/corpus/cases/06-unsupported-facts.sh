#!/usr/bin/env bash
# Case #6: known-event classification — Go kernel is the sole normative executor
# D: D-39 | Source: codex-27b / codex-27k §2.1 terminal closure
# Mode: hctl-wire (strict); pure only covers envelope/unknown-type narrow helper
set -euo pipefail
CORPUS_CASE_ID="06-unsupported-facts"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

known='CLAIM,VERDICT,CANCEL'
BLOB='0123456789abcdef0123456789abcdef01234567'

# --- pure: narrow helper only (unsupported type / major / envelope) ---
u=$(corpus_py derive_rules.py three-way \
  '{"schema_version":1,"type":"HANDOFF","actor":{"seat":"claude","machine":"m","session":null},"created_at":"2026-07-27T00:00:00Z"}' \
  claude "$known")
[ "$u" = "UNSUPPORTED_FACTS" ] || corpus_fail "unknown type => UNSUPPORTED, got $u"

v=$(corpus_py derive_rules.py three-way \
  '{"schema_version":9,"type":"CLAIM","actor":{"seat":"claude","machine":"m","session":null},"created_at":"2026-07-27T00:00:00Z"}' \
  claude "$known")
[ "$v" = "UNSUPPORTED_FACTS" ] || corpus_fail "future major => UNSUPPORTED, got $v"

miss=$(corpus_py derive_rules.py three-way '{"schema_version":1,"type":"CLAIM"}' claude "$known")
[ "$miss" = "CORRUPT_CHAIN" ] || corpus_fail "missing envelope => CORRUPT, got $miss"

# Known type body is NOT classified OK by Python (must DEFER)
defer=$(corpus_py derive_rules.py three-way \
  '{"schema_version":1,"type":"CLAIM","actor":{"seat":"claude","machine":"m","session":null},"created_at":"2026-07-27T00:00:00Z"}' \
  claude "$known")
[ "$defer" = "KNOWN_TYPE_DEFER" ] || corpus_fail "known type must DEFER to kernel, got $defer"

# --- wire: table-driven plant on refs/coop/* → hctl exact classification ---
corpus_require_hctl
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/hctl_fixture.sh"

plant_and_expect() {
  local seat=$1 json=$2 expect_sub=$3 label=$4 tip old
  tip=$(corpus_event "$HCTL_FIXTURE_REPO" '' "$json" "case6 $label")
  old=$(git -C "$HCTL_FIXTURE_REPO" rev-parse -q --verify "refs/coop/$seat" 2>/dev/null || true)
  if [ -n "$old" ]; then
    git -C "$HCTL_FIXTURE_REPO" update-ref "refs/coop/$seat" "$tip" "$old"
  else
    git -C "$HCTL_FIXTURE_REPO" update-ref "refs/coop/$seat" "$tip" ''
  fi
  git -C "$HCTL_FIXTURE_REPO" push -q --force origin "refs/coop/$seat"
  git -C "$HCTL_FIXTURE_REPO" update-ref -d "refs/hctl/remotes/origin/coop/$seat" 2>/dev/null || true
  set +e
  out=$(corpus_hctl "$seat" status 2>&1)
  code=$?
  set -e
  echo "$out" | grep -qiE "$expect_sub" || corpus_fail "$label: expected /$expect_sub/ in: $out"
  [ "$code" -ne 0 ] || corpus_fail "$label: status must be unhealthy"
}

legal_claim() {
  python3 - <<PY
import json, obligation
BLOB="$BLOB"
pre = {"assignment": {"id": "demo", "blob": BLOB}, "kind": "author", "target": "demo", "aspect": None}
oid = obligation.obligation_id(pre)
print(json.dumps({
  "schema_version": 1, "type": "CLAIM",
  "actor": {"seat": "codex", "machine": "m", "session": None},
  "created_at": "2026-07-27T00:00:00Z",
  "obligation": {
    "preimage": pre, "id": oid,
    "source": {"commit": BLOB, "path": ".hctl/assignments/demo.toml", "blob": BLOB},
  },
  "tip_at_claim": None, "reclaim_of": None, "reclaim_reason": None,
}, separators=(",", ":")))
PY
}

corpus_sandbox
corpus_hctl_fixture

# 1) legal CLAIM — no CORRUPT; status may still warn bootstrap but chain readable
legal=$(legal_claim)
tip=$(corpus_event "$HCTL_FIXTURE_REPO" '' "$legal" 'legal claim')
git -C "$HCTL_FIXTURE_REPO" update-ref refs/coop/codex "$tip" ''
git -C "$HCTL_FIXTURE_REPO" push -q --force origin refs/coop/codex
git -C "$HCTL_FIXTURE_REPO" update-ref -d refs/hctl/remotes/origin/coop/codex 2>/dev/null || true
set +e
out_ok=$(corpus_hctl codex status 2>&1)
set -e
echo "$out_ok" | grep -qiE 'CORRUPT_CHAIN' && corpus_fail "legal CLAIM must not CORRUPT: $out_ok"

# 2) static no-source CLAIM
no_src=$(python3 - <<PY
import json, obligation
BLOB="$BLOB"
pre = {"assignment": {"id": "demo", "blob": BLOB}, "kind": "author", "target": "demo", "aspect": None}
oid = obligation.obligation_id(pre)
print(json.dumps({
  "schema_version": 1, "type": "CLAIM",
  "actor": {"seat": "grok", "machine": "m", "session": None},
  "created_at": "2026-07-27T00:00:00Z",
  "obligation": {"preimage": pre, "id": oid},
  "tip_at_claim": None, "reclaim_of": None, "reclaim_reason": None,
}, separators=(",", ":")))
PY
)
plant_and_expect grok "$no_src" 'static obligation requires source|CORRUPT' 'no-source'

# 3) numeric session
num_sess=$(python3 - <<PY
import json, obligation
BLOB="$BLOB"
pre = {"assignment": {"id": "demo", "blob": BLOB}, "kind": "author", "target": "demo", "aspect": None}
oid = obligation.obligation_id(pre)
print(json.dumps({
  "schema_version": 1, "type": "CLAIM",
  "actor": {"seat": "claude", "machine": "m", "session": 7},
  "created_at": "2026-07-27T00:00:00Z",
  "obligation": {
    "preimage": pre, "id": oid,
    "source": {"commit": BLOB, "path": "p", "blob": BLOB},
  },
  "tip_at_claim": None, "reclaim_of": None, "reclaim_reason": None,
}, separators=(",", ":")))
PY
)
plant_and_expect claude "$num_sess" 'session must be a string or null|CORRUPT' 'numeric-session'

# 4) non-UTC timestamp
non_utc=$(python3 - <<PY
import json, obligation
BLOB="$BLOB"
pre = {"assignment": {"id": "demo", "blob": BLOB}, "kind": "author", "target": "demo", "aspect": None}
oid = obligation.obligation_id(pre)
print(json.dumps({
  "schema_version": 1, "type": "CLAIM",
  "actor": {"seat": "codex", "machine": "m2", "session": None},
  "created_at": "2026-07-27T08:00:00+08:00",
  "obligation": {
    "preimage": pre, "id": oid,
    "source": {"commit": BLOB, "path": "p", "blob": BLOB},
  },
  "tip_at_claim": None, "reclaim_of": None, "reclaim_reason": None,
}, separators=(",", ":")))
PY
)
# force new tip on codex over previous legal
plant_and_expect codex "$non_utc" 'RFC3339 UTC|CORRUPT' 'non-utc'

# 5) invalid VERDICT scope
bad_scope=$(python3 - <<PY
import json, obligation
BLOB="$BLOB"
pre = {"assignment": {"id": "demo", "blob": BLOB}, "kind": "gate", "target": "demo",
       "aspect": {"gate_id": "review", "gater_seat": "grok"}}
oid = obligation.obligation_id(pre)
print(json.dumps({
  "schema_version": 1, "type": "VERDICT",
  "actor": {"seat": "grok", "machine": "m", "session": None},
  "created_at": "2026-07-27T00:00:00Z",
  "obligation": {
    "preimage": pre, "id": oid,
    "source": {"commit": BLOB, "path": "p", "blob": BLOB},
  },
  "claim": BLOB,
  "pr": 1,
  "revision": {"base": BLOB, "head": BLOB},
  "decision": "APPROVE",
  "report": {"commit": BLOB, "path": "memory/r.md", "blob": BLOB},
  "scope": {"kind": "bogus"},
  "completeness": "COMPLETE",
}, separators=(",", ":")))
PY
)
plant_and_expect grok "$bad_scope" 'invalid scope kind|CORRUPT|scope' 'bad-scope'

# 6) invalid CANCEL target kind
bad_cancel='{"schema_version":1,"type":"CANCEL","actor":{"seat":"claude","machine":"m","session":null},"created_at":"2026-07-27T00:00:00Z","target":{"kind":"bogus"},"reason":"test","authority":{"kind":"user"}}'
plant_and_expect claude "$bad_cancel" 'invalid cancel target kind|CORRUPT' 'bad-cancel'

corpus_pass

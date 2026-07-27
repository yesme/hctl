#!/usr/bin/env bash
# Case #6: known-event classification — Go kernel is the sole normative executor
# D: D-39 | Source: codex-27b / codex-27k §2.1 / claude-27v terminal handoff
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

# --- wire: one fresh fixture per row; exact event/seat/code/reason attribution ---
corpus_require_hctl
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/hctl_fixture.sh"

build_payload() {
  local kind=$1 seat=$2 main assignment_blob
  main=$(git -C "$HCTL_FIXTURE_REPO" rev-parse HEAD)
  assignment_blob=$(git -C "$HCTL_FIXTURE_REPO" rev-parse "$main:.hctl/assignments/demo.toml")
  python3 - "$kind" "$seat" "$BLOB" "$main" "$assignment_blob" <<'PY'
import json
import sys

import obligation

kind, seat, fake_oid, main, assignment_blob = sys.argv[1:]
created_at = "2026-07-27T00:00:00Z"

if kind == "bad-cancel":
    event = {
        "schema_version": 1,
        "type": "CANCEL",
        "actor": {"seat": seat, "machine": "m", "session": None},
        "created_at": created_at,
        "target": {"kind": "bogus"},
        "reason": "test",
        "authority": {"kind": "user"},
    }
elif kind == "bad-scope":
    preimage = {
        "assignment": {"id": "demo", "blob": fake_oid},
        "kind": "gate",
        "target": "demo",
        "aspect": {"gate_id": "review", "gater_seat": seat},
    }
    event = {
        "schema_version": 1,
        "type": "VERDICT",
        "actor": {"seat": seat, "machine": "m", "session": None},
        "created_at": created_at,
        "obligation": {
            "preimage": preimage,
            "id": obligation.obligation_id(preimage),
            "source": {"commit": fake_oid, "path": "p", "blob": fake_oid},
        },
        "claim": fake_oid,
        "pr": 1,
        "revision": {"base": fake_oid, "head": fake_oid},
        "decision": "APPROVE",
        "report": {"commit": fake_oid, "path": "memory/r.md", "blob": fake_oid},
        "scope": {"kind": "bogus"},
        "completeness": "COMPLETE",
    }
else:
    source_oid = assignment_blob if kind == "legal-claim" else fake_oid
    source_commit = main if kind == "legal-claim" else fake_oid
    preimage = {
        "assignment": {"id": "demo", "blob": source_oid},
        "kind": "author",
        "target": "demo",
        "aspect": None,
    }
    actor_session = 7 if kind == "numeric-session" else None
    event = {
        "schema_version": 1,
        "type": "CLAIM",
        "actor": {"seat": seat, "machine": "m", "session": actor_session},
        "created_at": (
            "2026-07-27T08:00:00+08:00"
            if kind == "non-utc"
            else created_at
        ),
        "obligation": {
            "preimage": preimage,
            "id": obligation.obligation_id(preimage),
        },
        "tip_at_claim": None,
        "reclaim_of": None,
        "reclaim_reason": None,
    }
    if kind != "no-source":
        event["obligation"]["source"] = {
            "commit": source_commit,
            "path": ".hctl/assignments/demo.toml",
            "blob": source_oid,
        }

print(json.dumps(event, separators=(",", ":")))
PY
}

run_vector() (
  local label=$1 seat=$2 expected_code=$3 expected_detail=$4 kind=$5
  local json tip out code ref

  # Subshell + sandbox trap gives every row an independent origin, clone, and coop set.
  corpus_sandbox
  corpus_hctl_fixture
  json=$(build_payload "$kind" "$seat")
  tip=$(corpus_event "$HCTL_FIXTURE_REPO" '' "$json" "case6 $label")
  ref="refs/coop/$seat"
  git -C "$HCTL_FIXTURE_REPO" update-ref "$ref" "$tip" ''
  git -C "$HCTL_FIXTURE_REPO" push -q --force-with-lease="$ref": origin "$ref"

  set +e
  out=$(corpus_hctl "$seat" status --json)
  code=$?
  set -e

  if [ "$expected_code" = "ACCEPT" ]; then
    [ "$code" -eq 0 ] || corpus_fail "$label: accepted event must leave status healthy"
  else
    [ "$code" -ne 0 ] || corpus_fail "$label: rejected event must leave status unhealthy"
  fi

  if ! STATUS_JSON="$out" python3 - "$label" "$seat" "$tip" "$expected_code" "$expected_detail" <<'PY'
import json
import os
import sys

label, seat, tip, expected_code, expected_detail = sys.argv[1:]
document = json.loads(os.environ["STATUS_JSON"])
problems = document.get("problems", [])
classifications = [
    problem
    for problem in problems
    if problem.get("code") in {"CORRUPT_CHAIN", "UNSUPPORTED_FACTS"}
]

if expected_code == "ACCEPT":
    if document.get("fact_tips", {}).get(seat) != tip:
        raise SystemExit(f"{label}: planted event is not the observed fact tip")
    if classifications:
        raise SystemExit(
            f"{label}: accepted event received classification errors: {classifications}"
        )
else:
    matches = [
        problem
        for problem in classifications
        if problem.get("code") == expected_code
        and problem.get("seat") == seat
        and tip in problem.get("detail", "")
        and expected_detail in problem.get("detail", "")
    ]
    if len(classifications) != 1 or len(matches) != 1:
        raise SystemExit(
            f"{label}: expected sole {expected_code} for {seat}/{tip} "
            f"containing {expected_detail!r}; got {classifications}"
        )
PY
  then
    corpus_fail "$label: kernel classification did not match the table row"
  fi
)

# label | chain seat | exact class | exact reason substring | payload builder mode
while IFS='|' read -r label seat expected_code expected_detail kind; do
  run_vector "$label" "$seat" "$expected_code" "$expected_detail" "$kind"
done <<'EOF'
legal-claim|codex|ACCEPT||legal-claim
no-source|grok|CORRUPT_CHAIN|static obligation requires source|no-source
numeric-session|claude|CORRUPT_CHAIN|session must be a string or null|numeric-session
non-utc|codex|CORRUPT_CHAIN|must be an RFC3339 UTC date-time|non-utc
bad-scope|grok|CORRUPT_CHAIN|invalid scope kind "bogus"|bad-scope
bad-cancel|claude|CORRUPT_CHAIN|invalid cancel target kind "bogus"|bad-cancel
EOF

corpus_pass

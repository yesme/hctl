#!/usr/bin/env bash
# Case #29: memo-only base advance may carry one exact candidate; whitespace,
# commit-message, and non-memo base changes must fail closed.
# D: D-04/D-40/D-41 | Source: codex-27k §3.1 | Mode: hctl-wire
set -euo pipefail
CORPUS_CASE_ID="29-regate-carry"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/hctl_fixture.sh"

corpus_require_hctl
corpus_sandbox
corpus_hctl_fixture

repo=$HCTL_FIXTURE_REPO
origin=$HCTL_FIXTURE_ORIGIN
pr=29
old_base=$(git -C "$repo" rev-parse main)

git -C "$repo" switch -q -c work/codex/demo
printf 'if ready:\n' >"$repo/candidate.py"
git -C "$repo" add candidate.py
git -C "$repo" commit -q -m candidate
old_head=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" push -q origin work/codex/demo
git -C "$origin" update-ref "refs/pull/$pr/head" "$old_head"

status=$(corpus_hctl grok status --json)
gate_id=$(printf '%s' "$status" | python3 -c '
import json, sys
doc = json.load(sys.stdin)
for item in doc["obligations"]:
    if item["assignment"] == "demo" and item["kind"] == "gate" and item["holder"] == "grok":
        print(item["obligation"]["id"])
        break
')
[ -n "$gate_id" ] || corpus_fail "gate obligation missing"

corpus_hctl grok claim "$gate_id" >/dev/null
corpus_hctl grok verdict "$gate_id" \
  --decision APPROVE \
  --report memory/review.md \
  --completeness COMPLETE \
  --scope full \
  --pr "$pr" >/dev/null
verdict_oid=$(git -C "$origin" rev-parse refs/coop/grok)

# Positive: advance main only under memory/, then replay the candidate via rebase.
git -C "$repo" switch -q main
printf 'round memo\n' >"$repo/memory/grok-round.md"
git -C "$repo" add memory/grok-round.md
git -C "$repo" commit -q -m 'merge gate memo'
git -C "$repo" push -q origin main
memo_base=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" switch -q work/codex/demo
git -C "$repo" rebase -q main
equivalent_head=$(git -C "$repo" rev-parse HEAD)
[ "$equivalent_head" != "$old_head" ] || corpus_fail "fixture did not rewrite candidate head"
git -C "$repo" push -q --force origin work/codex/demo
git -C "$origin" update-ref "refs/pull/$pr/head" "$equivalent_head"

status=$(corpus_hctl grok status --json)
printf '%s' "$status" | python3 -c '
import json, sys
old_base, old_head, verdict = sys.argv[1:]
doc = json.load(sys.stdin)
gate = next(
    item for item in doc["obligations"]
    if item["assignment"] == "demo" and item["kind"] == "gate" and item["holder"] == "grok"
)
carry = gate.get("carried")
assert gate["state"] == "satisfied", gate
assert carry and carry["kind"] == "memo-base-equivalent", gate
assert carry["verdict"] == verdict, carry
assert carry["revision"] == {"base": old_base, "head": old_head}, carry
assert carry["report"]["commit"] == old_base, carry
assert carry["report"]["path"] == "memory/review.md", carry
' "$old_base" "$old_head" "$verdict_oid" || corpus_fail "positive carry evidence mismatch"

# Carried green must reach the real merge guard, not merely decorate status.
merge_id=$(printf '%s' "$status" | python3 -c '
import json, sys
doc = json.load(sys.stdin)
merge = next(
    item for item in doc["obligations"]
    if item["assignment"] == "demo" and item["kind"] == "merge"
)
assert merge["green"] is True, merge
print(merge["obligation"]["id"])
')
[ -n "$merge_id" ] || corpus_fail "merge obligation missing after carry"
corpus_hctl claude claim "$merge_id" >/dev/null
merge_check=$(corpus_hctl claude merge "$pr" --check)
printf '%s' "$merge_check" | grep -q 'Hctl-Head:' ||
  corpus_fail "carried quorum did not pass merge --check: $merge_check"

assert_no_carry() {
  local label=$1 json=$2
  printf '%s' "$json" | python3 -c '
import json, sys
label = sys.argv[1]
doc = json.load(sys.stdin)
gate = next(
    item for item in doc["obligations"]
    if item["assignment"] == "demo" and item["kind"] == "gate" and item["holder"] == "grok"
)
assert "carried" not in gate, (label, gate)
assert gate["state"] != "satisfied", (label, gate)
' "$label" || corpus_fail "$label unexpectedly carried"
}

publish_rewrite() {
  local base=$1 content=$2 message=$3
  git -C "$repo" switch -q -C work/codex/demo "$base"
  printf '%s' "$content" >"$repo/candidate.py"
  git -C "$repo" add candidate.py
  git -C "$repo" commit -q -m "$message"
  local head
  head=$(git -C "$repo" rev-parse HEAD)
  git -C "$repo" push -q --force origin work/codex/demo
  git -C "$origin" update-ref "refs/pull/$pr/head" "$head"
}

# Adversary 1: git patch-id would erase this whitespace distinction; exact
# blob/tree identity must not.
publish_rewrite "$memo_base" $'    if ready:\n' candidate
status=$(corpus_hctl grok status --json)
assert_no_carry whitespace "$status"

# Adversary 2: identical tree delta but changed full commit-message bytes.
publish_rewrite "$memo_base" $'if ready:\n' 'candidate changed'
status=$(corpus_hctl grok status --json)
assert_no_carry commit-message "$status"

# Negative base branch: even an exact candidate cannot carry when main moved
# outside memory/.
git -C "$repo" switch -q main
printf 'source movement\n' >"$repo/README.md"
git -C "$repo" add README.md
git -C "$repo" commit -q -m 'source base movement'
git -C "$repo" push -q origin main
source_base=$(git -C "$repo" rev-parse HEAD)
publish_rewrite "$source_base" $'if ready:\n' candidate
status=$(corpus_hctl grok status --json)
assert_no_carry non-memo-base "$status"

corpus_pass

#!/usr/bin/env bash
# Case #7: receipt fact-tips + historical replay; mutations hit claimed guards only
# D: D-37 | Source: codex-27b / codex-27k §2.2 terminal closure
# Mode: hctl-wire
set -euo pipefail
CORPUS_CASE_ID="07-receipt-fact-tips-parent"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

corpus_require_hctl
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/hctl_fixture.sh"

# Force-update origin main + drop tracking so hctl fetch cannot non-ff fail.
plant_main() {
  local oid=$1
  git -C "$HCTL_FIXTURE_REPO" push -q --force origin "$oid:refs/heads/main"
  git -C "$HCTL_FIXTURE_REPO" update-ref -d refs/hctl/remotes/origin/heads/main 2>/dev/null || true
}

# Build a full happy-path fixture; prints paths via globals.
run_happy_path() {
  corpus_sandbox
  corpus_hctl_fixture
  corpus_hctl codex begin demo >/dev/null
  echo change > "$HCTL_FIXTURE_REPO/change.txt"
  git -C "$HCTL_FIXTURE_REPO" add change.txt
  git -C "$HCTL_FIXTURE_REPO" commit -q -m change
  git -C "$HCTL_FIXTURE_REPO" push -q origin work/codex/demo
  head=$(git -C "$HCTL_FIXTURE_REPO" rev-parse HEAD)
  git -C "$HCTL_FIXTURE_ORIGIN" update-ref refs/pull/1/head "$head"

  status_g=$(corpus_hctl grok status --json)
  gate_id=$(printf '%s' "$status_g" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(next(o["obligation"]["id"] for o in d["obligations"] if o["kind"]=="gate" and o.get("holder")=="grok"))')
  corpus_hctl grok claim "$gate_id" >/dev/null
  corpus_hctl grok verdict "$gate_id" \
    --decision APPROVE --report memory/review.md --completeness COMPLETE --pr 1 >/dev/null

  status_c=$(corpus_hctl claude status --json)
  merge_id=$(printf '%s' "$status_c" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(next(o["obligation"]["id"] for o in d["obligations"] if o["kind"]=="merge"))')
  claim_out=$(corpus_hctl claude claim "$merge_id")
  merge_claim=$(echo "$claim_out" | sed -n 's/^fencing_token=//p')
  [ -n "$merge_claim" ] || corpus_fail "merge claim missing"

  check_out=$(corpus_hctl claude merge 1 --check)
  receipt_body=$(printf '%s\n' "$check_out" | sed -n '/^receipt:/,$p' | sed '1d')
  [ -n "$receipt_body" ] || corpus_fail "empty receipt body"

  base=$(git -C "$HCTL_FIXTURE_REPO" rev-parse refs/remotes/origin/main)
  tree=$(git -C "$HCTL_FIXTURE_REPO" rev-parse "$head^{tree}")
  merge_oid=$(git -C "$HCTL_FIXTURE_REPO" commit-tree "$tree" -p "$base" -m "demo (#1)

$receipt_body")
  plant_main "$merge_oid"
  git -C "$HCTL_FIXTURE_ORIGIN" update-ref -d refs/heads/work/codex/demo 2>/dev/null || true

  status_after=$(corpus_hctl claude status --json)
  printf '%s' "$status_after" | python3 -c '
import json,sys
d=json.load(sys.stdin)
m=next(o for o in d["obligations"] if o["kind"]=="merge")
assert m.get("completed") or m.get("state")=="completed", m
print("completed")
'
}

# --- 1) happy path ---
run_happy_path

# Capture good receipt pieces for mutations (sibling squash: parent==Hctl-Base)
GOOD_RECEIPT=$receipt_body
GOOD_BASE=$base
GOOD_TREE=$tree
GOOD_HEAD=$head
GOOD_MERGE_CLAIM=$merge_claim

# --- 2) non-prefix fact tip (fresh fixture, identity-valid squash) ---
# Independent sandbox so only this guard is exercised.
corpus_sandbox
corpus_hctl_fixture
# Re-run happy path into this sandbox to get a valid base receipt, then replace tip.
run_happy_path
empty=$(git -C "$HCTL_FIXTURE_REPO" mktree </dev/null)
orphan=$(git -C "$HCTL_FIXTURE_REPO" commit-tree "$empty" -m orphan)
# parent must equal Hctl-Base; tree equals head tree — only tip is wrong
bad_prefix_body=$(printf '%s\n' "$receipt_body" | sed "s/Hctl-Fact-Tip: grok=[0-9a-f]*/Hctl-Fact-Tip: grok=$orphan/")
printf '%s\n' "$bad_prefix_body" | corpus_py receipt.py parse - | grep -q 'ok tips=' \
  || corpus_fail "non-prefix body must shape-parse"
# Plant as the only integration tip: parent=Hctl-Base from receipt
hctl_base=$(echo "$receipt_body" | sed -n 's/^Hctl-Base: //p' | head -1)
prefix_oid=$(git -C "$HCTL_FIXTURE_REPO" commit-tree "$tree" -p "$hctl_base" -m "demo-prefix (#1)

$bad_prefix_body")
plant_main "$prefix_oid"
set +e
out_pre=$(corpus_hctl claude status 2>&1)
set -e
# Must hit prefix guard — not identity/parent mismatch
echo "$out_pre" | grep -qiE 'is not a prefix of current facts|absent from the current chain set' \
  || corpus_fail "non-prefix must hit fact-tip prefix guard: $out_pre"
echo "$out_pre" | grep -qi 'identity fields do not match' \
  && corpus_fail "non-prefix must NOT fail as identity mismatch: $out_pre"

# --- 3) inactive merge claim (fresh fixture) ---
corpus_sandbox
corpus_hctl_fixture
run_happy_path
fake_claim=$(git -C "$HCTL_FIXTURE_REPO" commit-tree "$(git -C "$HCTL_FIXTURE_REPO" mktree </dev/null)" -m fake-claim)
bad_claim_body=$(printf '%s\n' "$receipt_body" | sed "s/^Hctl-Merge-Claim: .*/Hctl-Merge-Claim: $fake_claim/")
hctl_base=$(echo "$receipt_body" | sed -n 's/^Hctl-Base: //p' | head -1)
# shape-ok
printf '%s\n' "$bad_claim_body" | corpus_py receipt.py parse - | grep -q 'ok tips=' \
  || corpus_fail "inactive-claim body must shape-parse"
inactive_oid=$(git -C "$HCTL_FIXTURE_REPO" commit-tree "$tree" -p "$hctl_base" -m "demo-inactive (#1)

$bad_claim_body")
plant_main "$inactive_oid"
set +e
out_in=$(corpus_hctl claude status 2>&1)
set -e
echo "$out_in" | grep -qiE 'is not active|not active \(active=' \
  || corpus_fail "inactive merge claim must hit active-claim guard: $out_in"
echo "$out_in" | grep -qi 'identity fields do not match' \
  && corpus_fail "inactive claim must NOT fail as identity mismatch: $out_in"

corpus_pass

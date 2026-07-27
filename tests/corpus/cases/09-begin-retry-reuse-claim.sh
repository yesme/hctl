#!/usr/bin/env bash
# Case #9: formal begin：claim 成功、branch 失败、重跑复用原 claim
# D: D-43 | Source: codex-27b | Mechanical: state-aware begin reuses active author claim
# Mode: hctl-wire
set -euo pipefail
CORPUS_CASE_ID="09-begin-retry-reuse-claim"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

corpus_require_hctl
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/hctl_fixture.sh"
corpus_sandbox
corpus_hctl_fixture

# Block parent branch ref: create refs/heads/work/codex/demo/child so creating
# work/codex/demo as a branch ref fails (namespace conflict).
git -C "$HCTL_FIXTURE_REPO" update-ref refs/heads/work/codex/demo/child \
  "$(git -C "$HCTL_FIXTURE_REPO" rev-parse HEAD)"

set +e
out1=$(corpus_hctl codex begin demo 2>&1)
code1=$?
set -e
[ "$code1" -ne 0 ] || corpus_fail "first begin must fail while branch namespace blocked: $out1"
echo "$out1" | grep -qiE 'branch|fencing_token|claim|fail' || corpus_fail "failure should mention branch/claim: $out1"

# Claim must already be on the remote chain (claim succeeded before branch setup failed).
count1=$(git -C "$HCTL_FIXTURE_ORIGIN" rev-list --count refs/coop/codex 2>/dev/null || echo 0)
[ "$count1" = "1" ] || corpus_fail "after failed begin, chain must have exactly 1 CLAIM, got $count1"
claim1=$(git -C "$HCTL_FIXTURE_ORIGIN" rev-parse refs/coop/codex)

# Unblock and retry: reuses same claim, succeeds.
git -C "$HCTL_FIXTURE_REPO" update-ref -d refs/heads/work/codex/demo/child
out2=$(corpus_hctl codex begin demo)
echo "$out2" | grep -q 'begin formal' || corpus_fail "retry begin formal: $out2"
claim2=$(echo "$out2" | sed -n 's/^claim=//p')
[ -n "$claim2" ] && [ "$claim2" != "<none>" ] || corpus_fail "retry must report claim"
[ "$claim1" = "$claim2" ] || corpus_fail "retry must reuse claim $claim1 != $claim2"

count2=$(git -C "$HCTL_FIXTURE_ORIGIN" rev-list --count refs/coop/codex)
[ "$count2" = "1" ] || corpus_fail "retry must not mint second CLAIM, got $count2"

# Second successful begin still reuses (idempotent complete path).
out3=$(corpus_hctl codex begin demo)
claim3=$(echo "$out3" | sed -n 's/^claim=//p')
[ "$claim1" = "$claim3" ] || corpus_fail "third begin must still reuse claim"

corpus_pass

#!/usr/bin/env bash
# Case #8: authority:user 不冒充机器证明；缺 assignment 写动作拒
# D: D-38 | Source: codex-27b | Mechanical: level-4 declaration vs level-2 assignment required
# Mode: pure note + hctl-wire
set -euo pipefail
CORPUS_CASE_ID="08-authority-and-assignment-gate"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

# Spec note (NOT an assertion of kernel behavior): authority.kind=user is a declaration.
# wire-pending illustration only — does not contribute to PASS by itself.
# Real check: unknown assignment / claim without derived obligation is rejected by hctl.

corpus_require_hctl
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/hctl_fixture.sh"
corpus_sandbox
corpus_hctl_fixture

# status works (assignment present)
corpus_hctl codex status --json | grep -q '"complete"'

# claim unknown obligation id → fail (no assignment backref)
set +e
out=$(corpus_hctl codex claim 'sha256:0000000000000000000000000000000000000000000000000000000000000000' 2>&1)
code=$?
set -e
[ "$code" -ne 0 ] || corpus_fail "claim unknown obligation must fail"
echo "$out" | grep -qiE 'unknown|obligation' || corpus_fail "error should mention unknown obligation"

# begin unknown assignment → fail
set +e
out=$(corpus_hctl codex begin 'no-such-assignment' 2>&1)
code=$?
set -e
[ "$code" -ne 0 ] || corpus_fail "begin without assignment must fail"
echo "$out" | grep -qiE 'unknown|assignment' || corpus_fail "error should mention unknown assignment"

corpus_pass

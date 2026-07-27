#!/usr/bin/env bash
# Case #12: 重复 logical assignment id ⇒ loader 拒
# D: D-33 | Source: codex/grok | Mechanical: loader unique logical id
# Mode: hctl-wire (ad-hoc Python stub removed per 27n F1)
set -euo pipefail
CORPUS_CASE_ID="12-dup-assignment-logical-id"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

corpus_require_hctl
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/hctl_fixture.sh"
corpus_sandbox
corpus_hctl_fixture

# valid single id loads
corpus_hctl codex status --json | python3 -c 'import json,sys; d=json.load(sys.stdin); assert any(o.get("obligation") for o in d.get("obligations",[]))'

# duplicate logical id on main
cp "$HCTL_FIXTURE_REPO/.hctl/assignments/demo.toml" "$HCTL_FIXTURE_REPO/.hctl/assignments/demo-dup.toml"
git -C "$HCTL_FIXTURE_REPO" add .hctl/assignments/demo-dup.toml
git -C "$HCTL_FIXTURE_REPO" commit -q -m 'dup assignment id'
git -C "$HCTL_FIXTURE_REPO" push -q origin main

set +e
out=$(corpus_hctl codex status 2>&1)
code=$?
set -e
# fail-closed: either non-zero exit or ERROR/AMBIGUOUS in output
if [ "$code" -eq 0 ]; then
  echo "$out" | grep -qiE 'AMBIGUOUS|duplicate|error' || corpus_fail "dup id must not look healthy: $out"
else
  echo "$out" | grep -qiE 'AMBIGUOUS|duplicate|assignment|error|ERROR' || corpus_fail "error should mention ambiguity: $out"
fi

corpus_pass

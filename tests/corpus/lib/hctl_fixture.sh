#!/usr/bin/env bash
# Minimal hctl-ready sandbox (bash port of internal/app/app_test.go appFixture).
# Requires: corpus_sandbox already called; CORPUS_WORK is cwd.
# Sets: HCTL_FIXTURE_REPO HCTL_FIXTURE_ORIGIN
# shellcheck shell=bash

corpus_hctl_fixture() {
  local seats_extra=${1:-}
  git init -q --bare --initial-branch=main origin.git
  git clone -q origin.git work
  git -C work config user.name corpus
  git -C work config user.email corpus@hctl.invalid
  mkdir -p work/.hctl/assignments work/memory
  cat > work/.hctl/seats.toml <<'EOF'
schema_version = 1
kernel = "hctl@bootstrap"
merge_coordinator = "claude"
enforcement = "bootstrap"
[seats.claude]
harness = "claude"
model = "test"
capabilities = []
write_scope = "canonical"
author_concurrency = 1
author_branch_patterns = ["refs/heads/work/claude/*"]
memo_branch_patterns = ["refs/heads/memo/claude/*"]
[seats.claude.features]
session_resume = "unknown"
parallel_sessions = "unknown"
subagents = "unknown"
skill = "unknown"
door_file = "CLAUDE.md"
[seats.codex]
harness = "codex"
model = "test"
capabilities = []
write_scope = "canonical"
author_concurrency = 1
author_branch_patterns = ["refs/heads/work/codex/*"]
memo_branch_patterns = ["refs/heads/memo/codex/*"]
[seats.codex.features]
session_resume = "unknown"
parallel_sessions = "unknown"
subagents = "unknown"
skill = "unknown"
door_file = "AGENTS.md"
[seats.grok]
harness = "grok"
model = "test"
capabilities = []
write_scope = "canonical"
author_concurrency = 1
author_branch_patterns = ["refs/heads/work/grok/*"]
memo_branch_patterns = ["refs/heads/memo/grok/*"]
[seats.grok.features]
session_resume = "unknown"
parallel_sessions = "unknown"
subagents = "unknown"
skill = "unknown"
door_file = "AGENTS.md"
EOF
  # optional extra seats fragment not used; seats_extra reserved
  : "$seats_extra"
  cat > work/.hctl/assignments/demo.toml <<'EOF'
schema_version = 1
id = "demo"
kind = "change"
needs = []
base_ref = "refs/heads/main"
[author]
seat = "codex"
branch_slug = "demo"
claim_timeout_seconds = 3600
[[gates]]
id = "review"
mode = "required"
threshold = "P1"
claim_timeout_seconds = 3600
[gates.requirement]
seat = "grok"
[gates.on_timeout]
action = "escalate"
[merge]
method = "squash"
EOF
  echo '# review' > work/memory/review.md
  git -C work add .hctl memory/review.md
  git -C work commit -q -m bootstrap
  git -C work push -q origin main
  # tracking-style fetch for coop (no force '+'; doctor requires non-ff rollback visibility)
  git -C work config --add remote.origin.fetch 'refs/coop/*:refs/hctl/remotes/origin/coop/*'
  HCTL_FIXTURE_REPO=$(pwd)/work
  HCTL_FIXTURE_ORIGIN=$(pwd)/origin.git
  export HCTL_FIXTURE_REPO HCTL_FIXTURE_ORIGIN
}

# Run hctl against fixture. Args after seat are hctl subcommand args.
# Usage: corpus_hctl <seat> <cmd...>
corpus_hctl() {
  local seat=$1
  shift
  [ -n "${HCTL:-}" ] && [ -x "${HCTL}" ] || corpus_fail "corpus_hctl requires executable HCTL"
  [ -n "${HCTL_FIXTURE_REPO:-}" ] || corpus_fail "corpus_hctl_fixture not initialized"
  corpus_hctl_wire_stamp
  "$HCTL" -C "$HCTL_FIXTURE_REPO" --remote origin --seat "$seat" --machine "corpus-${seat}" "$@"
}

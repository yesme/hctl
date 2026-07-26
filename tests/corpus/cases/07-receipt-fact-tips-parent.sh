#!/usr/bin/env bash
# Case #7: receipt 多 Hctl-Fact-Tip 重放 quorum；main parent==Hctl-Base
# D: D-37 | Source: codex-27b | Mechanical: receipt trailers + squash parent check
# Mode: hybrid  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="07-receipt-fact-tips-parent"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

corpus_sandbox
git init -q
empty=$(git mktree </dev/null)
base=$(git commit-tree "$empty" -m base)
# squash-like: single parent == base
head=$(git commit-tree "$empty" -p "$base" -m 'squash (#1)
Hctl-Version: 1
Hctl-Assignment: p1-demo
Hctl-Obligation: sha256:deadbeef
Hctl-PR: 1
Hctl-Base: '"$base"'
Hctl-Head: abc
Hctl-Merge-Claim: def
Hctl-Method: squash
Hctl-Fact-Tip: claude=aaa
Hctl-Fact-Tip: codex=bbb
Hctl-Fact-Tip: grok=ccc
')
parent=$(git rev-parse "$head^")
[ "$parent" = "$base" ] || corpus_fail "squash parent must equal Hctl-Base"
msg=$(git log -1 --format=%B "$head")
echo "$msg" | grep -q "Hctl-Base: $base" || corpus_fail "Hctl-Base trailer"
n=$(echo "$msg" | grep -c '^Hctl-Fact-Tip:' || true)
[ "$n" -eq 3 ] || corpus_fail "expected 3 Fact-Tip lines, got $n"
# replay quorum prefixes from tips (presence check)
echo "$msg" | grep -q 'Hctl-Fact-Tip: claude=' || corpus_fail "claude tip"
echo "$msg" | grep -q 'Hctl-Fact-Tip: codex=' || corpus_fail "codex tip"
corpus_pass

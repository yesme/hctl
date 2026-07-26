#!/usr/bin/env bash
# 语料（反面展品）：git notes 基质下 CLAIM 互斥失效——官方合并策略 cat_sort_uniq = union
# 断言候选基质的失效模式本身（弃 notes 的可执行证据，K-21/K-28）；非 hctl 行为断言。
# 出处：memory/claude-2026-07-27c.md §3B。
set -euo pipefail

export GIT_AUTHOR_NAME=corpus GIT_AUTHOR_EMAIL=corpus@hctl.invalid
export GIT_COMMITTER_NAME=corpus GIT_COMMITTER_EMAIL=corpus@hctl.invalid
export GIT_AUTHOR_DATE='2026-07-27T00:00:00+00:00' GIT_COMMITTER_DATE='2026-07-27T00:00:00+00:00'

work=$(mktemp -d "${TMPDIR:-/tmp}/hctl-corpus.XXXXXX")
trap 'rm -rf "$work"' EXIT
cd "$work"

fail() { echo "FAIL(notes-union-antimutex): $*" >&2; exit 1; }

NREF=refs/notes/hctl/claude

git -c init.defaultBranch=main init -q --bare origin.git
git clone -q origin.git mac 2>/dev/null
git clone -q origin.git ubuntu 2>/dev/null
git -C mac commit -q --allow-empty -m seed
git -C mac push -q origin main
git -C ubuntu fetch -q origin
seed=$(git -C ubuntu rev-parse origin/main)

# ── B1 化身 mac 认领：note append + push
git -C mac notes --ref="$NREF" append -m '{"v":0,"type":"CLAIM","obligation":"gate:demo","machine":"mac","ts":"1753600000"}' main
git -C mac push -q origin "$NREF" || fail "B1 首发 notes push 应成功"

# ── B2 化身 ubuntu 同窗口认领：push 被拒（至此与链同性质）
git -C ubuntu notes --ref="$NREF" append -m '{"v":0,"type":"CLAIM","obligation":"gate:demo","machine":"ubuntu","ts":"1753600001"}' "$seed"
if git -C ubuntu push -q origin "$NREF" 2>/dev/null; then
  fail "B2 竞争 push 应被拒绝"
fi

# ── B3 官方冲突处理：fetch + notes merge -s cat_sort_uniq → 成功 → push 成功
git -C ubuntu fetch -q origin "+$NREF:refs/notes/hctl-remote/claude"
git -C ubuntu notes --ref="$NREF" merge -s cat_sort_uniq refs/notes/hctl-remote/claude >/dev/null 2>&1 \
  || fail "B3 官方合并路径应成功（问题恰在于它成功）"
git -C ubuntu push -q origin "$NREF" || fail "B3 合并后 push 应成功"

# ── B4 后果：两条 CLAIM 并存于同一 note——互斥被 union 消解，无法承担 CLAIM winner
n=$(git -C ubuntu notes --ref="$NREF" show "$seed" | grep -c '"type":"CLAIM"')
[ "$n" -eq 2 ] || fail "B4 预期两条 CLAIM 并存，实得 $n"

echo "PASS notes-union-antimutex（反面展品：union 合并使双 CLAIM 并存）"

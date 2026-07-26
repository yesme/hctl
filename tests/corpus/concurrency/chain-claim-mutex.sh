#!/usr/bin/env bash
# 语料：event commit chain 上的 CLAIM 双层互斥（K-28 / K-30）
# 断言对象：git ref 物理护栏（update-ref CAS + push --force-with-lease），不断言 LLM 行为（K-17）。
# 出处：memory/claude-2026-07-27c.md §3A；事件形态按 §5 改判（tree 单文件 event.json + message 摘要）。
set -euo pipefail

export GIT_AUTHOR_NAME=corpus GIT_AUTHOR_EMAIL=corpus@hctl.invalid
export GIT_COMMITTER_NAME=corpus GIT_COMMITTER_EMAIL=corpus@hctl.invalid
export GIT_AUTHOR_DATE='2026-07-27T00:00:00+00:00' GIT_COMMITTER_DATE='2026-07-27T00:00:00+00:00'

work=$(mktemp -d "${TMPDIR:-/tmp}/hctl-corpus.XXXXXX")
trap 'rm -rf "$work"' EXIT
cd "$work"

fail() { echo "FAIL(chain-claim-mutex): $*" >&2; exit 1; }

REF=refs/coop/claude

# event <clone> <parent-oid-or-empty> <json> <summary> → 新 event commit oid
event() {
  local c=$1 parent=$2 json=$3 summary=$4 blob tree
  blob=$(printf '%s\n' "$json" | git -C "$c" hash-object -w --stdin)
  tree=$(printf '100644 blob %s\tevent.json\n' "$blob" | git -C "$c" mktree)
  if [ -n "$parent" ]; then
    git -C "$c" commit-tree "$tree" -p "$parent" -m "$summary"
  else
    git -C "$c" commit-tree "$tree" -m "$summary"
  fi
}

git -c init.defaultBranch=main init -q --bare origin.git
git clone -q origin.git mac 2>/dev/null
git clone -q origin.git ubuntu 2>/dev/null

# ── A1 化身 mac 发布 CLAIM：本地 CAS 创建 + lease push（空 expect = 远端必须不存在）
e1=$(event mac '' '{"v":0,"type":"CLAIM","obligation":"gate:demo","actor":{"seat":"claude","machine":"mac"},"ts":"1753600000"}' 'CLAIM gate:demo by mac')
git -C mac update-ref "$REF" "$e1" ''
git -C mac push -q --force-with-lease="$REF": origin "$REF" || fail "A1 首发 push 应成功"
[ "$(git -C origin.git rev-parse "$REF")" = "$e1" ] || fail "A1 远端 tip 应为 e1"

# ── A2 化身 ubuntu 同窗口竞争 CLAIM：push 必须被拒，远端不动
e2=$(event ubuntu '' '{"v":0,"type":"CLAIM","obligation":"gate:demo","actor":{"seat":"claude","machine":"ubuntu"},"ts":"1753600001"}' 'CLAIM gate:demo by ubuntu')
git -C ubuntu update-ref "$REF" "$e2" ''
if git -C ubuntu push -q --force-with-lease="$REF": origin "$REF" 2>/dev/null; then
  fail "A2 竞争 push 应被拒绝"
fi
[ "$(git -C origin.git rev-parse "$REF")" = "$e1" ] || fail "A2 远端 tip 必须仍为 e1（原子性）"

# ── A3 输家对账让位：fetch 权威 → CAS 复位 → 链上续发自家事件 → lease push（expect=e1）
git -C ubuntu fetch -q origin "$REF"
r=$(git -C ubuntu rev-parse FETCH_HEAD)
[ "$r" = "$e1" ] || fail "A3 fetch 应看到赢家 e1"
git -C ubuntu update-ref "$REF" "$r" "$e2"
e3=$(event ubuntu "$r" '{"v":0,"type":"VERDICT","obligation":"gate:demo","revision":{"base":"b0","head":"h0"},"decision":"APPROVE","actor":{"seat":"claude","machine":"ubuntu"},"ts":"1753600100"}' 'VERDICT APPROVE by ubuntu')
git -C ubuntu update-ref "$REF" "$e3" "$r"
git -C ubuntu push -q --force-with-lease="$REF":"$e1" origin "$REF" || fail "A3 让位后续发应成功"
[ "$(git -C origin.git rev-parse "$REF")" = "$e3" ] || fail "A3 远端 tip 应推进到 e3"

# ── A4 链性质：线性、payload 内容寻址、时序=拓扑序（零时钟依赖）
git -C ubuntu merge-base --is-ancestor "$e1" "$e3" || fail "A4 e1 必须是 e3 祖先（线性）"
[ "$(git -C ubuntu rev-list --count "$REF")" -eq 2 ] || fail "A4 链长应为 2"
[ "$(git -C ubuntu log --format=%H "$REF" | tail -1)" = "$e1" ] || fail "A4 链根应为 e1"
git -C ubuntu show "$e3:event.json" | grep -q '"type":"VERDICT"' || fail "A4 payload 应可按 <oid>:event.json 寻址"

# ── A5 同机双 tab（共享 .git）：第二写者被 update-ref CAS 挡出（响亮失败）
tip=$(git -C ubuntu rev-parse "$REF")
a=$(event ubuntu "$tip" '{"v":0,"type":"CLAIM","obligation":"x","actor":{"seat":"claude","machine":"ubuntu","session":"tab-a"},"ts":"1753600200"}' 'CLAIM x by tab-a')
b=$(event ubuntu "$tip" '{"v":0,"type":"CLAIM","obligation":"x","actor":{"seat":"claude","machine":"ubuntu","session":"tab-b"},"ts":"1753600201"}' 'CLAIM x by tab-b')
git -C ubuntu update-ref "$REF" "$a" "$tip" || fail "A5 tab-a CAS 应成功"
if git -C ubuntu update-ref "$REF" "$b" "$tip" 2>/dev/null; then
  fail "A5 tab-b CAS 应失败（响亮）"
fi
[ "$(git -C ubuntu rev-parse "$REF")" = "$a" ] || fail "A5 本地 tip 应为 tab-a"

# ── A6 回卷检测：裸 push 会把远端回卷无声「修复」；lease push（expect=最后已知远端）响亮失败
git -C origin.git update-ref "$REF" "$e1"    # 模拟事故：远端链被回退到祖先 e1
git -C ubuntu push -q origin "$REF" || fail "A6a 裸 push 预期成功（纯 ff）——这正是盲区所在"
[ "$(git -C origin.git rev-parse "$REF")" = "$a" ] || fail "A6a 裸 push 应已无声覆盖回卷"
git -C origin.git update-ref "$REF" "$e1"    # 复原事故现场
if git -C ubuntu push -q --force-with-lease="$REF":"$e3" origin "$REF" 2>/dev/null; then
  fail "A6b lease push 在远端≠expect 时应失败（回卷必须响亮）"
fi
[ "$(git -C origin.git rev-parse "$REF")" = "$e1" ] || fail "A6b 远端应停在回卷位等待完整性处置，而非被静默修复"

echo "PASS chain-claim-mutex"

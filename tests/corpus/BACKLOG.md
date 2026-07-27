# Corpus BACKLOG — 语料清单 #1–#28（P0 fixtures 规格）

> 语料原则（D-17）：断言护栏与 git 物理，不断言 LLM 行为；每案自带沙盘、零网络、退出码判定。
> 已可执行：concurrency 两案 + `cases/01`–`28`（`bash tests/corpus/run.sh`；门禁用 `HCTL=… CORPUS_REQUIRE_HCTL=1`）。
> 执行注记（codex-27e，非新条件）：#23–26 receipt/完整性校验失败先红灯再谈 slot（C7 优先级）；#27 derive 先跑 ref integrity 再进 progress 分类（回卷≠environment reset）；#28 parser 须保留 duplicate-key / lone-surrogate 检测（禁静默 last-wins map parser）。
> **接线（fix-forward / p1-corpus-terminal）**：`run.sh` 冻结 manifest；wire 案真实调用 `hctl`（#3/#6–#9/#11–#12/#28）。known-event 结构以 Go 为唯一执行者；#6 table 每行使用独立 fixture，并按 event OID/seat/code/reason 精确归因，禁止跨席残留红态代打。pure helper 仅 envelope/unknown-type（KNOWN_TYPE_DEFER）。#7 receipt 负例须命中声明 guard 子串（codex-27k §2.2）。

| # | 案 | 机械层 | 源 | 脚本 |
|---|---|---|---|---|
| 0a | 链上 CLAIM 双层互斥 | update-ref CAS + lease push + 回卷检测 | 27c | `concurrency/chain-claim-mutex.sh` |
| 0b | notes union 反面展品 | cat_sort_uniq=union 破互斥 | 27c | `concurrency/notes-union-antimutex.sh` |
| 1 | gate head 不变、base 前进 ⇒ claim 与 verdict 双 stale | D-04/D-34 | codex-27b | `cases/01-gate-base-advance-stale.sh` |
| 2 | 同 coordinator 两个 merge obligations ⇒ 第二 claim 被 capacity=1 拒 | D-37 | codex-27b | `cases/02-merge-capacity-one.sh` |
| 3 | merge obligation 指派非 coordinator ⇒ loader 拒 | D-37 | codex-27b | `cases/03-merge-assignee-coordinator.sh` |
| 4 | ambiguous push 后远端已 append descendant ⇒ ancestry 判 delivered | D-34 | codex-27b | `cases/04-push-ancestry-delivered.sh` |
| 5 | pending 与远端 winner 分叉 ⇒ 判 lost + recovery ref | D-34 | codex-27b | `cases/05-push-diverged-lost.sh` |
| 6 | 合法 JSON 未知 type/version ⇒ `UNSUPPORTED_FACTS`、merge 拒 | D-39 | codex-27b | `cases/06-unsupported-facts.sh` |
| 7 | receipt 多 `Hctl-Fact-Tip` 重放 quorum；main parent==`Hctl-Base` | D-37 | codex-27b | `cases/07-receipt-fact-tips-parent.sh` |
| 8 | `authority:user` 不冒充机器证明；缺 assignment 的写动作拒 | D-38 | codex-27b | `cases/08-authority-and-assignment-gate.sh` |
| 9 | formal begin：claim 成功、branch 失败、重跑复用原 claim | D-43 | codex-27b | `cases/09-begin-retry-reuse-claim.sh` |
| 10 | 同 payload blob 异 parent ⇒ 两个事件，不按 blob 折叠 | D-34 | codex-27b | `cases/10-same-blob-two-events.sh` |
| 11 | author/memo pattern 跨席跨 lane 两两不相交（doctor） | D-08 | grok-27b | `cases/11-branch-pattern-disjoint.sh` |
| 12 | 重复 logical assignment id ⇒ loader 拒 | D-33 | codex/grok | `cases/12-dup-assignment-logical-id.sh` |
| 13 | assignment blob 无语义变更 ⇒ status 亮 `assignment_revision moved` | D-33 | grok-27b | `cases/13-assignment-revision-moved.sh` |
| 14 | escalated 义务 re-claim 被拒；CANCEL(claim) 后计数清零可再 claim | D-34 | grok-27b | `cases/14-escalated-reclaim-frozen.sh` |
| 15 | `pending_accept` 义务他席抢 claim 被拒（P2，HANDOFF 解冻后） | D-36 | 主笔 | `cases/15-pending-accept-deferred.sh` |
| 16 | 双化身同 tick 竞争 re-claim 同 active ⇒ 唯一赢家、`reclaim_of` 失配拒 | D-34/D-35 | grok 一审 | `cases/16-dual-avatar-reclaim-race.sh` |
| 17 | `pending_accept` 临界：ACCEPT 与 TIMEOUT_RETURN 异 ref 并发 ⇒ 报 unsupported | D-36 | codex-27c | `cases/17-pending-accept-cross-ref-dual-win.sh` |
| 18 | coordinator rebind 时旧席有 active merge claim ⇒ 拒或先 fence | D-37 | codex-27c | `cases/18-rebind-active-merge-fence.sh` |
| 19 | escalated 后出现 branch progress ⇒ 不自动解冻；仅三径可清 | D-34 | codex-27c | `cases/19-escalated-progress-no-thaw.sh` |
| 20 | 同 preimage 不同 key order/escape ⇒ 同 id；非 canonical 拒 | D-33 | codex-27c | `cases/20-obligation-id-jcs-stable.sh` |
| 21 | composite scope verdict 可校验；delta-only COMPLETE 不满足 full quorum | D-41 | codex-27c | `cases/21-composite-scope-quorum.sh` |
| 22 | merge 前 late finding 发 REQUEST_CHANGES ⇒ latest-wins 失绿阻断 | D-41 | codex-27c | `cases/22-late-finding-blocks-merge.sh` |
| 23 | 普通 config PR 后 receipt 按 `Hctl-Base` 的旧 config 合法关 claim | D-37 | codex-27d | `cases/23-receipt-closes-after-config-change.sh` |
| 24 | rebind A→B：M 前只有 A 可 claim，M 后只有 B | D-37 | codex-27d | `cases/24-rebind-claim-rights.sh` |
| 25 | 旧 worker 持旧 config 在 M 后新 claim ⇒ 级②拒 | D-37 | codex-27d | `cases/25-stale-config-claim-after-rebind.sh` |
| 26 | receipt 声称 config ≠ `Hctl-Base` 所见 blob ⇒ 拒 | D-37 | codex-27d | `cases/26-receipt-config-mismatch-reject.sh` |
| 27 | H1→H2→H1 摆动不清 streak；base-only 标 env-reset 且清；ancestry 前进清 | D-34 | codex-27d | `cases/27-streak-forward-env-reset.sh` |
| 28 | JCS 向量：key 序/escape 同 id；非 ASCII grammar 拒；dup key / lone surrogate 拒 | D-33 | codex-27d | `cases/28-jcs-vectors.sh` |

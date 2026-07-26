# Corpus BACKLOG — 语料清单 #1–#28（P0 fixtures 规格）

> 语料原则（D-17）：断言护栏与 git 物理，不断言 LLM 行为；每案自带沙盘、零网络、退出码判定。
> 已可执行：#7 批的两案（`concurrency/`）。其余为 P1 实装的验收规格；「机械层」列指被测护栏。
> 执行注记（codex-27e，非新条件）：#23–26 receipt/完整性校验失败先红灯再谈 slot（C7 优先级）；#27 derive 先跑 ref integrity 再进 progress 分类（回卷≠environment reset）；#28 parser 须保留 duplicate-key 检测（禁静默 last-wins map parser）。

| # | 案 | 机械层 | 源 |
|---|---|---|---|
| 0a | 链上 CLAIM 双层互斥（已落 `concurrency/chain-claim-mutex.sh`） | update-ref CAS + lease push + 回卷检测 | 27c |
| 0b | notes union 反面展品（已落 `concurrency/notes-union-antimutex.sh`） | cat_sort_uniq=union 破互斥 | 27c |
| 1 | gate head 不变、base 前进 ⇒ claim 与 verdict 双 stale | D-04/D-34 | codex-27b |
| 2 | 同 coordinator 两个 merge obligations ⇒ 第二 claim 被 capacity=1 拒 | D-37 | codex-27b |
| 3 | merge obligation 指派非 coordinator ⇒ loader 拒 | D-37 | codex-27b |
| 4 | ambiguous push 后远端已 append descendant ⇒ ancestry 判 delivered | D-34 | codex-27b |
| 5 | pending 与远端 winner 分叉 ⇒ 判 lost + recovery ref | D-34 | codex-27b |
| 6 | 合法 JSON 未知 type/version ⇒ `UNSUPPORTED_FACTS`、merge 拒 | D-39 | codex-27b |
| 7 | receipt 多 `Hctl-Fact-Tip` 重放 quorum；main parent==`Hctl-Base` | D-37 | codex-27b |
| 8 | `authority:user` 不冒充机器证明；缺 assignment 的写动作拒 | D-38 | codex-27b |
| 9 | formal begin：claim 成功、branch 失败、重跑复用原 claim | D-43 | codex-27b |
| 10 | 同 payload blob 异 parent ⇒ 两个事件，不按 blob 折叠 | D-34 | codex-27b |
| 11 | author/memo pattern 跨席跨 lane 两两不相交（doctor） | D-08 | grok-27b |
| 12 | 重复 logical assignment id ⇒ loader 拒 | D-33 | codex/grok |
| 13 | assignment blob 无语义变更 ⇒ status 亮 `assignment_revision moved` | D-33 | grok-27b |
| 14 | escalated 义务 re-claim 被拒；CANCEL(claim) 后计数清零可再 claim | D-34 | grok-27b |
| 15 | `pending_accept` 义务他席抢 claim 被拒（P2，HANDOFF 解冻后） | D-36 | 主笔 |
| 16 | 双化身同 tick 竞争 re-claim 同 active ⇒ 唯一赢家、`reclaim_of` 失配拒 | D-34/D-35 | grok 一审 |
| 17 | `pending_accept` 临界：ACCEPT 与 TIMEOUT_RETURN 异 ref 并发 ⇒ 报 unsupported（DEFERRED 证据案） | D-36 | codex-27c |
| 18 | coordinator rebind 时旧席有 active merge claim ⇒ 拒或先 fence | D-37 | codex-27c |
| 19 | escalated 后出现 branch progress ⇒ 不自动解冻；仅三径可清 | D-34 | codex-27c |
| 20 | 同 preimage 不同 key order/escape ⇒ 同 id；非 canonical 拒 | D-33 | codex-27c |
| 21 | composite scope verdict 可校验；delta-only COMPLETE 不满足 full quorum | D-41 | codex-27c |
| 22 | merge 前 late finding 发 REQUEST_CHANGES ⇒ latest-wins 失绿阻断 | D-41 | codex-27c |
| 23 | 普通 config PR 后 receipt 按 `Hctl-Base` 的旧 config 合法关 claim | D-37 | codex-27d |
| 24 | rebind A→B：M 前只有 A 可 claim，M 后只有 B | D-37 | codex-27d |
| 25 | 旧 worker 持旧 config 在 M 后新 claim ⇒ 级②拒 | D-37 | codex-27d |
| 26 | receipt 声称 config ≠ `Hctl-Base` 所见 blob ⇒ 拒 | D-37 | codex-27d |
| 27 | H1→H2→H1 摆动不清 streak；base-only 标 env-reset 且清；ancestry 前进清；integrity 先于分类 | D-34 | codex-27d |
| 28 | JCS 向量：key 序/空白/escape 同 id；NFC/NFD 靠 grammar 拒；dup key/lone surrogate 拒；aspect 字段序同 id | D-33 | codex-27d |

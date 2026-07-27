# METHOD — hctl 规范

> 本文件是唯一规范（normative）。裁定出处与状态见 [DECISIONS.md](DECISIONS.md)（D-xx 引用即指向该台账）；论证与演化史在 `memory/`（考古层，默认不读、指针驱动）。措辞约定：**必须/禁止** = 法律（违反即实现缺陷）；**应** = 强默认；**可** = 明示自由度。

## 1. 定位与范围

hctl 是 **seat-based multi-harness orchestration kernel**（席位制多 harness 协作编排内核）：多家 LLM harness（席位）共享一个 git 仓库协作开发，以 PR 为协作原子、git 事实为唯一真相源。hctl 是**叶子命令**，不是 process orchestrator / 进程编排器（D-13）：不开窗口、不起服务、不驱动 harness、不常驻——编排的是 git 事实与义务，不是 agent 进程。机械层零 LLM、零 REST 常规路径（GitHub API 仅 `gh pr create` / `gh pr merge`，D-01）。信任边界：护栏防失误与漂移，不防恶意；单人类账号模型，多真人/对抗场景显式不在当前范围（D-17/D-38）。

## 2. 术语表（业界词优先，D-20）

| 术语 | 义 | 出处 |
|---|---|---|
| seat（席位） | 协作身份 = harness×model，「脑子」；quorum 计票单位 | 自造（D-26） |
| 化身 | 席位的一个执行实例（machine×session），「椅子上的人」；纯溯源 | 自造（D-26） |
| memo lane | 叙事层：`memory/` 流水账，微 PR 即合、advisory、不占 slot | 自造（D-10） |
| assignment | 带 author/gates/merge 的冻结工作项 | 通用（D-07） |
| obligation | 可认领的义务：author / gate / merge | 通用（D-33） |
| claim / fencing token | 义务认领事件；其 OID 即栅栏令牌 | 分布式系统通用（D-34/D-35） |
| revision `{base, head}` | 判定所钉的精确版本对 | Gerrit patchset 语义（D-04） |
| attention set（欠账） | derive 出的当前待办集合 | Gerrit（D-06） |
| APPROVE / REQUEST_CHANGES | verdict 取值 | GitHub（D-40） |
| submit requirement 形态 quorum | 结构化 AST：seat/all_of/any_of/at_least | Gerrit（D-05） |
| reconcile / level-triggered | 每次调用全量推导；通知只唤醒 | Kubernetes（D-02） |
| event commit chain | per-seat 线性事件链 | Gerrit NoteDb meta-ref 形态（D-31） |
| JCS | RFC 8785 JSON Canonicalization Scheme | IETF（D-33） |

## 3. 事实模型：三层承载（D-01）

1. **事件层** `refs/coop/<seat>`：协调事实（CLAIM/VERDICT/CANCEL…），机器判定的唯一依据。
2. **叙事层** `memory/`：review 全文、架构、设计、讨论的家；内核零解析（opaque，D-10）；`memo/<seat>/<slug>` 分支、微 PR 即合。
3. **变更层** PR 本体：任务分支 `work/<seat>/<slug>`、head、merge 状态；GitHub 只当 merge 机器，页面零义务，PR 评论永不承载语义。

链接纪律：VERDICT 带 `report={commit,path,blob}` memo 指针，指针必须已可达于声明的 base 分支（先合 memo 后落 verdict，级②硬门，D-40）。

## 4. 事件层规范（D-30/D-31/D-32）

**形态**：一事件一 commit；tree 恰含单文件 `event.json`（schema/event.schema.json）；message 是人读摘要、不承载语义；parent 链给出席内全序（拓扑序，**禁止**以任何时钟重建顺序）。

**写路径**（全 plumbing，零 checkout）：

```
blob=$(git hash-object -w --stdin < event.json)
tree=$(printf '100644 blob %s\tevent.json\n' "$blob" | git mktree)
new=$(git commit-tree "$tree" ${tip:+-p "$tip"} -m "<summary>")
git update-ref refs/coop/<seat> "$new" "$tip"          # 本地 CAS（同机多 tab 互斥）
git push --force-with-lease=refs/coop/<seat>:"$tip" origin refs/coop/<seat>   # 远端 CAS；创建=空 expect
```

**禁止**裸 push（检测不了回卷）；**禁止** merge commit 入链；payload seat 必须等于 ref seat。

**观察分离**（D-32）：tracking ref `refs/hctl/remotes/<remote>/coop/<seat>` 仅由 fetch 更新、refspec 不带 force；`publishing ≠ tracking` ⇒ 存在未决发布，本地其他 writer 必须先恢复、禁止 append。恢复判据（D-34）：`merge-base --is-ancestor <pending> <remote-tip>` ⇒ delivered；分叉 ⇒ lost，先存 recovery ref 再 CAS 对齐。

**读入三分法**（D-39）：结构损坏 ⇒ `CORRUPT_CHAIN`（整链 quarantine、写动作 fail-closed）；结构合法语义不支持 ⇒ `UNSUPPORTED_FACTS`（保留、展示、写动作 fail-closed、提示升级）；已知非语义事件 ⇒ 校验后可忽略正文。观测不完整 ⇒ `INCOMPLETE_FACTS` ≠ 空集（D-39）。

**读路径**：batch plumbing（单次 `git log` / `cat-file --batch`），禁止 per-object exec（D-24）。增量读走 tip-keyed 可丢缓存（`.git/hctl/`）；链结构校验自上次已验 tip 增量进行；缓存可丢、算法不可含糊；不设计 compaction。

## 5. 身份：seat 与化身（D-26/D-27/D-29）

- quorum 按席计票；同席多化身是一个席位。
- `machine`（安装 UUID/别名）与 `session`（可 null）只进事件 provenance，**禁止**参与 quorum、ownership、freshness、命名空间、obligation id。
- 任务不贴化身名牌（slug 禁机器/化身后缀，级②字符集校验）；worktree 本地目录名自由。
- 两机=两 clone 交点只有 origin；同机多 tab 共享 `.git` 靠本地 CAS。

## 6. Obligation identity（D-33）

```
preimage = {
  "assignment": {"id","blob"} | {"assign_event"},   # 静态 | 动态(P2)
  "kind": "author" | "gate" | "merge",
  "target": <branch_slug>,
  "aspect": null | {"gate_id","gater_seat"}
}
obligation_id = "sha256:" + hex(sha256("hctl-obligation-v1\0" + JCS(preimage)))
```

- **head 永不进 id**；事件同存 preimage 与派生 id；全长禁截断。
- identity token 一律 ASCII 闭集 grammar（schema pattern）；唯一管线：parse I-JSON → schema 校验 → JCS → 重算比对；dup key / lone surrogate / 非 I-JSON ⇒ 结构损坏拒收。JCS 不做 Unicode normalization——非 ASCII 由 grammar 拒于 identity 之外。
- 静态 assignment 的 wire 事件保留 `source={commit,path,blob}`（审计反查，不进 hash）。
- assignment 变更 ⇒ blob 变 ⇒ 新 id ⇒ 旧 id 义务死（写动作级②拒）；无语义变更同样生效（合法 re-gate 风暴），status 亮 `assignment_revision moved`。

## 7. CLAIM（D-28/D-34/D-35/D-36）

**工作循环标准形**：`wait → derive 出义务 → claim → 干活 → verdict/交付`。一切远端副作用前必须持有 active claim；本地起草（`--draft-only`）自由——写锁在发布不在思考。

**获取 = 两阶段 CAS**：本地 `update-ref` CAS + 远端 lease push；**远端成功才算 acquired**。publishing ref 兼任 durable pending journal（§4 恢复流程）。

**stale 分岔**：gate/merge 类 claim 记 `revision_at_claim={base,head}`，任一分量前进 ⇒ claim stale 回池（= 显式 re-claim eligibility，非自动转移）；author 类记 `tip_at_claim`（初建可 null），跟 assignment+slug 生命周期、不因 head 前进丢 claim。

**Fencing（D-35）**：完成/转移类事件必须引用 active claim OID；derive 拒 stale-claim 产物；不设 epoch。

**re-claim 纪律**：**永不自动 re-claim**；`reclaim_of` 必须等于当前 active claim OID（push CAS 定赢家）；`reclaim_reason ∈ {overdue, operator}`。

**阻尼确定函数**：

```
escalated ⇒ frozen：仅三径解冻（用户 CANCEL(claim) / CANCEL(obligation) / 新 assignment revision）；进度不清红
否则，对相邻 accepted re-claim 分类：
  forward   ：锚点变且 is-ancestor(旧,新)（author 的 null→非 null 视同）⇒ streak 清（claimant progress）
  env-reset ：gate/merge 的 head 同 base 变 ⇒ streak 清，标 environment reset（不归功 claimant）
  其他      ：锚点同，或变而无祖先关系（改写/回卷/摆动）⇒ streak+1，另报 rewrite/rollback 观测位
streak==1 ⇒ 黄（期望 failover）；streak≥2 ⇒ 红 + escalated
```

窗口 =（assignment revision, obligation）自上次 completion/CANCEL 起；只计远端 accepted CLAIM；derive 先跑 ref integrity 再进分类。overdue 与 escalated 都是**推导事实**（无定时器、无事件类型，D-06）。

**转手（D-36）**：同席换化身零协议事件（claim 是席级资产）。跨席：P1 走用户 CANCEL + 新 static assignment revision；HANDOFF 事件 DEFERRED（打开条件=单 CAS 仲裁域 + corpus）。

## 8. Gate 与 verdict（D-05/D-40/D-41）

**Gate 三轴**：mode（required/advisory/observe）× quorum（AST 闭集）× on_timeout（escalate / 预声明 deputy / proceed——required+proceed 非法）。threshold 是展示给 gater 的裁量参数，内核不读 severity。

**法律**：freshness = exact `{base,head}`；gater ≠ author；required 禁 silent skip；同席只计一票。

**Verdict**：必须引用 active claim OID + exact revision + report 指针（§3 硬门）+ `scope` + `completeness`。per (gater, obligation, revision) **latest-wins**。required quorum 只计四条件齐备者：exact revision、COMPLETE、APPROVE、scope 覆盖要求的评审面。

**评审协议（D-41）**：首轮 scope=full；后续轮 = fix_verification（findings 清单销账）+ delta（blob 两端），范围由机械规则定义、单调收缩。INCOMPLETE 必须明示未覆盖面；partial-silent 禁止。晚期发现走逸出程序：标 late-finding+原因；merge 前=latest-wins verdict（REQUEST_CHANGES 失绿阻断）；merge 后=NOTE+人裁修复 assignment；不重启封口面。

## 9. Merge（D-37）

- **串行化**：唯一 `merge_coordinator` 席 + repo 级 capacity=1；全部 merge claim 落该席链 = 单 CAS 域 = capacity 谓词可判。merge obligations 只可派给 coordinator（loader 拒）。
- **前置**：claim(kind=merge) 要求 derive 于同快照见 required quorum green 且无 active merge claim（级②）。
- **流程**：claim → fetch 全事实 re-check → `gh pr merge --squash --match-head-commit <head> --body-file <receipt>` → fetch main → 验 squash 单 parent == `Hctl-Base` → 验 receipt → 关 claim。post-verify 不符 = 完整性事故（不假装事前拒绝）。
- **Receipt 封闭字段**：`Hctl-Version: 1` / `Hctl-Assignment` / `Hctl-Obligation` / `Hctl-PR` / `Hctl-Base` / `Hctl-Head` / `Hctl-Merge-Claim` / `Hctl-Method: squash` / `Hctl-Fact-Tip: <seat>=<oid>`（可重复；无时钟重放 quorum 事实集）。
- **config cutover 两时点**：claim/pre-merge 按 current main 校验 `coordinator_config={commit,path,blob}`（固定单文件 = `.hctl/seats.toml`）与 actor=该 revision 的 coordinator；**receipt 按 `Hctl-Base` 所见 config 校验**；merge 后新 claim 按新 revision。派生态 `merged_pending_receipt`：旧 claim 不占新 slot、仅 receipt 清审计债。rebind 经旧 slot 落地；coordinator 失能走人裁 CANCEL（P2/adopters）。
- 严格 base 松弛：P3 语料前禁旋钮；放松必须 schema_version + DECISIONS supersede。
- 非 squash ⇒ `UNJUDGEABLE_MERGE`；绕过 hctl ⇒ `UNRECORDED_MERGE` 审计债（补记 forward-only、要素齐全前红灯、禁 NOTE 偷渡）。

## 10. 授权与信任边界（D-38）

| 不变量 | 等级 | 执行 |
|---|---|---|
| ASSIGN/CANCEL 确由人发起 | ④ 信任前提 | `authority:{kind:"user"}` 是声明非证明；审计靠 provenance+链序 |
| begin/verdict/merge 回溯冻结 assignment | ② | 写动作查 assignment 存在性 |
| escalate 是席位唯一自主出口 | ②/④ | 永不隐式创建 ASSIGN |
| `.hctl/**` 变更须 required gate ≥1 非 author 席 | ②（activation 后） | `enforcement=active` 起 loader/writer 拒；doctor 硬检 |

activation 是可重放事实：`enforcement` 字段由 gated PR 置 `active`（P1 交付即其 gate）；此前明标 bootstrap trust、自开自合例外生效，随 activation 自动终止。

## 11. 并发物理层（D-08/D-09/D-14）

- 单写者命名空间三粒度：分支（一任务）/文件（一实例）/ref（一席一链）。
- 一 seat 一常驻 worktree；checkout 权属前台 session；后台零 checkout（对象库直读 / `scratch` 一次性阅览 worktree）；旁路发布走 plumbing。
- 分支双 lane：`work/<seat>/<slug>`（author-class，合并即死）与 `memo/<seat>/<slug>`（memo lane）；pattern 跨席跨 lane 两两不相交（doctor）。机器名退出远端命名空间。

## 12. 监听与等待（D-18）

监听面 = ref 平面（一条 `ls-remote` 全量覆盖：main、work/memo 分支、refs/pull/*/head、refs/coop/*），不是文件系统。轮询只存在于 `wait` 阻塞期间；无人等待时系统里没有任何东西在跑。快照缓存在 `.git/hctl/` 可丢。间隔可配有下限保底；无变化不打输出（静默续等），仅 attention 变化或 timeout 返回。P2：clone 级合并轮询（同 clone 单 poll leader、快照共享、退避+jitter）。

## 13. 驻留模式（D-19）

驻班（fire 一次进入 wait↔work 自循环，消化 derive 名下义务）/ 待命（需点火）。授权边界：无 assignment 不动作，意外一律 escalate。班次有界 = token 卫生建议非法律；永续双 fire 对正确性无害（level-triggered + CLAIM 互斥 + 幂等恢复）。人的角色终局：**排班、点火、裁决**。

## 14. Session 实践与 context 经济学（D-15/D-16，推荐实践）

一任务/一 gate 指派（含完整 re-gate 循环）/一咨询话题一 session；并行度放 session 内 sub-agent；session 是 cache、memory 是数据库。知识金字塔：门面（AGENTS，1-2 页硬预算）→ 现状（status/台账）→ 结论（DECISIONS）→ 正文（METHOD/共享文档）→ 考古（memory，指针驱动）；知识持续向上迁移，收口（closeout）是标准节点类型；派工 brief 带指针不带语料。

## 15. 交互模型与命令面（D-13/D-43）

输出三形态：人读表格 / `--json` / exit code；自描述输出必须含 obligation id 与 holder（弱模型跟着输出走）。P1 命令面：

```
hctl doctor                      # preflight：refspec、hooks、席位身份、lane 不相交、gh auth、enforcement
hctl status [--json]             # 即全量 reconcile；含 overdue/escalated 黄红灯与下一步动作
hctl claim <obligation> [--reclaim <claim-oid>]
hctl begin <assignment> [--adopt <branch>|--draft-only]   # 默认 formal：先 claim 后建分支；重跑复用 active claim
hctl verdict <gate-obligation>
hctl merge <pr> [--check]
hctl wait                        # 前台阻塞至 attention 变化或 timeout
```

helpers：`scratch` / `memo` / `trailer`（模型+effort 取会话实况，取不到即拒绝，D-23）；`run` 属 adapters 不属内核。`hctl help` 是 canonical 文档（与二进制同版本）；AGENTS 片段 ≤10 行；SKILL 薄壳可选不装正文（D-22）。

## 16. 执行等级表（D-17）

| 不变量 | 级 | 机械措施 |
|---|---|---|
| 同席同刻单写者（链） | ①② | update-ref CAS + lease push |
| 事件链单 parent/schema 合法/seat 对 ref | ①② | writer 构造；reader quarantine |
| 相关远端 refs 已完整 fetch | ② | 写动作前比对 advertised/fetched tips |
| verdict 引用 active claim + exact revision | ② | event validator + derive |
| memo 指针已合入且 blob 匹配 | ② | verdict writer + derive 双检 |
| gater≠author；同席一票 | ② | evaluator 集合语义 |
| required 禁 silent skip；required+proceed 非法 | ①② | schema 排除组合 |
| slot/author 并发 | ② | begin 查 slot；pr create 前置 claim |
| slug 无化身后缀 | ② | begin 字符集校验 |
| merge 有结构化 receipt | ② | wrapper 控制 squash body |
| trailer/日期/ref 名/head 由工具生成 | ② | LLM 只做选择题 |
| assignment fire 后不原地变义 | ①② | blob revision 引用 |
| coop 链不倒退/不消失 | ③ | doctor 集合比对 + recovery ref + 可选第二 remote |
| 无 receipt 的 main commit | ③ | `UNRECORDED_MERGE` 审计 |
| 橡皮图章 gate | ③ | 短周转重复 APPROVE 黄灯 |
| hooks 在位 | ③ | doctor（hooks 可 `--no-verify`，不冒充法律） |
| memo 质量、知识上迁 | ④ | 点名 + closeout 兜底 |
| ASSIGN/CANCEL 确由人发起 | ④ | 信任前提（§10） |

## 17. 失败行为（D-39 汇总）

`INCOMPLETE_FACTS`（观测不完整：读=不可判定，写=拒）；`CORRUPT_CHAIN`（结构坏：quarantine+fail-closed）；`UNSUPPORTED_FACTS`（未知语义：fail-closed+提示升级）；`AMBIGUOUS_ASSIGNMENT`（同 id 冲突 payload：fail-closed）；`UNJUDGEABLE_MERGE`（非 squash）；`UNRECORDED_MERGE`（绕过 hctl：审计债，forward-only 补记）；coop 回卷（先存 recovery ref 再红灯，不自动跟随/重建）；merge post-verify 不符（完整性事故上报）。通则：**宁停摆不带病合入；宁红灯不静默猜**。

## 18. Phase（D-44）

- **P0**（本批）：METHOD / DECISIONS / 三 schema / corpus BACKLOG。
- **P1** 单竖切：static assignment → 同席 CLAIM → VERDICT → quorum → coordinator merge slot → squash receipt；事件 CLAIM/VERDICT/CANCEL；doctor/status/wait 基础形；activation 交付。
- **P2**：动态 ASSIGN、跨席 HANDOFF（待仲裁域）、NOTE/presence、轮询合并、memo plumbing、scratch、standby、repair、consult。
- **P3**：abacistopia 影子 round；全语料过绿删旧件；严格 base 松弛再议。
- 不做：git notes、webhook/daemon、SQLite、表达式语言、author_concurrency>1、chain compaction、server-side enforcement。

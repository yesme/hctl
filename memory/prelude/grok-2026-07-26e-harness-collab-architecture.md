# Grok — Harness 协作架构方法论评价（独立部署向）

> 日期：2026-07-26  
> Worker：`grok-mac`  
> 性质：设计讨论评价落档；**零施工**  
> 背景：用户要清理「script / policy / guideline 满天飞」，设计**独立于 CoAgNET、可单独部署**的 harness 协作方法论；round-01 负面案例作反面教材  
> 基线：`origin/main` @ 评价写作时含 #868–#873 邻家讨论（本档为 Grok 独立综合，不复述他家全文）

## Topics

- 多 harness（Codex/Claude/Grok/GLM/Kimi/AGY）共享 repo、分 branch 协作内核
- 写串行 vs 读并行；通知/订阅可靠性；gater 代数；DAG vs ad-hoc
- 业界先例；与现有 `scripts/round` / ROUND-PROTOCOL 的关系

## Outcomes（Grok 独立判断）

### 1. 总判断

要的不是再叠一层文档，而是：

> **事实在 git/PR，调度是 level-triggered 的机械推导，人只 fire 与裁决；LLM 只在 author/gate 节点内部干活。**

当前混乱的根因是**同一行为多个真相源**（protocol / 启动文件 / shell / title 约定 / 口头 prompt），不是「规则还不够多」。

收束形态：**一份规范 + 一份 seat/policy 配置 + 一个机械程序 + 可丢弃本地缓存。**

### 2. 业界：无现成产品，有可抄零件

| 领域 | 先例 | 可抄 |
|---|---|---|
| 合并串行 | Bors / merge queue / **Zuul** | 写落地串行、静态 Depends-On |
| 评审代数 | **Gerrit** labels + submit requirements | gater 属性与 quorum 布尔式 |
| 协调模型 | **K8s controller** | level-triggered reconcile：醒了不算账，全量 `derive` |
| Review 入 git | Gerrit refs、git-appraise、Radicle | 跨机可搬、不绑 inode |
| 多 agent 框架 | LangGraph / AutoGen / CrewAI | **不对路**（进程内 + LLM 路由） |

实践侧常见 worktree 隔离 + 人粘贴 prompt，**缺可靠订阅层**——正是要补的洞。

### 3. 三层拆开（治乱）

| 层 | 装什么 |
|---|---|
| **Kernel**（宜独立 repo） | wake / derive / verdict；schema；采用指南 |
| **Seat profile**（工作 repo） | 能力矩阵、token、路径白名单、AGY 围栏 |
| **Work signals**（永远在工作 repo） | PR、结构化 gate 事件、可选 `refs/coop/*` |

能力边界（精密件 / 愿景件 / 对抗副手 / AGY 只讨论）是 **profile 数据**，不进内核状态机。

### 4. 并发：写锁在发布，不在思考

| 工作 | 并发 |
|---|---|
| Author（改正式内容 + 推进 PR head） | **每 harness 最多 1** |
| Review/gate（只读 + 结构化评论） | 任意并行 |
| Memo | 并行，**默认不进被审 author PR 分支** |

**纠正用户表述一处：** 不同 memo 文件 ≠ 可安全并进同一 PR——仍会移动 head，导致钉 head 的 gate 全失效。Reviewer memo 走自家 harness 分支或独立 memory PR。

**active PR 等待（pros/cons）：**

- 严格「每 harness 同时 1 个 open author PR」：基线唯一、少 rebase；代价是空转与队头阻塞。  
- **推荐组合，忌隐式 sleep 队列：**  
  1. 发布串行（slot free 才能 `pr create`）；  
  2. 起草流水线（占用时仍可本地写下一件，不可开第二 PR）；  
  3. 锁在 harness×branch，不在 tab；  
  4. derive 显示 `blocked: author_slot_held`，人不靠隐式 wait。

复杂度主要来自**隐式等待**，不是来自「一次一个 open PR」。

### 5. 通知：宽进严出；严在结构，不在 LLM「理解内容」

```
wake（便宜）→ derive（纯函数重扫）→ 欠账列表
```

- **醒：** 本地 `for-each-ref`；跨机 `fetch` + `ls-remote`（transport）。已知 active PR 可补 `gh` ETag（304 近零成本）。  
- **滤：** 结构化字段（writer/kind/pr/head/result）；**title 永不作判据**。  
- 用户「按内容理解」应落实为**结构化字段**，再引 LLM 路由会把漏订变成曲解。

**事实源（Grok 裁定倾向）：**

- **C：git 结构化 ref（或等价）为权威 + comment 尽力镜像**  
- 纯 comment 真相（A）在单账号 + REST 下已被 round-01 打脸  
- 「不 access API 也能 handle comments」⇔ **comment 不是唯一真相**

### 6. Gater 四轴 + 小布尔 quorum（Gerrit 形）

| 轴 | 取值 |
|---|---|
| blocking | blocking / advisory |
| verdict | explicit / lazy |
| depth | full / p0-only |
| on_timeout | wait / escalate→deputy / drop-if-advisory |

Quorum 例：`primary AND (deputy OR reserve)`、`n_of(2, …)`。  
Grok 默认常为 **advisory + full**，除非 edge 写死 blocking。

**硬规则：blocking gater 禁止 silent skip**（沉默=同意 × 订阅漏 = 最危）。  
**head 变 → 该 head 全部 verdict 作废**（必须保留）。

### 7. DAG 与 ad-hoc：统一 edge schema

- DAG = 冻结 edge 集合；可反复 re-gate；**禁止运行时长依赖**。  
- Ad-hoc = 同一 schema 的单条授权。  
Worker 只认 edge，不认「这是图还是口头」。

### 8. 活性：从权威降为 UX（round-01 最大教训）

| 案例 | 含义 |
|---|---|
| 探针指错目录 | 活性传感器不可靠 → 不进正确性 |
| `grok-mac-mac` | 工具生成 id 必须校验 |
| 做完停心跳 → 假死 interrupt | 完成≠离场；或取消毒全场 |
| 漏 comment | level-triggered + 结构字段 |
| 一家死拖死全员 | 禁默认全局 poison |

**建议：** 默认取消「假死即全局 interrupt」；watchdog 只报告；人显式 `cancel generation` 与误判毒杀分开。  
心跳若保留：只服务 dashboard，**不写入正确性路径**。  
`scripts/round/` 可冻结服务到迁移完成；**不要把「活性=权威」搬进新方法论 repo**。  
generation 隔离、exact-head merge 等是好零件，可与「活性降级」并存。

### 9. 角色（profile）

与用户划分一致：Codex/GLM 精密件；Claude/Kimi 愿景与 plan；Grok 对抗+万金油 co-gate；AGY 只讨论+自家 memo（pre-push 白名单）。  
形态：一 tab 一 harness；**author 槽 1 + 读槽 N**。

### 10. 收敛与清理

最小工具：`wake` + `derive` + `verdict` + `policy` + dashboard。  
方法论宜**另开 repo**（工具家）；信号永远在工作 repo（信号家）。  
本项目第一采用者：删重复 guideline，只留「采用内核 + seat profile」。

### 11. 待用户拍板（五条）

1. 事实源：git 权威 + comment 镜像（C）？  
2. 写锁：每 harness 同时最多 1 open author PR + 起草流水线？  
3. blocking 超时：禁 silent skip，只许 escalate/deputy？  
4. 全局 interrupt：退役为 advisory + 显式 cancel？  
5. 是否另开方法论 repo（Grok **同意该开**）？

## Process Notes

- 评价在 #868 合入后、#869–#873 邻家讨论已上 main 的语境下写成；立场独立，与 Claude level-triggered / Codex memo-head 风险 / Kimi Zuul-Gerrit 零件说 **一致处不重复论证，分歧处写明**。  
- 与 Claude 最大共识：level-triggered、结构化信号、blocking 禁 silent skip、工具/信号分家。  
- 与「active PR 隐式 wait」保持距离：主张显式 slot + derive 可见 blocked。

## Open Threads

- 五拍板项未裁 → 不写实现 PR。  
- 若 go：独立 repo 骨架 + 一页 kernel 设计（仍不搬 `scripts/round` 病根）。

## Commits 本次会话

- 本 memory 单文件 PR。

## Decisions Reference Quick Card

| 项 | Grok 立场 |
|---|---|
| 内核 | level-triggered wake→derive；LLM 不进路由 |
| 事实源 | git 结构化权威 + comment 镜像 |
| 写锁 | 每 harness 1 open author PR + 起草流水线；禁隐式 wait |
| Memo | 不进被审 PR head |
| Gater | 四轴 + 布尔 quorum；blocking 禁 silent skip |
| 活性 | 降为 UX；取消默认全局 poison interrupt |
| 部署 | 另开方法论 repo；信号留在工作 repo |

# Prelude — hctl 的来时路（冻结导入）

本目录是 hctl 立项之前、各 harness 在 abacistopia 仓就「harness 协作架构」独立评估的原始 memo，自 `github.com/yesme/abacistopia` @ `7184819` 原样拷贝，**只读冻结，不再编辑**。综合与定案见 [../claude-2026-07-27.md](../claude-2026-07-27.md)。

| 文件 | 席位 | 原 PR | 要点 |
|---|---|---|---|
| claude-2026-07-26j.md | claude / Fable-5 | #869 + #871（追记） | level-triggered 内核、coop refs、gater 代数、活性降级；另开 repo 判是 |
| codex-2026-07-26g.md | codex / GPT-5.6 Sol | #870 | Harness Collaboration Kernel 全案；**`hctl` 之名出处** |
| glm-2026-07-26f.md | opencode / GLM-5.2 | #872 | Prow-lite 判断、业界对照表、三真难点 |
| kimi-2026-07-26g.md | kimi / Kimi-K3 | #873 | 业界零件说（Zuul/Gerrit/Temporal）、四待定点 pros/cons、round-01 反面案例五硬需求 |
| grok-2026-07-26e-harness-collab-architecture.md | grok / Grok-4.5 | #874 | 独立综合；事实源倾向 git 权威；「写锁在发布不在思考」 |
| agy-2026-07-26.md | agy / Gemini-3.6 | — | 旧 ROUND-PROTOCOL v1 背书（旧系统对照：静态 DAG 即全部授权、fail-closed 哲学） |

勘误：kimi 篇称「Grok #872」系误记——#872 为 GLM 篇，grok 篇为 #874 补交。

# hctl — 席位接入片段（bootstrap 版）

本仓是 harness collaboration kernel（方法论 + 内核）的开发仓，同时是自己方法论的零号采用者。

## 加载协定（谁读什么、按什么顺序）

1. **门牌文件**：每家 harness 自动加载的文件名由其实现决定，本仓不做主——Claude Code 读 `CLAUDE.md`，Codex/OpenCode 系直读本文件（`AGENTS.md`），其余各家门牌名逐家实测后补（结论记 `.hctl/seats.toml` 注记）。门牌文件只放一行指针，指向本文件。
2. **共享正文**：即本文件，内容 seat-agnostic——身份来自 worktree（目录后缀），席位参数在 `.hctl/seats.toml`，prose 不写任何一家的身份。
3. **个席补充**：仅当某席确有特例（如 AGY 的 memo-only 围栏说明）才存在 `agents/<seat>.md`，读完本文件后再读它；无特例不建文件——避免多份文件转述同一规则的漂移病（abacistopia 六份启动文件之鉴）。

- **你的 seat = 所在 worktree 目录后缀**（`hctl-claude` → seat `claude`）。席位声明见 `.hctl/seats.toml`。
- **入口（知识金字塔序，D-16）**：结论层 [DECISIONS.md](DECISIONS.md)（裁定台账 D-01..D-45，终裁 2026-07-27）→ 规范层 [METHOD.md](METHOD.md)。立项与三席评审全史在 `memory/`（考古层，指针驱动、默认不通读）；前史见 `memory/prelude/`（冻结只读）。
- **写规则（bootstrap 期，enforcement=bootstrap）**：memo 走 `memo/<你的seat>/<slug>` 分支、只写 `memory/<你的seat>-*.md`；author 任务走 `work/<你的seat>/<slug>`（D-08 双 lane）；不动别家文件；canonical（`~/workspace/hctl`）只读不作业；main 只经 PR（无 round 时自开自合、一律 squash——此例外随 P1 activation 终止，D-38）。
- **commit trailer** 必带模型名与 effort（`Co-Authored-By`），取会话实况，不猜不降级。
- **memo 文件名日期取系统时钟**（`date +%F`），同日多篇加 b/c 后缀，写前查重名。
- hctl CLI 尚不存在（P1 交付）；当前协调 = 本文件 + memory + 用户点火。

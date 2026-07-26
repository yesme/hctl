# hctl — 席位接入片段（bootstrap 版）

本仓是 harness collaboration kernel（方法论 + 内核）的开发仓，同时是自己方法论的零号采用者。

- **你的 seat = 所在 worktree 目录后缀**（`hctl-claude` → seat `claude`）。席位声明见 `.hctl/seats.toml`。
- **入口**：先读 `memory/claude-2026-07-27.md`（立项设计全记录 + 三堂会审审题）。前史见 `memory/prelude/`（冻结导入，只读）。
- **写规则（bootstrap 期）**：只写 `memory/<你的seat>-*.md` 与自己的任务分支；不动别家文件；canonical（`~/workspace/hctl`）只读不作业；main 只经 PR（无 round 时自开自合、一律 squash）。
- **commit trailer** 必带模型名与 effort（`Co-Authored-By`），取会话实况，不猜不降级。
- **memo 文件名日期取系统时钟**（`date +%F`），同日多篇加 b/c 后缀，写前查重名。
- hctl CLI 尚不存在（P1 交付）；当前协调 = 本文件 + memory + 用户点火。

# tests/corpus — 语料测试

语料断言**护栏与 git 物理**，不断言 LLM 行为（D-17）。每案自带沙盘（mktemp 一次性 repo），零网络；身份与时钟冻结、可重复；退出码即判定（0=PASS，1=FAIL，77=SKIP）。案头注明对应定案与出处。

**规范权威永远是 Go 内核**（D-24/27m；codex-27k §2.1）。`lib/*.py` 是**窄 helper / 差分探针**——可生成 payload 与 JCS/obligation id；**不得**以 Python 完整 known-event union 宣称镜像 `protocol` 或把 Python `OK` 当 gate 证据。known CLAIM/VERDICT/CANCEL 的 OK/CORRUPT 必须以真实 `hctl` 路径断言。

## 布局

| 路径 | 内容 |
|---|---|
| `concurrency/` | 基质选型证据两案（#0a/#0b，27c） |
| `cases/` | BACKLOG #1–#28 可执行案 |
| `lib/` | 共享沙盘 `common.sh` / `hctl_fixture.sh`；JCS / obligation / derive / receipt 差分 oracle |
| `run.sh` | **冻结 manifest** runner：缺案/多案失败；strict 下 SKIP 失败 |
| `BACKLOG.md` | 清单规格与执行注记 |

## 运行

```bash
# 本地探索（允许 wire 案在无二进制时 SKIP）
bash tests/corpus/run.sh

# 门禁 / 接线验收：必须提供真实 hctl；manifest 完整；零 SKIP
go build -o /tmp/hctl ./cmd/hctl
HCTL=/tmp/hctl CORPUS_REQUIRE_HCTL=1 bash tests/corpus/run.sh
# 等价：
HCTL=/tmp/hctl CORPUS_STRICT=1 bash tests/corpus/run.sh
```

单案：

```bash
HCTL=/tmp/hctl bash tests/corpus/cases/09-begin-retry-reuse-claim.sh
```

`HCTL=/usr/bin/true` 在 require 模式下会红：wire 案校验 help/version/status 行为，不是空钩子。

## 模式

- **pure**：差分 oracle / schema 纪律（不单独代表 kernel PASS）。
- **hybrid**：git 沙盘物理 + 差分 oracle。
- **deferred-p2**：P1 必须报 unsupported 的边界（如跨席 HANDOFF）。
- **hctl-wire**：调用真实 `hctl` CLI；未提供时 SKIP(77)；`CORPUS_REQUIRE_HCTL=1` 下 SKIP=FAIL。

## Runner 纪律（P1-01 修复）

- 期望集合冻结为 concurrency 2 + cases #01–#28；**不**靠 glob「发现什么算什么」。
- 缺案 / 意外多案 → FAIL。
- `CORPUS_STRICT=1` 或 `CORPUS_REQUIRE_HCTL=1`：任一 SKIP → FAIL；且 HCTL 必须可执行。

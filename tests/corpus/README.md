# tests/corpus — 语料测试

语料断言**护栏与 git 物理**，不断言 LLM 行为（D-17）。每案自带沙盘（mktemp 一次性 repo），零网络；身份与时钟冻结、可重复；退出码即判定（0=PASS，1=FAIL，77=SKIP）。案头注明对应定案与出处。

## 布局

| 路径 | 内容 |
|---|---|
| `concurrency/` | 基质选型证据两案（#0a/#0b，27c） |
| `cases/` | BACKLOG #1–#28 可执行案 |
| `lib/` | 共享沙盘 `common.sh`；JCS / obligation id / derive 纯谓词 |
| `run.sh` | 薄壳 runner：逐案跑、汇总 pass/fail/skip |
| `BACKLOG.md` | 清单规格与执行注记 |

## 运行

```bash
bash tests/corpus/run.sh
# 单案：
bash tests/corpus/cases/28-jcs-vectors.sh
bash tests/corpus/concurrency/chain-claim-mutex.sh
```

可选：`HCTL=/path/to/hctl` 或 `PATH` 含 `hctl` 时，标为 `hctl-wire` 的案可追加 CLI 实装断言（当前 #1–28 以 **pure/hybrid 规格护栏** 为主，与 p1-kernel **并行起草**；kernel 落 main 后接线收口——见 assignment `p1-corpus`）。

## 模式

- **pure**：不依赖 git 多 clone 的判定函数 / schema 纪律（可独立于 hctl）。
- **hybrid**：git 沙盘物理 + 纯谓词（CAS、lease、ancestry、双 ref 反例等）。
- **deferred-p2**：P1 必须报 unsupported 的边界（如跨席 HANDOFF）。
- **hctl-wire**：需二进制；未提供时 SKIP(77)，不记 fail。

## 与 p1-kernel

本 assignment 的 quorum green 预期发生在：语料 runner 全绿 **且** 需 CLI 的案在 kernel 入 main 后接线仍绿。`needs=[]` 并行是设计意图（27j）。

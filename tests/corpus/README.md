# tests/corpus — 语料测试

语料断言**护栏与 git 物理**，不断言 LLM 行为（K-17：语料测试测的是护栏）。每案自带沙盘（mktemp 一次性 repo），零网络、零外部依赖；身份与时钟冻结、可重复；退出码即判定。案头注明对应定案与出处 memo。

## concurrency/

事件层基质选型的可执行证据（[memory/claude-2026-07-27c.md](../../memory/claude-2026-07-27c.md)，K-21/K-28/K-30）：

- `chain-claim-mutex.sh` — 正面：event commit chain 上 CLAIM 双层互斥成立（`update-ref` CAS 挡同机双 tab；`push --force-with-lease` 挡跨机竞争；远端回卷响亮失败而非被裸 push 无声修复）。
- `notes-union-antimutex.sh` — 反面展品：git notes 官方合并路径（`cat_sort_uniq`）为 union，双 CLAIM 并存——弃 notes 的失效模式证据。

运行：`bash tests/corpus/concurrency/<案>.sh`。P1 起语料 runner 收编入 Python 内核测试（K-24）；语料本体保持最小 git 命令序列。

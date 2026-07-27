#!/usr/bin/env bash
# Case #13: assignment blob 无语义变更 ⇒ status 亮 assignment_revision moved
# D: D-33 | Source: grok-27b | Mechanical: status signal on blob change
# Mode: hybrid (differential oracle on blob identity)
set -euo pipefail
CORPUS_CASE_ID="13-assignment-revision-moved"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

corpus_sandbox
git init -q
echo 'id = "demo"' > a.toml
git add a.toml
git -c user.email=c@c -c user.name=c commit -q -m a1
b1=$(git rev-parse HEAD:a.toml)
printf 'id = "demo"\n\n' > a.toml
git add a.toml
git -c user.email=c@c -c user.name=c commit -q -m a2
b2=$(git rev-parse HEAD:a.toml)
[ "$b1" != "$b2" ] || corpus_fail "blob should change"

moved=$(python3 -c "import derive_rules; print(derive_rules.assignment_revision_moved('$b1','$b2'))")
[ "$moved" = "True" ] || corpus_fail "blob change => assignment_revision moved"
same=$(python3 -c "import derive_rules; print(derive_rules.assignment_revision_moved('$b2','$b2'))")
[ "$same" = "False" ] || corpus_fail "identical blob => not moved"

corpus_pass

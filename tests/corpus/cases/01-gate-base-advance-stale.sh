#!/usr/bin/env bash
# Case #1: gate head 不变、base 前进 ⇒ claim 与 verdict 双 stale
# D: D-04/D-34 | Source: codex-27b | Mechanical: revision_at_claim exact {base,head}
# Mode: pure  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="01-gate-base-advance-stale"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

corpus_sandbox
git init -q
empty=$(git mktree </dev/null)
base1=$(git commit-tree "$empty" -m base1)
base2=$(git commit-tree "$empty" -p "$base1" -m base2)
head0=$(git commit-tree "$empty" -m head0)
stale=$(python3 -c "import derive_rules; print(derive_rules.claim_stale_gate({'base':'$base1','head':'$head0'},{'base':'$base2','head':'$head0'}))")
fresh=$(python3 -c "import derive_rules; print(derive_rules.claim_stale_gate({'base':'$base1','head':'$head0'},{'base':'$base1','head':'$head0'}))")
head_move=$(python3 -c "import derive_rules; print(derive_rules.claim_stale_gate({'base':'$base1','head':'$head0'},{'base':'$base1','head':'$base2'}))")
[ "$stale" = "True" ] || corpus_fail "base advance must stale claim+verdict"
[ "$fresh" = "False" ] || corpus_fail "exact match fresh"
[ "$head_move" = "True" ] || corpus_fail "head move must stale"
corpus_pass

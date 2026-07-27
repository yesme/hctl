#!/usr/bin/env bash
# Case #27: H1→H2→H1 摆动不清 streak；base-only env-reset；ancestry 前进清；integrity 先
# D: D-34 | Source: codex-27d | Mechanical: forward progress function F4'
# Mode: hybrid (differential oracle + git ancestry)
set -euo pipefail
CORPUS_CASE_ID="27-streak-forward-env-reset"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

corpus_sandbox
git init -q
empty=$(git mktree </dev/null)
H1=$(git commit-tree "$empty" -m H1)
H2=$(git commit-tree "$empty" -p "$H1" -m H2)
c1=$(corpus_py derive_rules.py progress gate . "{\"base\":\"$H1\",\"head\":\"$H1\"}" "{\"base\":\"$H1\",\"head\":\"$H2\"}")
c2=$(corpus_py derive_rules.py progress gate . "{\"base\":\"$H1\",\"head\":\"$H2\"}" "{\"base\":\"$H1\",\"head\":\"$H1\"}")
[ "$c1" = "forward" ] || corpus_fail "H1->H2 forward got $c1"
[ "$c2" = "non_forward" ] || corpus_fail "H2->H1 non_forward got $c2"
B1=$H1; B2=$H2; Hd=$H1
c3=$(corpus_py derive_rules.py progress gate . "{\"base\":\"$B1\",\"head\":\"$Hd\"}" "{\"base\":\"$B2\",\"head\":\"$Hd\"}")
[ "$c3" = "environment_reset" ] || corpus_fail "base-only env reset got $c3"
out=$(corpus_py derive_rules.py streak '["forward","non_forward","non_forward"]')
echo "$out" | python3 -c 'import json,sys; s=json.load(sys.stdin); assert s["escalated"] and s["color"]=="red"'
out2=$(corpus_py derive_rules.py streak '["non_forward","environment_reset","non_forward"]')
echo "$out2" | python3 -c 'import json,sys; s=json.load(sys.stdin); assert s["color"]=="yellow" and s["streak"]==1'

python3 - <<'PY'
import derive_rules
assert derive_rules.integrity_before_progress(False) == "CORRUPT_CHAIN"
assert derive_rules.integrity_before_progress(True) is None
s = derive_rules.streak_after_reclaims(["forward"])
assert s["color"] == "green" and not s["escalated"]
print("ok")
PY
corpus_pass

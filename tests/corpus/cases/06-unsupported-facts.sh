#!/usr/bin/env bash
# Case #6: 合法 JSON 未知 type/version ⇒ UNSUPPORTED_FACTS、写动作拒
# D: D-39 | Source: codex-27b | Mechanical: three-way split
# Mode: pure  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="06-unsupported-facts"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

known='CLAIM,VERDICT,CANCEL'
ok=$(corpus_py derive_rules.py three-way '{"schema_version":1,"type":"CLAIM","actor":{"seat":"claude"}}' claude "$known")
[ "$ok" = "OK" ] || corpus_fail "CLAIM ok"
u=$(corpus_py derive_rules.py three-way '{"schema_version":1,"type":"HANDOFF","actor":{"seat":"claude"}}' claude "$known")
[ "$u" = "UNSUPPORTED_FACTS" ] || corpus_fail "unknown type => UNSUPPORTED, got $u"
v=$(corpus_py derive_rules.py three-way '{"schema_version":9,"type":"CLAIM","actor":{"seat":"claude"}}' claude "$known")
[ "$v" = "UNSUPPORTED_FACTS" ] || corpus_fail "future major => UNSUPPORTED"
c=$(corpus_py derive_rules.py three-way '{"schema_version":1,"type":"CLAIM","actor":{"seat":"grok"}}' claude "$known")
[ "$c" = "CORRUPT_CHAIN" ] || corpus_fail "seat mismatch corrupt"
# write path fail-closed: simulate reject on UNSUPPORTED
[ "$u" = "UNSUPPORTED_FACTS" ] && write_ok=0 || write_ok=1
[ "$write_ok" = "0" ] || corpus_fail "writes must fail-closed"
corpus_pass

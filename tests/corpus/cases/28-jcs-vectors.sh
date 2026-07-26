#!/usr/bin/env bash
# Case #28: JCS 向量：key 序/空白/escape 同 id；非 ASCII token 拒；dup key 拒；aspect 字段序同 id
# D: D-33 | Source: codex-27d | Mechanical: RFC 8785 + ASCII grammar
# Mode: pure  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="28-jcs-vectors"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

python3 - <<'PY'
import json
from jcs import jcs, parse_i_json
import obligation

pre = {
  "assignment": {"id": "demo", "blob": "0123456789abcdef0123456789abcdef01234567"},
  "kind": "author",
  "target": "p1-corpus",
  "aspect": None,
}
assert obligation.obligation_id(pre) == obligation.obligation_id({
  "aspect": None,
  "target": "p1-corpus",
  "kind": "author",
  "assignment": {"blob": "0123456789abcdef0123456789abcdef01234567", "id": "demo"},
})

# escape equivalence for solidus optional — strings equal after parse
a = parse_i_json('{"x":"a"}')
b = parse_i_json('{"x":"\\u0061"}')
assert jcs(a) == jcs(b)

# duplicate keys rejected
try:
    parse_i_json('{"a":1,"a":2}')
    raise SystemExit('dup should fail')
except ValueError:
    pass

# non-ASCII identity token rejected
try:
    obligation.obligation_id({
      "assignment": {"id": "dém", "blob": "0123456789abcdef0123456789abcdef01234567"},
      "kind": "author", "target": "p1-corpus", "aspect": None,
    })
    raise SystemExit('non-ascii should fail')
except ValueError:
    pass

# gate aspect field order
g1 = {"assignment":{"id":"demo","blob":"0123456789abcdef0123456789abcdef01234567"},
      "kind":"gate","target":"p1","aspect":{"gate_id":"precision","gater_seat":"codex"}}
g2 = {"assignment":{"id":"demo","blob":"0123456789abcdef0123456789abcdef01234567"},
      "kind":"gate","target":"p1","aspect":{"gater_seat":"codex","gate_id":"precision"}}
assert obligation.obligation_id(g1)==obligation.obligation_id(g2)
print("ok")
PY
corpus_pass

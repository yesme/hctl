#!/usr/bin/env bash
# Case #20: 同 preimage 不同 key order/escape ⇒ 同 id；非 canonical 管线仍同 id
# D: D-33 | Source: codex-27c | Mechanical: JCS identity pipeline
# Mode: pure  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="20-obligation-id-jcs-stable"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

python3 - <<'PY'
import json, obligation
pre = {
  "assignment": {"id": "demo", "blob": "a" * 40},
  "kind": "gate",
  "target": "p1-corpus",
  "aspect": {"gate_id": "precision", "gater_seat": "codex"},
}
# different key insertion orders
pre2 = {
  "target": "p1-corpus",
  "kind": "gate",
  "aspect": {"gater_seat": "codex", "gate_id": "precision"},
  "assignment": {"blob": "a" * 40, "id": "demo"},
}
id1 = obligation.obligation_id(pre)
id2 = obligation.obligation_id(pre2)
assert id1 == id2, (id1, id2)
assert id1.startswith("sha256:") and len(id1) == 7 + 64
# wire variants with whitespace parse equal
raw1 = json.dumps(pre, separators=(",", ":"))
raw2 = json.dumps(pre, indent=2)
from jcs import parse_i_json
assert obligation.obligation_id(parse_i_json(raw1)) == obligation.obligation_id(parse_i_json(raw2))
print(id1)
print("ok")
PY
corpus_pass

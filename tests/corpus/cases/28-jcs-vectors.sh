#!/usr/bin/env bash
# Case #28: JCS 向量：key 序/escape 同 id；非 ASCII/dup key/lone surrogate 拒
# D: D-33 | Source: codex-27d | Mechanical: RFC 8785 + ASCII grammar + I-JSON
# Mode: pure (differential oracle) + hctl-wire sanity
set -euo pipefail
CORPUS_CASE_ID="28-jcs-vectors"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

python3 - <<'PY'
import obligation
from jcs import jcs, parse_i_json

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

a = parse_i_json('{"x":"a"}')
b = parse_i_json('{"x":"\\u0061"}')
assert jcs(a) == jcs(b)

try:
    parse_i_json('{"a":1,"a":2}')
    raise SystemExit('dup should fail')
except ValueError:
    pass

# lone surrogate escape rejected (frozen BACKLOG vector)
try:
    parse_i_json('"\\ud800"')
    raise SystemExit('lone surrogate should fail')
except ValueError:
    pass

# non-ASCII identity token rejected (NFC/NFD surface covered by grammar reject)
try:
    obligation.obligation_id({
      "assignment": {"id": "dém", "blob": "0123456789abcdef0123456789abcdef01234567"},
      "kind": "author", "target": "p1-corpus", "aspect": None,
    })
    raise SystemExit('non-ascii should fail')
except ValueError:
    pass

# dynamic assignment additionalProperties:false
try:
    obligation.obligation_id({
      "assignment": {
        "assign_event": "0123456789abcdef0123456789abcdef01234567",
        "extra": "x",
      },
      "kind": "author", "target": "p1-corpus", "aspect": None,
    })
    raise SystemExit('extra assign_event keys should fail')
except ValueError:
    pass

g1 = {"assignment":{"id":"demo","blob":"0123456789abcdef0123456789abcdef01234567"},
      "kind":"gate","target":"p1","aspect":{"gate_id":"precision","gater_seat":"codex"}}
g2 = {"assignment":{"id":"demo","blob":"0123456789abcdef0123456789abcdef01234567"},
      "kind":"gate","target":"p1","aspect":{"gater_seat":"codex","gate_id":"precision"}}
assert obligation.obligation_id(g1)==obligation.obligation_id(g2)
print("ok")
PY

# hctl-wire: real binary must respond to version/help (rejects /usr/bin/true under require)
if [ -n "${HCTL:-}" ] && [ -x "${HCTL}" ]; then
  out=$(corpus_hctl_run version 2>&1 || true)
  echo "$out" | grep -qi 'hctl' || corpus_fail "HCTL version does not identify as hctl (stub binary?)"
  # JCS path is internal; kernel unit test is the normative cross-check
  repo_root=$(cd "$CORPUS_ROOT/../.." && pwd)
  if [ -f "$repo_root/go.mod" ]; then
    (cd "$repo_root" && GOPROXY=off go test -mod=vendor -count=1 ./internal/jcs/ ./internal/protocol/ -run 'JCS|Obligation|Parse' 2>/dev/null) \
      || (cd "$repo_root" && GOPROXY=off go test -mod=vendor -count=1 ./internal/jcs/ ./internal/protocol/) \
      || corpus_fail "Go JCS/protocol tests failed"
  fi
fi

corpus_pass

#!/usr/bin/env bash
# Case #21: composite scope verdict 可校验；delta-only COMPLETE 不满足 full quorum
# D: D-41 | Source: codex-27c | Mechanical: quorum four conditions + coverage
# Mode: pure  (pure | hybrid | deferred-p2 | hctl-wire)
set -euo pipefail
CORPUS_CASE_ID="21-composite-scope-quorum"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

python3 - <<'PY'
import json, derive_rules
full = {"decision":"APPROVE","completeness":"COMPLETE","scope":{"kind":"full"}}
delta_only = {"decision":"APPROVE","completeness":"COMPLETE","scope":{"kind":"composite","parts":[{"kind":"delta","from_blob":"a","to_blob":"b"}]}}
composite = {"decision":"APPROVE","completeness":"COMPLETE","scope":{"kind":"composite","parts":[
  {"kind":"fix_verification","findings":["F3"],"target_blob":"x"},
  {"kind":"delta","from_blob":"a","to_blob":"b"},
]}}
inc = {"decision":"APPROVE","completeness":"INCOMPLETE","scope":{"kind":"full"}}
rc = {"decision":"REQUEST_CHANGES","completeness":"COMPLETE","scope":{"kind":"full"}}
assert derive_rules.quorum_counts(full, required_coverage="full")
assert not derive_rules.quorum_counts(delta_only, required_coverage="full")
assert derive_rules.quorum_counts(composite, required_coverage="full")
assert not derive_rules.quorum_counts(inc, required_coverage="full")
assert not derive_rules.quorum_counts(rc, required_coverage="full")
print("ok")
PY
corpus_pass

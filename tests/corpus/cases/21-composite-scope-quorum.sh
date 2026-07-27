#!/usr/bin/env bash
# Case #21: composite scope verdict 可校验；delta-only COMPLETE 不满足 full quorum
# D: D-41 | Source: codex-27c | Mechanical: quorum four conditions + coverage + exact revision
# Mode: pure (differential oracle)
set -euo pipefail
CORPUS_CASE_ID="21-composite-scope-quorum"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

python3 - <<'PY'
import derive_rules
rev = {"base": "b" * 40, "head": "h" * 40}
wrong = {"base": "b" * 40, "head": "x" * 40}
full = {
  "decision": "APPROVE", "completeness": "COMPLETE",
  "scope": {"kind": "full"}, "revision": rev,
}
delta_only = {
  "decision": "APPROVE", "completeness": "COMPLETE",
  "scope": {"kind": "composite", "parts": [{"kind": "delta", "from_blob": "a", "to_blob": "b"}]},
  "revision": rev,
}
composite = {
  "decision": "APPROVE", "completeness": "COMPLETE",
  "scope": {"kind": "composite", "parts": [
    {"kind": "fix_verification", "findings": ["F3"], "target_blob": "x"},
    {"kind": "delta", "from_blob": "a", "to_blob": "b"},
  ]},
  "revision": rev,
}
inc = {**full, "completeness": "INCOMPLETE"}
rc = {**full, "decision": "REQUEST_CHANGES"}
stale = {**full, "revision": wrong}

assert derive_rules.quorum_counts(full, required_coverage="full", expected_revision=rev)
assert not derive_rules.quorum_counts(delta_only, required_coverage="full", expected_revision=rev)
assert derive_rules.quorum_counts(composite, required_coverage="full", expected_revision=rev)
assert not derive_rules.quorum_counts(inc, required_coverage="full", expected_revision=rev)
assert not derive_rules.quorum_counts(rc, required_coverage="full", expected_revision=rev)
assert not derive_rules.quorum_counts(stale, required_coverage="full", expected_revision=rev)
# missing expected_revision is a hard error (no silent skip of freshness)
try:
    derive_rules.quorum_counts(full, required_coverage="full")
    raise SystemExit("expected TypeError for missing expected_revision")
except TypeError:
    pass
print("ok")
PY
corpus_pass

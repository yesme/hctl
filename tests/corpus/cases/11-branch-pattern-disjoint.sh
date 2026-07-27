#!/usr/bin/env bash
# Case #11: author/memo pattern 跨席跨 lane 两两不相交（doctor）
# D: D-08 | Source: grok-27b | Mechanical: doctor pattern disjointness
# Mode: pure (differential oracle) + hctl-wire
set -euo pipefail
CORPUS_CASE_ID="11-branch-pattern-disjoint"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

python3 - <<'PY'
import derive_rules
seats = {
  "claude": {
    "author_branch_patterns": ["refs/heads/work/claude/*", "refs/heads/claude/*"],
    "memo_branch_patterns": ["refs/heads/memo/claude/*"],
  },
  "codex": {
    "author_branch_patterns": ["refs/heads/work/codex/*", "refs/heads/codex/*"],
    "memo_branch_patterns": ["refs/heads/memo/codex/*"],
  },
  "grok": {
    "author_branch_patterns": ["refs/heads/work/grok/*", "refs/heads/grok/*"],
    "memo_branch_patterns": ["refs/heads/memo/grok/*"],
  },
}
v = derive_rules.seats_patterns_ok(seats)
assert v == [], v
bad = {
  "a": {"author_branch_patterns": ["refs/heads/work/*"], "memo_branch_patterns": ["refs/heads/memo/a/*"]},
  "b": {"author_branch_patterns": ["refs/heads/work/b/*"], "memo_branch_patterns": ["refs/heads/memo/b/*"]},
}
v2 = derive_rules.seats_patterns_ok(bad)
assert v2, "expected overlap detection"
assert derive_rules.patterns_disjoint([
  "refs/heads/work/grok/*", "refs/heads/memo/grok/*"
])
print("ok")
PY

if [ -n "${HCTL:-}" ] && [ -x "${HCTL}" ]; then
  corpus_require_hctl
  # shellcheck source=/dev/null
  source "$CORPUS_ROOT/lib/hctl_fixture.sh"
  corpus_sandbox
  corpus_hctl_fixture
  # valid fixture loads (doctor may exit non-zero for missing go.mod in sandbox — check config line only)
  doc_out=$(corpus_hctl claude doctor 2>&1 || true)
  echo "$doc_out" | grep -qE 'config[[:space:]]+ok' || corpus_fail "doctor config should be ok on disjoint seats: $doc_out"

  # overlapping patterns: seats.toml invalid at decode — status/doctor must fail closed
  cat > "$HCTL_FIXTURE_REPO/.hctl/seats.toml" <<'EOF'
schema_version = 1
kernel = "hctl@bootstrap"
merge_coordinator = "claude"
enforcement = "bootstrap"
[seats.claude]
harness = "claude"
model = "test"
capabilities = []
write_scope = "canonical"
author_concurrency = 1
author_branch_patterns = ["refs/heads/work/*"]
memo_branch_patterns = ["refs/heads/memo/claude/*"]
[seats.claude.features]
session_resume = "unknown"
parallel_sessions = "unknown"
subagents = "unknown"
skill = "unknown"
door_file = "CLAUDE.md"
[seats.codex]
harness = "codex"
model = "test"
capabilities = []
write_scope = "canonical"
author_concurrency = 1
author_branch_patterns = ["refs/heads/work/codex/*"]
memo_branch_patterns = ["refs/heads/memo/codex/*"]
[seats.codex.features]
session_resume = "unknown"
parallel_sessions = "unknown"
subagents = "unknown"
skill = "unknown"
door_file = "AGENTS.md"
[seats.grok]
harness = "grok"
model = "test"
capabilities = []
write_scope = "canonical"
author_concurrency = 1
author_branch_patterns = ["refs/heads/work/grok/*"]
memo_branch_patterns = ["refs/heads/memo/grok/*"]
[seats.grok.features]
session_resume = "unknown"
parallel_sessions = "unknown"
subagents = "unknown"
skill = "unknown"
door_file = "AGENTS.md"
EOF
  git -C "$HCTL_FIXTURE_REPO" add .hctl/seats.toml
  git -C "$HCTL_FIXTURE_REPO" commit -q -m 'overlap seats'
  git -C "$HCTL_FIXTURE_REPO" push -q origin main
  set +e
  out=$(corpus_hctl claude doctor 2>&1)
  code=$?
  set -e
  # Must fail closed: non-zero exit AND no healthy config ok line
  [ "$code" -ne 0 ] || corpus_fail "doctor must exit non-zero on overlapping patterns (code=$code): $out"
  echo "$out" | grep -qiE 'INVALID_CONFIG|overlap' || corpus_fail "doctor must mention INVALID_CONFIG|overlap: $out"
  if echo "$out" | grep -qE 'config[[:space:]]+ok'; then
    corpus_fail "doctor must not report config ok under overlap: $out"
  fi
fi

corpus_pass

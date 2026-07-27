#!/usr/bin/env bash
# Corpus runner: frozen manifest + optional strict gate mode.
# Exit 0 only when every expected case exists, no unexpected cases appear,
# no FAIL, and (under strict / CORPUS_REQUIRE_HCTL) no SKIP.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")" && pwd)
cd "$ROOT/../.."
export PYTHONPATH="$ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

# Frozen expected set (P0 #0a–#28 plus P1 carry #29). Do not glob-discover.
CONCURRENCY_CASES=(
  chain-claim-mutex
  notes-union-antimutex
)
NUMBERED_CASES=(
  01-gate-base-advance-stale
  02-merge-capacity-one
  03-merge-assignee-coordinator
  04-push-ancestry-delivered
  05-push-diverged-lost
  06-unsupported-facts
  07-receipt-fact-tips-parent
  08-authority-and-assignment-gate
  09-begin-retry-reuse-claim
  10-same-blob-two-events
  11-branch-pattern-disjoint
  12-dup-assignment-logical-id
  13-assignment-revision-moved
  14-escalated-reclaim-frozen
  15-pending-accept-deferred
  16-dual-avatar-reclaim-race
  17-pending-accept-cross-ref-dual-win
  18-rebind-active-merge-fence
  19-escalated-progress-no-thaw
  20-obligation-id-jcs-stable
  21-composite-scope-quorum
  22-late-finding-blocks-merge
  23-receipt-closes-after-config-change
  24-rebind-claim-rights
  25-stale-config-claim-after-rebind
  26-receipt-config-mismatch-reject
  27-streak-forward-env-reset
  28-jcs-vectors
  29-regate-carry
)

# Cases that must exercise the real hctl binary when HCTL is available / required.
WIRE_CASES=(
  03-merge-assignee-coordinator
  06-unsupported-facts
  07-receipt-fact-tips-parent
  08-authority-and-assignment-gate
  09-begin-retry-reuse-claim
  11-branch-pattern-disjoint
  12-dup-assignment-logical-id
  28-jcs-vectors
  29-regate-carry
)

pass=0
fail=0
skip=0
strict=0
if [ "${CORPUS_STRICT:-0}" = "1" ] || [ "${CORPUS_REQUIRE_HCTL:-0}" = "1" ]; then
  strict=1
fi

# Resolve HCTL early so wire cases see a consistent binary.
if [ -z "${HCTL:-}" ] && command -v hctl >/dev/null 2>&1; then
  HCTL=$(command -v hctl)
  export HCTL
fi
if [ "${CORPUS_REQUIRE_HCTL:-0}" = "1" ]; then
  if [ -z "${HCTL:-}" ] || [ ! -x "${HCTL}" ]; then
    echo "FAIL  runner: CORPUS_REQUIRE_HCTL=1 but HCTL is missing or not executable" >&2
    exit 1
  fi
fi

run_one() {
  local script=$1 name out code
  name=$(basename "$script" .sh)
  set +e
  out=$(bash "$script" 2>&1)
  code=$?
  set -e
  if [ "$code" -eq 0 ]; then
    pass=$((pass + 1))
    echo "PASS  $name"
  elif [ "$code" -eq 77 ]; then
    skip=$((skip + 1))
    echo "SKIP  $name"
    if [ "$strict" -eq 1 ]; then
      echo "  (strict mode: SKIP is a failure)"
      fail=$((fail + 1))
      skip=$((skip - 1))
      printf '%s\n' "$out" | tail -n 20
    fi
  else
    fail=$((fail + 1))
    echo "FAIL  $name (exit $code)"
    printf '%s\n' "$out" | tail -n 30
  fi
}

# --- manifest integrity ---
manifest_fail=0
declare -A expected=()
for name in "${CONCURRENCY_CASES[@]}"; do
  expected["concurrency/$name.sh"]=1
  path="$ROOT/concurrency/$name.sh"
  if [ ! -f "$path" ]; then
    echo "FAIL  manifest missing: concurrency/$name.sh" >&2
    manifest_fail=1
  fi
done
for name in "${NUMBERED_CASES[@]}"; do
  expected["cases/$name.sh"]=1
  path="$ROOT/cases/$name.sh"
  if [ ! -f "$path" ]; then
    echo "FAIL  manifest missing: cases/$name.sh" >&2
    manifest_fail=1
  fi
done

for path in "$ROOT"/concurrency/*.sh "$ROOT"/cases/*.sh; do
  [ -f "$path" ] || continue
  rel=${path#"$ROOT/"}
  if [ -z "${expected[$rel]:-}" ]; then
    echo "FAIL  manifest unexpected: $rel" >&2
    manifest_fail=1
  fi
done

if [ "$manifest_fail" -ne 0 ]; then
  echo "SUMMARY pass=0 fail=manifest skip=0"
  exit 1
fi

# Wire set non-empty under HCTL / require mode.
if [ -n "${HCTL:-}" ] || [ "${CORPUS_REQUIRE_HCTL:-0}" = "1" ]; then
  if [ "${#WIRE_CASES[@]}" -eq 0 ]; then
    echo "FAIL  runner: wire case set is empty but HCTL is in play" >&2
    exit 1
  fi
fi

# Per-case wire stamps (corpus_require_hctl / corpus_hctl write these).
if [ -n "${HCTL:-}" ] || [ "${CORPUS_REQUIRE_HCTL:-0}" = "1" ]; then
  CORPUS_WIRE_STAMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/hctl-wire-stamps.XXXXXX")
  export CORPUS_WIRE_STAMP_DIR
  # shellcheck disable=SC2064
  trap 'rm -rf "$CORPUS_WIRE_STAMP_DIR"' EXIT
fi

echo "== concurrency =="
for name in "${CONCURRENCY_CASES[@]}"; do
  run_one "$ROOT/concurrency/$name.sh"
done

echo "== cases #01-29 =="
for name in "${NUMBERED_CASES[@]}"; do
  run_one "$ROOT/cases/$name.sh"
done

# When HCTL is in play, every declared wire case must have stamped a real invoke.
if [ -n "${CORPUS_WIRE_STAMP_DIR:-}" ]; then
  for name in "${WIRE_CASES[@]}"; do
    if [ ! -f "$CORPUS_WIRE_STAMP_DIR/${name}.wired" ]; then
      echo "FAIL  wire case $name did not stamp an HCTL invoke" >&2
      fail=$((fail + 1))
    fi
  done
fi

echo
echo "SUMMARY pass=$pass fail=$fail skip=$skip strict=$strict hctl=${HCTL:-<none>}"
if [ "$fail" -ne 0 ]; then
  exit 1
fi
if [ "$strict" -eq 1 ] && [ "$skip" -ne 0 ]; then
  exit 1
fi
exit 0

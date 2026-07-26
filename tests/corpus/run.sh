#!/usr/bin/env bash
# Corpus runner: execute concurrency + cases, summarize exit codes.
# Exit 0 only if no FAIL; SKIP (77) counted separately (non-fatal).
set -euo pipefail
ROOT=$(cd "$(dirname "$0")" && pwd)
cd "$ROOT/../.."
export PYTHONPATH="$ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

pass=0
fail=0
skip=0

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
  else
    fail=$((fail + 1))
    echo "FAIL  $name (exit $code)"
    printf '%s\n' "$out" | tail -n 30
  fi
}

echo "== concurrency =="
for s in "$ROOT"/concurrency/*.sh; do
  [ -f "$s" ] || continue
  run_one "$s"
done

echo "== cases #01-28 =="
for s in "$ROOT"/cases/*.sh; do
  [ -f "$s" ] || continue
  run_one "$s"
done

echo
echo "SUMMARY pass=$pass fail=$fail skip=$skip"
if [ "$fail" -ne 0 ]; then
  exit 1
fi
exit 0

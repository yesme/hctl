#!/usr/bin/env bash
# Shared sandbox helpers for corpus cases (D-17).
# Source from case scripts:  # shellcheck source=...
#   CORPUS_ROOT=...; source "$CORPUS_ROOT/lib/common.sh"
set -euo pipefail

export GIT_AUTHOR_NAME=${GIT_AUTHOR_NAME:-corpus}
export GIT_AUTHOR_EMAIL=${GIT_AUTHOR_EMAIL:-corpus@hctl.invalid}
export GIT_COMMITTER_NAME=${GIT_COMMITTER_NAME:-corpus}
export GIT_COMMITTER_EMAIL=${GIT_COMMITTER_EMAIL:-corpus@hctl.invalid}
export GIT_AUTHOR_DATE=${GIT_AUTHOR_DATE:-'2026-07-27T00:00:00+00:00'}
export GIT_COMMITTER_DATE=${GIT_COMMITTER_DATE:-'2026-07-27T00:00:00+00:00'}

CORPUS_CASE_ID=${CORPUS_CASE_ID:-unknown}

corpus_fail() { echo "FAIL(${CORPUS_CASE_ID}): $*" >&2; exit 1; }
corpus_pass() { echo "PASS ${CORPUS_CASE_ID}"; }

# Exit 77 = SKIP (runner treats as non-fail). Used when HCTL binary required but absent.
corpus_skip() { echo "SKIP(${CORPUS_CASE_ID}): $*" >&2; exit 77; }

corpus_require_hctl() {
  if [ -n "${HCTL:-}" ] && [ -x "${HCTL}" ]; then
    return 0
  fi
  if command -v hctl >/dev/null 2>&1; then
    HCTL=$(command -v hctl)
    export HCTL
    return 0
  fi
  if [ "${CORPUS_REQUIRE_HCTL:-0}" = "1" ]; then
    corpus_fail "hctl binary required but not found (set HCTL= path)"
  fi
  corpus_skip "hctl binary not available — pure/spec portion may still be covered; wire CLI after p1-kernel lands"
}

# mktemp workdir + trap; sets CORPUS_WORK and cds there.
corpus_sandbox() {
  CORPUS_WORK=$(mktemp -d "${TMPDIR:-/tmp}/hctl-corpus.XXXXXX")  # one-shot sandbox
  # shellcheck disable=SC2064
  trap 'rm -rf "$CORPUS_WORK"' EXIT
  cd "$CORPUS_WORK"
}

# init bare origin + one or more clones named as args
corpus_init_origin() {
  git -c init.defaultBranch=main init -q --bare origin.git
  local name
  for name in "$@"; do
    git clone -q origin.git "$name" 2>/dev/null
  done
}

# event <clone> <parent-oid-or-empty> <json> <summary> → new event commit oid
corpus_event() {
  local c=$1 parent=$2 json=$3 summary=$4 blob tree
  blob=$(printf '%s' "$json" | git -C "$c" hash-object -w --stdin)
  tree=$(printf '100644 blob %s\tevent.json\n' "$blob" | git -C "$c" mktree)
  if [ -n "$parent" ]; then
    git -C "$c" commit-tree "$tree" -p "$parent" -m "$summary"
  else
    git -C "$c" commit-tree "$tree" -m "$summary"
  fi
}

# empty commit on main in clone $1, push, return oid
corpus_seed_main() {
  local c=$1 msg=${2:-seed}
  git -C "$c" commit -q --allow-empty -m "$msg"
  git -C "$c" push -q origin main
  git -C "$c" rev-parse HEAD
}

# python helper path
corpus_py() {
  local script=$1
  shift
  python3 "$CORPUS_ROOT/lib/$script" "$@"
}

#!/usr/bin/env bash
# Case #15: pending_accept 他席抢 claim 被拒（P2 HANDOFF 解冻后）
# D: D-36 | Source: 主笔 | Mechanical: P2; P1 marks DEFERRED/unsupported
# Mode: deferred-p2 — documents that HANDOFF is not in P1 closed event set
set -euo pipefail
CORPUS_CASE_ID="15-pending-accept-deferred"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
export PYTHONPATH="$CORPUS_ROOT/lib${PYTHONPATH:+:$PYTHONPATH}"

# P1 closed set is CLAIM/VERDICT/CANCEL — HANDOFF is unknown type ⇒ UNSUPPORTED_FACTS
# (differential oracle three_way; kernel authority is Go protocol parser)
known='CLAIM,VERDICT,CANCEL'
u=$(corpus_py derive_rules.py three-way \
  '{"schema_version":1,"type":"HANDOFF","actor":{"seat":"claude","machine":"m","session":null},"created_at":"2026-07-27T00:00:00Z"}' \
  claude "$known")
[ "$u" = "UNSUPPORTED_FACTS" ] || corpus_fail "HANDOFF must be UNSUPPORTED on P1, got $u"

# ACCEPT is also not a P1 type
a=$(corpus_py derive_rules.py three-way \
  '{"schema_version":1,"type":"ACCEPT","actor":{"seat":"claude","machine":"m","session":null},"created_at":"2026-07-27T00:00:00Z"}' \
  claude "$known")
[ "$a" = "UNSUPPORTED_FACTS" ] || corpus_fail "ACCEPT must be UNSUPPORTED on P1, got $a"

corpus_pass

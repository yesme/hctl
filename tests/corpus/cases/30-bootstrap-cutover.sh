#!/usr/bin/env bash
# Case #30: bootstrap→active cutover bounds merge audit debt (codex-27k §3.3)
# D: D-38/D-39 | Source: codex-27k §3.3 | Mode: hctl-wire
# Pre-cutover UNRECORDED_MERGE = error + write blocked; after the flip the same
# debt is ACKNOWLEDGED_BOOTSTRAP_HISTORY (warning), post-cutover debt is error
# again, rollback/double-flip are INVALID_CUTOVER. STALE_BINARY may appear
# post-flip when the test binary carries no vcs revision; classification
# assertions are independent of it and WriteGuard-allow is proven in
# internal/derive unit tests (TestMarkMergeDebtAcknowledgesBootstrapHistory).
set -euo pipefail
CORPUS_CASE_ID="30-bootstrap-cutover"
CORPUS_ROOT=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/common.sh"
# shellcheck source=/dev/null
source "$CORPUS_ROOT/lib/hctl_fixture.sh"

corpus_require_hctl
corpus_sandbox
corpus_hctl_fixture
repo=$HCTL_FIXTURE_REPO
origin=$HCTL_FIXTURE_ORIGIN

problem_codes() { # stdin: status --json → stdout: sorted unique problem codes
  python3 -c 'import json,sys; d=json.load(sys.stdin); print("\n".join(sorted({p["code"] for p in d.get("problems",[])})))'
}

assert_problem() { # $1 json  $2 code  $3 expected severity  $4 detail substring
  printf '%s' "$1" | python3 -c '
import json, sys
code, severity, sub = sys.argv[1], sys.argv[2], sys.argv[3]
d = json.load(sys.stdin)
hits = [p for p in d.get("problems", []) if p["code"] == code]
assert hits, f"missing problem {code}: {d[chr(39)+chr(39)] if False else d.get('problems')}"
assert all(p["severity"] == severity for p in hits), hits
assert any(sub in p["detail"] for p in hits), hits
print("ok")
' "$2" "$3" "$4" >/dev/null || corpus_fail "expected $2 severity=$3 containing '$4'"
}

assert_no_problem() { # $1 json  $2 code
  printf '%s' "$1" | python3 -c '
import json, sys
d = json.load(sys.stdin)
assert not [p for p in d.get("problems", []) if p["code"] == sys.argv[1]], d["problems"]
' "$2" || corpus_fail "problem $2 must be absent"
}

# --- 1) bootstrap-era unrecorded merge: error + write blocked ---
corpus_hctl codex begin demo >/dev/null
echo change > "$repo/change.txt"
git -C "$repo" add change.txt
git -C "$repo" commit -q -m change
git -C "$repo" push -q origin work/codex/demo
head=$(git -C "$repo" rev-parse HEAD)
git -C "$origin" update-ref refs/pull/1/head "$head"
base=$(git -C "$repo" rev-parse refs/remotes/origin/main)
tree=$(git -C "$repo" rev-parse "$head^{tree}")
merge_oid=$(git -C "$repo" commit-tree "$tree" -p "$base" -m "demo unrecorded (#1)")
git -C "$repo" push -q origin "$merge_oid:refs/heads/main"

set +e
st=$(corpus_hctl codex status --json 2>/dev/null)
code=$?
set -e
[ "$code" -ne 0 ] || corpus_fail "bootstrap-era unrecorded merge must be unhealthy"
assert_problem "$st" UNRECORDED_MERGE error "demo"
# write is blocked by WriteGuard
gate_id=$(printf '%s' "$st" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(next(o["obligation"]["id"] for o in d["obligations"] if o["kind"]=="gate"))')
set +e
out=$(corpus_hctl grok claim "$gate_id" 2>&1)
code=$?
set -e
[ "$code" -ne 0 ] || corpus_fail "WriteGuard must block claim under unacknowledged debt"
echo "$out" | grep -q "UNRECORDED_MERGE" || corpus_fail "claim rejection must name UNRECORDED_MERGE: $out"

# --- 2) flip to active: same debt becomes acknowledged warning ---
pin=$("$HCTL" version | sed -n 's/.*revision=\([0-9a-f]\{40\}\).*/\1/p')
[ -n "$pin" ] || pin=$(printf '0%.0s' $(seq 40))
git -C "$repo" fetch -q origin main
git -C "$repo" checkout -q main 2>/dev/null || git -C "$repo" checkout -qb main origin/main
git -C "$repo" reset -q --hard origin/main
sed -i "s/^kernel .*$/kernel                = \"hctl@$pin\"/; s/^enforcement .*$/enforcement           = \"active\"/" "$repo/.hctl/seats.toml"
grep -q 'enforcement           = "active"' "$repo/.hctl/seats.toml" || corpus_fail "flip edit failed"
git -C "$repo" add .hctl/seats.toml
git -C "$repo" commit -q -m "activation: enforcement=active"
cutover=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" push -q origin main

set +e
st2=$(corpus_hctl codex status --json 2>/dev/null)
set -e
assert_problem "$st2" ACKNOWLEDGED_BOOTSTRAP_HISTORY warning "UNRECORDED_MERGE"
assert_problem "$st2" ACKNOWLEDGED_BOOTSTRAP_HISTORY warning "$cutover"
assert_no_problem "$st2" UNRECORDED_MERGE
assert_no_problem "$st2" INVALID_CUTOVER
assert_no_problem "$st2" BOOTSTRAP_TRUST

# --- 3) post-cutover unrecorded merge is an error again ---
cat > "$repo/.hctl/assignments/demo2.toml" <<'EOF'
schema_version = 1
id = "demo2"
kind = "change"
needs = []
base_ref = "refs/heads/main"
[author]
seat = "codex"
branch_slug = "demo2"
claim_timeout_seconds = 3600
[[gates]]
id = "review"
mode = "required"
threshold = "P1"
claim_timeout_seconds = 3600
[gates.requirement]
seat = "grok"
[gates.on_timeout]
action = "escalate"
[merge]
method = "squash"
EOF
git -C "$repo" add .hctl/assignments/demo2.toml
git -C "$repo" commit -q -m "add demo2"
echo change2 > "$repo/change2.txt"
git -C "$repo" add change2.txt
git -C "$repo" commit -q -m change2
head2=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" push -q origin "$head2:refs/heads/work/codex/demo2"
git -C "$origin" update-ref refs/pull/2/head "$head2"
base2=$(git -C "$repo" rev-parse HEAD~1)
git -C "$repo" push -q origin "$base2:refs/heads/main"
tree2=$(git -C "$repo" rev-parse "$head2^{tree}")
merge2=$(git -C "$repo" commit-tree "$tree2" -p "$base2" -m "demo2 unrecorded (#2)")
git -C "$repo" push -q -f origin "$merge2:refs/heads/main"

set +e
st3=$(corpus_hctl codex status --json 2>/dev/null)
code=$?
set -e
[ "$code" -ne 0 ] || corpus_fail "post-cutover unrecorded merge must be unhealthy"
assert_problem "$st3" UNRECORDED_MERGE error "demo2"
assert_problem "$st3" ACKNOWLEDGED_BOOTSTRAP_HISTORY warning "demo"


# --- 3.5) tree-reuse side candidate stays UNJUDGEABLE error (codex-pr72#P1-02) ---
# Candidate created after the cutover whose tree equals the covered cutover
# commit's tree: attribution must be ancestry-bound, never fall back to the old
# first-parent OID and inherit its acknowledgment.
cat > "$repo/.hctl/assignments/demo3.toml" <<'EOF3'
schema_version = 1
id = "demo3"
kind = "change"
needs = []
base_ref = "refs/heads/main"
[author]
seat = "codex"
branch_slug = "demo3"
claim_timeout_seconds = 3600
[[gates]]
id = "review"
mode = "required"
threshold = "P1"
claim_timeout_seconds = 3600
[gates.requirement]
seat = "grok"
[gates.on_timeout]
action = "escalate"
[merge]
method = "squash"
EOF3
git -C "$repo" fetch -q origin main
git -C "$repo" reset -q --hard origin/main
git -C "$repo" add .hctl/assignments/demo3.toml
git -C "$repo" commit -q -m "add demo3"
t3=$(git -C "$repo" rev-parse HEAD)
git -C "$repo" push -q origin main
side=$(git -C "$repo" commit-tree "$cutover^{tree}" -p "$t3" -m "demo3 candidate reusing cutover tree")
git -C "$repo" push -q origin "$side:refs/heads/work/codex/demo3"
git -C "$origin" update-ref refs/pull/3/head "$side"
integration=$(git -C "$repo" commit-tree "$t3^{tree}" -p "$t3" -p "$side" -m "demo3 two-parent integration (#3)")
git -C "$repo" push -q origin "$integration:refs/heads/main"

set +e
st35=$(corpus_hctl codex status --json 2>/dev/null)
code=$?
set -e
[ "$code" -ne 0 ] || corpus_fail "tree-reuse candidate must be unhealthy"
assert_problem "$st35" UNJUDGEABLE_MERGE error "demo3"
printf '%s' "$st35" | python3 -c '
import json, sys
d = json.load(sys.stdin)
bad = [p for p in d.get("problems", []) if p["code"] == "ACKNOWLEDGED_BOOTSTRAP_HISTORY" and "demo3" in p["detail"]]
assert not bad, bad
' || corpus_fail "demo3 must never inherit bootstrap acknowledgment"

# --- 4) rollback active→bootstrap: INVALID_CUTOVER fail-closed ---
git -C "$repo" fetch -q origin main
git -C "$repo" reset -q --hard origin/main
sed -i 's/^enforcement .*$/enforcement           = "bootstrap"/' "$repo/.hctl/seats.toml"
git -C "$repo" add .hctl/seats.toml
git -C "$repo" commit -q -m "rollback to bootstrap"
git -C "$repo" push -q origin main
set +e
st4=$(corpus_hctl codex status --json 2>/dev/null)
code=$?
set -e
[ "$code" -ne 0 ] || corpus_fail "rollback must be unhealthy"
assert_problem "$st4" INVALID_CUTOVER error "active"

# --- 5) double flip active→bootstrap→active: still INVALID_CUTOVER ---
sed -i "s/^enforcement .*$/enforcement           = \"active\"/" "$repo/.hctl/seats.toml"
git -C "$repo" add .hctl/seats.toml
git -C "$repo" commit -q -m "second activation"
git -C "$repo" push -q origin main
set +e
st5=$(corpus_hctl codex status --json 2>/dev/null)
code=$?
set -e
[ "$code" -ne 0 ] || corpus_fail "double flip must be unhealthy"
assert_problem "$st5" INVALID_CUTOVER error "flip"

corpus_pass

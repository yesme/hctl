package config

import (
	"strings"
	"testing"
)

const validSeats = `
schema_version = 1
kernel = "hctl@bootstrap"
merge_coordinator = "claude"
enforcement = "bootstrap"

[seats.claude]
harness = "claude-code"
model = "fable"
capabilities = ["merge"]
write_scope = "canonical"
author_concurrency = 1
author_branch_patterns = ["refs/heads/work/claude/*"]
memo_branch_patterns = ["refs/heads/memo/claude/*"]
token_class = "full"
[seats.claude.features]
session_resume = "yes"
parallel_sessions = "yes"
subagents = "yes"
skill = "native"
door_file = "CLAUDE.md"

[seats.codex]
harness = "codex"
model = "gpt"
capabilities = ["implementation"]
write_scope = "canonical"
author_concurrency = 1
author_branch_patterns = ["refs/heads/work/codex/*"]
memo_branch_patterns = ["refs/heads/memo/codex/*"]
token_class = "full"
[seats.codex.features]
session_resume = "unknown"
parallel_sessions = "unknown"
subagents = "unknown"
skill = "unknown"
door_file = "AGENTS.md"
`

func TestDecodeSeatsAndPatternDisjointness(t *testing.T) {
	t.Parallel()
	if _, err := DecodeSeats([]byte(validSeats)); err != nil {
		t.Fatal(err)
	}
	overlap := strings.Replace(validSeats,
		`memo_branch_patterns = ["refs/heads/memo/codex/*"]`,
		`memo_branch_patterns = ["refs/heads/work/claude/x*"]`, 1)
	if _, err := DecodeSeats([]byte(overlap)); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("expected pattern overlap, got %v", err)
	}
}

func TestAssignmentCrossFieldValidation(t *testing.T) {
	t.Parallel()
	seats, err := DecodeSeats([]byte(validSeats))
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`
schema_version = 1
id = "demo"
kind = "change"
needs = []
base_ref = "refs/heads/main"
[author]
seat = "codex"
branch_slug = "demo"
claim_timeout_seconds = 60
[[gates]]
id = "review"
mode = "required"
threshold = "P1"
claim_timeout_seconds = 60
[gates.requirement]
seat = "claude"
[gates.on_timeout]
action = "escalate"
[merge]
method = "squash"
`)
	assignment, err := DecodeAssignment(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAssignment(assignment, seats); err != nil {
		t.Fatal(err)
	}
	assignment.Gates[0].Requirement.Seat = "codex"
	if err := ValidateAssignment(assignment, seats); err == nil || !strings.Contains(err.Error(), "also the author") {
		t.Fatalf("expected author/gater rejection, got %v", err)
	}
}

func TestDuplicateLogicalAssignmentIsAmbiguous(t *testing.T) {
	t.Parallel()
	seats, err := DecodeSeats([]byte(validSeats))
	if err != nil {
		t.Fatal(err)
	}
	base := Assignment{
		SchemaVersion: 1,
		ID:            "demo",
		Kind:          "change",
		Needs:         []string{},
		BaseRef:       "refs/heads/main",
		Author:        Author{Seat: "codex", BranchSlug: "demo", ClaimTimeoutSeconds: 60},
		Merge:         Merge{Method: "squash"},
	}
	records := []AssignmentRecord{{Assignment: base}, {Assignment: base}}
	if err := ValidateAssignments(records, seats); err == nil || !strings.Contains(err.Error(), "AMBIGUOUS_ASSIGNMENT") {
		t.Fatalf("expected ambiguity, got %v", err)
	}
}

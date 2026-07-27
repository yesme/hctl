package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusClaimAndBeginReuseOneClaim(t *testing.T) {
	t.Parallel()
	repo, origin := appFixture(t)
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		instance := &App{Stdout: &stdout, Stderr: &stderr}
		global := []string{"-C", repo, "--remote", "origin", "--seat", "codex", "--machine", "test-machine"}
		code := instance.Run(context.Background(), append(global, args...))
		return code, stdout.String(), stderr.String()
	}
	code, out, stderr := run("status", "--json")
	if code != 0 {
		t.Fatalf("status failed: code=%d stderr=%s output=%s", code, stderr, out)
	}
	var document struct {
		Complete    bool `json:"complete"`
		Obligations []struct {
			Kind       string `json:"kind"`
			Obligation struct {
				ID string `json:"id"`
			} `json:"obligation"`
		} `json:"obligations"`
	}
	if err := json.Unmarshal([]byte(out), &document); err != nil {
		t.Fatal(err)
	}
	var authorID string
	for _, obligation := range document.Obligations {
		if obligation.Kind == "author" {
			authorID = obligation.Obligation.ID
		}
	}
	if !document.Complete || authorID == "" {
		t.Fatalf("unexpected status document: %+v", document)
	}
	code, out, stderr = run("claim", authorID)
	if code != 0 {
		t.Fatalf("claim failed: code=%d stderr=%s output=%s", code, stderr, out)
	}
	if !strings.Contains(out, "fencing_token=") {
		t.Fatalf("claim output is not self-describing: %s", out)
	}
	for i := 0; i < 2; i++ {
		code, out, stderr = run("begin", "demo")
		if code != 0 {
			t.Fatalf("begin #%d failed: code=%d stderr=%s output=%s", i+1, code, stderr, out)
		}
		if !strings.Contains(out, "claim=") || !strings.Contains(out, "branch=refs/heads/work/codex/demo") {
			t.Fatalf("begin output is not self-describing: %s", out)
		}
	}
	count := strings.TrimSpace(git(t, origin, "rev-list", "--count", "refs/coop/codex"))
	if count != "1" {
		t.Fatalf("begin minted a second claim; chain length=%s", count)
	}
}

func TestGateVerdictQuorumAndMergeCheckFlow(t *testing.T) {
	t.Parallel()
	repo, origin := appFixture(t)
	run := func(seat string, args ...string) (int, string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		instance := &App{Stdout: &stdout, Stderr: &stderr}
		global := []string{"-C", repo, "--remote", "origin", "--seat", seat, "--machine", seat + "-machine"}
		code := instance.Run(context.Background(), append(global, args...))
		return code, stdout.String(), stderr.String()
	}
	code, out, stderr := run("codex", "begin", "demo")
	if code != 0 {
		t.Fatalf("begin failed: %s", stderr)
	}
	if err := os.WriteFile(filepath.Join(repo, "change.txt"), []byte("change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitDir(t, repo, "add", "change.txt")
	gitDir(t, repo, "commit", "-q", "-m", "change")
	gitDir(t, repo, "push", "-q", "origin", "work/codex/demo")
	head := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))
	gitDir(t, origin, "update-ref", "refs/pull/1/head", head)

	status := readStatus(t, run, "grok")
	gateID := obligationID(status, "gate", "grok")
	if gateID == "" {
		t.Fatal("gate obligation was not derived")
	}
	code, out, stderr = run("grok", "claim", gateID)
	if code != 0 || !strings.Contains(out, "review threshold P1") {
		t.Fatalf("gate claim was not self-describing: code=%d stderr=%s output=%s", code, stderr, out)
	}
	code, _, stderr = run("grok", "verdict", gateID,
		"--decision", "APPROVE",
		"--report", "memory/review.md",
		"--completeness", "COMPLETE",
	)
	if code != 0 {
		t.Fatalf("verdict failed: %s", stderr)
	}
	code, out, stderr = run("grok", "verdict", gateID,
		"--decision", "APPROVE",
		"--report", "memory/review.md",
		"--completeness", "COMPLETE",
	)
	if code != 0 || !strings.Contains(out, "verdict already-current") {
		t.Fatalf("identical verdict was not idempotent: code=%d stderr=%s output=%s", code, stderr, out)
	}
	if count := strings.TrimSpace(git(t, origin, "rev-list", "--count", "refs/coop/grok")); count != "2" {
		t.Fatalf("identical verdict minted a new event; chain length=%s", count)
	}
	status = readStatus(t, run, "claude")
	mergeID := obligationID(status, "merge", "claude")
	if mergeID == "" {
		t.Fatal("merge obligation was not derived")
	}
	code, _, stderr = run("claude", "claim", mergeID)
	if code != 0 {
		t.Fatalf("merge claim failed: %s", stderr)
	}
	code, out, stderr = run("claude", "merge", "1", "--check")
	if code != 0 {
		t.Fatalf("merge --check failed: %s\n%s", stderr, out)
	}
	for _, field := range []string{
		"Hctl-Version: 1", "Hctl-Assignment: demo", "Hctl-PR: 1",
		"Hctl-Merge-Claim:", "Hctl-Fact-Tip: claude=", "Hctl-Fact-Tip: grok=",
	} {
		if !strings.Contains(out, field) {
			t.Errorf("merge check output is missing %q:\n%s", field, out)
		}
	}
	_, receiptBody, ok := strings.Cut(out, "receipt:\n")
	if !ok {
		t.Fatalf("cannot extract receipt body:\n%s", out)
	}
	base := strings.TrimSpace(git(t, repo, "rev-parse", "refs/remotes/origin/main"))
	tree := strings.TrimSpace(git(t, repo, "rev-parse", head+"^{tree}"))
	mergeCommit := strings.TrimSpace(git(t, repo,
		"commit-tree", tree, "-p", base, "-m", "demo (#1)", "-m", receiptBody))
	gitDir(t, repo, "push", "-q", "origin", mergeCommit+":refs/heads/main")
	gitDir(t, origin, "update-ref", "-d", "refs/heads/work/codex/demo")
	status = readStatus(t, run, "claude")
	merge := obligationState(status, "merge", "claude")
	if merge == nil || !merge.Completed || merge.State != "completed" || merge.MergedCommit != mergeCommit {
		t.Fatalf("receipt replay did not close merge obligation: %+v", merge)
	}
}

type appStatus struct {
	Complete    bool `json:"complete"`
	Obligations []struct {
		Kind         string `json:"kind"`
		Holder       string `json:"holder"`
		State        string `json:"state"`
		Completed    bool   `json:"completed"`
		MergedCommit string `json:"merged_commit"`
		Obligation   struct {
			ID string `json:"id"`
		} `json:"obligation"`
	} `json:"obligations"`
}

func readStatus(
	t *testing.T,
	run func(string, ...string) (int, string, string),
	seat string,
) appStatus {
	t.Helper()
	code, out, stderr := run(seat, "status", "--json")
	if code != 0 {
		t.Fatalf("status failed: code=%d stderr=%s output=%s", code, stderr, out)
	}
	var status appStatus
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		t.Fatal(err)
	}
	return status
}

func obligationID(status appStatus, kind, holder string) string {
	for _, obligation := range status.Obligations {
		if obligation.Kind == kind && obligation.Holder == holder {
			return obligation.Obligation.ID
		}
	}
	return ""
}

func obligationState(status appStatus, kind, holder string) *struct {
	Kind         string `json:"kind"`
	Holder       string `json:"holder"`
	State        string `json:"state"`
	Completed    bool   `json:"completed"`
	MergedCommit string `json:"merged_commit"`
	Obligation   struct {
		ID string `json:"id"`
	} `json:"obligation"`
} {
	for i := range status.Obligations {
		if status.Obligations[i].Kind == kind && status.Obligations[i].Holder == holder {
			return &status.Obligations[i]
		}
	}
	return nil
}

func appFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	gitDir(t, root, "init", "--bare", "-q", "--initial-branch=main", origin)
	repo := filepath.Join(root, "work")
	gitDir(t, root, "clone", "-q", origin, repo)
	gitDir(t, repo, "config", "user.name", "hctl-test")
	gitDir(t, repo, "config", "user.email", "test@hctl.invalid")
	if err := os.MkdirAll(filepath.Join(repo, ".hctl", "assignments"), 0o755); err != nil {
		t.Fatal(err)
	}
	seats := `
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
author_branch_patterns = ["refs/heads/work/claude/*"]
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
`
	assignment := `
schema_version = 1
id = "demo"
kind = "change"
needs = []
base_ref = "refs/heads/main"
[author]
seat = "codex"
branch_slug = "demo"
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
`
	if err := os.WriteFile(filepath.Join(repo, ".hctl", "seats.toml"), []byte(seats), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".hctl", "assignments", "demo.toml"), []byte(assignment), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "memory", "review.md"), []byte("# review\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitDir(t, repo, "add", ".hctl", "memory/review.md")
	gitDir(t, repo, "commit", "-q", "-m", "bootstrap")
	gitDir(t, repo, "push", "-q", "origin", "main")
	return repo, origin
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return gitDir(t, dir, args...)
}

func gitDir(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=hctl-test", "GIT_AUTHOR_EMAIL=test@hctl.invalid",
		"GIT_COMMITTER_NAME=hctl-test", "GIT_COMMITTER_EMAIL=test@hctl.invalid",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
	return string(out)
}

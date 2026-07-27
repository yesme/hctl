package app

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yesme/hctl/internal/config"
	"github.com/yesme/hctl/internal/gitx"
	"github.com/yesme/hctl/internal/identity"
)

// Frozen assignment blob on main for p1-attribution (codex-pr65#P1-01).
// Any self-edit of that file changes the git blob OID and fails this test.
const frozenP1AttributionAssignmentBlob = "3006b502c208cb0fc3c810a795da941d5a442141"

func TestP1AttributionAssignmentBlobUnchanged(t *testing.T) {
	// Mutation sensitivity: restoring the comment-only self-edit must make this red.
	root := findModuleRoot(t)
	path := filepath.Join(root, ".hctl", "assignments", "p1-attribution.toml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	oid := gitBlobOID(raw)
	if oid != frozenP1AttributionAssignmentBlob {
		t.Fatalf("p1-attribution assignment blob drifted: got %s want %s (PR must not edit its own frozen assignment)",
			oid, frozenP1AttributionAssignmentBlob)
	}
}

func gitBlobOID(data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func findModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func TestResolveCoauthorEmailPrecedenceAndOwnership(t *testing.T) {
	seats := config.Seats{
		Seats: map[string]config.Seat{
			"grok":   {CoauthorEmail: "302482056+a-grok-build-bot[bot]@users.noreply.github.com"},
			"claude": {CoauthorEmail: "281844019+a-claude-code-bot[bot]@users.noreply.github.com"},
		},
	}
	local := identity.LocalConfig{
		Seat:          "grok",
		CoauthorEmail: "local-override@example.com",
	}

	t.Setenv("HCTL_COAUTHOR_EMAIL", "")
	email, source, err := ResolveCoauthorEmail("grok", seats, local)
	if err != nil || source != "local" || email != "local-override@example.com" {
		t.Fatalf("local path: email=%q source=%q err=%v", email, source, err)
	}

	emptyLocal := identity.LocalConfig{}
	email, source, err = ResolveCoauthorEmail("grok", seats, emptyLocal)
	if err != nil || source != "seats" || !strings.Contains(email, "a-grok-build-bot") {
		t.Fatalf("seats path: email=%q source=%q err=%v", email, source, err)
	}

	t.Setenv("HCTL_COAUTHOR_EMAIL", "env-override@example.com")
	email, source, err = ResolveCoauthorEmail("grok", seats, local)
	if err != nil || source != "env" || email != "env-override@example.com" {
		t.Fatalf("env path: email=%q source=%q err=%v", email, source, err)
	}

	// Local seat mismatch blocks even when env would otherwise win (P1-03).
	t.Setenv("HCTL_COAUTHOR_EMAIL", "env-override@example.com")
	_, _, err = ResolveCoauthorEmail("grok", seats, identity.LocalConfig{
		Seat:          "claude",
		CoauthorEmail: "local@example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected seat mismatch before env, got %v", err)
	}

	// Cross-seat known bot rejected on every source (shared ownership guard).
	t.Setenv("HCTL_COAUTHOR_EMAIL", "281844019+a-claude-code-bot[bot]@users.noreply.github.com")
	_, _, err = ResolveCoauthorEmail("grok", seats, identity.LocalConfig{Seat: "grok"})
	if err == nil || !strings.Contains(err.Error(), "configured for seat") {
		t.Fatalf("expected cross-seat env rejection, got %v", err)
	}
	t.Setenv("HCTL_COAUTHOR_EMAIL", "")
	_, _, err = ResolveCoauthorEmail("grok", seats, identity.LocalConfig{
		Seat:          "grok",
		CoauthorEmail: "281844019+a-claude-code-bot[bot]@users.noreply.github.com",
	})
	if err == nil || !strings.Contains(err.Error(), "configured for seat") {
		t.Fatalf("expected cross-seat local rejection, got %v", err)
	}

	// Happy: own bot + custom unowned email.
	email, _, err = ResolveCoauthorEmail("grok", seats, identity.LocalConfig{})
	if err != nil || !strings.Contains(email, "a-grok-build-bot") {
		t.Fatalf("own seats email: %q %v", email, err)
	}
	t.Setenv("HCTL_COAUTHOR_EMAIL", "custom@example.com")
	email, source, err = ResolveCoauthorEmail("grok", seats, identity.LocalConfig{Seat: "grok"})
	if err != nil || source != "env" || email != "custom@example.com" {
		t.Fatalf("custom unowned: email=%q source=%q err=%v", email, source, err)
	}

	_, _, err = ResolveCoauthorEmail("unknown", seats, identity.LocalConfig{})
	if err == nil {
		t.Fatal("expected unknown seat error")
	}
}

func TestSeatInitAndTrailerAttribution(t *testing.T) {
	repo, _ := appFixtureWithCoauthors(t)
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		instance := &App{Stdout: &stdout, Stderr: &stderr}
		global := []string{"-C", repo, "--remote", "origin", "--seat", "grok", "--machine", "test-machine"}
		code := instance.Run(context.Background(), append(global, args...))
		return code, stdout.String(), stderr.String()
	}

	t.Setenv("HCTL_MODEL_DISPLAY", "")
	t.Setenv("HCTL_REASONING_EFFORT", "")
	t.Setenv("HCTL_COAUTHOR_EMAIL", "")

	code, _, stderr := run("trailer")
	if code == 0 || !strings.Contains(stderr, "HCTL_MODEL_DISPLAY") {
		t.Fatalf("trailer without model/effort should fail closed: code=%d stderr=%s", code, stderr)
	}

	t.Setenv("HCTL_MODEL_DISPLAY", "Grok 4.5")
	t.Setenv("HCTL_REASONING_EFFORT", "high")
	code, out, stderr := run("trailer")
	if code != 0 {
		t.Fatalf("trailer with seats email failed: code=%d stderr=%s", code, stderr)
	}
	want := "Co-authored-by: Grok 4.5 (high effort) <302482056+a-grok-build-bot[bot]@users.noreply.github.com>"
	if !strings.Contains(out, want) {
		t.Fatalf("trailer output mismatch:\n%s", out)
	}
	if !strings.Contains(stderr, "email_source=seats") {
		t.Fatalf("expected seats email source, stderr=%s", stderr)
	}

	code, out, stderr = run("seat", "init", "--machine-alias", "lab-1")
	if code != 0 {
		t.Fatalf("seat init failed: code=%d stderr=%s out=%s", code, stderr, out)
	}
	if !strings.Contains(out, "seat init ok") || !strings.Contains(out, "coauthor_email=302482056") {
		t.Fatalf("seat init output unexpected: %s", out)
	}
	// Path must be per-worktree git dir, not common dir.
	if !strings.Contains(out, filepath.Join(repo, ".git", "hctl", "local.toml")) {
		t.Fatalf("expected per-worktree local path in output: %s", out)
	}
	localPath := filepath.Join(repo, ".git", "hctl", "local.toml")
	raw, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `seat = "grok"`) || !strings.Contains(string(raw), "machine_alias") {
		t.Fatalf("local.toml content unexpected: %s", raw)
	}

	// Idempotent re-init with custom unowned email
	code, out, stderr = run("seat", "init", "--email", "custom@example.com")
	if code != 0 {
		t.Fatalf("seat init override failed: %s", stderr)
	}
	// seat init with other seat's bot must fail (ownership on producer path)
	code, _, stderr = run("seat", "init", "--email", "281844019+a-claude-code-bot[bot]@users.noreply.github.com")
	if code == 0 || !strings.Contains(stderr, "configured for seat") {
		t.Fatalf("seat init must reject other-seat bot: code=%d stderr=%s", code, stderr)
	}

	code, out, stderr = run("trailer")
	if code != 0 {
		t.Fatalf("trailer after local init failed: %s", stderr)
	}
	if !strings.Contains(out, "<custom@example.com>") {
		t.Fatalf("expected local override email, got: %s", out)
	}
	if !strings.Contains(stderr, "email_source=local") {
		t.Fatalf("expected local email source, stderr=%s", stderr)
	}

	_, out, _ = run("doctor")
	attrLine := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "attribution") {
			attrLine = line
			break
		}
	}
	if !strings.Contains(attrLine, "ok") || !strings.Contains(attrLine, "source=local") {
		t.Fatalf("doctor missing attribution ok line: %q\nfull:\n%s", attrLine, out)
	}
}

// P1-03 original counterexample: same payload trailer vs doctor must not fork.
func TestTrailerDoctorSamePayloadNoForkOnCrossSeatEmail(t *testing.T) {
	repo, _ := appFixtureWithCoauthors(t)
	t.Setenv("HCTL_MODEL_DISPLAY", "Grok 4.5")
	t.Setenv("HCTL_REASONING_EFFORT", "high")
	t.Setenv("HCTL_COAUTHOR_EMAIL", "281844019+a-claude-code-bot[bot]@users.noreply.github.com")

	run := func(cmd string) (int, string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		instance := &App{Stdout: &stdout, Stderr: &stderr}
		code := instance.Run(context.Background(), []string{
			"-C", repo, "--remote", "origin", "--seat", "grok", cmd,
		})
		return code, stdout.String(), stderr.String()
	}

	tCode, tOut, tErr := run("trailer")
	dCode, dOut, dErr := run("doctor")
	if tCode == 0 {
		t.Fatalf("trailer must reject cross-seat bot (original counterexample closed): out=%s err=%s", tOut, tErr)
	}
	if !strings.Contains(tErr, "configured for seat") {
		t.Fatalf("trailer error must name ownership: %s", tErr)
	}
	// Doctor must also surface ownership error (same ResolveCoauthorEmail).
	if !strings.Contains(dOut, "configured for seat") && !strings.Contains(dErr, "configured for seat") {
		t.Fatalf("doctor must use same ownership guard: code=%d out=%s err=%s", dCode, dOut, dErr)
	}
	// Fork closed: trailer no longer exit-0 while doctor exit-1 on this payload.
	if tCode == 0 && dCode != 0 {
		t.Fatal("producer/doctor fork reappeared")
	}
}

// P1-02: linked worktrees must not share local.toml; each seat keeps its bot.
func TestLinkedWorktreeLocalAttributionIsolation(t *testing.T) {
	repo, _ := appFixtureWithCoauthors(t)
	// Create two linked worktrees from the same clone.
	wtGrok := filepath.Join(filepath.Dir(repo), "wt-grok")
	wtCodex := filepath.Join(filepath.Dir(repo), "wt-codex")
	gitDir(t, repo, "branch", "-f", "work/grok/iso", "HEAD")
	gitDir(t, repo, "branch", "-f", "work/codex/iso", "HEAD")
	gitDir(t, repo, "worktree", "add", "-q", wtGrok, "work/grok/iso")
	gitDir(t, repo, "worktree", "add", "-q", wtCodex, "work/codex/iso")

	t.Setenv("HCTL_MODEL_DISPLAY", "model")
	t.Setenv("HCTL_REASONING_EFFORT", "high")
	t.Setenv("HCTL_COAUTHOR_EMAIL", "")

	run := func(dir, seat string, args ...string) (int, string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		instance := &App{Stdout: &stdout, Stderr: &stderr}
		global := []string{"-C", dir, "--remote", "origin", "--seat", seat}
		code := instance.Run(context.Background(), append(global, args...))
		return code, stdout.String(), stderr.String()
	}

	code, out, err := run(wtGrok, "grok", "seat", "init")
	if code != 0 {
		t.Fatalf("grok seat init: %s %s", err, out)
	}
	grokPath := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "path=") {
			grokPath = strings.TrimPrefix(line, "path=")
		}
	}
	code, out, err = run(wtCodex, "codex", "seat", "init")
	if code != 0 {
		t.Fatalf("codex seat init: %s %s", err, out)
	}
	codexPath := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "path=") {
			codexPath = strings.TrimPrefix(line, "path=")
		}
	}
	if grokPath == "" || codexPath == "" || grokPath == codexPath {
		t.Fatalf("linked worktrees must use distinct local.toml paths: grok=%q codex=%q", grokPath, codexPath)
	}
	// Must not land under common dir shared path alone.
	commonLocal := filepath.Join(repo, ".git", "hctl", "local.toml")
	if grokPath == commonLocal && codexPath == commonLocal {
		t.Fatal("both paths collapsed to CommonDir (mutation: CommonDir would fail isolation)")
	}

	// Adjacent happy: each trailer keeps own bot after the other init.
	code, out, err = run(wtGrok, "grok", "trailer")
	if code != 0 || !strings.Contains(out, "a-grok-build-bot") {
		t.Fatalf("grok trailer after dual init: code=%d out=%s err=%s", code, out, err)
	}
	code, out, err = run(wtCodex, "codex", "trailer")
	if code != 0 || !strings.Contains(out, "a-chatgpt-codex-bot") {
		t.Fatalf("codex trailer after dual init: code=%d out=%s err=%s", code, out, err)
	}

	// git --git-path agreement
	gitPath := func(dir string) string {
		t.Helper()
		cmd := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-path", "hctl/local.toml")
		cmd.Dir = dir
		raw, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("git-path: %s %v", raw, e)
		}
		return strings.TrimSpace(string(raw))
	}
	if gitPath(wtGrok) != grokPath {
		t.Fatalf("app path vs git-path grok: app=%s git=%s", grokPath, gitPath(wtGrok))
	}
	if gitPath(wtCodex) != codexPath {
		t.Fatalf("app path vs git-path codex: app=%s git=%s", codexPath, gitPath(wtCodex))
	}

	// Files exist and hold distinct seats.
	gRaw, _ := os.ReadFile(grokPath)
	cRaw, _ := os.ReadFile(codexPath)
	if !strings.Contains(string(gRaw), `seat = "grok"`) || !strings.Contains(string(cRaw), `seat = "codex"`) {
		t.Fatalf("local contents mixed: grok=%s codex=%s", gRaw, cRaw)
	}
}

// P1-04: origin unreachable must not block offline trailer/seat init when local evidence is complete.
func TestAttributionOfflineWhenOriginUnreachable(t *testing.T) {
	repo, _ := appFixtureWithCoauthors(t)
	// Break remote after local seats are already on HEAD.
	gitDir(t, repo, "remote", "set-url", "origin", "https://127.0.0.1:1/unreachable.git")

	t.Setenv("HCTL_MODEL_DISPLAY", "Grok 4.5")
	t.Setenv("HCTL_REASONING_EFFORT", "high")
	t.Setenv("HCTL_COAUTHOR_EMAIL", "")

	run := func(args ...string) (int, string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		instance := &App{Stdout: &stdout, Stderr: &stderr}
		global := []string{"-C", repo, "--remote", "origin", "--seat", "grok"}
		code := instance.Run(context.Background(), append(global, args...))
		return code, stdout.String(), stderr.String()
	}

	code, out, err := run("seat", "init")
	if code != 0 {
		t.Fatalf("seat init must work offline: code=%d err=%s out=%s", code, err, out)
	}
	code, out, err = run("trailer")
	if code != 0 {
		t.Fatalf("trailer must work offline with local evidence: code=%d err=%s out=%s", code, err, out)
	}
	if !strings.Contains(out, "a-grok-build-bot") {
		t.Fatalf("offline trailer shape: %s", out)
	}

	// Reachable vs unreachable is single-variable: doctor may still need network,
	// but attribution producers must not.
}

func TestLocalConfigPathIsGitDirNotCommonDir(t *testing.T) {
	repo, _ := appFixtureWithCoauthors(t)
	r, err := gitx.Open(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	path := identity.LocalConfigPath(r)
	if !strings.HasPrefix(path, r.GitDir) {
		t.Fatalf("local path %q must be under GitDir %q", path, r.GitDir)
	}
	// On a primary worktree GitDir may equal CommonDir; force the structural rule:
	// path must not use CommonDir when GitDir differs (covered by linked-worktree test).
	if r.GitDir != r.CommonDir && strings.HasPrefix(path, r.CommonDir) && !strings.HasPrefix(path, r.GitDir) {
		t.Fatalf("must not place local.toml only under CommonDir when worktrees differ")
	}
}

func appFixtureWithCoauthors(t *testing.T) (string, string) {
	t.Helper()
	repo, origin := appFixture(t)
	seatsPath := filepath.Join(repo, ".hctl", "seats.toml")
	patched := `
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
coauthor_email = "281844019+a-claude-code-bot[bot]@users.noreply.github.com"
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
coauthor_email = "281847692+a-chatgpt-codex-bot[bot]@users.noreply.github.com"
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
coauthor_email = "302482056+a-grok-build-bot[bot]@users.noreply.github.com"
[seats.grok.features]
session_resume = "unknown"
parallel_sessions = "unknown"
subagents = "unknown"
skill = "unknown"
door_file = "AGENTS.md"
`
	if err := os.WriteFile(seatsPath, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	gitDir(t, repo, "add", ".hctl/seats.toml")
	gitDir(t, repo, "commit", "-q", "-m", "seats coauthor emails")
	gitDir(t, repo, "push", "-q", "origin", "main")
	return repo, origin
}

package derive

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yesme/hctl/internal/facts"
	"github.com/yesme/hctl/internal/gitx"
)

// initCutoverRepo builds a real repo whose first-parent history carries the
// given enforcement sequence (root first). Values: "none" (no seats file),
// "bootstrap", "active", "garbage" (undecodable seats file).
func initCutoverRepo(t *testing.T, sequence []string) (*gitx.Repo, []facts.MainCommit) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=c", "GIT_AUTHOR_EMAIL=c@c.invalid",
			"GIT_COMMITTER_NAME=c", "GIT_COMMITTER_EMAIL=c@c.invalid",
			"GIT_AUTHOR_DATE=2026-07-27T00:00:00Z", "GIT_COMMITTER_DATE=2026-07-27T00:00:00Z",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "--initial-branch=main")
	seatsPath := filepath.Join(dir, ".hctl", "seats.toml")
	var history []facts.MainCommit
	for i, enforcement := range sequence {
		switch enforcement {
		case "none":
			_ = os.RemoveAll(filepath.Join(dir, ".hctl"))
		case "garbage":
			if err := os.MkdirAll(filepath.Dir(seatsPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(seatsPath, []byte("not toml ["), 0o644); err != nil {
				t.Fatal(err)
			}
		default:
			if err := os.MkdirAll(filepath.Dir(seatsPath), 0o755); err != nil {
				t.Fatal(err)
			}
			content := fmt.Sprintf("schema_version = 1\nenforcement = %q\n", enforcement)
			if err := os.WriteFile(seatsPath, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		run("add", "-A")
		run("commit", "-q", "--allow-empty", "-m", fmt.Sprintf("c%d %s", i, enforcement))
		oid := run("rev-parse", "HEAD")
		tree := run("rev-parse", "HEAD^{tree}")
		history = append(history, facts.MainCommit{OID: oid, Tree: tree})
	}
	// tip first
	for l, r := 0, len(history)-1; l < r; l, r = l+1, r-1 {
		history[l], history[r] = history[r], history[l]
	}
	repo, err := gitx.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return repo, history
}

func detect(t *testing.T, sequence []string) (*bootstrapCutover, *facts.Problem) {
	t.Helper()
	repo, history := initCutoverRepo(t, sequence)
	engine := Engine{Repo: repo}
	return engine.detectCutover(context.Background(), &facts.Snapshot{MainHistory: history})
}

func TestDetectCutoverShapes(t *testing.T) {
	if cut, problem := detect(t, []string{"none", "bootstrap", "bootstrap"}); cut != nil || problem != nil {
		t.Fatalf("bootstrap era must yield no cutover: %+v %+v", cut, problem)
	}
	if cut, problem := detect(t, []string{"active", "active"}); cut != nil || problem != nil {
		t.Fatalf("genesis-active must yield no cutover and no problem: %+v %+v", cut, problem)
	}
	if cut, problem := detect(t, []string{"bootstrap", "active", "bootstrap"}); cut != nil || problem == nil || problem.Code != invalidCutoverCode {
		t.Fatalf("rollback active→bootstrap must be INVALID_CUTOVER: %+v %+v", cut, problem)
	}
	if cut, problem := detect(t, []string{"bootstrap", "active", "bootstrap", "active"}); cut != nil || problem == nil || problem.Code != invalidCutoverCode {
		t.Fatalf("double flip must be INVALID_CUTOVER: %+v %+v", cut, problem)
	}
	if cut, problem := detect(t, []string{"garbage", "active", "active"}); cut == nil || problem != nil {
		t.Fatalf("undecodable ancestor counts as not-active below the flip: %+v %+v", cut, problem)
	}
}

func TestDetectCutoverBoundary(t *testing.T) {
	repo, history := initCutoverRepo(t, []string{"none", "bootstrap", "active", "active"})
	engine := Engine{Repo: repo}
	cut, problem := engine.detectCutover(context.Background(), &facts.Snapshot{MainHistory: history})
	if problem != nil || cut == nil {
		t.Fatalf("expected cutover: %+v %+v", cut, problem)
	}
	// history is tip-first: [active-tip, active-flip, bootstrap, none]
	if cut.OID != history[1].OID {
		t.Fatalf("cutover must be the deepest active commit: got %s want %s", cut.OID, history[1].OID)
	}
	if !cut.covers(history[1].OID) || !cut.covers(history[2].OID) || !cut.covers(history[3].OID) {
		t.Fatalf("cutover and its first-parent ancestors must be acknowledged: %+v", cut.acknowledged)
	}
	if cut.covers(history[0].OID) {
		t.Fatal("commits after the cutover must not be acknowledged")
	}
	if cut.covers("") {
		t.Fatal("unplaced debt must never be acknowledged")
	}
}

func TestMarkMergeDebtAcknowledgesBootstrapHistory(t *testing.T) {
	engine := Engine{}
	cut := &bootstrapCutover{OID: "c", acknowledged: map[string]bool{"m1": true}}
	state := &ObligationState{}

	inside := &Result{}
	engine.markMergeDebt(inside, []*ObligationState{state}, cut, "m1", "UNRECORDED_MERGE", "d")
	if state.AuditDebt != acknowledgedBootstrapCode {
		t.Fatalf("state must carry acknowledged code: %q", state.AuditDebt)
	}
	if len(inside.Problems) != 1 ||
		inside.Problems[0].Code != acknowledgedBootstrapCode ||
		inside.Problems[0].Severity != facts.SeverityWarning ||
		!strings.Contains(inside.Problems[0].Detail, "UNRECORDED_MERGE") ||
		!strings.Contains(inside.Problems[0].Detail, "cutover c") {
		t.Fatalf("acknowledged debt must be a warning naming the original code and cutover: %+v", inside.Problems)
	}
	inside.Complete = true
	if err := inside.WriteGuard(); err != nil {
		t.Fatalf("acknowledged bootstrap history must not trip WriteGuard: %v", err)
	}

	outside := &Result{}
	engine.markMergeDebt(outside, []*ObligationState{{}}, cut, "m2", "UNRECORDED_MERGE", "d")
	outside.Complete = true
	if err := outside.WriteGuard(); err == nil || !strings.Contains(err.Error(), "UNRECORDED_MERGE") {
		t.Fatalf("post-cutover debt must stay a WriteGuard error: %v", err)
	}

	unplaced := &Result{}
	engine.markMergeDebt(unplaced, []*ObligationState{{}}, cut, "", "UNJUDGEABLE_MERGE", "d")
	unplaced.Complete = true
	if err := unplaced.WriteGuard(); err == nil {
		t.Fatal("unplaced debt must stay a WriteGuard error")
	}

	noCut := &Result{}
	engine.markMergeDebt(noCut, []*ObligationState{{}}, nil, "m1", "UNRECORDED_MERGE", "d")
	noCut.Complete = true
	if err := noCut.WriteGuard(); err == nil {
		t.Fatal("without a cutover nothing is acknowledged")
	}
}

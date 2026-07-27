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
	if cut, problem := detect(t, []string{"garbage", "active", "active"}); cut != nil || problem == nil || problem.Code != invalidCutoverCode {
		t.Fatalf("undecodable seats file must be INVALID_CUTOVER, never a boundary: %+v %+v", cut, problem)
	}
	if cut, problem := detect(t, []string{"none", "bootstrap", "active"}); cut == nil || problem != nil {
		t.Fatalf("provably absent seats path stays a defined non-active happy path: %+v %+v", cut, problem)
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

// deleteSeatsBlob removes the loose object of `.hctl/seats.toml` at the given
// history entry, reproducing codex-pr72#P1-01's real-object mutations.
func deleteSeatsBlob(t *testing.T, repo *gitx.Repo, oid string) {
	t.Helper()
	out, err := repo.Output(context.Background(), "rev-parse", oid+":.hctl/seats.toml")
	if err != nil {
		t.Fatal(err)
	}
	blob := strings.TrimSpace(out)
	path := filepath.Join(repo.CommonDir, "objects", blob[:2], blob[2:])
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func TestDetectCutoverUnknownObjectFailsClosed(t *testing.T) {
	// Mutation A (boundary widening): deleting the mid active blob must not
	// move the cutover to tip — it must refuse to derive any boundary.
	repo, history := initCutoverRepo(t, []string{"bootstrap", "active", "active"})
	deleteSeatsBlob(t, repo, history[1].OID) // mid active
	engine := Engine{Repo: repo}
	cut, problem := engine.detectCutover(context.Background(), &facts.Snapshot{MainHistory: history})
	if cut != nil || problem == nil || problem.Code != invalidCutoverCode {
		t.Fatalf("unreadable active blob must fail closed: %+v %+v", cut, problem)
	}

	// Mutation B (hidden flip): deleting the root active blob must not silence
	// the multi-flip INVALID_CUTOVER.
	repo2, history2 := initCutoverRepo(t, []string{"active", "bootstrap", "active"})
	deleteSeatsBlob(t, repo2, history2[2].OID) // root active
	engine2 := Engine{Repo: repo2}
	cut2, problem2 := engine2.detectCutover(context.Background(), &facts.Snapshot{MainHistory: history2})
	if cut2 != nil || problem2 == nil || problem2.Code != invalidCutoverCode {
		t.Fatalf("unreadable object must not hide an older flip: %+v %+v", cut2, problem2)
	}
}

func TestMarkMergeDebtAllowlist(t *testing.T) {
	engine := Engine{}
	cut := &bootstrapCutover{OID: "c", acknowledged: map[string]bool{"inside": true}}
	rows := []struct {
		code     string
		merged   string
		wantCode string
		wantErr  bool
	}{
		{"UNRECORDED_MERGE", "inside", acknowledgedBootstrapCode, false},
		{"UNRECORDED_MERGE", "outside", "UNRECORDED_MERGE", true},
		{"UNRECORDED_MERGE", "", "UNRECORDED_MERGE", true},
		{"INVALID_RECEIPT", "inside", "INVALID_RECEIPT", true},
		{"INVALID_RECEIPT", "outside", "INVALID_RECEIPT", true},
		{"INVALID_RECEIPT", "", "INVALID_RECEIPT", true},
		{"UNJUDGEABLE_MERGE", "inside", "UNJUDGEABLE_MERGE", true},
		{"UNJUDGEABLE_MERGE", "outside", "UNJUDGEABLE_MERGE", true},
		{"UNJUDGEABLE_MERGE", "", "UNJUDGEABLE_MERGE", true},
	}
	for _, row := range rows {
		state := &ObligationState{}
		result := &Result{Complete: true}
		engine.markMergeDebt(result, []*ObligationState{state}, cut, row.merged, row.code, "d")
		if state.AuditDebt != row.wantCode || len(result.Problems) != 1 || result.Problems[0].Code != row.wantCode {
			t.Fatalf("%s/%s: got debt=%q problems=%+v", row.code, row.merged, state.AuditDebt, result.Problems)
		}
		if err := result.WriteGuard(); (err != nil) != row.wantErr {
			t.Fatalf("%s/%s: WriteGuard=%v want err=%v", row.code, row.merged, err, row.wantErr)
		}
	}
}

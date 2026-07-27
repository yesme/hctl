package store

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yesme/hctl/internal/facts"
	"github.com/yesme/hctl/internal/gitx"
	"github.com/yesme/hctl/internal/protocol"
)

func TestAppendUsesLeaseAndLeavesLoserPending(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	runGit(t, root, nil, "init", "--bare", "-q", origin)
	a := clone(t, root, origin, "a")
	b := clone(t, root, origin, "b")
	repoA := openRepo(t, a)
	repoB := openRepo(t, b)
	payloadA := claimPayload(t, "codex", "a")
	payloadB := claimPayload(t, "codex", "b")
	pubA := Publisher{Repo: repoA, Remote: "origin", Seat: "codex"}
	pubB := Publisher{Repo: repoB, Remote: "origin", Seat: "codex"}
	oidA, err := pubA.Append(ctx, "", payloadA, "CLAIM demo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pubB.Append(ctx, "", payloadB, "CLAIM demo"); err == nil {
		t.Fatal("second empty-expect lease unexpectedly won")
	}
	remote := revParse(t, origin, facts.CoopRef("codex"))
	if remote != oidA {
		t.Fatalf("remote moved after losing lease: got %s want %s", remote, oidA)
	}
	if local, ok, err := repoB.Resolve(ctx, facts.CoopRef("codex")); err != nil || !ok || local == oidA {
		t.Fatalf("loser should retain a distinct pending journal: %s %v %v", local, ok, err)
	}
}

func TestDivergedPendingIsPreservedBeforeAlignment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	runGit(t, root, nil, "init", "--bare", "-q", origin)
	a := clone(t, root, origin, "a")
	b := clone(t, root, origin, "b")
	repoA := openRepo(t, a)
	repoB := openRepo(t, b)
	pubA := Publisher{Repo: repoA, Remote: "origin", Seat: "codex"}
	pubB := Publisher{Repo: repoB, Remote: "origin", Seat: "codex"}
	winner, err := pubA.Append(ctx, "", claimPayload(t, "codex", "a"), "CLAIM winner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pubB.Append(ctx, "", claimPayload(t, "codex", "b"), "CLAIM loser"); err == nil {
		t.Fatal("expected lease loss")
	}
	_, err = repoB.Output(ctx, "fetch", "--no-tags", "origin", facts.CoopRef("codex")+":"+facts.TrackingCoopRef("origin", "codex"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := pubB.Recover(ctx, winner)
	var lost *LostPublicationError
	if err == nil || !errorAs(err, &lost) {
		t.Fatalf("expected lost recovery, got result=%+v err=%v", result, err)
	}
	recovery, ok, err := repoB.Resolve(ctx, result.RecoveryRef)
	if err != nil || !ok || recovery != lost.Pending {
		t.Fatalf("pending was not preserved: %s %v %v", recovery, ok, err)
	}
	aligned, ok, err := repoB.Resolve(ctx, facts.CoopRef("codex"))
	if err != nil || !ok || aligned != winner {
		t.Fatalf("publishing ref not aligned to winner: %s %v %v", aligned, ok, err)
	}
}

func TestPendingAncestorOfRemoteDescendantIsDelivered(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	runGit(t, root, nil, "init", "--bare", "-q", origin)
	a := clone(t, root, origin, "a")
	b := clone(t, root, origin, "b")
	repoA := openRepo(t, a)
	repoB := openRepo(t, b)
	pubA := Publisher{Repo: repoA, Remote: "origin", Seat: "codex"}
	pubB := Publisher{Repo: repoB, Remote: "origin", Seat: "codex"}
	first, err := pubA.Append(ctx, "", claimPayload(t, "codex", "a"), "CLAIM first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repoB.Output(ctx, "fetch", "--no-tags", "origin",
		facts.CoopRef("codex")+":"+facts.TrackingCoopRef("origin", "codex")); err != nil {
		t.Fatal(err)
	}
	second, err := pubB.Append(ctx, first, claimPayload(t, "codex", "b"), "CLAIM second")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repoA.Output(ctx, "fetch", "--no-tags", "origin",
		facts.CoopRef("codex")+":"+facts.TrackingCoopRef("origin", "codex")); err != nil {
		t.Fatal(err)
	}
	result, err := pubA.Recover(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != RecoveryDelivered || result.Publishing != second {
		t.Fatalf("pending ancestor was not classified delivered: %+v", result)
	}
	aligned, ok, err := repoA.Resolve(ctx, facts.CoopRef("codex"))
	if err != nil || !ok || aligned != second {
		t.Fatalf("publishing ref did not fast-forward after delivery: %s %v %v", aligned, ok, err)
	}
}

func claimPayload(t *testing.T, seat, machine string) []byte {
	t.Helper()
	source := protocol.Source{
		Commit: strings.Repeat("b", 40), Path: ".hctl/assignments/demo.toml", Blob: strings.Repeat("a", 40),
	}
	obligation, err := protocol.NewStaticObligation("demo", source.Blob, "author", "demo", nil, source)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := protocol.MarshalClaim(protocol.Claim{
		SchemaVersion:     1,
		Actor:             protocol.Actor{Seat: seat, Machine: machine},
		CreatedAt:         time.Now().UTC().Truncate(time.Second),
		Obligation:        obligation,
		TipAtClaimPresent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func clone(t *testing.T, root, origin, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	runGit(t, root, nil, "clone", "-q", origin, dir)
	runGit(t, dir, nil, "config", "user.name", "hctl-test")
	runGit(t, dir, nil, "config", "user.email", "test@hctl.invalid")
	return dir
}

func openRepo(t *testing.T, dir string) *gitx.Repo {
	t.Helper()
	repo, err := gitx.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func revParse(t *testing.T, repo, ref string) string {
	t.Helper()
	out := runGit(t, repo, nil, "rev-parse", ref)
	return strings.TrimSpace(out)
}

func runGit(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Env = append(cmd.Env,
		"GIT_AUTHOR_NAME=hctl-test", "GIT_AUTHOR_EMAIL=test@hctl.invalid",
		"GIT_COMMITTER_NAME=hctl-test", "GIT_COMMITTER_EMAIL=test@hctl.invalid",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
	return string(out)
}

func errorAs(err error, target **LostPublicationError) bool {
	for err != nil {
		if value, ok := err.(*LostPublicationError); ok {
			*target = value
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			break
		}
		err = u.Unwrap()
	}
	return false
}

package facts

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yesme/hctl/internal/gitx"
	"github.com/yesme/hctl/internal/protocol"
)

func TestReadLinearEventChain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := initRepo(t)
	source := protocol.Source{Commit: strings.Repeat("b", 40), Path: ".hctl/assignments/demo.toml", Blob: strings.Repeat("a", 40)}
	obligation, err := protocol.NewStaticObligation("demo", source.Blob, "author", "demo", nil, source)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := protocol.MarshalClaim(protocol.Claim{
		SchemaVersion:     1,
		Actor:             protocol.Actor{Seat: "codex", Machine: "test"},
		CreatedAt:         time.Now().UTC().Truncate(time.Second),
		Obligation:        obligation,
		TipAtClaimPresent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	blob, err := repo.HashObject(ctx, append(raw, '\n'))
	if err != nil {
		t.Fatal(err)
	}
	tree, err := repo.MakeSingleFileTree(ctx, "event.json", blob)
	if err != nil {
		t.Fatal(err)
	}
	oid, err := repo.CommitTree(ctx, tree, "", "CLAIM demo")
	if err != nil {
		t.Fatal(err)
	}
	chains, problems := readChains(ctx, repo, map[string]string{"codex": oid})
	if len(problems) != 0 {
		t.Fatalf("problems: %+v", problems)
	}
	if got := chains["codex"].Events; len(got) != 1 || got[0].OID != oid {
		t.Fatalf("unexpected events: %+v", got)
	}
}

func TestExtraTreeEntryQuarantinesWholeChain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := initRepo(t)
	blob, err := repo.HashObject(ctx, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	treeInput := []byte("100644 blob " + blob + "\tevent.json\n100644 blob " + blob + "\textra\n")
	tree, err := repo.Run(ctx, treeInput, "mktree")
	if err != nil {
		t.Fatal(err)
	}
	oid, err := repo.CommitTree(ctx, strings.TrimSpace(tree), "", "bad")
	if err != nil {
		t.Fatal(err)
	}
	chains, problems := readChains(ctx, repo, map[string]string{"codex": oid})
	if chains["codex"].Corrupt == nil || len(problems) != 1 || problems[0].Code != "CORRUPT_CHAIN" {
		t.Fatalf("expected quarantine: chain=%+v problems=%+v", chains["codex"], problems)
	}
}

func TestUnknownEventExposesOnlyKnownPrefix(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := initRepo(t)
	source := protocol.Source{Commit: strings.Repeat("b", 40), Path: ".hctl/assignments/demo.toml", Blob: strings.Repeat("a", 40)}
	obligation, err := protocol.NewStaticObligation("demo", source.Blob, "author", "demo", nil, source)
	if err != nil {
		t.Fatal(err)
	}
	known, err := protocol.MarshalClaim(protocol.Claim{
		SchemaVersion: 1, Actor: protocol.Actor{Seat: "codex", Machine: "test"},
		CreatedAt: time.Now().UTC().Truncate(time.Second), Obligation: obligation,
		TipAtClaimPresent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	unknown := []byte(`{
	  "schema_version":1,
	  "type":"FUTURE",
	  "actor":{"seat":"codex","machine":"test","session":null},
	  "created_at":"2026-07-27T00:00:00Z",
	  "control":true
	}`)
	appendEvent := func(parent string, raw []byte) string {
		t.Helper()
		blob, err := repo.HashObject(ctx, append(raw, '\n'))
		if err != nil {
			t.Fatal(err)
		}
		tree, err := repo.MakeSingleFileTree(ctx, "event.json", blob)
		if err != nil {
			t.Fatal(err)
		}
		oid, err := repo.CommitTree(ctx, tree, parent, "event")
		if err != nil {
			t.Fatal(err)
		}
		return oid
	}
	first := appendEvent("", known)
	future := appendEvent(first, unknown)
	last := appendEvent(future, known)
	chains, problems := readChains(ctx, repo, map[string]string{"codex": last})
	chain := chains["codex"]
	if len(problems) != 1 || problems[0].Code != "UNSUPPORTED_FACTS" {
		t.Fatalf("expected unsupported problem, got %+v", problems)
	}
	if len(chain.Events) != 1 || chain.Events[0].OID != first {
		t.Fatalf("reader derived through unknown control fact: %+v", chain.Events)
	}
	if len(chain.Commits) != 3 {
		t.Fatalf("chain object retention lost commits: %+v", chain.Commits)
	}
}

func initRepo(t *testing.T) *gitx.Repo {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=hctl-test", "GIT_AUTHOR_EMAIL=test@hctl.invalid",
			"GIT_COMMITTER_NAME=hctl-test", "GIT_COMMITTER_EMAIL=test@hctl.invalid",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	run("init", "-q")
	run("config", "user.name", "hctl-test")
	run("config", "user.email", "test@hctl.invalid")
	if err := os.WriteFile(filepath.Join(dir, "seed"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "seed")
	run("commit", "-q", "-m", "seed")
	repo, err := gitx.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

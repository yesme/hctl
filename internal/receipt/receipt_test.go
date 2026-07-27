package receipt

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/yesme/hctl/internal/gitx"
)

func TestBodyParseAndVerifySquashCommit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := receiptRepo(t)
	base, ok, err := repo.Resolve(ctx, "HEAD")
	if err != nil || !ok {
		t.Fatalf("resolve base: %s %v %v", base, ok, err)
	}
	if _, err := repo.Output(ctx, "commit", "--allow-empty", "-q", "-m", "head"); err != nil {
		t.Fatal(err)
	}
	head, ok, err := repo.Resolve(ctx, "HEAD")
	if err != nil || !ok {
		t.Fatalf("resolve head: %s %v %v", head, ok, err)
	}
	headObject, err := repo.NewBatch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rawHead, err := headObject.Read(head)
	if err != nil {
		t.Fatal(err)
	}
	parsedHead, err := gitx.ParseCommit(rawHead.Data)
	if err != nil {
		t.Fatal(err)
	}
	if err := headObject.Close(); err != nil {
		t.Fatal(err)
	}
	expected := Receipt{
		Version: 1, Assignment: "demo",
		Obligation: "sha256:" + strings.Repeat("a", 64),
		PR:         7, Base: base, Head: head, MergeClaim: strings.Repeat("b", 40), Method: "squash",
		FactTips: map[string]string{"grok": strings.Repeat("c", 40), "claude": strings.Repeat("d", 40)},
	}
	body, err := expected.Body()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(body, "claude=") > strings.Index(body, "grok=") {
		t.Fatal("fact tips are not deterministically sorted")
	}
	merge, err := repo.CommitTree(ctx, parsedHead.Tree, base, "demo (#7)\n\n"+body)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyCommit(ctx, repo, merge, expected); err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse("subject\n\n" + body)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.FactTips) != 2 || parsed.FactTips["grok"] != expected.FactTips["grok"] {
		t.Fatalf("fact tips did not round trip: %+v", parsed)
	}
}

func TestRejectDuplicateReceiptField(t *testing.T) {
	t.Parallel()
	message := strings.Join([]string{
		"Hctl-Version: 1",
		"Hctl-Version: 1",
		"Hctl-Assignment: demo",
	}, "\n")
	if _, err := Parse(message); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate rejection, got %v", err)
	}
}

func TestRejectMalformedOrUnknownReceiptField(t *testing.T) {
	t.Parallel()
	base := strings.Join([]string{
		"Hctl-Version: 1",
		"Hctl-Assignment: demo",
		"Hctl-Obligation: sha256:" + strings.Repeat("a", 64),
		"Hctl-PR: 7",
		"Hctl-Base: " + strings.Repeat("b", 40),
		"Hctl-Head: " + strings.Repeat("c", 40),
		"Hctl-Merge-Claim: " + strings.Repeat("d", 40),
		"Hctl-Method: squash",
	}, "\n")
	for _, extra := range []string{"Hctl-Unknown: value", "Hctl-Unknown:value"} {
		if _, err := Parse(base + "\n" + extra + "\n"); err == nil {
			t.Errorf("expected closed receipt rejection for %q", extra)
		}
	}
}

func receiptRepo(t *testing.T) *gitx.Repo {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.name", "hctl-test"},
		{"config", "user.email", "test@hctl.invalid"},
		{"commit", "--allow-empty", "-q", "-m", "base"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = os.Environ()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	repo, err := gitx.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

package identity

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yesme/hctl/internal/gitx"
)

func TestLocalConfigRoundTrip(t *testing.T) {
	root := t.TempDir()
	repoDir := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "--initial-branch=main"},
		{"config", "user.name", "t"},
		{"config", "user.email", "t@t.invalid"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	repo, err := gitx.Open(context.Background(), repoDir)
	if err != nil {
		t.Fatal(err)
	}

	empty, err := LoadLocal(repo)
	if err != nil || empty.Seat != "" {
		t.Fatalf("missing file should yield empty config: %+v %v", empty, err)
	}

	want := LocalConfig{
		Seat:          "grok",
		CoauthorEmail: "302482056+a-grok-build-bot[bot]@users.noreply.github.com",
		MachineAlias:  "lab",
	}
	if err := SaveLocal(repo, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadLocal(repo)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("round-trip: got %+v want %+v", got, want)
	}

	if err := SaveLocal(repo, LocalConfig{Seat: "x", CoauthorEmail: "no-at-sign"}); err == nil {
		t.Fatal("expected invalid email rejection")
	}
	if err := ValidateCoauthorEmail("a\nb@c.com"); err == nil {
		t.Fatal("expected newline rejection")
	}
	if err := ValidateCoauthorEmail(""); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty rejection, got %v", err)
	}
}

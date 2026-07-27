package derive

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/yesme/hctl/internal/gitx"
)

func initDeriveRepo(t *testing.T) *gitx.Repo {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", out, err)
	}
	for _, args := range [][]string{
		{"config", "user.name", "hctl-test"},
		{"config", "user.email", "test@hctl.invalid"},
	} {
		cmd = exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = os.Environ()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, ".hctl", "assignments"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hctl", "assignments", "demo.toml"), []byte("id = \"demo\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "memory", "review.md"), []byte("# review\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", ".hctl/assignments/demo.toml", "memory/review.md"},
		{"commit", "-q", "-m", "seed"},
	} {
		cmd = exec.Command("git", args...)
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

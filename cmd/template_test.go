package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTemplateRepoURL(t *testing.T) {
	u, err := templateRepoURL("vapour-computer", "godot")
	if err != nil {
		t.Fatal(err)
	}
	if u != "https://github.com/vapour-computer/vapour-template-godot.git" {
		t.Fatalf("unexpected url: %s", u)
	}
	if _, err := templateRepoURL("vapour-computer", "other"); err == nil {
		t.Fatal("expected error for invalid engine")
	}
}

func TestCloneRemovesGitDirectory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	src := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, src, "init")
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, src, "add", ".")
	runGit(t, src, "commit", "-m", "init")

	dst := filepath.Join(t.TempDir(), "dst")
	if err := clone(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "README.md")); err != nil {
		t.Fatalf("cloned file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, ".git")); !os.IsNotExist(err) {
		t.Fatalf(".git should be removed, got err=%v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Vapour",
		"GIT_AUTHOR_EMAIL=dev@vapour.ink",
		"GIT_COMMITTER_NAME=Vapour",
		"GIT_COMMITTER_EMAIL=dev@vapour.ink",
	)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

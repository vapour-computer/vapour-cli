package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSkillTarget(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/cfg")
	got, err := skillTarget("", false)
	if err != nil || got != filepath.Join("/cfg", "skills", "vapour") {
		t.Fatalf("user target = %q, %v", got, err)
	}
	dir := t.TempDir()
	t.Chdir(dir)
	got, err = skillTarget("", true)
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := filepath.EvalSymlinks(filepath.Join(dir, ".claude", "skills", "vapour")); got != want && got != filepath.Join(dir, ".claude", "skills", "vapour") {
		t.Fatalf("project target = %q", got)
	}
}

func TestInstallSkillClonesAndKeepsGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	src := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, src, "init")
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("---\nname: vapour\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, src, "add", ".")
	runGit(t, src, "commit", "-m", "init")

	dst := filepath.Join(t.TempDir(), "skills", "vapour")
	if err := installSkill(src, dst, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, ".git")); err != nil {
		t.Fatalf("expected .git to be kept for updates: %v", err)
	}
	if err := installSkill(src, dst, false, false); err == nil {
		t.Fatal("expected error when already installed")
	}
	if err := installSkill(src, dst, true, false); err != nil {
		t.Fatalf("--force should replace: %v", err)
	}
}

func TestInstallSkillRejectsUntrustedSource(t *testing.T) {
	err := installSkill("https://example.com/evil/skill.git", filepath.Join(t.TempDir(), "vapour"), false, false)
	if err == nil {
		t.Fatal("expected untrusted source error")
	}
}

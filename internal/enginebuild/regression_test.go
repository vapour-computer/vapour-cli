package enginebuild

import (
	"bytes"
	"context"
	"github.com/vapour-computer/vapour-cli/internal/project"
	"os"
	"path/filepath"
	"testing"
)

func TestRejectRecursiveAssetsOutput(t *testing.T) {
	root := t.TempDir()
	m := &project.Manifest{Project: project.Project{Engine: "bevy"}, Build: project.Build{Dir: "assets/web"}}
	if e := Build(context.Background(), root, m, &bytes.Buffer{}); e == nil {
		t.Fatal("accepted recursive output")
	}
	if _, e := os.Stat(filepath.Join(root, "assets")); !os.IsNotExist(e) {
		t.Fatal("created invalid output")
	}
}
func TestReplaceAssetsRemovesDeletedFiles(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "assets")
	dest := filepath.Join(root, "web/assets")
	write(t, filepath.Join(src, "new"), "new")
	write(t, filepath.Join(dest, "old"), "old")
	if e := replaceAssets(src, dest); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(dest, "old")); !os.IsNotExist(e) {
		t.Fatal("stale asset survived")
	}
	if _, e := os.Stat(filepath.Join(dest, "new")); e != nil {
		t.Fatal(e)
	}
}
func TestUnityFallbackBoot(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "Build/game.wasm.unityweb"), "fixture")
	m := &project.Manifest{Project: project.Project{Engine: "unity"}}
	meta, e := Inspect(root, m, &bytes.Buffer{})
	if e != nil {
		t.Fatal(e)
	}
	if !meta.Files[0].Boot {
		t.Fatal("fallback payload not boot")
	}
}

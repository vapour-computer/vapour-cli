package perfcheck

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vapour-computer/vapour-cli/internal/project"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGodotChecks(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "build/web/index.html"), `<!doctype html><script>const GODOT_THREADS_ENABLED = false;</script>`)
	write(t, filepath.Join(dir, "export_presets.cfg"), "variant/thread_support=false\n")
	write(t, filepath.Join(dir, "project.godot"), `
[physics]
3d/physics_engine="Bullet"
[display]
window/hdr/request_hdr_output=true
[rendering]
scaling_3d/mode=2
viewport/hdr_2d=true
`)
	wasm := make([]byte, 31<<20)
	if err := os.WriteFile(filepath.Join(dir, "build/web/game.wasm"), wasm, 0o644); err != nil {
		t.Fatal(err)
	}
	m := &project.Manifest{Project: project.Project{Engine: "godot"}, Build: project.Build{Dir: "build/web"}}
	findings := Check(dir, m, "")
	if !Contains(findings, "GODOT_THREADS_ENABLED = false") {
		t.Fatal("expected thread warning")
	}
	if !Contains(findings, "thread_support=false") {
		t.Fatal("expected export preset warning")
	}
	if !Contains(findings, "Bullet") {
		t.Fatal("expected Bullet warning")
	}
	if !Contains(findings, "FSR2") {
		t.Fatal("expected FSR2 warning")
	}
}

func TestUnityChecks(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "Build/game.framework.js"), "// Development Build\n")
	write(t, filepath.Join(dir, "Build/game.loader.js"), `{"decompressionFallback":true}`)
	if err := os.WriteFile(filepath.Join(dir, "Build/game.data"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Build/game.wasm"), []byte("wasm"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &project.Manifest{Project: project.Project{Engine: "unity"}, Build: project.Build{Dir: "Build"}}
	findings := Check(dir, m, "")
	if !Contains(findings, "Development Build") {
		t.Fatal("expected development build warning")
	}
	if !Contains(findings, "not precompressed") {
		t.Fatal("expected compression info")
	}
	if !Contains(findings, "decompressionFallback") {
		t.Fatal("expected decompressionFallback info")
	}
}

func TestBevyChecks(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "index.html"), `<link data-trunk rel="rust" data-cargo-profile="dist" />`)
	write(t, filepath.Join(dir, "build/web/index.html"), `<!doctype html><script src="app.js"></script>`)
	write(t, filepath.Join(dir, "Cargo.toml"), `
[profile.dist]
opt-level = "s"
codegen-units = 8
`)
	m := &project.Manifest{Project: project.Project{Engine: "bevy"}, Build: project.Build{Dir: "build/web"}}
	findings := Check(dir, m, "")
	if !Contains(findings, "profile `dist`") {
		t.Fatal("expected profile from source index.html")
	}
	if !Contains(findings, "opt-level") {
		t.Fatal("expected opt-level info")
	}
	if !Contains(findings, "no LTO") {
		t.Fatal("expected lto info")
	}
	if !Contains(findings, "codegen-units") {
		t.Fatal("expected codegen-units info")
	}
	if !Contains(findings, "data-wasm-opt") {
		t.Fatal("expected data-wasm-opt info")
	}
}

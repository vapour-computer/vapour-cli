package guardrails

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vapour-computer/vapour-cli/internal/project"
)

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func manifest(engine string) *project.Manifest {
	return &project.Manifest{Project: project.Project{Engine: engine}, Build: project.Build{Dir: "build"}}
}

func codes(fs []Finding) map[string]Severity {
	m := map[string]Severity{}
	for _, f := range fs {
		m[f.Code] = f.Severity
	}
	return m
}

func TestGodotCSharpBlocks(t *testing.T) {
	d := t.TempDir()
	write(t, d, "project.godot", "[rendering]\nrenderer/rendering_method=\"gl_compatibility\"\n")
	write(t, d, "Game.csproj", "<Project/>")
	f := Check(d, manifest("godot"), Options{})
	if codes(f)["godot_csharp"] != Block {
		t.Fatalf("want godot_csharp block, got %v", f)
	}
	if len(Blocking(f, []string{"godot_csharp"})) != 0 {
		t.Fatal("allow list should clear the block")
	}
}

func TestGodotRendererWarn(t *testing.T) {
	d := t.TempDir()
	write(t, d, "project.godot", "[rendering]\nrenderer/rendering_method=\"forward_plus\"\n")
	if codes(Check(d, manifest("godot"), Options{}))["godot_renderer"] != Warn {
		t.Fatal("want renderer warning")
	}
	write(t, d, "project.godot", "[rendering]\nrenderer/rendering_method=\"forward_plus\"\nrenderer/rendering_method.web=\"gl_compatibility\"\n")
	if _, ok := codes(Check(d, manifest("godot"), Options{}))["godot_renderer"]; ok {
		t.Fatal("web override should clear the warning")
	}
}

func TestUnitySockets(t *testing.T) {
	d := t.TempDir()
	write(t, d, "Assets/Net.cs", "using System.Net.Sockets;")
	write(t, d, "Assets/Plugins/Linux/x.so", "x")
	c := codes(Check(d, manifest("unity"), Options{}))
	if c["raw_sockets"] != Warn || c["native_plugins"] != Warn {
		t.Fatalf("got %v", c)
	}
}

func TestBuildChecks(t *testing.T) {
	d := t.TempDir()
	write(t, d, "build/index.html", `<script src="/game.js"></script>`)
	write(t, d, "build/game.service.worker.js", "x")
	c := codes(Check(d, manifest("godot"), Options{}))
	if c["absolute_paths"] != Warn || c["pwa_service_worker"] != Warn {
		t.Fatalf("got %v", c)
	}
}

func TestLargeFileBlocksUnlessChunked(t *testing.T) {
	es := []entry{{Path: "a.pck", Size: 600 << 20, Read: func(int64) []byte { return nil }}}
	if len(checkBuild(es, "godot", Options{})) == 0 {
		t.Fatal("want block")
	}
	if len(checkBuild(es, "godot", Options{Chunked: true})) != 0 {
		t.Fatal("chunked lifts the limit")
	}
}

func TestSkipsBuildAndLibrary(t *testing.T) {
	d := t.TempDir()
	write(t, d, "Library/Net.cs", "UdpClient")
	write(t, d, "build/Net.cs", "UdpClient")
	if _, ok := codes(Check(d, manifest("unity"), Options{}))["raw_sockets"]; ok {
		t.Fatal("must not scan Library or the build dir")
	}
}

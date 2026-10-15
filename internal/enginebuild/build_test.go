package enginebuild

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/vapour-computer/vapour-cli/internal/project"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(s), 0755); e != nil {
		t.Fatal(e)
	}
}

// Fake installed tools exercise the real subprocess interface. These are adapter
// contract tests, not evidence that Unity/Bevy themselves exported a game.
func TestAdapters(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture executables")
	}
	for _, engine := range []string{"godot", "unity", "bevy"} {
		t.Run(engine, func(t *testing.T) {
			root := t.TempDir()
			bin := t.TempDir()
			dest := filepath.Join(root, "build", "web")
			log := filepath.Join(root, "commands")
			t.Setenv("BUILD_LOG", log)
			t.Setenv("BUILD_DEST", dest)
			t.Setenv("BUILD_ROOT", root)
			script := `#!/bin/sh
printf '%s\n' "$*" >> "$BUILD_LOG"
case "$1" in
 --version) echo 4.7.2.stable.official.test; exit 0;;
 target) echo wasm32-unknown-unknown; exit 0;;
 metadata) printf '{"target_directory":"%s/target","packages":[{"manifest_path":"%s/Cargo.toml","targets":[{"name":"demo","kind":["bin"]}]}]}' "$BUILD_ROOT" "$BUILD_ROOT"; exit 0;;
esac
mkdir -p "$BUILD_DEST"
printf '<html>fixture</html>' > "$BUILD_DEST/index.html"
printf 'wasm fixture' > "$BUILD_DEST/game_bg.wasm"
`
			exe := filepath.Join(bin, "exporter")
			write(t, exe, script)
			for _, env := range []string{"GODOT", "UNITY_EDITOR", "CARGO", "WASM_BINDGEN", "RUSTUP"} {
				t.Setenv(env, exe)
			}
			write(t, filepath.Join(root, "project.godot"), "config_version=5")
			write(t, filepath.Join(root, "export_presets.cfg"), "[preset.0]\nname = \"VapourWeb\"\nplatform = \"Web\"\nvariant/thread_support=0\nprogressive_web_app/enabled=false\n")
			write(t, filepath.Join(root, "Assets/Editor/VapourBuild.cs"), "fixture")
			write(t, filepath.Join(root, "ProjectSettings/ProjectVersion.txt"), "fixture")
			write(t, filepath.Join(root, "Packages/manifest.json"), `{"dependencies":{}}`)
			write(t, filepath.Join(root, "assets/level.bin"), "lazy asset")
			if engine == "godot" {
				t.Setenv("GODOT_WEB_PRESET", "VapourWeb")
			}
			memory := 256
			m := &project.Manifest{Project: project.Project{Engine: engine}, Build: project.Build{Dir: "build/web"}, Runtime: project.Runtime{Threads: true, MemoryMB: &memory}}
			var out bytes.Buffer
			if e := Build(context.Background(), root, m, &out); e != nil {
				t.Fatal(e)
			}
			commands, e := os.ReadFile(log)
			if e != nil {
				t.Fatal(e)
			}
			want := map[string]string{"godot": "--export-release VapourWeb", "unity": "-executeMethod Vapour.Build.Perform", "bevy": "build --release --target wasm32-unknown-unknown"}[engine]
			if !strings.Contains(string(commands), want) {
				t.Fatalf("missing %q in %s", want, commands)
			}
			if engine == "unity" {
				for _, needle := range []string{"-vapourThreads true", "-vapourMemoryMB 256"} {
					if !strings.Contains(string(commands), needle) {
						t.Fatalf("missing %q in %s", needle, commands)
					}
				}
			}
			b, e := os.ReadFile(filepath.Join(root, ".vapour/build.json"))
			if e != nil {
				t.Fatal(e)
			}
			var meta Metadata
			if e := json.Unmarshal(b, &meta); e != nil {
				t.Fatal(e)
			}
			for _, f := range meta.Files {
				if f.Path == "index.html" && !f.Boot {
					t.Fatal("index not boot")
				}
				if strings.HasPrefix(f.Path, "assets/") && f.Boot {
					t.Fatal("Bevy assets not lazy")
				}
			}
		})
	}
}

func TestInspectRulesAndWarning(t *testing.T) {
	for _, engine := range []string{"godot", "unity", "bevy"} {
		t.Run(engine, func(t *testing.T) {
			root := t.TempDir()
			write(t, filepath.Join(root, "index.html"), "index")
			name := map[string]string{"godot": "main.pck", "unity": "Build/game.data", "bevy": "assets/level.bin"}[engine]
			p := filepath.Join(root, name)
			write(t, p, "")
			if e := os.Truncate(p, 2<<20); e != nil {
				t.Fatal(e)
			}
			m := &project.Manifest{Project: project.Project{Engine: engine}, Build: project.Build{SplitWarnMiB: 1}}
			var out bytes.Buffer
			meta, e := Inspect(root, m, &out)
			if e != nil {
				t.Fatal(e)
			}
			if !strings.Contains(out.String(), "Warning:") {
				t.Fatal("no size warning")
			}
			for _, f := range meta.Files {
				if f.Path == name && f.Boot != (engine != "bevy") {
					t.Fatal("wrong boot rule")
				}
			}
			m.Build.Boot = []string{"index.html"}
			meta, e = Inspect(root, m, &out)
			if e != nil {
				t.Fatal(e)
			}
			for _, f := range meta.Files {
				if f.Path == name && f.Boot {
					t.Fatal("boot override ignored")
				}
			}
		})
	}
}

func TestValidation(t *testing.T) {
	root := t.TempDir()
	for _, b := range []project.Build{{Dir: "../escape"}, {Dir: "."}, {Dir: "build", Command: "invalid"}, {Dir: "build", Command: "custom"}, {Dir: "build", Boot: []string{"["}}, {Dir: "build", SplitWarnMiB: -1}} {
		m := &project.Manifest{Build: b}
		if e := Build(context.Background(), root, m, &bytes.Buffer{}); e == nil {
			t.Fatalf("accepted %+v", b)
		}
	}
}

func TestMissingAndFailingTools(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	root := t.TempDir()
	t.Setenv("GODOT", filepath.Join(root, "missing"))
	m := &project.Manifest{Project: project.Project{Engine: "godot"}, Build: project.Build{Dir: "build"}}
	if e := Build(context.Background(), root, m, &bytes.Buffer{}); e == nil {
		t.Fatal("missing tool accepted")
	}
	exe := filepath.Join(root, "fail")
	write(t, exe, "#!/bin/sh\nexit 7\n")
	m.Build.Command = "custom"
	m.Build.Args = []string{exe}
	if e := Build(context.Background(), root, m, &bytes.Buffer{}); e == nil {
		t.Fatal("export failure ignored")
	}
}

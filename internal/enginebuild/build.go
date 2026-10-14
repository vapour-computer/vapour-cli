// Package enginebuild runs installed exporters; it never installs tools.
package enginebuild

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vapour-computer/vapour-cli/internal/project"
)

const SplitGuide = "https://github.com/vapour-computer/vapour-docs/tree/main/docs/engines"

type Entry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Boot bool   `json:"boot"`
}
type Metadata struct {
	Schema int     `json:"schema"`
	Engine string  `json:"engine"`
	Files  []Entry `json:"files"`
}

func tool(env, name string) (string, error) {
	if p := os.Getenv(env); env != "" && p != "" {
		return exec.LookPath(p)
	}
	p, e := exec.LookPath(name)
	if e != nil {
		return "", fmt.Errorf("%s is required; install it yourself or set %s", name, env)
	}
	return p, nil
}
func run(ctx context.Context, dir string, out io.Writer, exe string, args ...string) error {
	c := exec.CommandContext(ctx, exe, args...)
	c.Dir = dir
	c.Stdout = out
	c.Stderr = out
	if e := c.Run(); e != nil {
		return fmt.Errorf("%s failed: %w", filepath.Base(exe), e)
	}
	return nil
}
func require(p string) error {
	if _, e := os.Stat(p); e != nil {
		return fmt.Errorf("required build input %s: %w", p, e)
	}
	return nil
}

func Build(ctx context.Context, dir string, m *project.Manifest, out io.Writer) error {
	root, e := filepath.Abs(dir)
	if e != nil {
		return e
	}
	if m.Build.Dir == "" {
		return fmt.Errorf("build.dir is required")
	}
	dest := filepath.Join(root, m.Build.Dir)
	rel, e := filepath.Rel(root, dest)
	if e != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(m.Build.Dir) {
		return fmt.Errorf("build.dir must be a subdirectory of the project")
	}
	if m.Project.Engine == "bevy" && (rel == "assets" || strings.HasPrefix(rel, "assets"+string(os.PathSeparator))) {
		return fmt.Errorf("Bevy build.dir must not be inside the source assets directory")
	}
	for p := dest; p != root; p = filepath.Dir(p) {
		st, err := os.Lstat(p)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("build.dir must not traverse symlinks")
		}
	}
	if m.Build.SplitWarnMiB < 0 || m.Build.SplitWarnMiB > 4096 {
		return fmt.Errorf("split_warn_mib must be between 0 (default 1024) and 4096")
	}
	for _, g := range m.Build.Boot {
		if _, e := filepath.Match(g, ""); e != nil {
			return fmt.Errorf("boot glob: %w", e)
		}
	}
	mode := m.Build.Command
	if mode == "" {
		mode = "auto"
	}
	switch mode {
	case "none":
	case "custom":
		if len(m.Build.Args) == 0 {
			return fmt.Errorf("command=custom requires build.args (executable followed by arguments)")
		}
		if e := run(ctx, root, out, m.Build.Args[0], m.Build.Args[1:]...); e != nil {
			return e
		}
	case "auto":
		if e := os.MkdirAll(dest, 0755); e != nil {
			return e
		}
		switch m.Project.Engine {
		case "godot":
			e = godot(ctx, root, dest, out)
		case "unity":
			e = unity(ctx, root, dest, out)
		case "bevy":
			e = bevy(ctx, root, dest, out)
		default:
			return fmt.Errorf("automatic exporter unavailable for %q; use command=custom or none", m.Project.Engine)
		}
		if e != nil {
			return e
		}
	default:
		return fmt.Errorf("build.command must be auto, custom or none")
	}
	if e := require(filepath.Join(dest, "index.html")); e != nil {
		return e
	}
	meta, e := Inspect(dest, m, out)
	if e != nil {
		return e
	}
	b, e := json.MarshalIndent(meta, "", "  ")
	if e != nil {
		return e
	}
	if e := os.MkdirAll(filepath.Join(root, ".vapour"), 0755); e != nil {
		return e
	}
	if e := os.WriteFile(filepath.Join(root, ".vapour", "build.json"), b, 0644); e != nil {
		return e
	}
	fmt.Fprintf(out, "Web build ready: %s (%d files); boot metadata: .vapour/build.json\n", m.Build.Dir, len(meta.Files))
	return nil
}

func godot(ctx context.Context, root, dest string, out io.Writer) error {
	exe, e := tool("GODOT", "godot")
	if e != nil {
		return e
	}
	if e := require(filepath.Join(root, "project.godot")); e != nil {
		return e
	}
	preset, e := os.ReadFile(filepath.Join(root, "export_presets.cfg"))
	if e != nil {
		return fmt.Errorf("Godot requires export_presets.cfg with a Web preset: %w", e)
	}
	// Match name and platform in the same preset section, tolerating whitespace.
	valid := false
	for _, section := range strings.Split(string(preset), "[preset.") {
		lines := strings.Split(section, "\n")
		name, platform := false, false
		for _, line := range lines {
			key, value, ok := strings.Cut(line, "=")
			if ok {
				key = strings.TrimSpace(key)
				value = strings.TrimSpace(value)
				name = name || (key == "name" && value == `"Web"`)
				platform = platform || (key == "platform" && value == `"Web"`)
			}
		}
		valid = valid || (name && platform)
	}
	if !valid {
		return fmt.Errorf("Godot requires a preset named Web with platform Web")
	}
	c := exec.CommandContext(ctx, exe, "--version")
	c.Dir = root
	b, e := c.Output()
	if e != nil {
		return fmt.Errorf("Godot version check: %w", e)
	}
	version := strings.TrimSpace(string(b))
	parts := strings.Split(version, ".")
	if len(parts) < 4 || parts[0] != "4" {
		return fmt.Errorf("Godot 4 required, got %q", version)
	}
	// Godot itself resolves platform-specific template paths and produces the
	// authoritative missing-template error; --export-release performs that check.
	fmt.Fprintf(out, "Godot %s; Web export templates must match the editor.\n", version)
	if e := run(ctx, root, out, exe, "--headless", "--path", root, "--import"); e != nil {
		return e
	}
	return run(ctx, root, out, exe, "--headless", "--path", root, "--export-release", "Web", filepath.Join(dest, "index.html"))
}

func unity(ctx context.Context, root, dest string, out io.Writer) error {
	exe, e := tool("UNITY_EDITOR", "Unity")
	if e != nil {
		return e
	}
	if e := require(filepath.Join(root, "Assets", "Editor", "VapourBuild.cs")); e != nil {
		return fmt.Errorf("install the Vapour/Build editor script from vapour create: %w", e)
	}
	if e := require(filepath.Join(root, "ProjectSettings", "ProjectVersion.txt")); e != nil {
		return e
	}
	packages, e := os.ReadFile(filepath.Join(root, "Packages", "manifest.json"))
	if e != nil {
		return e
	}
	var p struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if e := json.Unmarshal(packages, &p); e != nil {
		return e
	}
	if p.Dependencies["com.unity.addressables"] == "" {
		fmt.Fprintln(out, "Warning: Addressables is not configured; split large content into remote Addressables bundles. "+SplitGuide+"/unity.md")
	}
	return run(ctx, root, out, exe, "-batchmode", "-quit", "-nographics", "-projectPath", root, "-buildTarget", "WebGL", "-executeMethod", "Vapour.Build.Perform", "-vapourOutput", dest, "-logFile", "-")
}

func bevy(ctx context.Context, root, dest string, out io.Writer) error {
	cargo, e := tool("CARGO", "cargo")
	if e != nil {
		return e
	}
	bindgen, e := tool("WASM_BINDGEN", "wasm-bindgen")
	if e != nil {
		return e
	}
	rustup, e := tool("RUSTUP", "rustup")
	if e != nil {
		return e
	}
	c := exec.CommandContext(ctx, rustup, "target", "list", "--installed")
	c.Dir = root
	b, e := c.Output()
	if e != nil {
		return e
	}
	if !strings.Contains(string(b), "wasm32-unknown-unknown") {
		return fmt.Errorf("install wasm32-unknown-unknown using rustup before building")
	}
	c = exec.CommandContext(ctx, cargo, "metadata", "--no-deps", "--format-version", "1")
	c.Dir = root
	b, e = c.Output()
	if e != nil {
		return fmt.Errorf("cargo metadata: %w", e)
	}
	var metadata struct {
		TargetDirectory string `json:"target_directory"`
		Packages        []struct {
			ManifestPath string `json:"manifest_path"`
			Targets      []struct {
				Name string   `json:"name"`
				Kind []string `json:"kind"`
			} `json:"targets"`
		} `json:"packages"`
	}
	if e = json.Unmarshal(b, &metadata); e != nil {
		return e
	}
	var names []string
	for _, p := range metadata.Packages {
		if filepath.Clean(p.ManifestPath) == filepath.Join(root, "Cargo.toml") {
			for _, t := range p.Targets {
				for _, k := range t.Kind {
					if k == "bin" {
						names = append(names, t.Name)
					}
				}
			}
		}
	}
	if len(names) != 1 {
		return fmt.Errorf("Bevy auto build requires one binary in the root package; use custom for workspaces/multiple binaries")
	}
	if e := run(ctx, root, out, cargo, "build", "--release", "--target", "wasm32-unknown-unknown", "--bin", names[0]); e != nil {
		return e
	}
	wasm := filepath.Join(metadata.TargetDirectory, "wasm32-unknown-unknown", "release", names[0]+".wasm")
	if e := run(ctx, root, out, bindgen, "--target", "web", "--out-dir", dest, "--out-name", "game", wasm); e != nil {
		return e
	}
	if opt, e := exec.LookPath("wasm-opt"); e == nil {
		if e := run(ctx, root, out, opt, "-Oz", filepath.Join(dest, "game_bg.wasm"), "-o", filepath.Join(dest, "game_bg.wasm")); e != nil {
			return e
		}
	} else {
		fmt.Fprintln(out, "wasm-opt not installed; skipping optional optimization.")
	}
	assets := filepath.Join(root, "assets")
	if _, e := os.Stat(assets); e == nil {
		if e := replaceAssets(assets, filepath.Join(dest, "assets")); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	return os.WriteFile(filepath.Join(dest, "index.html"), []byte(`<!doctype html><meta charset="utf-8"><title>Vapour game</title><script type="module">import init from './game.js'; init().catch(console.error);</script>`), 0644)
}

func copyAssets(src, dest string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(src, p)
		if e != nil {
			return e
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("non-regular asset %s", p)
		}
		in, e := os.Open(p)
		if e != nil {
			return e
		}
		defer in.Close()
		out, e := os.Create(target)
		if e != nil {
			return e
		}
		_, e = io.Copy(out, in)
		ce := out.Close()
		if e != nil {
			return e
		}
		return ce
	})
}

// Stage the asset tree first so failed copies preserve the previous build and
// deleted source assets cannot leak into a subsequent publication.
func replaceAssets(src, dest string) error {
	stage, e := os.MkdirTemp(filepath.Dir(dest), ".vapour-assets-*")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	if e := copyAssets(src, stage); e != nil {
		return e
	}
	if e := os.RemoveAll(dest); e != nil {
		return e
	}
	return os.Rename(stage, dest)
}

func Inspect(root string, m *project.Manifest, out io.Writer) (Metadata, error) {
	meta := Metadata{Schema: 1, Engine: m.Project.Engine}
	threshold := m.Build.SplitWarnMiB
	if threshold == 0 {
		threshold = 1024
	}
	e := filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("non-regular export %s", p)
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		st, e := d.Info()
		if e != nil {
			return e
		}
		ext := strings.ToLower(filepath.Ext(rel))
		boot := rel == "index.html" || ext == ".js" || ext == ".wasm" || (m.Project.Engine == "godot" && ext == ".pck") || (m.Project.Engine == "unity" && (ext == ".data" || ext == ".br" || ext == ".gz" || ext == ".unityweb"))
		if m.Project.Engine == "bevy" && strings.HasPrefix(rel, "assets/") {
			boot = false
		}
		if len(m.Build.Boot) > 0 {
			boot = false
			for _, g := range m.Build.Boot {
				ok, e := filepath.Match(g, rel)
				if e != nil {
					return e
				}
				boot = boot || ok
			}
		}
		if st.Size() > threshold<<20 {
			fmt.Fprintf(out, "Warning: %s is %.1f MiB; WASM memory is limited. Split packs/bundles: %s/%s.md\n", rel, float64(st.Size())/(1<<20), SplitGuide, m.Project.Engine)
		}
		meta.Files = append(meta.Files, Entry{rel, st.Size(), boot})
		return nil
	})
	return meta, e
}

package perfcheck

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/vapour-computer/vapour-cli/internal/project"
)

type Severity string

const (
	Warn Severity = "warn"
	Info Severity = "info"
)

type Finding struct {
	Severity Severity
	Message  string
	FixHint  string
}

type buildEntry struct {
	Path string
	Size int64
	Read func(limit int64) ([]byte, error)
}

func Check(gameDir string, m *project.Manifest, engineOverride string) []Finding {
	engine := strings.ToLower(strings.TrimSpace(engineOverride))
	if engine == "" {
		engine = strings.ToLower(strings.TrimSpace(m.Project.Engine))
	}
	buildPath := filepath.Join(gameDir, m.Build.Dir)
	entries := collectBuildEntries(buildPath)
	findings := make([]Finding, 0)
	switch engine {
	case "godot":
		findings = append(findings, checkGodot(gameDir, entries)...)
	case "unity":
		findings = append(findings, checkUnity(entries)...)
	case "bevy":
		findings = append(findings, checkBevy(gameDir, entries)...)
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Severity == findings[j].Severity {
			return findings[i].Message < findings[j].Message
		}
		return findings[i].Severity == Warn
	})
	return findings
}

func collectBuildEntries(buildPath string) []buildEntry {
	st, err := os.Stat(buildPath)
	if err != nil {
		return nil
	}
	if st.IsDir() {
		out := make([]buildEntry, 0)
		_ = filepath.WalkDir(buildPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(buildPath, path)
			if err != nil {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			p := path
			out = append(out, buildEntry{
				Path: rel,
				Size: info.Size(),
				Read: func(limit int64) ([]byte, error) {
					f, err := os.Open(p)
					if err != nil {
						return nil, err
					}
					defer f.Close()
					return io.ReadAll(io.LimitReader(f, limit))
				},
			})
			return nil
		})
		return out
	}
	if !strings.HasSuffix(strings.ToLower(buildPath), ".zip") {
		return nil
	}
	zr, err := zip.OpenReader(buildPath)
	if err != nil {
		return nil
	}
	defer zr.Close()
	out := make([]buildEntry, 0, len(zr.File))
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		zf := f
		out = append(out, buildEntry{
			Path: filepath.ToSlash(zf.Name),
			Size: int64(zf.UncompressedSize64),
			Read: func(limit int64) ([]byte, error) {
				r, err := zf.Open()
				if err != nil {
					return nil, err
				}
				defer r.Close()
				return io.ReadAll(io.LimitReader(r, limit))
			},
		})
	}
	return out
}

func checkGodot(gameDir string, entries []buildEntry) []Finding {
	findings := make([]Finding, 0)
	if html := readBuildText(entries, "index.html", 2<<20); strings.Contains(html, "GODOT_THREADS_ENABLED = false") {
		findings = append(findings, Finding{Warn, "Godot export is single-threaded (`GODOT_THREADS_ENABLED = false`).", "Enable Thread Support in the Web export preset (`variant/thread_support=true`)."})
	}
	if export := safeRead(filepath.Join(gameDir, "export_presets.cfg")); strings.Contains(strings.ToLower(export), "variant/thread_support=false") {
		findings = append(findings, Finding{Warn, "`export_presets.cfg` has `variant/thread_support=false`.", "Set `variant/thread_support=true` for web exports."})
	}
	proj := parseSimpleGodot(filepath.Join(gameDir, "project.godot"))
	physics := strings.Trim(proj["3d/physics_engine"], "\"")
	switch physics {
	case "":
		findings = append(findings, Finding{Info, "`project.godot` has no explicit 3D physics engine.", "Set `3d/physics_engine=\"Jolt Physics\"` for web perf."})
	case "GodotPhysics3D", "Jolt Physics":
	default:
		sev := Info
		if strings.EqualFold(physics, "Bullet") {
			sev = Warn
		}
		findings = append(findings, Finding{sev, fmt.Sprintf("`3d/physics_engine=%q` is not a recommended Godot 4 web setting.", physics), "Use `Jolt Physics` (or `GodotPhysics3D`)."})
	}
	if isTrue(proj["window/hdr/request_hdr_output"]) && !hasWebOverride(proj, "window/hdr/request_hdr_output") {
		findings = append(findings, Finding{Warn, "`window/hdr/request_hdr_output=true` has no `.web` override.", "Set `window/hdr/request_hdr_output.web=false` for web."})
	}
	if (proj["scaling_3d/mode"] == "2" || strings.Contains(strings.ToLower(proj["scaling_3d/mode"]), "metal")) && !hasWebOverride(proj, "scaling_3d/mode") {
		findings = append(findings, Finding{Warn, "`scaling_3d/mode` uses FSR2/MetalFX without a `.web` override.", "Set `scaling_3d/mode.web=0` (bilinear) for web."})
	}
	if isTrue(proj["viewport/hdr_2d"]) && !hasWebOverride(proj, "viewport/hdr_2d") {
		findings = append(findings, Finding{Info, "`viewport/hdr_2d=true` has no `.web` override.", "Set `viewport/hdr_2d.web=false` for web."})
	}
	appendLargeFileFinding(&findings, entries, ".wasm", 30<<20, "Wasm")
	appendLargeFileFinding(&findings, entries, ".pck", 256<<20, "PCK")
	return findings
}

func checkUnity(entries []buildEntry) []Finding {
	findings := make([]Finding, 0)
	devBuild := false
	decompressionFallback := false
	hasPlainData := false
	hasPlainWasm := false
	nameSet := map[string]struct{}{}
	for _, e := range entries {
		lower := strings.ToLower(e.Path)
		nameSet[lower] = struct{}{}
		if strings.Contains(lower, "development") {
			devBuild = true
		}
		if strings.HasSuffix(lower, ".framework.js") || strings.HasSuffix(lower, ".loader.js") || strings.HasSuffix(lower, ".json") {
			if text, err := e.Read(2 << 20); err == nil {
				t := strings.ToLower(string(text))
				if strings.Contains(t, "development build") || strings.Contains(t, "developmentbuild\":true") {
					devBuild = true
				}
				if strings.Contains(t, "decompressionfallback") {
					decompressionFallback = true
				}
			}
		}
		if strings.HasSuffix(lower, ".data") {
			hasPlainData = true
		}
		if strings.HasSuffix(lower, ".wasm") {
			hasPlainWasm = true
		}
	}
	if devBuild {
		findings = append(findings, Finding{Warn, "Unity build looks like a Development Build.", "Use a non-development build and set Code Optimization to Runtime Speed."})
	}
	if (hasPlainData && !hasCompressedVariant(nameSet, ".data")) || (hasPlainWasm && !hasCompressedVariant(nameSet, ".wasm")) {
		findings = append(findings, Finding{Info, "Unity data/wasm files are not precompressed.", "Enable Brotli or Gzip (or `.unityweb`) in Unity publishing settings."})
	}
	if decompressionFallback {
		findings = append(findings, Finding{Info, "Unity loader has `decompressionFallback` enabled.", "Disable fallback where possible; it slows startup."})
	}
	return findings
}

func checkBevy(gameDir string, entries []buildEntry) []Finding {
	findings := make([]Finding, 0)
	profileName := "release"
	index := safeRead(filepath.Join(gameDir, "index.html"))
	if index == "" {
		index = readBuildText(entries, "index.html", 2<<20)
	}
	if m := regexp.MustCompile(`data-cargo-profile\s*=\s*"([^"]+)"`).FindStringSubmatch(index); len(m) == 2 {
		profileName = strings.TrimSpace(m[1])
	}
	if strings.Contains(index, `data-trunk`) && strings.Contains(index, `rel="rust"`) && !strings.Contains(index, `data-wasm-opt`) {
		findings = append(findings, Finding{Info, "Trunk Rust link has no `data-wasm-opt`.", "Add `data-wasm-opt=\"3\"` (or similar) on the Rust link."})
	}
	if raw := safeRead(filepath.Join(gameDir, "Cargo.toml")); raw != "" {
		var cfg map[string]any
		if err := toml.Unmarshal([]byte(raw), &cfg); err == nil {
			profile := readProfile(cfg, profileName)
			if opt := strings.Trim(strings.TrimSpace(asString(profile["opt-level"])), "\""); opt == "s" || opt == "z" {
				findings = append(findings, Finding{Info, fmt.Sprintf("Bevy web profile `%s` uses `opt-level=%q`.", profileName, opt), "Use `opt-level = 3` for speed-focused web builds."})
			}
			if !isTruthy(profile["lto"]) {
				findings = append(findings, Finding{Info, fmt.Sprintf("Bevy web profile `%s` has no LTO.", profileName), "Set `lto = true` (or `\"fat\"`) for faster runtime."})
			}
			if v, ok := asInt(profile["codegen-units"]); !ok || v > 1 {
				findings = append(findings, Finding{Info, fmt.Sprintf("Bevy web profile `%s` has `codegen-units` unset or > 1.", profileName), "Set `codegen-units = 1` for faster runtime code."})
			}
		}
	}
	appendLargeFileFinding(&findings, entries, ".wasm", 30<<20, "Wasm")
	return findings
}

func readProfile(cfg map[string]any, profile string) map[string]any {
	profiles, _ := cfg["profile"].(map[string]any)
	p, _ := profiles[profile].(map[string]any)
	if p == nil {
		return map[string]any{}
	}
	return p
}

func safeRead(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func readBuildText(entries []buildEntry, suffix string, limit int64) string {
	for _, e := range entries {
		if strings.EqualFold(filepath.Base(e.Path), suffix) || strings.EqualFold(e.Path, suffix) {
			b, err := e.Read(limit)
			if err == nil {
				return string(b)
			}
		}
	}
	return ""
}

func parseSimpleGodot(path string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(safeRead(path), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}

func hasWebOverride(m map[string]string, key string) bool {
	_, ok := m[key+".web"]
	return ok
}

func isTrue(v string) bool {
	v = strings.TrimSpace(strings.ToLower(strings.Trim(v, "\"")))
	return v == "true" || v == "1"
}

func appendLargeFileFinding(findings *[]Finding, entries []buildEntry, ext string, threshold int64, label string) {
	for _, e := range entries {
		if strings.HasSuffix(strings.ToLower(e.Path), ext) && e.Size > threshold {
			*findings = append(*findings, Finding{Info, fmt.Sprintf("%s file `%s` is %.1f MiB.", label, e.Path, float64(e.Size)/(1<<20)), fmt.Sprintf("Reduce %s size for faster startup.", label)})
		}
	}
}

func hasCompressedVariant(names map[string]struct{}, ext string) bool {
	for name := range names {
		if strings.HasSuffix(name, ext+".br") || strings.HasSuffix(name, ext+".gz") || strings.HasSuffix(name, ext+".unityweb") {
			return true
		}
	}
	return false
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		return fmt.Sprint(v)
	}
}

func isTruthy(v any) bool {
	s := strings.ToLower(strings.TrimSpace(asString(v)))
	return s == "true" || s == "fat" || s == "thin" || s == "1"
}

func asInt(v any) (int, bool) {
	s := strings.TrimSpace(asString(v))
	if s == "" {
		return 0, false
	}
	i, err := strconv.Atoi(strings.Trim(s, "\""))
	if err != nil {
		return 0, false
	}
	return i, true
}

func Contains(findings []Finding, needle string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	for _, f := range findings {
		if strings.Contains(strings.ToLower(f.Message), needle) || strings.Contains(strings.ToLower(f.FixHint), needle) {
			return true
		}
	}
	return false
}

func FormatWarnings(findings []Finding) []string {
	if len(findings) == 0 {
		return nil
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		msg := bytes.TrimSpace([]byte(f.Message))
		line := fmt.Sprintf("[%s] %s", strings.ToUpper(string(f.Severity)), string(msg))
		if strings.TrimSpace(f.FixHint) != "" {
			line += " Fix: " + strings.TrimSpace(f.FixHint)
		}
		lines = append(lines, line)
	}
	return lines
}

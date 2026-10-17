// Package guardrails flags things Vapour cannot run, or can only run with extra work.
//
// Block findings are things the browser or the engine's web export cannot do; publish refuses them
// unless the developer lists the code under [guardrails] allow in vapour.toml. Warn findings are
// fixable on the platform side or need game-side work (see TODO.md).
package guardrails

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/vapour-computer/vapour-cli/internal/project"
)

type Severity string

const (
	Block Severity = "block"
	Warn  Severity = "warn"
	Info  Severity = "info"
)

type Finding struct {
	Severity Severity
	Code     string
	Message  string
	Hint     string
	Path     string
}

// Options tunes limits that depend on how the build is uploaded.
type Options struct {
	Chunked bool // --experimental-chunks lifts the per-file and ZIP upload limits
}

const (
	maxFileBytes   = 512 << 20
	maxExpandBytes = 1 << 30
	maxSourceRead  = 1 << 20
	maxSourceFiles = 20000
)

var skipDirs = map[string]bool{
	".git": true, ".godot": true, ".import": true, "Library": true, "Temp": true, "Logs": true,
	"node_modules": true, "target": true, ".vapour": true, "obj": true, "Packages": true,
}

type sourceRule struct {
	code, engine string
	exts         []string
	re           *regexp.Regexp
	sev          Severity
	msg, hint    string
}

var sourceRules = []sourceRule{
	{"raw_sockets", "unity", []string{".cs"}, regexp.MustCompile(`System\.Net\.Sockets|\bUdpClient\b|\bTcpClient\b|\bTcpListener\b|Ignorance|Telepathy`), Warn,
		"Source uses raw TCP/UDP sockets, which browsers cannot open.",
		"Switch to a WebSocket or WebRTC transport (for Mirror: SimpleWeb or WebSocket transport) and guard the socket code with #if !UNITY_WEBGL."},
	{"raw_sockets", "godot", []string{".gd", ".cs"}, regexp.MustCompile(`\bPacketPeerUDP\b|\bUDPServer\b|\bTCPServer\b|\bStreamPeerTCP\b|ENetMultiplayerPeer`), Warn,
		"Source uses raw TCP/UDP/ENet networking, which browsers cannot open.",
		"Use WebSocketMultiplayerPeer or WebRTCMultiplayerPeer."},
	{"raw_sockets", "bevy", []string{".rs"}, regexp.MustCompile(`std::net::(UdpSocket|TcpStream|TcpListener)|bevy_renet|bevy_quinnet`), Warn,
		"Source uses native sockets, which wasm32 builds cannot open.",
		"Use WebSocket or WebRTC (for example matchbox) behind a cfg(target_arch = \"wasm32\") switch."},
	{"external_apis", "unity", []string{".cs"}, regexp.MustCompile(`Application\.OpenURL|GUIUtility\.systemCopyBuffer|\bWebXR\b|UnityEngine\.XR`), Warn,
		"Source uses OpenURL, clipboard or XR, which the Vapour player sandbox blocks today.",
		"Avoid them, or wait for the sandbox allow-list change (TODO.md, iframe permissions)."},
	{"external_apis", "godot", []string{".gd", ".cs"}, regexp.MustCompile(`OS\.shell_open|DisplayServer\.clipboard_|XRServer|OpenXR`), Warn,
		"Source uses shell_open, clipboard or XR, which the Vapour player sandbox blocks today.",
		"Avoid them, or wait for the sandbox allow-list change (TODO.md, iframe permissions)."},
	{"user_saves", "godot", []string{".gd", ".cs"}, regexp.MustCompile(`user://`), Info,
		"Game saves to user://; all Vapour games currently share one browser origin, so saves can collide.",
		"Prefix save file names with your slug until games get their own origin."},
}

// Check runs the project-level and build-level checks.
func Check(gameDir string, m *project.Manifest, opt Options) []Finding {
	engine := strings.ToLower(strings.TrimSpace(m.Project.Engine))
	var out []Finding
	out = append(out, checkProject(gameDir, engine)...)
	out = append(out, scanSources(gameDir, m, engine)...)
	entries := collectBuild(filepath.Join(gameDir, m.Build.Dir))
	out = append(out, checkBuild(entries, engine, opt)...)
	if m.Runtime.Threads {
		out = append(out, Finding{Severity: Warn, Code: "threads",
			Message: "runtime.threads is on. Threaded builds need cross-origin isolation, which the player does not guarantee, and Safari never gets it.",
			Hint:    "Build single-threaded unless you really need threads."})
	}
	if engine == "other" {
		out = append(out, Finding{Severity: Info, Code: "engine_other",
			Message: "engine \"other\" has no build adapter or template; Vapour only tests Godot, Unity and Bevy web exports."})
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i].Severity) < rank(out[j].Severity) })
	return dedupe(out)
}

// Blocking returns the Block findings the developer has not allowed by code.
func Blocking(findings []Finding, allow []string) []Finding {
	ok := map[string]bool{}
	for _, a := range allow {
		ok[strings.TrimSpace(a)] = true
	}
	var out []Finding
	for _, f := range findings {
		if f.Severity == Block && !ok[f.Code] {
			out = append(out, f)
		}
	}
	return out
}

// Format renders findings as printable lines.
func Format(findings []Finding) []string {
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		line := fmt.Sprintf("[%s %s] %s", strings.ToUpper(string(f.Severity)), f.Code, f.Message)
		if f.Path != "" {
			line += " (" + f.Path + ")"
		}
		if f.Hint != "" {
			line += " Fix: " + f.Hint
		}
		lines = append(lines, line)
	}
	return lines
}

func rank(s Severity) int {
	switch s {
	case Block:
		return 0
	case Warn:
		return 1
	}
	return 2
}

func dedupe(in []Finding) []Finding {
	seen := map[string]bool{}
	out := in[:0]
	for _, f := range in {
		k := f.Code + "|" + f.Path
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, f)
	}
	return out
}

func checkProject(dir, engine string) []Finding {
	var out []Finding
	if engine == "godot" {
		if hasFile(dir, "*.csproj") || hasFile(dir, "*.sln") {
			out = append(out, Finding{Severity: Block, Code: "godot_csharp",
				Message: "This Godot project uses C# (.csproj/.sln); Godot's web export does not support C#.",
				Hint:    "Port the scripts to GDScript. If you ship your own web runtime, acknowledge this with [guardrails] allow = [\"godot_csharp\"] in vapour.toml."})
		}
		proj := read(filepath.Join(dir, "project.godot"))
		method := godotSetting(proj, "rendering_method")
		web := godotSetting(proj, "rendering_method.web")
		if web != "gl_compatibility" && method != "gl_compatibility" {
			out = append(out, Finding{Severity: Warn, Code: "godot_renderer",
				Message: "The Godot project does not use the Compatibility renderer; Forward+ and Mobile have no web export.",
				Hint:    "Set rendering/renderer/rendering_method.web=\"gl_compatibility\" in project.godot."})
		}
	}
	if engine == "unity" {
		for _, sub := range []string{"Android", "IOS", "Linux", "MacOS", "Win", "x86_64", "x86"} {
			if st, err := os.Stat(filepath.Join(dir, "Assets", "Plugins", sub)); err == nil && st.IsDir() {
				out = append(out, Finding{Severity: Warn, Code: "native_plugins",
					Message: "Assets/Plugins/" + sub + " holds native plugins; they are not included in WebGL builds.",
					Hint:    "Replace the plugin with a managed or .jslib implementation and guard calls with #if UNITY_WEBGL."})
				break
			}
		}
	}
	if engine == "bevy" {
		cargo := read(filepath.Join(dir, "Cargo.toml"))
		if strings.Contains(cargo, "\"atomics\"") || strings.Contains(read(filepath.Join(dir, ".cargo", "config.toml")), "+atomics") {
			out = append(out, Finding{Severity: Warn, Code: "threads",
				Message: "Bevy build uses wasm atomics (threads), which need cross-origin isolation.",
				Hint:    "Build without atomics for Vapour."})
		}
	}
	return out
}

func scanSources(dir string, m *project.Manifest, engine string) []Finding {
	var rules []sourceRule
	for _, r := range sourceRules {
		if r.engine == engine {
			rules = append(rules, r)
		}
	}
	if len(rules) == 0 {
		return nil
	}
	buildAbs, _ := filepath.Abs(filepath.Join(dir, m.Build.Dir))
	var out []Finding
	hit := map[string]bool{}
	n := 0
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] || (engine == "unity" && d.Name() == "Editor") || (buildAbs != "" && absEq(p, buildAbs)) {
				return filepath.SkipDir
			}
			return nil
		}
		if n >= maxSourceFiles {
			return filepath.SkipAll
		}
		ext := strings.ToLower(filepath.Ext(p))
		var todo []sourceRule
		for _, r := range rules {
			if !hit[r.code] && contains(r.exts, ext) {
				todo = append(todo, r)
			}
		}
		if len(todo) == 0 {
			return nil
		}
		n++
		b := readN(p, maxSourceRead)
		for _, r := range todo {
			if r.re.Match(b) {
				hit[r.code] = true
				rel, _ := filepath.Rel(dir, p)
				out = append(out, Finding{Severity: r.sev, Code: r.code, Message: r.msg, Hint: r.hint, Path: filepath.ToSlash(rel)})
			}
		}
		return nil
	})
	return out
}

type entry struct {
	Path string
	Size int64
	Read func(limit int64) []byte
}

var (
	absPathRe = regexp.MustCompile(`(?i)(src|href)\s*=\s*["']/[^/"']`)
	pwaRe     = regexp.MustCompile(`(?i)\.service\.worker\.js$|(^|/)manifest\.webmanifest$`)
)

func checkBuild(entries []entry, engine string, opt Options) []Finding {
	var out []Finding
	var total int64
	for _, e := range entries {
		total += e.Size
		lower := strings.ToLower(e.Path)
		if !opt.Chunked && e.Size > maxFileBytes {
			out = append(out, Finding{Severity: Block, Code: "file_too_large", Path: e.Path,
				Message: fmt.Sprintf("%s is %d MiB; the upload limit is 512 MiB per file.", e.Path, e.Size>>20),
				Hint:    "Split the pack, or use --experimental-chunks for heavy games."})
		}
		if pwaRe.MatchString(lower) {
			out = append(out, Finding{Severity: Warn, Code: "pwa_service_worker", Path: e.Path,
				Message: "Build ships a PWA service worker, which clashes with other games on the shared origin.",
				Hint:    "Disable PWA in the web export settings."})
		}
		if strings.HasSuffix(lower, ".html") && strings.Count(e.Path, "/") <= 1 {
			html := string(e.Read(2 << 20))
			if absPathRe.MatchString(html) {
				out = append(out, Finding{Severity: Warn, Code: "absolute_paths", Path: e.Path,
					Message: "Entry HTML uses root-absolute paths (src=\"/...\"); games are served under /play/{slug}/.",
					Hint:    "Use relative paths (./file.js); for Trunk use --public-url ./."})
			}
			if engine == "unity" && strings.Contains(html, "960px") && strings.Contains(html, "600px") {
				out = append(out, Finding{Severity: Warn, Code: "unity_default_template", Path: e.Path,
					Message: "Unity Default WebGL template detected (fixed 960x600 canvas).",
					Hint:    "Use the Minimal template or a responsive one."})
			}
		}
	}
	if !opt.Chunked && total > maxExpandBytes {
		out = append(out, Finding{Severity: Block, Code: "build_too_large",
			Message: fmt.Sprintf("Build is %d MiB uncompressed; the expanded limit is 1 GiB.", total>>20),
			Hint:    "Trim assets, compress textures, or use --experimental-chunks."})
	}
	return out
}

func collectBuild(buildPath string) []entry {
	st, err := os.Stat(buildPath)
	if err != nil {
		return nil
	}
	var out []entry
	if st.IsDir() {
		_ = filepath.WalkDir(buildPath, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(buildPath, p)
			path := p
			out = append(out, entry{filepath.ToSlash(rel), info.Size(), func(l int64) []byte { return readN(path, l) }})
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
	// The reader stays open for lazy HTML reads; the process is short-lived.
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		zf := f
		out = append(out, entry{filepath.ToSlash(zf.Name), int64(zf.UncompressedSize64), func(l int64) []byte {
			r, err := zf.Open()
			if err != nil {
				return nil
			}
			defer r.Close()
			b, _ := io.ReadAll(io.LimitReader(r, l))
			return b
		}})
	}
	return out
}

func godotSetting(proj, key string) string {
	re := regexp.MustCompile(`(?m)^(?:renderer/)?` + regexp.QuoteMeta(key) + `\s*=\s*"?([^"\r\n]*)"?`)
	if m := re.FindStringSubmatch(proj); m != nil {
		v := strings.ToLower(strings.TrimSpace(m[1]))
		if v == "gl_compatibility" || v == "compatibility" {
			return "gl_compatibility"
		}
		return v
	}
	return ""
}

func hasFile(dir, pattern string) bool {
	m, _ := filepath.Glob(filepath.Join(dir, pattern))
	return len(m) > 0
}

func read(p string) string { return string(readN(p, maxSourceRead)) }

func readN(p string, limit int64) []byte {
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	b, _ := io.ReadAll(io.LimitReader(f, limit))
	return b
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func absEq(a, b string) bool {
	x, err := filepath.Abs(a)
	return err == nil && x == b
}

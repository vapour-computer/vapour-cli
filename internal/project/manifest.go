// Package project reads and writes vapour.toml, the per-game manifest.
package project

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const File = "vapour.toml"

var Engines = []string{"godot", "unity", "bevy", "other"}

type Manifest struct {
	Project Project `toml:"project"`
	Build   Build   `toml:"build"`
	Runtime Runtime `toml:"runtime"`
	Mobile  Mobile  `toml:"mobile"`
	Store   Store   `toml:"store"`

	Controller Controller `toml:"controller"`

	Guardrails Guardrails `toml:"guardrails"`
}

// Guardrails lets a developer acknowledge a blocking `vapour` guardrail finding by code.
type Guardrails struct {
	Allow []string `toml:"allow,omitempty"`
}

type Runtime struct {
	Threads  bool   `toml:"threads,omitempty"`
	WebGPU   bool   `toml:"webgpu,omitempty"`
	MemoryMB *int   `toml:"memory_mb,omitempty"`
	COEP     string `toml:"coep,omitempty"`
	Entry    string `toml:"entry,omitempty"`
	OnQuit   string `toml:"on_quit,omitempty"`
}

type Mobile struct {
	Orientation string   `toml:"orientation,omitempty"`
	RenderScale *float64 `toml:"render_scale,omitempty"`
	MaxDPR      *float64 `toml:"max_dpr,omitempty"`
	// RotateMessage is HTML shown over the game when the phone is held in the wrong orientation.
	// RotateMessageFile is a path (relative to the game folder) to read it from instead.
	RotateMessage     string `toml:"rotate_message,omitempty"`
	RotateMessageFile string `toml:"rotate_message_file,omitempty"`
}

// Store points at the store-page files. Both are optional; paths are relative to the game folder.
type Store struct {
	// Page is the JSON template: game details, media files and the section layout.
	Page string `toml:"page,omitempty"`
	// Content is the Markdown text (the "template.md" that `vapour create` writes).
	Content string `toml:"content,omitempty"`
	// Notes is a legacy field kept for compatibility; publish ignores it.
	Notes string `toml:"notes,omitempty"`
}

type Project struct {
	ID     string `toml:"id,omitempty"`
	Title  string `toml:"title"`
	Slug   string `toml:"slug"`
	Engine string `toml:"engine"`
	// Platform is optional: desktop | native-mobile | port.
	Platform string `toml:"platform,omitempty"`
	Version  string `toml:"version"`
}

type Build struct {
	Command      string   `toml:"command,omitempty"`
	Args         []string `toml:"args,omitempty"`
	Boot         []string `toml:"boot,omitempty"`
	SplitWarnMiB int64    `toml:"split_warn_mib,omitempty"`
	// Dir is the web export folder (or a .zip file) to publish.
	Dir   string `toml:"dir"`
	Cover string `toml:"cover,omitempty"`
	// PointerLock controls the mouse-capture fix added to index.html when publishing a folder:
	// "auto" (default), "pause" (also sends Escape when browser drops lock), or "off".
	PointerLock string `toml:"pointer_lock,omitempty"`
}

// PointerLockEnabled reports whether publish should add the pointer-lock script.
func (b Build) PointerLockEnabled() bool { return b.PointerLock != "off" }

// PointerLockPause reports whether the browser lock-loss pause behavior is enabled.
func (b Build) PointerLockPause() bool { return b.PointerLock == "pause" }

var versionRe = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+_-]*$`)

func Load(dir string) (*Manifest, error) {
	p := filepath.Join(dir, File)
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no %s in %s (run `vapour create` or cd into your game folder)", File, dir)
		}
		return nil, err
	}
	var m Manifest
	if err := toml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return &m, nil
}

func (m *Manifest) Save(dir string) error {
	b, err := toml.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, File), b, 0o644)
}

// Validate checks the fields needed to publish.
func (m *Manifest) Validate() error {
	if strings.TrimSpace(m.Project.Title) == "" {
		return fmt.Errorf("%s: project.title is required", File)
	}
	ok := false
	for _, e := range Engines {
		if m.Project.Engine == e {
			ok = true
		}
	}
	if !ok {
		return fmt.Errorf("%s: project.engine must be one of %s", File, strings.Join(Engines, ", "))
	}
	if !versionRe.MatchString(m.Project.Version) {
		return fmt.Errorf("%s: project.version %q is not valid (example: 0.1.0)", File, m.Project.Version)
	}
	if strings.TrimSpace(m.Build.Dir) == "" {
		return fmt.Errorf("%s: build.dir is required (your web export folder or .zip)", File)
	}
	build := filepath.Clean(filepath.FromSlash(strings.TrimSpace(m.Build.Dir)))
	if filepath.IsAbs(build) || build == "." || build == ".." || strings.HasPrefix(build, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s: build.dir must be a relative path inside the game folder", File)
	}
	if err := validatePlatform(m.Project.Platform); err != nil {
		return fmt.Errorf("%s: %w", File, err)
	}
	if err := m.Controller.Validate(); err != nil {
		return fmt.Errorf("%s: %w", File, err)
	}
	switch m.Build.PointerLock {
	case "", "auto", "off", "pause":
	default:
		return fmt.Errorf("%s: build.pointer_lock must be \"auto\", \"pause\" or \"off\", not %q", File, m.Build.PointerLock)
	}
	if m.Runtime.COEP != "" && m.Runtime.COEP != "require-corp" && m.Runtime.COEP != "credentialless" {
		return fmt.Errorf("%s: runtime.coep must be require-corp or credentialless", File)
	}
	if strings.HasPrefix(m.Runtime.Entry, "/") || strings.Contains(m.Runtime.Entry, "..") {
		return fmt.Errorf("%s: runtime.entry must be a relative safe path", File)
	}
	if m.Runtime.MemoryMB != nil && *m.Runtime.MemoryMB <= 0 {
		return fmt.Errorf("%s: runtime.memory_mb must be > 0", File)
	}
	if onQuit := strings.TrimSpace(m.Runtime.OnQuit); onQuit != "" && !validOnQuit(onQuit) {
		return fmt.Errorf("%s: runtime.on_quit must be one of store, home, reload, stay", File)
	}
	orientation := m.Mobile.Orientation
	if orientation == "" {
		orientation = "any"
	}
	if orientation != "any" && orientation != "landscape" && orientation != "portrait" {
		return fmt.Errorf("%s: mobile.orientation must be any, landscape or portrait", File)
	}
	if m.Mobile.RenderScale != nil && (*m.Mobile.RenderScale < 0.25 || *m.Mobile.RenderScale > 1.0) {
		return fmt.Errorf("%s: mobile.render_scale must be between 0.25 and 1.0", File)
	}
	if m.Mobile.MaxDPR != nil && *m.Mobile.MaxDPR <= 0 {
		return fmt.Errorf("%s: mobile.max_dpr must be > 0", File)
	}
	return nil
}

func validOnQuit(value string) bool {
	switch value {
	case "store", "home", "reload", "stay":
		return true
	default:
		return false
	}
}

// ResolvePathInside resolves rel from gameDir, follows symlinks, and ensures
// the result stays inside gameDir.
func ResolvePathInside(gameDir, rel, label string) (string, error) {
	root, err := filepath.Abs(gameDir)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(filepath.FromSlash(strings.TrimSpace(rel)))
	if clean == "." || clean == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s must be a relative path inside the game folder", label)
	}
	p := filepath.Join(root, clean)
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		return "", fmt.Errorf("%s: %w", label, err)
	}
	r, err := filepath.Rel(root, p)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s must stay inside the game folder", label)
	}
	return p, nil
}

func (m *Manifest) MobileOrientation() string {
	if m.Mobile.Orientation == "" {
		return "any"
	}
	return m.Mobile.Orientation
}

func (m *Manifest) MobileRenderScale() float64 {
	if m.Mobile.RenderScale == nil {
		return 1
	}
	return *m.Mobile.RenderScale
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify turns a title into a URL slug: lowercase [a-z0-9-], at most 48 characters.
func Slugify(s string) string {
	s = nonSlug.ReplaceAllString(strings.ToLower(s), "-")
	s = strings.Trim(s, "-")
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	if s == "" {
		return "game"
	}
	return s
}

// MaxRotateMessage caps the rotate-your-phone HTML (characters).
const MaxRotateMessage = 4000

// RotateMessageHTML returns the HTML for the rotate overlay, from mobile.rotate_message or the file
// named by mobile.rotate_message_file (inside the game folder). Empty means "use the default text".
func (m *Manifest) RotateMessageHTML(dir string) (string, error) {
	s := m.Mobile.RotateMessage
	if f := strings.TrimSpace(m.Mobile.RotateMessageFile); f != "" {
		if s != "" {
			return "", fmt.Errorf("%s: set only one of mobile.rotate_message and mobile.rotate_message_file", File)
		}
		root, err := filepath.Abs(dir)
		if err != nil {
			return "", err
		}
		root, err = filepath.EvalSymlinks(root)
		if err != nil {
			return "", err
		}
		p := filepath.Join(root, f)
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			return "", fmt.Errorf("%s: mobile.rotate_message_file: %w", File, err)
		}
		if rel, err := filepath.Rel(root, resolved); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("%s: mobile.rotate_message_file must be inside the game folder", File)
		}
		b, err := os.ReadFile(resolved)
		if err != nil {
			return "", fmt.Errorf("%s: mobile.rotate_message_file: %w", File, err)
		}
		s = string(b)
	}
	s = strings.TrimSpace(s)
	if n := len([]rune(s)); n > MaxRotateMessage {
		return "", fmt.Errorf("%s: rotate message has %d characters; the limit is %d", File, n, MaxRotateMessage)
	}
	return s, nil
}

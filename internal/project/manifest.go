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
}

type Runtime struct {
	Threads  bool   `toml:"threads,omitempty"`
	WebGPU   bool   `toml:"webgpu,omitempty"`
	MemoryMB *int   `toml:"memory_mb,omitempty"`
	COEP     string `toml:"coep,omitempty"`
	Entry    string `toml:"entry,omitempty"`
}

type Mobile struct {
	Orientation string   `toml:"orientation,omitempty"`
	RenderScale *float64 `toml:"render_scale,omitempty"`
	MaxDPR      *float64 `toml:"max_dpr,omitempty"`
}

// Store points at the store-page files. Both are optional; paths are relative to the game folder.
type Store struct {
	// Page is the JSON template: game details, media files and the section layout.
	Page string `toml:"page,omitempty"`
	// Content is the Markdown text (the "template.md" that `vapour create` writes).
	Content string `toml:"content,omitempty"`
	// Notes is optional Markdown release notes for the version being published.
	Notes string `toml:"notes,omitempty"`
}

type Project struct {
	ID      string `toml:"id,omitempty"`
	Title   string `toml:"title"`
	Slug    string `toml:"slug"`
	Engine  string `toml:"engine"`
	Version string `toml:"version"`
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
	// "auto" (the default) adds it, "off" leaves index.html as exported.
	PointerLock string `toml:"pointer_lock,omitempty"`
}

// PointerLockEnabled reports whether publish should add the pointer-lock script.
func (b Build) PointerLockEnabled() bool { return b.PointerLock != "off" }

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
	switch m.Build.PointerLock {
	case "", "auto", "off":
	default:
		return fmt.Errorf("%s: build.pointer_lock must be \"auto\" or \"off\", not %q", File, m.Build.PointerLock)
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

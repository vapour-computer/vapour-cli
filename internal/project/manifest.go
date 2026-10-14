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
	Store   Store   `toml:"store"`
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
	// Dir is the web export folder (or a .zip file) to publish.
	Dir   string `toml:"dir"`
	Cover string `toml:"cover,omitempty"`
}

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
	return nil
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

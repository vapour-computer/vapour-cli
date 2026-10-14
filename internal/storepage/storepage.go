// Package storepage reads a game's store-page files (store/page.json and store/template.md),
// validates them with the same rules as the API, and prepares what `vapour publish` uploads.
package storepage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/vapour-computer/vapour-cli/internal/media"
	"github.com/vapour-computer/vapour-cli/internal/project"
)

var Categories = []string{"action", "adventure", "arcade", "casual", "horror", "platformer", "puzzle", "racing", "retro", "rpg", "shooter", "simulation", "sports", "strategy", "other"}

const (
	MaxDescription = 20000
	MaxNotes       = 5000
	MaxTagline     = 140
	MaxPageJSON    = 16 << 10
)

// File mirrors store/page.json. Unknown keys are an error so typos are caught early.
type File struct {
	Schema   string    `json:"$schema,omitempty"`
	Version  int       `json:"version"`
	Game     Game      `json:"game"`
	Media    Media     `json:"media"`
	Sections []Section `json:"sections"`
	Details  []Detail  `json:"details"`
	Links    []Link    `json:"links"`
}

type Game struct {
	Tagline  string   `json:"tagline"`
	Category string   `json:"category"`
	Tags     []string `json:"tags"`
}

type Media struct {
	Banner      string   `json:"banner"`
	Cover       string   `json:"cover"`
	Screenshots []string `json:"screenshots"`
	Video       string   `json:"video"`
}

type Section struct {
	Type   string `json:"type"`
	Anchor string `json:"anchor,omitempty"`
	Title  string `json:"title,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type Detail struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// Upload is one media file for the multipart request.
type Upload struct {
	Field string // banner, cover, screenshot, video
	Path  string
	Bytes int64
}

// Bundle is everything `PUT /projects/:id/store` needs, already validated.
type Bundle struct {
	Metadata map[string]any
	Uploads  []Upload
	Notes    string
	Warnings []string
}

// TotalBytes is the combined size of the media files.
func (b *Bundle) TotalBytes() int64 {
	var n int64
	for _, u := range b.Uploads {
		n += u.Bytes
	}
	return n
}

var (
	anchorRe = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)
	tagRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9 -]{0,23}$`)
	h2Re     = regexp.MustCompile(`(?m)^##[ \t]+(.+?)[ \t]*#*[ \t]*$`)
	slugBad  = regexp.MustCompile(`[^a-z0-9]+`)
)

func isCategory(c string) bool {
	for _, v := range Categories {
		if v == c {
			return true
		}
	}
	return false
}

// Anchors lists the section anchors a Markdown text provides: "intro" for text before the first "## " heading,
// then one slug per level-2 heading.
func Anchors(md string) []string {
	out := []string{"intro"}
	for _, m := range h2Re.FindAllStringSubmatch(md, -1) {
		s := strings.Trim(slugBad.ReplaceAllString(strings.ToLower(m[1]), "-"), "-")
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func readText(path string, max int, what string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", what, err)
	}
	if !utf8.Valid(b) {
		return "", fmt.Errorf("%s %s is not valid UTF-8 text", what, path)
	}
	s := strings.ReplaceAll(string(b), "\x00", "")
	if utf8.RuneCountInString(s) > max {
		return "", fmt.Errorf("%s %s has %d characters; the limit is %d", what, path, utf8.RuneCountInString(s), max)
	}
	return s, nil
}

// Load reads and validates the store files named in the manifest. It returns (nil, nil) when the project has no store files
// configured. gameDir is the folder containing vapour.toml.
func Load(gameDir string, m *project.Manifest) (*Bundle, error) {
	if m.Store.Page == "" && m.Store.Content == "" && m.Store.Notes == "" && m.Build.Cover == "" {
		return nil, nil
	}
	b := &Bundle{Metadata: map[string]any{}}

	if m.Store.Content != "" {
		md, err := readText(filepath.Join(gameDir, m.Store.Content), MaxDescription, "store content")
		if err != nil {
			return nil, err
		}
		b.Metadata["description"] = md
		defer func() { b.Warnings = append(b.Warnings, anchorWarnings(b, md)...) }()
	}
	if m.Store.Notes != "" {
		n, err := readText(filepath.Join(gameDir, m.Store.Notes), MaxNotes, "release notes")
		if err != nil {
			return nil, err
		}
		b.Notes = n
	}
	// Legacy: [build] cover = "..." still works when no page file names a cover.
	legacyCover := m.Build.Cover

	if m.Store.Page == "" {
		if legacyCover != "" {
			if err := addImage(b, gameDir, "cover", legacyCover, media.Cover); err != nil {
				return nil, err
			}
		}
		return b, nil
	}

	pagePath := filepath.Join(gameDir, m.Store.Page)
	raw, err := os.ReadFile(pagePath)
	if err != nil {
		return nil, fmt.Errorf("store page: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%s: %w", m.Store.Page, err)
	}
	if err := validatePage(&f); err != nil {
		return nil, fmt.Errorf("%s: %w", m.Store.Page, err)
	}

	// Game details become API metadata fields.
	if t := strings.TrimSpace(f.Game.Tagline); t != "" {
		b.Metadata["tagline"] = t
	}
	if c := strings.ToLower(strings.TrimSpace(f.Game.Category)); c != "" {
		b.Metadata["category"] = c
	}
	if f.Game.Tags != nil {
		tags := make([]string, 0, len(f.Game.Tags))
		for _, t := range f.Game.Tags {
			tags = append(tags, strings.ToLower(strings.TrimSpace(t)))
		}
		b.Metadata["tags"] = tags
	}
	// The layout is sent as the `page` object (without media paths or game details, which travel separately).
	page := map[string]any{"version": f.Version, "sections": f.Sections, "details": nonNilDetails(f.Details), "links": nonNilLinks(f.Links)}
	pj, _ := json.Marshal(page)
	if len(pj) > MaxPageJSON {
		return nil, fmt.Errorf("%s: layout is %d bytes; the limit is %d", m.Store.Page, len(pj), MaxPageJSON)
	}
	b.Metadata["page"] = page

	// Media paths are relative to the folder holding page.json.
	base := filepath.Dir(pagePath)
	rel := func(p string) string { return filepath.Join(base, filepath.FromSlash(p)) }
	cover := f.Media.Cover
	if cover == "" {
		cover = legacyCover
		if cover != "" {
			cover = filepath.ToSlash(mustRel(base, filepath.Join(gameDir, filepath.FromSlash(cover))))
		}
	}
	if f.Media.Banner != "" {
		if err := addImage(b, "", "banner", rel(f.Media.Banner), media.Banner); err != nil {
			return nil, err
		}
	}
	if cover != "" {
		if err := addImage(b, "", "cover", rel(cover), media.Cover); err != nil {
			return nil, err
		}
	}
	for _, s := range f.Media.Screenshots {
		if s == "" {
			continue
		}
		if err := addImage(b, "", "screenshot", rel(s), media.Screenshot); err != nil {
			return nil, err
		}
	}
	if f.Media.Video != "" {
		n, err := media.CheckVideo(rel(f.Media.Video))
		if err != nil {
			return nil, err
		}
		b.Uploads = append(b.Uploads, Upload{Field: "video", Path: rel(f.Media.Video), Bytes: n})
	}
	if total := b.TotalBytes(); total > media.MaxRequestBytes-2*media.MiB {
		return nil, fmt.Errorf("store media totals %.1f MiB; one upload must stay under %d MiB (compress the video or screenshots)",
			float64(total)/media.MiB, (media.MaxRequestBytes-2*media.MiB)/media.MiB)
	}
	return b, nil
}

func mustRel(base, target string) string {
	r, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}
	return r
}

func addImage(b *Bundle, dir, field, path string, spec media.Spec) error {
	if dir != "" {
		path = filepath.Join(dir, path)
	}
	n, err := media.CheckImage(path, spec)
	if err != nil {
		return err
	}
	b.Uploads = append(b.Uploads, Upload{Field: field, Path: path, Bytes: n})
	return nil
}

func nonNilDetails(d []Detail) []Detail {
	if d == nil {
		return []Detail{}
	}
	return d
}

func nonNilLinks(l []Link) []Link {
	if l == nil {
		return []Link{}
	}
	return l
}

func anchorWarnings(b *Bundle, md string) []string {
	page, ok := b.Metadata["page"].(map[string]any)
	if !ok {
		return nil
	}
	have := map[string]bool{}
	for _, a := range Anchors(md) {
		have[a] = true
	}
	var out []string
	for _, s := range page["sections"].([]Section) {
		if s.Type == "markdown" && !have[s.Anchor] {
			out = append(out, fmt.Sprintf("section %q has no matching \"## \" heading in the content file, so it will be empty", s.Anchor))
		}
	}
	return out
}

func validatePage(f *File) error {
	if f.Version != 1 {
		return fmt.Errorf("version must be 1")
	}
	if utf8.RuneCountInString(f.Game.Tagline) > MaxTagline {
		return fmt.Errorf("game.tagline is longer than %d characters", MaxTagline)
	}
	if c := strings.ToLower(strings.TrimSpace(f.Game.Category)); c != "" && !isCategory(c) {
		return fmt.Errorf("game.category %q is not valid; use one of: %s", f.Game.Category, strings.Join(Categories, ", "))
	}
	if len(f.Game.Tags) > 5 {
		return fmt.Errorf("game.tags has %d entries; the limit is 5", len(f.Game.Tags))
	}
	for _, t := range f.Game.Tags {
		if !tagRe.MatchString(strings.ToLower(strings.TrimSpace(t))) {
			return fmt.Errorf("game.tags: %q must be 1-24 characters (letters, digits, spaces or hyphens)", t)
		}
	}
	if len(f.Media.Screenshots) > media.MaxScreenshots {
		return fmt.Errorf("media.screenshots has %d entries; the limit is %d", len(f.Media.Screenshots), media.MaxScreenshots)
	}
	if n := len(f.Sections); n < 1 || n > 12 {
		return fmt.Errorf("sections must have 1 to 12 entries (found %d)", n)
	}
	seen, anchors := map[string]bool{}, map[string]bool{}
	for i, s := range f.Sections {
		switch s.Type {
		case "markdown":
			if !anchorRe.MatchString(s.Anchor) {
				return fmt.Errorf("sections[%d]: markdown needs an anchor of 1-40 lowercase letters, digits or hyphens", i)
			}
			if anchors[s.Anchor] {
				return fmt.Errorf("sections[%d]: anchor %q is used twice", i, s.Anchor)
			}
			anchors[s.Anchor] = true
			if utf8.RuneCountInString(s.Title) > 60 {
				return fmt.Errorf("sections[%d]: title is longer than 60 characters", i)
			}
		case "release_notes":
			if s.Limit != 0 && (s.Limit < 1 || s.Limit > 20) {
				return fmt.Errorf("sections[%d]: limit must be 1 to 20", i)
			}
		case "media", "details", "links":
			if s.Anchor != "" || s.Title != "" || s.Limit != 0 {
				return fmt.Errorf("sections[%d]: %q takes no options", i, s.Type)
			}
		default:
			return fmt.Errorf("sections[%d]: unknown type %q (use media, markdown, release_notes, details or links)", i, s.Type)
		}
		if s.Type != "markdown" {
			if seen[s.Type] {
				return fmt.Errorf("sections[%d]: %q can appear only once", i, s.Type)
			}
			seen[s.Type] = true
		}
	}
	if len(f.Details) > 20 {
		return fmt.Errorf("details has %d entries; the limit is 20", len(f.Details))
	}
	for i, d := range f.Details {
		if l := utf8.RuneCountInString(d.Label); l < 1 || l > 40 {
			return fmt.Errorf("details[%d].label must be 1-40 characters", i)
		}
		if l := utf8.RuneCountInString(d.Value); l < 1 || l > 200 {
			return fmt.Errorf("details[%d].value must be 1-200 characters", i)
		}
	}
	if len(f.Links) > 6 {
		return fmt.Errorf("links has %d entries; the limit is 6", len(f.Links))
	}
	for i, l := range f.Links {
		if n := utf8.RuneCountInString(l.Label); n < 1 || n > 40 {
			return fmt.Errorf("links[%d].label must be 1-40 characters", i)
		}
		if !strings.HasPrefix(l.URL, "https://") || len(l.URL) > 300 {
			return fmt.Errorf("links[%d].url must start with https:// and be at most 300 characters", i)
		}
	}
	return nil
}

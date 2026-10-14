package storepage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vapour-computer/vapour-cli/internal/project"
	"github.com/vapour-computer/vapour-cli/internal/scaffold"
)

func newGame(t *testing.T) (string, *project.Manifest) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "game")
	if err := scaffold.Write(dir, "godot", scaffold.Data{Title: "My Game", Slug: "my-game", Engine: "godot"}); err != nil {
		t.Fatal(err)
	}
	m := &project.Manifest{
		Project: project.Project{Title: "My Game", Slug: "my-game", Engine: "godot", Version: "0.1.0"},
		Build:   project.Build{Dir: "build/web"},
		Store:   project.Store{Page: "store/page.json", Content: "store/template.md"},
	}
	return dir, m
}

func setPage(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "store", "page.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScaffoldedTemplateIsValid(t *testing.T) {
	dir, m := newGame(t)
	b, err := Load(dir, m)
	if err != nil || b == nil {
		t.Fatalf("scaffold should validate: %v", err)
	}
	if b.Metadata["category"] != "casual" || b.Metadata["description"] == "" {
		t.Errorf("metadata: %+v", b.Metadata)
	}
	page := b.Metadata["page"].(map[string]any)
	if len(page["sections"].([]Section)) != 9 || len(b.Uploads) != 0 {
		t.Errorf("sections/uploads: %v %v", page["sections"], b.Uploads)
	}
	if len(b.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", b.Warnings)
	}
}

func TestRejections(t *testing.T) {
	dir, m := newGame(t)
	base := func(extra string) string {
		return `{"version":1,"sections":[{"type":"media"}]` + extra + `}`
	}
	cases := map[string]string{
		"unknown key":        `{"version":1,"sections":[{"type":"media"}],"colour":"red"}`,
		"bad version":        `{"version":2,"sections":[{"type":"media"}]}`,
		"no sections":        `{"version":1,"sections":[]}`,
		"bad category":       base(`,"game":{"category":"vibes"}`),
		"too many tags":      base(`,"game":{"tags":["a","b","c","d","e","f"]}`),
		"long tagline":       base(`,"game":{"tagline":"` + strings.Repeat("x", 141) + `"}`),
		"unknown section":    `{"version":1,"sections":[{"type":"iframe"}]}`,
		"markdown no anchor": `{"version":1,"sections":[{"type":"markdown"}]}`,
		"dup anchor":         `{"version":1,"sections":[{"type":"markdown","anchor":"a"},{"type":"markdown","anchor":"a"}]}`,
		"dup details":        `{"version":1,"sections":[{"type":"details"},{"type":"details"}]}`,
		"media with option":  `{"version":1,"sections":[{"type":"media","title":"x"}]}`,
		"http link":          base(`,"links":[{"label":"x","url":"http://example.com"}]`),
		"js link":            base(`,"links":[{"label":"x","url":"javascript:alert(1)"}]`),
		"long label":         base(`,"details":[{"label":"` + strings.Repeat("x", 41) + `","value":"v"}]`),
		"seven links":        base(`,"links":[` + strings.Repeat(`{"label":"x","url":"https://e.com"},`, 6) + `{"label":"x","url":"https://e.com"}]`),
		"not json":           `{ nope`,
	}
	for name, body := range cases {
		setPage(t, dir, body)
		if _, err := Load(dir, m); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// Thirteen sections.
	many := `{"version":1,"sections":[`
	for i := 0; i < 13; i++ {
		many += `{"type":"markdown","anchor":"a` + string(rune('a'+i)) + `"},`
	}
	setPage(t, dir, strings.TrimSuffix(many, ",")+`]}`)
	if _, err := Load(dir, m); err == nil {
		t.Error("13 sections accepted")
	}
}

func TestAnchorsAndWarnings(t *testing.T) {
	got := Anchors("hook\n\n## About\ntext\n## How to Play!\nmore\n")
	want := []string{"intro", "about", "how-to-play"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("anchors %v want %v", got, want)
	}
	dir, m := newGame(t)
	setPage(t, dir, `{"version":1,"sections":[{"type":"markdown","anchor":"missing"}]}`)
	b, err := Load(dir, m)
	if err != nil || len(b.Warnings) != 1 || !strings.Contains(b.Warnings[0], "missing") {
		t.Errorf("warnings: %v %v", err, b)
	}
}

func TestLimitsOnText(t *testing.T) {
	dir, m := newGame(t)
	os.WriteFile(filepath.Join(dir, "store", "template.md"), []byte(strings.Repeat("a", MaxDescription+1)), 0o644)
	if _, err := Load(dir, m); err == nil {
		t.Error("oversize description accepted")
	}
	os.WriteFile(filepath.Join(dir, "store", "template.md"), []byte("ok \x00 text"), 0o644)
	b, err := Load(dir, m)
	if err != nil || strings.Contains(b.Metadata["description"].(string), "\x00") {
		t.Errorf("NUL not stripped: %v", err)
	}
}

func TestNoStoreConfigured(t *testing.T) {
	b, err := Load(t.TempDir(), &project.Manifest{})
	if b != nil || err != nil {
		t.Errorf("want nil,nil got %v,%v", b, err)
	}
}

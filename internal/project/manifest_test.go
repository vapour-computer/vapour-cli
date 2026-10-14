package project

import "testing"

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"My Game!": "my-game", "  Hello   World ": "hello-world", "***": "game", "Über Spiel 2": "ber-spiel-2",
	} {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q)=%q want %q", in, got, want)
		}
	}
}

func TestManifestRoundTripAndValidate(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{Project: Project{ID: "g1", Title: "T", Slug: "t", Engine: "godot", Version: "0.1.0"}, Build: Build{Dir: "build/web"}}
	if err := m.Save(dir); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil || *got != *m {
		t.Fatalf("round trip: %v %+v", err, got)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	got.Project.Engine = "flash"
	if got.Validate() == nil {
		t.Error("bad engine accepted")
	}
	got.Project.Engine, got.Project.Version = "godot", "bad version"
	if got.Validate() == nil {
		t.Error("bad version accepted")
	}
	if _, err := Load(t.TempDir()); err == nil {
		t.Error("missing manifest accepted")
	}
}

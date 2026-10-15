package project

import (
	"reflect"
	"testing"
)

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
	renderScale := 0.75
	maxDPR := 2.0
	m := &Manifest{
		Project: Project{ID: "g1", Title: "T", Slug: "t", Engine: "godot", Version: "0.1.0"},
		Build:   Build{Dir: "build/web", Command: "custom", Args: []string{"exporter", "--web"}, Boot: []string{"*.html"}, SplitWarnMiB: 1024},
		Runtime: Runtime{Threads: true, WebGPU: false, COEP: "require-corp", Entry: "index.html"},
		Mobile:  Mobile{Orientation: "landscape", RenderScale: &renderScale, MaxDPR: &maxDPR},
	}
	if err := m.Save(dir); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil || !reflect.DeepEqual(got, m) {
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
	got.Project.Version = "0.1.0"
	got.Runtime.COEP = "bad"
	if got.Validate() == nil {
		t.Error("bad coep accepted")
	}
	got.Runtime.COEP = "require-corp"
	got.Mobile.Orientation = "upside-down"
	if got.Validate() == nil {
		t.Error("bad orientation accepted")
	}
	got.Mobile.Orientation = "portrait"
	rs := 2.0
	got.Mobile.RenderScale = &rs
	if got.Validate() == nil {
		t.Error("bad render_scale accepted")
	}
	rs = 0.75
	got.Mobile.RenderScale = &rs
	memory := 0
	got.Runtime.MemoryMB = &memory
	if got.Validate() == nil {
		t.Error("bad runtime.memory_mb accepted")
	}
	if _, err := Load(t.TempDir()); err == nil {
		t.Error("missing manifest accepted")
	}
}

func TestPointerLockSetting(t *testing.T) {
	m := &Manifest{Project: Project{Title: "g", Engine: "godot", Version: "0.1.0"}, Build: Build{Dir: "b"}}
	if !m.Build.PointerLockEnabled() {
		t.Error("pointer lock should default to on")
	}
	m.Build.PointerLock = "off"
	if m.Build.PointerLockEnabled() || m.Validate() != nil {
		t.Error("off should disable and validate")
	}
	m.Build.PointerLock = "sometimes"
	if m.Validate() == nil {
		t.Error("unknown value accepted")
	}
}

func TestMobileDefaults(t *testing.T) {
	m := &Manifest{}
	if got := m.MobileOrientation(); got != "any" {
		t.Fatalf("orientation=%q", got)
	}
	if got := m.MobileRenderScale(); got != 1 {
		t.Fatalf("render scale=%v", got)
	}
}

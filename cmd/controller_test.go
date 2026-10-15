package cmd

import (
	"github.com/vapour-computer/vapour-cli/internal/project"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestControllerInitAndCheck(t *testing.T) {
	wd, _ := os.Getwd()
	work := t.TempDir()
	os.Chdir(work)
	defer os.Chdir(wd)

	if _, err := run(t, "create", "Pad Test", "--offline"); err != nil {
		t.Fatal(err)
	}
	game := filepath.Join(work, "pad-test")
	if _, err := run(t, "controller", "init", "--dir", game); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(game, "vapour.toml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"[controller]", "mode = 'keyboard-map'", "[[controller.controls]]", "type = 'joystick'", "repeat_hz = 30"} {
		if !strings.Contains(s, want) {
			t.Errorf("vapour.toml misses %q:\n%s", want, s)
		}
	}
	out, err := run(t, "controller", "check", "--dir", game)
	if err != nil || !strings.Contains(out, "Controller config looks good") {
		t.Fatalf("controller check: %v %q", err, out)
	}
	out, err = run(t, "controller", "preview", "--dir", game)
	if err != nil || !strings.Contains(out, "Preview (D=dpad, J=joystick, B=button)") {
		t.Fatalf("controller preview: %v %q", err, out)
	}
	out, err = run(t, "controller", "preview", "--dir", game, "--html", "--out", "layout.html")
	if err != nil || !strings.Contains(out, "Wrote HTML preview") {
		t.Fatalf("controller html preview: %v %q", err, out)
	}
	html, err := os.ReadFile(filepath.Join(game, "layout.html"))
	if err != nil {
		t.Fatal(err)
	}
	hs := string(html)
	if !strings.Contains(hs, "Vapour Controller Preview") || !strings.Contains(hs, "left-stick") {
		t.Fatalf("unexpected HTML preview content: %q", hs)
	}
}

func TestCreateRejectsUnknownPlatform(t *testing.T) {
	wd, _ := os.Getwd()
	work := t.TempDir()
	os.Chdir(work)
	defer os.Chdir(wd)
	if _, err := run(t, "create", "Bad", "--offline", "--platform", "toaster"); err == nil || !strings.Contains(err.Error(), "--platform") {
		t.Fatalf("expected platform validation error, got %v", err)
	}
}

func TestControllerInitSupportsActionMapAliasAndBackup(t *testing.T) {
	wd, _ := os.Getwd()
	work := t.TempDir()
	os.Chdir(work)
	defer os.Chdir(wd)

	if _, err := run(t, "create", "Alias", "--offline", "--platform", "native-mobile"); err != nil {
		t.Fatal(err)
	}
	game := filepath.Join(work, "alias")
	if _, err := run(t, "controller", "init", "--dir", game, "--mode", "action-map", "--preset", "dpad-only"); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "controller", "check", "--dir", game)
	if err != nil || !strings.Contains(out, "Controller mode: dedicated") {
		t.Fatalf("controller check alias mode: %v %q", err, out)
	}

	if _, err := run(t, "controller", "init", "--dir", game, "--mode", "action-map"); err == nil {
		t.Fatal("expected init without --force to fail")
	}
	out, err = run(t, "controller", "init", "--dir", game, "--mode", "action-map", "--force")
	if err != nil || !strings.Contains(out, "Backed up existing") {
		t.Fatalf("controller init --force backup: %v %q", err, out)
	}
	entries, err := os.ReadDir(game)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "vapour.toml.bak-") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected vapour.toml backup file")
	}
}

func TestPublishMetadataIncludesControllerAndRotateMessage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rotate.html"), []byte("<h1>Turn it</h1>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &project.Manifest{}
	m.Mobile.Orientation = "landscape"
	m.Mobile.RotateMessageFile = "rotate.html"
	m.Controller = project.Controller{Mode: "action-map", Controls: []project.Control{{ID: "a", Type: "button", X: .8, Y: .7, W: .1, H: .1, Action: "fire"}}}
	meta, err := publishMetadata(m, dir)
	if err != nil {
		t.Fatal(err)
	}
	mobile := meta["mobile"].(map[string]any)
	if mobile["rotate_message"] != "<h1>Turn it</h1>" || mobile["orientation"] != "landscape" {
		t.Fatalf("mobile = %v", mobile)
	}
	ctl := meta["controller"].(map[string]any)
	if ctl["mode"] != "dedicated" {
		t.Fatalf("controller = %v", ctl)
	}

	m.Mobile.RotateMessageFile = "../x.html"
	if _, err := publishMetadata(m, dir); err == nil {
		t.Fatal("expected error for file outside game folder")
	}
	m.Mobile.RotateMessageFile = ""
	m.Mobile.RotateMessage = strings.Repeat("x", project.MaxRotateMessage+1)
	if _, err := publishMetadata(m, dir); err == nil {
		t.Fatal("expected error for oversized message")
	}
}

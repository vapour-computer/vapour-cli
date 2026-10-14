package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeAPI implements just enough of the contract for the CLI flow.
func fakeAPI(t *testing.T, seen *map[string]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer vpr_good" {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":{"code":"unauthorized","message":"bad token"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/me":
			io.WriteString(w, `{"user":{"uid":"u1","name":"Dev","email":"d@x","role":"dev"},"dev":{"uid":"u1"},"auth":"token"}`)
		case "/projects":
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(map[string]string{"id": "g1", "slug": b["slug"], "title": b["title"], "engine": b["engine"], "status": "draft"})
		case "/projects/g1/publish":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("multipart: %v", err)
			}
			(*seen)["version"] = r.FormValue("version")
			f, _, err := r.FormFile("build")
			if err != nil {
				t.Errorf("build part: %v", err)
			} else {
				b, _ := io.ReadAll(f)
				(*seen)["magic"] = string(b[:2])
			}
			io.WriteString(w, `{"game":{"id":"g1","title":"My Game","version":"0.2.0"},"url":"http://web/games/my-game"}`)
		default:
			w.WriteHeader(404)
		}
	}))
}

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestLoginCreatePublishFlow(t *testing.T) {
	seen := map[string]string{}
	srv := fakeAPI(t, &seen)
	defer srv.Close()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("VAPOUR_API", "")
	t.Setenv("VAPOUR_TOKEN", "")
	wd, _ := os.Getwd()
	work := t.TempDir()
	os.Chdir(work)
	defer os.Chdir(wd)

	if _, err := run(t, "login", "--api", srv.URL, "--token", "vpr_bad"); err == nil {
		t.Fatal("bad token accepted")
	}
	if out, err := run(t, "login", "--api", srv.URL, "--token", "vpr_good"); err != nil || !strings.Contains(out, "Logged in as Dev (dev)") {
		t.Fatalf("login: %v %q", err, out)
	}
	if out, err := run(t, "whoami"); err != nil || !strings.Contains(out, "Dev <d@x> (dev)") {
		t.Fatalf("whoami: %v %q", err, out)
	}
	if _, err := run(t, "create", "My Game", "--engine", "godot"); err != nil {
		t.Fatal(err)
	}
	game := filepath.Join(work, "my-game")
	mf, err := os.ReadFile(filepath.Join(game, "vapour.toml"))
	if err != nil || !strings.Contains(string(mf), `id = 'g1'`) {
		t.Fatalf("manifest: %v\n%s", err, mf)
	}
	for _, f := range []string{"README.md", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(game, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	if _, err := run(t, "publish", "--dir", game); err == nil {
		t.Fatal("publish without a build should fail")
	}
	if err := os.MkdirAll(filepath.Join(game, "build/web"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(game, "build/web/index.html"), []byte("<html></html>"), 0o644)
	if out, err := run(t, "publish", "--dir", game, "--dry-run"); err != nil || !strings.Contains(out, "Dry run") {
		t.Fatalf("dry run: %v %q", err, out)
	}
	out, err := run(t, "publish", "--dir", game, "--version", "0.2.0")
	if err != nil || !strings.Contains(out, "http://web/games/my-game") {
		t.Fatalf("publish: %v %q", err, out)
	}
	if seen["version"] != "0.2.0" || seen["magic"] != "PK" {
		t.Errorf("server saw %v", seen)
	}
}

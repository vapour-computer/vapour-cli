package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeAPI implements just enough of the contract for the CLI flow and records what it receives.
type fakeAPI struct {
	mu        sync.Mutex
	versions  map[string]bool
	seen      map[string]string
	storeMeta map[string]any
	storeFlds []string
	srv       *httptest.Server
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{versions: map[string]bool{}, seen: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer vpr_good" {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":{"code":"unauthorized","message":"bad token"}}`)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/me":
			io.WriteString(w, `{"user":{"uid":"u1","name":"Dev","email":"d@x","role":"dev"},"dev":{"uid":"u1"},"auth":"token"}`)
		case "/projects":
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(map[string]string{"id": "g1", "slug": b["slug"], "title": b["title"], "engine": b["engine"], "status": "draft"})
		case "/projects/g1/store":
			if r.Method != http.MethodPut {
				w.WriteHeader(405)
				return
			}
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				t.Errorf("store multipart: %v", err)
			}
			f.storeMeta = map[string]any{}
			json.Unmarshal([]byte(r.FormValue("metadata")), &f.storeMeta)
			f.storeFlds = nil
			for field, files := range r.MultipartForm.File {
				for range files {
					f.storeFlds = append(f.storeFlds, field)
				}
			}
			io.WriteString(w, `{"id":"g1","slug":"my-game","title":"My Game","status":"draft"}`)
		case "/projects/g1/publish":
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				t.Errorf("publish multipart: %v", err)
			}
			v := r.FormValue("version")
			if f.versions[v] {
				w.WriteHeader(409)
				io.WriteString(w, `{"error":{"code":"version_exists","message":"version already published"}}`)
				return
			}
			f.versions[v] = true
			f.seen["version"], f.seen["notes"] = v, r.FormValue("notes")
			file, _, err := r.FormFile("build")
			if err != nil {
				t.Errorf("build part: %v", err)
			} else {
				b, _ := io.ReadAll(file)
				f.seen["magic"] = string(b[:2])
			}
			io.WriteString(w, `{"game":{"id":"g1","title":"My Game","version":"`+v+`"},"url":"http://web/games/my-game"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	return f
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

func pngFile(t *testing.T, path string, w, h int) {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoginCreateStorePublishFlow(t *testing.T) {
	api := newFakeAPI(t)
	defer api.srv.Close()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("VAPOUR_API", "")
	t.Setenv("VAPOUR_TOKEN", "")
	wd, _ := os.Getwd()
	work := t.TempDir()
	os.Chdir(work)
	defer os.Chdir(wd)

	if _, err := run(t, "login", "--api", api.srv.URL, "--token", "vpr_bad"); err == nil {
		t.Fatal("bad token accepted")
	}
	if out, err := run(t, "login", "--api", api.srv.URL, "--token", "vpr_good"); err != nil || !strings.Contains(out, "Logged in as Dev (dev)") {
		t.Fatalf("login: %v %q", err, out)
	}
	if out, err := run(t, "whoami"); err != nil || !strings.Contains(out, "Dev <d@x> (dev)") {
		t.Fatalf("whoami: %v %q", err, out)
	}

	// create scaffolds the store template files and registers the store paths in vapour.toml.
	if _, err := run(t, "create", "My Game", "--engine", "godot"); err != nil {
		t.Fatal(err)
	}
	game := filepath.Join(work, "my-game")
	mf, _ := os.ReadFile(filepath.Join(game, "vapour.toml"))
	for _, want := range []string{"id = 'g1'", "page = 'store/page.json'", "content = 'store/template.md'"} {
		if !strings.Contains(string(mf), want) {
			t.Errorf("vapour.toml misses %q:\n%s", want, mf)
		}
	}
	for _, f := range []string{"store/template.md", "store/page.json", "store/page.schema.json", "store/README.md", "README.md", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(game, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	if out, err := run(t, "store", "check", "--dir", game); err != nil || !strings.Contains(out, "Store page looks good") {
		t.Fatalf("store check on the scaffold: %v %q", err, out)
	}

	// No build yet: publish must fail before touching the server.
	if _, err := run(t, "publish", "--dir", game, "--version", "0.1.0", "--notes", "Initial test release"); err == nil {
		t.Fatal("publish without a build should fail")
	}
	if err := os.MkdirAll(filepath.Join(game, "build/web"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(game, "build/web/index.html"), []byte("<html></html>"), 0o644)

	// Add real media to the page template.
	pngFile(t, filepath.Join(game, "store/banner.png"), 1920, 620)
	pngFile(t, filepath.Join(game, "store/shot-1.png"), 1280, 720)
	pagePath := filepath.Join(game, "store/page.json")
	pj, _ := os.ReadFile(pagePath)
	pj = []byte(strings.Replace(string(pj), `"banner": ""`, `"banner": "banner.png"`, 1))
	pj = []byte(strings.Replace(string(pj), `"screenshots": []`, `"screenshots": ["shot-1.png"]`, 1))
	pj = []byte(strings.Replace(string(pj), `"category": "casual"`, `"category": "arcade"`, 1))
	os.WriteFile(pagePath, pj, 0o644)

	if out, err := run(t, "publish", "--dir", game, "--dry-run", "--version", "0.1.0", "--notes", "Dry run check"); err != nil || !strings.Contains(out, "Dry run") || !strings.Contains(out, "banner") {
		t.Fatalf("dry run: %v %q", err, out)
	}
	if len(api.versions) != 0 || api.storeMeta != nil {
		t.Fatal("dry run touched the server")
	}

	out, err := run(t, "publish", "--dir", game, "--version", "0.2.0", "--notes", "First public release")
	if err != nil || !strings.Contains(out, "http://web/games/my-game") || !strings.Contains(out, "Store page updated") {
		t.Fatalf("publish: %v %q", err, out)
	}
	if api.seen["version"] != "0.2.0" || api.seen["notes"] != "First public release" || api.seen["magic"] != "PK" {
		t.Errorf("server saw %v", api.seen)
	}
	if api.storeMeta["category"] != "arcade" || api.storeMeta["tagline"] == nil {
		t.Errorf("store metadata: %v", api.storeMeta)
	}
	if d, _ := api.storeMeta["description"].(string); !strings.Contains(d, "## About") {
		t.Errorf("description (template.md) not sent: %q", d)
	}
	page, _ := api.storeMeta["page"].(map[string]any)
	if page == nil || page["version"] != float64(1) || len(page["sections"].([]any)) != 9 {
		t.Errorf("page layout not sent: %v", page)
	}
	if _, leaked := api.storeMeta["media"]; leaked {
		t.Error("media paths leaked into metadata")
	}
	got := strings.Join(api.storeFlds, ",")
	if !strings.Contains(got, "banner") || !strings.Contains(got, "screenshot") {
		t.Errorf("uploaded media fields: %q", got)
	}

	// The same version cannot be published twice; the message tells the developer what to do.
	if _, err := run(t, "publish", "--dir", game, "--version", "0.2.0", "--notes", "Duplicate", "--skip-store"); err == nil || !strings.Contains(err.Error(), "Pass a new --version") {
		t.Errorf("repeat version: %v", err)
	}
	// --skip-store publishes a new version without touching the store page.
	api.storeMeta = nil
	if _, err := run(t, "publish", "--dir", game, "--version", "0.3.0", "--notes", "Skip store", "--skip-store"); err != nil || api.storeMeta != nil {
		t.Errorf("skip-store: %v meta=%v", err, api.storeMeta)
	}
	// store push updates only the page.
	if out, err := run(t, "store", "push", "--dir", game); err != nil || !strings.Contains(out, "Store page updated") {
		t.Errorf("store push: %v %q", err, out)
	}
}

func TestRejectsBadMediaBeforeUploading(t *testing.T) {
	api := newFakeAPI(t)
	defer api.srv.Close()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("VAPOUR_TOKEN", "vpr_good")
	t.Setenv("VAPOUR_API", api.srv.URL)
	wd, _ := os.Getwd()
	work := t.TempDir()
	os.Chdir(work)
	defer os.Chdir(wd)
	if _, err := run(t, "create", "Bad Media"); err != nil {
		t.Fatal(err)
	}
	game := filepath.Join(work, "bad-media")
	os.MkdirAll(filepath.Join(game, "build/web"), 0o755)
	os.WriteFile(filepath.Join(game, "build/web/index.html"), []byte("<html></html>"), 0o644)
	os.WriteFile(filepath.Join(game, "store/evil.png"), []byte("<svg onload=alert(1)></svg>"), 0o644)
	p := filepath.Join(game, "store/page.json")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, []byte(strings.Replace(string(b), `"banner": ""`, `"banner": "evil.png"`, 1)), 0o644)
	if _, err := run(t, "publish", "--dir", game, "--version", "0.2.0", "--notes", "Bad media test"); err == nil || !strings.Contains(err.Error(), "PNG, JPEG or WebP") {
		t.Errorf("svg disguised as png: %v", err)
	}
	if len(api.versions) != 0 || api.storeMeta != nil {
		t.Error("bad media reached the server")
	}
}

func TestErrorsPointToTheDocs(t *testing.T) {
	msg := ErrorMessage(errors.New("could not reach the server"))
	if !strings.HasPrefix(msg, "error: could not reach the server") || !strings.Contains(msg, "https://vapour-dev-docs.pages.dev/") {
		t.Errorf("error message: %q", msg)
	}
	out, _ := run(t, "--help")
	if !strings.Contains(out, "https://vapour-dev-docs.pages.dev/") {
		t.Errorf("help does not mention the docs:\n%s", out)
	}
}

func TestPublishRequiresVersionAndNotes(t *testing.T) {
	api := newFakeAPI(t)
	defer api.srv.Close()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("VAPOUR_API", "")
	t.Setenv("VAPOUR_TOKEN", "")
	wd, _ := os.Getwd()
	work := t.TempDir()
	os.Chdir(work)
	defer os.Chdir(wd)

	if _, err := run(t, "login", "--api", api.srv.URL, "--token", "vpr_good"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "create", "Flag Checks", "--engine", "godot"); err != nil {
		t.Fatal(err)
	}
	game := filepath.Join(work, "flag-checks")
	if err := os.MkdirAll(filepath.Join(game, "build/web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(game, "build/web/index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := run(t, "publish", "--dir", game, "--skip-store", "--notes", "x"); err == nil || !strings.Contains(err.Error(), "--version is required") {
		t.Fatalf("missing version: %v", err)
	}
	if _, err := run(t, "publish", "--dir", game, "--skip-store", "--version", "0.2.0"); err == nil || !strings.Contains(err.Error(), "release notes are required") {
		t.Fatalf("missing notes: %v", err)
	}
	notesPath := filepath.Join(game, "notes.md")
	if err := os.WriteFile(notesPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "publish", "--dir", game, "--skip-store", "--version", "0.2.0", "--notes", "x", "--notes-file", notesPath); err == nil || !strings.Contains(err.Error(), "exactly one of --notes or --notes-file") {
		t.Fatalf("both notes flags: %v", err)
	}
	if _, err := run(t, "publish", "--dir", game, "--skip-store", "--version", "0.2.0", "--notes-file", filepath.Join(game, "missing-notes.md")); err == nil || !strings.Contains(err.Error(), "release notes file") {
		t.Fatalf("missing notes file: %v", err)
	}
	if err := os.WriteFile(notesPath, []byte("\n \t\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "publish", "--dir", game, "--skip-store", "--version", "0.2.0", "--notes-file", notesPath); err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("empty notes file: %v", err)
	}
	if _, err := run(t, "publish", "--dir", game, "--dry-run", "--skip-store", "--notes", "x"); err == nil || !strings.Contains(err.Error(), "--version is required") {
		t.Fatalf("dry-run without version: %v", err)
	}
	if _, err := run(t, "publish", "--dir", game, "--dry-run", "--skip-store", "--version", "0.2.0"); err == nil || !strings.Contains(err.Error(), "release notes are required") {
		t.Fatalf("dry-run without notes: %v", err)
	}
}

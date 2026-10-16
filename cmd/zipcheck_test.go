package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vapour-computer/vapour-cli/internal/zipsign"
)

var eicarParts = []string{
	"X5O!P%@AP[4\\PZX54(P^)7CC)7}$",
	"EICAR-STANDARD-ANTIVIRUS-TEST-FILE!",
	"$H+H*",
}

func eicarString() string { return strings.Join(eicarParts, "") }

type fakeZipAPI struct {
	mu sync.Mutex

	requests int

	publishParts map[string][]byte
	publishTypes map[string]string

	verifyStatus int
	verifyBody   string

	srv *httptest.Server
}

func newFakeZipAPI(t *testing.T) *fakeZipAPI {
	t.Helper()
	f := &fakeZipAPI{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests++
		f.mu.Unlock()

		if r.Header.Get("Authorization") != "Bearer vpr_good" {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":{"code":"unauthorized","message":"bad token"}}`)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/g1":
			io.WriteString(w, `{"id":"g1","slug":"zip-check","title":"Zip Check","releases":[]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/projects/g1/publish":
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				t.Errorf("publish multipart: %v", err)
			}
			parts := map[string][]byte{}
			types := map[string]string{}
			for field, files := range r.MultipartForm.File {
				for _, h := range files {
					file, err := h.Open()
					if err != nil {
						t.Errorf("open part %s: %v", field, err)
						continue
					}
					b, err := io.ReadAll(file)
					file.Close()
					if err != nil {
						t.Errorf("read part %s: %v", field, err)
						continue
					}
					parts[field] = b
					types[field] = h.Header.Get("Content-Type")
				}
			}
			f.mu.Lock()
			f.publishParts = parts
			f.publishTypes = types
			f.mu.Unlock()
			io.WriteString(w, `{"game":{"id":"g1","title":"Zip Check","version":"0.1.0"},"url":"http://web/games/zip-check"}`)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/projects/g1/versions/") && strings.HasSuffix(r.URL.Path, "/verification"):
			f.mu.Lock()
			status := f.verifyStatus
			body := f.verifyBody
			f.mu.Unlock()
			if status != 0 {
				w.WriteHeader(status)
			}
			if body == "" {
				body = `{"status":"verified"}`
			}
			io.WriteString(w, body)
		case r.Method == http.MethodPost && r.URL.Path == "/projects":
			w.WriteHeader(201)
			io.WriteString(w, `{"id":"g1","slug":"zip-check","title":"Zip Check","engine":"godot","status":"draft"}`)
		default:
			w.WriteHeader(404)
			io.WriteString(w, `{"error":{"code":"not_found","message":"no such route"}}`)
		}
	}))
	return f
}

func (f *fakeZipAPI) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func (f *fakeZipAPI) setVerification(status int, body string) {
	f.mu.Lock()
	f.verifyStatus = status
	f.verifyBody = body
	f.mu.Unlock()
}

func zipEnv(t *testing.T) *fakeZipAPI {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("VAPOUR_TOKEN", "vpr_good")
	api := newFakeZipAPI(t)
	t.Setenv("VAPOUR_API", api.srv.URL)
	t.Cleanup(api.srv.Close)
	return api
}

func writeZipFile(t *testing.T, path string, files map[string][]byte) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Unix(0, 0).UTC()}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFolderProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	manifest := `[project]
title = "Zip Check"
slug = "zip-check"
engine = "godot"
id = "g1"
version = "0.1.0"

[build]
dir = "build/web"
`
	if err := os.WriteFile(filepath.Join(dir, "vapour.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "build", "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "build", "web", "index.html"), []byte("<html><body>hi</body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeZipProject(t *testing.T, files map[string][]byte) (string, string) {
	t.Helper()
	dir := t.TempDir()
	manifest := `[project]
title = "Zip Check"
slug = "zip-check"
engine = "godot"
id = "g1"
version = "0.1.0"

[build]
dir = "build.zip"
`
	if err := os.WriteFile(filepath.Join(dir, "vapour.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(dir, "build.zip")
	writeZipFile(t, zipPath, files)
	return dir, zipPath
}

func setupAttestedZip(t *testing.T) (dir string, zipPath string, attPath string) {
	t.Helper()
	dir, zipPath = writeZipProject(t, map[string][]byte{"index.html": []byte("<html>ok</html>")})
	out, err := run(t, "publish", "--dir", dir, "--dry-run", "--skip-store", "--version", "0.1.0", "--notes", "x")
	if err != nil {
		t.Fatalf("publish dry-run: %v\n%s", err, out)
	}
	attPath = filepath.Join(dir, ".vapour", attestationName)
	return dir, zipPath, attPath
}

func TestScanCommandModes(t *testing.T) {
	zipEnv(t)
	projectDir := writeFolderProject(t)
	cleanZip := filepath.Join(t.TempDir(), "clean.zip")
	writeZipFile(t, cleanZip, map[string][]byte{"index.html": []byte("<html>ok</html>")})

	out, err := run(t, "scan", cleanZip)
	if err != nil || !strings.Contains(out, "No problems found.") {
		t.Fatalf("scan zip: err=%v out=%q", err, out)
	}
	out, err = run(t, "scan", "--json", cleanZip)
	if err != nil || !strings.Contains(out, `"findings": []`) {
		t.Fatalf("scan zip --json: err=%v out=%q", err, out)
	}

	out, err = run(t, "scan", filepath.Join(projectDir, "build", "web"))
	if err != nil || !strings.Contains(out, "No problems found.") {
		t.Fatalf("scan folder: err=%v out=%q", err, out)
	}
	out, err = run(t, "scan", "--dir", projectDir)
	if err != nil || !strings.Contains(out, "No problems found.") {
		t.Fatalf("scan no arg: err=%v out=%q", err, out)
	}
}

func TestScanRejectsEICAR(t *testing.T) {
	zipEnv(t)
	badZip := filepath.Join(t.TempDir(), "bad.zip")
	writeZipFile(t, badZip, map[string][]byte{"eicar.txt": []byte(eicarString())})

	out, err := run(t, "scan", badZip)
	if err == nil || !strings.Contains(err.Error(), "build rejected") || !strings.Contains(out, "test_malware_signature") {
		t.Fatalf("scan eicar: err=%v out=%q", err, out)
	}

	out, err = run(t, "scan", "--json", badZip)
	if err == nil || !strings.Contains(out, "test_malware_signature") {
		t.Fatalf("scan eicar --json: err=%v out=%q", err, out)
	}
}

func TestPublishDryRunSignsWithoutNetwork(t *testing.T) {
	api := zipEnv(t)
	dir := writeFolderProject(t)

	out, err := run(t, "publish", "--dir", dir, "--dry-run", "--version", "0.1.0", "--notes", "x")
	if err != nil {
		t.Fatalf("publish dry-run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Signed: SHA-256") {
		t.Fatalf("missing signed line: %q", out)
	}
	if api.count() != 0 {
		t.Fatalf("dry-run touched network: %d request(s)", api.count())
	}

	attPath := filepath.Join(dir, ".vapour", attestationName)
	if _, err := os.Stat(attPath); err != nil {
		t.Fatalf("attestation missing: %v", err)
	}
	keyPath := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "vapour", "signing.key")
	st, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("signing key missing: %v", err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("signing key mode = %o, want 600", st.Mode().Perm())
	}
}

func TestPublishRejectsUnsafeBuildBeforeHTTP(t *testing.T) {
	tests := []struct {
		name  string
		files map[string][]byte
	}{
		{name: "dll", files: map[string][]byte{"index.html": []byte("<html>ok</html>"), "evil.dll": []byte("x")}},
		{name: "eicar", files: map[string][]byte{"index.html": []byte("<html>ok</html>"), "eicar.txt": []byte(eicarString())}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api := zipEnv(t)
			dir, _ := writeZipProject(t, tc.files)

			out, err := run(t, "publish", "--dir", dir, "--version", "0.1.0", "--notes", "x", "--skip-store")
			if err == nil || !strings.Contains(err.Error(), "Nothing was uploaded") {
				t.Fatalf("publish should fail: err=%v out=%q", err, out)
			}
			if api.count() != 0 {
				t.Fatalf("unsafe build reached server: %d request(s)", api.count())
			}
			if _, statErr := os.Stat(filepath.Join(dir, ".vapour", attestationName)); !os.IsNotExist(statErr) {
				t.Fatalf("attestation should not exist, stat err=%v", statErr)
			}
		})
	}
}

func TestPublishUploadsAttestationAndBuild(t *testing.T) {
	api := zipEnv(t)
	dir := writeFolderProject(t)

	out, err := run(t, "publish", "--dir", dir, "--version", "0.1.0", "--notes", "x", "--skip-store")
	if err != nil {
		t.Fatalf("publish: %v\n%s", err, out)
	}
	if !strings.Contains(out, "vapour verify --remote --version 0.1.0") {
		t.Fatalf("missing verify hint: %q", out)
	}

	api.mu.Lock()
	attBytes := append([]byte(nil), api.publishParts["attestation"]...)
	buildBytes := append([]byte(nil), api.publishParts["build"]...)
	attType := api.publishTypes["attestation"]
	api.mu.Unlock()

	if len(attBytes) == 0 {
		t.Fatal("missing attestation part")
	}
	if len(buildBytes) == 0 {
		t.Fatal("missing build part")
	}
	if attType != "application/json" {
		t.Fatalf("attestation content-type = %q, want application/json", attType)
	}

	att, err := zipsign.Parse(attBytes)
	if err != nil {
		t.Fatalf("parse attestation: %v", err)
	}
	tmpZip := filepath.Join(t.TempDir(), "uploaded.zip")
	if err := os.WriteFile(tmpZip, buildBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := zipsign.Verify(tmpZip, att); err != nil {
		t.Fatalf("verify attestation against build part: %v", err)
	}
}

func TestChunkUploadCannotBypassVerificationGate(t *testing.T) {
	api := zipEnv(t)
	dir := writeFolderProject(t)
	out, err := run(t, "publish", "--dir", dir, "--version", "0.1.0", "--notes", "x", "--skip-store", "--experimental-chunks")
	if err == nil || !strings.Contains(err.Error(), "cannot upload without a signed build attestation") {
		t.Fatalf("chunk upload should be blocked: err=%v out=%q", err, out)
	}
	if api.count() != 0 {
		t.Fatalf("chunk upload reached server: %d request(s)", api.count())
	}
}

func TestVerifyLocalZipAndTampering(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		zipEnv(t)
		dir, zipPath, _ := setupAttestedZip(t)
		out, err := run(t, "verify", "--dir", dir, zipPath)
		if err != nil || !strings.Contains(out, "OK: signature valid") {
			t.Fatalf("verify ok: err=%v out=%q", err, out)
		}
	})

	t.Run("hash mismatch json", func(t *testing.T) {
		zipEnv(t)
		dir, zipPath, _ := setupAttestedZip(t)
		b, err := os.ReadFile(zipPath)
		if err != nil {
			t.Fatal(err)
		}
		if len(b) == 0 {
			t.Fatal("zip is empty")
		}
		b[len(b)-1] ^= 0x01
		if err := os.WriteFile(zipPath, b, 0o644); err != nil {
			t.Fatal(err)
		}

		out, err := run(t, "verify", "--dir", dir, "--json", zipPath)
		if err == nil || !strings.Contains(out, `"ok": false`) || !strings.Contains(out, `"local": "hash_mismatch"`) {
			t.Fatalf("verify mismatch: err=%v out=%q", err, out)
		}
	})

	t.Run("bad signature after entries edit", func(t *testing.T) {
		zipEnv(t)
		dir, zipPath, attPath := setupAttestedZip(t)
		raw, err := os.ReadFile(attPath)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		m["entries"] = 999.0
		b, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(attPath, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}

		out, err := run(t, "verify", "--dir", dir, "--json", zipPath)
		if err == nil || !strings.Contains(out, `"local": "bad_signature"`) {
			t.Fatalf("verify bad signature: err=%v out=%q", err, out)
		}
	})
}

func TestVerifyRemoteStatuses(t *testing.T) {
	api := zipEnv(t)
	dir, _, attPath := setupAttestedZip(t)

	raw, err := os.ReadFile(attPath)
	if err != nil {
		t.Fatal(err)
	}
	att, err := zipsign.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}

	api.setVerification(200, `{"status":"verified","sha256":"`+att.SHA256+`"}`)
	out, err := run(t, "verify", "--dir", dir, "--remote", "--version", "0.1.0")
	if err != nil || !strings.Contains(out, "OK: the server recorded the same hash") {
		t.Fatalf("remote verified: err=%v out=%q", err, out)
	}

	api.setVerification(200, `{"status":"pending","sha256":"`+att.SHA256+`"}`)
	_, err = run(t, "verify", "--dir", dir, "--remote", "--version", "0.1.0")
	if err == nil || !strings.Contains(err.Error(), `"pending"`) {
		t.Fatalf("remote pending: %v", err)
	}

	api.setVerification(200, `{"status":"rejected","sha256":"`+att.SHA256+`"}`)
	_, err = run(t, "verify", "--dir", dir, "--remote", "--version", "0.1.0")
	if err == nil || !strings.Contains(err.Error(), `"rejected"`) {
		t.Fatalf("remote rejected: %v", err)
	}

	api.setVerification(200, `{"status":"verified","sha256":"deadbeef"}`)
	_, err = run(t, "verify", "--dir", dir, "--remote", "--version", "0.1.0")
	if err == nil || !strings.Contains(err.Error(), "the server recorded SHA-256") {
		t.Fatalf("remote hash mismatch: %v", err)
	}

	api.setVerification(404, `{"error":{"code":"not_found","message":"no such route"}}`)
	_, err = run(t, "verify", "--dir", dir, "--remote", "--version", "0.1.0")
	if err == nil {
		t.Fatal("remote 404 should fail")
	}

	_, err = run(t, "verify", "--dir", dir)
	if err == nil || !strings.Contains(err.Error(), "pass the zip to check, or use --remote") {
		t.Fatalf("missing zip and --remote: %v", err)
	}
}

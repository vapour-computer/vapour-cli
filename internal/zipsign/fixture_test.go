package zipsign

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vapour-computer/vapour-cli/internal/zipscan"
)

var update = flag.Bool("update", false, "rewrite testdata")

func SignedBytesForTest(a *Attestation) ([]byte, error) { return a.signedBytes() }

type fixtureFiles struct {
	zip       []byte
	att       []byte
	canonical []byte
	pubkey    []byte
}

func buildInteropFixture(t *testing.T) fixtureFiles {
	t.Helper()

	var z bytes.Buffer
	zw := zip.NewWriter(&z)
	fixed := time.Unix(0, 0).UTC()
	entries := []struct {
		name string
		data []byte
	}{
		{name: "index.html", data: []byte("<html><body>interop</body></html>\n")},
		{name: "game.js", data: []byte("console.log('interop');\n")},
	}
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Store, Modified: fixed}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	tmp := filepath.Join(t.TempDir(), "sample.zip")
	if err := os.WriteFile(tmp, z.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	seed := bytes.Repeat([]byte{7}, 32)
	priv := ed25519.NewKeyFromSeed(seed)
	rep := &zipscan.Report{
		Entries: 2,
		Findings: []zipscan.Finding{{
			Severity: zipscan.SeverityWarn,
			Code:     "interop_warn",
			Path:     "a<b>&c.js",
			Message:  "contains < and &\t and é and \u2028",
		}},
	}
	att, err := Sign(tmp, rep, priv, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	attJSON, err := att.JSON()
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := SignedBytesForTest(att)
	if err != nil {
		t.Fatal(err)
	}
	pub := priv.Public().(ed25519.PublicKey)

	return fixtureFiles{
		zip:       z.Bytes(),
		att:       append(attJSON, '\n'),
		canonical: canonical,
		pubkey:    []byte(base64.StdEncoding.EncodeToString(pub) + "\n"),
	}
}

func writeFixture(t *testing.T, f fixtureFiles) {
	t.Helper()
	dir := filepath.Join("testdata")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, b := range map[string][]byte{
		filepath.Join(dir, "sample.zip"):                  f.zip,
		filepath.Join(dir, "sample.zip.attestation.json"): f.att,
		filepath.Join(dir, "sample.canonical.txt"):        f.canonical,
		filepath.Join(dir, "sample.pubkey.b64"):           f.pubkey,
	} {
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readFixture(t *testing.T) fixtureFiles {
	t.Helper()
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	return fixtureFiles{
		zip:       read("sample.zip"),
		att:       read("sample.zip.attestation.json"),
		canonical: read("sample.canonical.txt"),
		pubkey:    read("sample.pubkey.b64"),
	}
}

func TestInteropFixture(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	want := buildInteropFixture(t)
	if *update {
		writeFixture(t, want)
	}

	got := readFixture(t)
	for name, pair := range map[string]struct{ want, got []byte }{
		"sample.zip":                  {want: want.zip, got: got.zip},
		"sample.zip.attestation.json": {want: want.att, got: got.att},
		"sample.canonical.txt":        {want: want.canonical, got: got.canonical},
		"sample.pubkey.b64":           {want: want.pubkey, got: got.pubkey},
	} {
		if !bytes.Equal(pair.want, pair.got) {
			t.Fatalf("%s drifted; run go test ./internal/zipsign -update", name)
		}
	}

	tmp := filepath.Join(t.TempDir(), "sample.zip")
	if err := os.WriteFile(tmp, got.zip, 0o644); err != nil {
		t.Fatal(err)
	}
	att, err := Parse(got.att)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(tmp, att); err != nil {
		t.Fatalf("fixture verify: %v", err)
	}

	canon := string(got.canonical)
	if !strings.HasPrefix(canon, "vapour-zip-attestation-v1\n{\"entries\":") {
		t.Fatalf("canonical prefix mismatch: %q", canon)
	}
	if !strings.Contains(canon, "\\u2028") {
		t.Fatal("canonical missing U+2028")
	}
	if !strings.Contains(canon, "<") || !strings.Contains(canon, "&") || !strings.Contains(canon, " ") {
		t.Fatal("canonical missing literal <, & or space")
	}
}

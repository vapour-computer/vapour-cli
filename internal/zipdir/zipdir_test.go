package zipdir

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckAndZip(t *testing.T) {
	d := t.TempDir()
	if Check(d) == nil {
		t.Error("missing index.html accepted")
	}
	write(t, d, "index.html", "<html></html>")
	write(t, d, "sub/game.wasm", "\x00asm")
	write(t, d, ".git/config", "x")
	if err := Check(d); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	n, err := Zip(d, &buf)
	if err != nil || n != 2 {
		t.Fatalf("zip: %d %v", n, err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["index.html"] || !names["sub/game.wasm"] || names[".git/config"] {
		t.Errorf("entries: %v", names)
	}
	write(t, d, "game.exe", "MZ")
	if Check(d) == nil {
		t.Error("native binary accepted")
	}
}

func TestIsZip(t *testing.T) {
	d := t.TempDir()
	write(t, d, "a.zip", "PK\x03\x04rest")
	write(t, d, "b.zip", "nope")
	if !IsZip(filepath.Join(d, "a.zip")) || IsZip(filepath.Join(d, "b.zip")) || IsZip(filepath.Join(d, "none")) {
		t.Error("IsZip wrong")
	}
}

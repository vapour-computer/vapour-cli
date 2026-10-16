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
	write(t, d, "sub/game.js", "console.log(1)")
	write(t, d, "sub/game.wasm.unityweb", "\x1f\x8b\x08unity")
	write(t, d, "sub/game2.wasm.unityweb", "plain")
	write(t, d, ".git/config", "x")
	if err := Check(d); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	n, err := Zip(d, &buf)
	if err != nil || n != 5 {
		t.Fatalf("zip: %d %v", n, err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	methods := map[string]uint16{}
	for _, f := range zr.File {
		names[f.Name] = true
		methods[f.Name] = f.Method
	}
	if !names["index.html"] || !names["sub/game.wasm"] || !names["sub/game.js"] || !names["sub/game.wasm.unityweb"] || !names["sub/game2.wasm.unityweb"] || names[".git/config"] {
		t.Errorf("entries: %v", names)
	}
	if methods["sub/game.wasm"] != zip.Deflate {
		t.Fatalf("wasm method = %d, want deflate", methods["sub/game.wasm"])
	}
	if methods["sub/game.js"] != zip.Deflate {
		t.Fatalf("js method = %d, want deflate", methods["sub/game.js"])
	}
	if methods["sub/game.wasm.unityweb"] != zip.Store {
		t.Fatalf("gzip unityweb method = %d, want store", methods["sub/game.wasm.unityweb"])
	}
	if methods["sub/game2.wasm.unityweb"] != zip.Deflate {
		t.Fatalf("plain unityweb method = %d, want deflate", methods["sub/game2.wasm.unityweb"])
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

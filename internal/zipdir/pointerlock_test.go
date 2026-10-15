package zipdir

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestInjectPointerLock(t *testing.T) {
	cases := map[string]string{
		"head":    "<html><HEAD lang=x><title>g</title></HEAD><body></body></html>",
		"no head": "<html><body></body></html>",
		"bare":    "just text",
	}
	for name, in := range cases {
		out := InjectPointerLock([]byte(in))
		if bytes.Count(out, []byte(PointerLockMarker)) != 1 {
			t.Errorf("%s: want one script, got %q", name, out)
		}
		if !strings.Contains(string(out), "requestPointerLock") {
			t.Errorf("%s: script body missing", name)
		}
		if again := InjectPointerLock(out); !bytes.Equal(again, out) {
			t.Errorf("%s: second injection changed the page", name)
		}
	}
	got := string(InjectPointerLock([]byte(cases["head"])))
	if !strings.HasPrefix(got, "<html><HEAD lang=x><script") {
		t.Errorf("script should be first in <head>: %q", got)
	}
	if !strings.Contains(got, "target=this") {
		t.Errorf("script should remember request target: %q", got)
	}
	if strings.Contains(got, "querySelector('canvas')") {
		t.Errorf("script should re-request lock on saved target, not first canvas: %q", got)
	}
}

func TestZipWithPointerLock(t *testing.T) {
	d := t.TempDir()
	write(t, d, "index.html", "<html><head></head></html>")
	write(t, d, "sub/index.html", "<html></html>")
	write(t, d, "game.pck", "data")

	read := func(opts Options) map[string]string {
		var buf bytes.Buffer
		if _, err := ZipWith(d, &buf, opts); err != nil {
			t.Fatal(err)
		}
		zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		if err != nil {
			t.Fatal(err)
		}
		files := map[string]string{}
		for _, f := range zr.File {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			files[f.Name] = string(b)
		}
		return files
	}

	on := read(Options{PointerLock: true})
	if !strings.Contains(on["index.html"], PointerLockMarker) {
		t.Error("root index.html not injected")
	}
	if strings.Contains(on["sub/index.html"], PointerLockMarker) {
		t.Error("nested index.html must not be touched")
	}
	if on["game.pck"] != "data" {
		t.Error("other files must be unchanged")
	}
	off := read(Options{})
	if strings.Contains(off["index.html"], PointerLockMarker) {
		t.Error("injected although disabled")
	}
	entry := read(Options{PointerLock: true, PointerLockEntry: "sub/index.html"})
	if strings.Contains(entry["index.html"], PointerLockMarker) {
		t.Error("root index.html should not be injected when runtime.entry is set")
	}
	if !strings.Contains(entry["sub/index.html"], PointerLockMarker) {
		t.Error("runtime.entry HTML should be injected")
	}
}

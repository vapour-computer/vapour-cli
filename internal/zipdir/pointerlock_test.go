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
		out := InjectPointerLock([]byte(in), false)
		if bytes.Count(out, []byte(PointerLockMarker)) != 1 {
			t.Errorf("%s: want one script, got %q", name, out)
		}
		if !strings.Contains(string(out), "requestPointerLock") {
			t.Errorf("%s: script body missing", name)
		}
		if again := InjectPointerLock(out, false); !bytes.Equal(again, out) {
			t.Errorf("%s: second injection changed the page", name)
		}
	}
	got := string(InjectPointerLock([]byte(cases["head"]), true))
	if !strings.HasPrefix(got, "<html><HEAD lang=x><script") {
		t.Errorf("script should be first in <head>: %q", got)
	}
	if !strings.Contains(got, "target=this") {
		t.Errorf("script should remember request target: %q", got)
	}
	for _, want := range []string{"Click to play", "Click to resume", "stopImmediatePropagation", "unadjustedMovement", "pauseOnLoss=true", "return request(this)", "return res", "spikeUntil=performance.now()+250", "now<spikeUntil", "(pointer: fine)", "sawMouse"} {
		if !strings.Contains(got, want) {
			t.Errorf("script missing %q: %q", want, got)
		}
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
	if !strings.Contains(on["index.html"], "pauseOnLoss=false") {
		t.Error("default pointer lock mode should not pause on lock loss")
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
	pause := read(Options{PointerLock: true, PointerLockPause: true})
	if !strings.Contains(pause["index.html"], "pauseOnLoss=true") {
		t.Error("pause mode should be injected")
	}
	entry := read(Options{PointerLock: true, PointerLockEntry: "sub/index.html"})
	if strings.Contains(entry["index.html"], PointerLockMarker) {
		t.Error("root index.html should not be injected when runtime.entry is set")
	}
	if !strings.Contains(entry["sub/index.html"], PointerLockMarker) {
		t.Error("runtime.entry HTML should be injected")
	}
}

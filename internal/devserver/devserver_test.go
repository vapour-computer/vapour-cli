package devserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func site() fstest.MapFS {
	return fstest.MapFS{
		"index.html": {Data: []byte("<html><head><title>g</title></head><body></body></html>")},
		"game.wasm":  {Data: []byte("\x00asm")},
	}
}

func get(t *testing.T, h http.Handler, method, path string, hdr map[string]string, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestInjectsOverlayOnlyWhenEnabled(t *testing.T) {
	on := get(t, Handler(Options{FS: site(), Telemetry: true}), "GET", "/", nil, "")
	if !strings.Contains(on.Body.String(), scriptTag+"</head>") {
		t.Fatalf("overlay not injected before </head>: %s", on.Body)
	}
	off := get(t, Handler(Options{FS: site()}), "GET", "/", nil, "")
	if strings.Contains(off.Body.String(), "__vapour") {
		t.Fatalf("overlay injected with telemetry off: %s", off.Body)
	}
	if w := get(t, Handler(Options{FS: site()}), "GET", fpsPath, nil, ""); w.Code != http.StatusNotFound {
		t.Fatalf("fps.js served with telemetry off: %d", w.Code)
	}
}

func TestIsolationHeadersAndWasmType(t *testing.T) {
	w := get(t, Handler(Options{FS: site()}), "GET", "/game.wasm", nil, "")
	if w.Header().Get("Cross-Origin-Opener-Policy") != "same-origin" || w.Header().Get("Cross-Origin-Embedder-Policy") != "require-corp" {
		t.Fatalf("missing isolation headers: %v", w.Header())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/wasm" {
		t.Fatalf("wasm content type = %q", ct)
	}
}

func TestReport(t *testing.T) {
	var got Report
	h := Handler(Options{FS: site(), Telemetry: true, OnReport: func(r Report) { got = r }})
	body := `{"frames":120,"avgFps":59.5,"low1Fps":31,"worstMs":48}`

	if w := get(t, h, "POST", reportPath, map[string]string{"Origin": "http://evil.example"}, body); w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin report accepted: %d", w.Code)
	}
	if w := get(t, h, "GET", reportPath, nil, ""); w.Code != http.StatusForbidden {
		t.Fatalf("GET report accepted: %d", w.Code)
	}
	if w := get(t, h, "POST", reportPath, map[string]string{"Origin": "http://example.com"}, body); w.Code != http.StatusNoContent {
		t.Fatalf("same-origin report rejected: %d", w.Code)
	}
	if got.Frames != 120 || got.Low1FPS != 31 {
		t.Fatalf("report = %+v", got)
	}
}

func TestInjectFallbacks(t *testing.T) {
	if s := string(inject([]byte("<body>x</body>"))); s != "<body>x"+scriptTag+"</body>" {
		t.Fatalf("body fallback: %s", s)
	}
	if s := string(inject([]byte("plain"))); s != "plain"+scriptTag {
		t.Fatalf("append fallback: %s", s)
	}
}

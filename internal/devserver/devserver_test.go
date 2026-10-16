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
		"index.html":              {Data: []byte("<html><head><title>g</title></head><body></body></html>")},
		"game.wasm":               {Data: []byte("\x00asm")},
		"Build/GAME.WASM.BR":      {Data: []byte("wasm")},
		"Build/game.symbols.json": {Data: []byte("{}")},
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
	if w.Header().Get("Cross-Origin-Resource-Policy") != "cross-origin" {
		t.Fatalf("CORP mismatch: %q", w.Header().Get("Cross-Origin-Resource-Policy"))
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("nosniff missing")
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/wasm" {
		t.Fatalf("wasm content type = %q", ct)
	}
}

func TestPrecompressedHeadersUseInnerType(t *testing.T) {
	w := get(t, Handler(Options{FS: site()}), "GET", "/Build/GAME.WASM.BR", nil, "")
	if ct := w.Header().Get("Content-Type"); ct != "application/wasm" {
		t.Fatalf("content type = %q", ct)
	}
	if enc := w.Header().Get("Content-Encoding"); enc != "br" {
		t.Fatalf("encoding = %q", enc)
	}
}

func TestPlayerModeServesIframeShell(t *testing.T) {
	w := get(t, Handler(Options{FS: site(), Player: true, Mobile: true}), "GET", "/", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("shell status: %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "sandbox=\"allow-scripts allow-same-origin") || !strings.Contains(body, "src=\"/play/local/index.html?__vapour_mobile=1\"") {
		t.Fatalf("shell missing iframe attrs: %s", body)
	}
	if !strings.Contains(body, "const mode='store'") {
		t.Fatalf("shell should default to on_quit=store: %s", body)
	}

	asset := get(t, Handler(Options{FS: site(), Player: true}), "GET", "/play/local/game.wasm", nil, "")
	if asset.Code != http.StatusOK || asset.Header().Get("Content-Type") != "application/wasm" {
		t.Fatalf("player asset serve failed: code=%d ct=%q", asset.Code, asset.Header().Get("Content-Type"))
	}
}

func TestPlayerModeUsesManifestOnQuitAndInjectsDevRuntime(t *testing.T) {
	h := Handler(Options{FS: site(), Player: true, OnQuit: "reload"})
	shell := get(t, h, "GET", "/", nil, "")
	if !strings.Contains(shell.Body.String(), "const mode='reload'") || strings.Contains(shell.Body.String(), "e.data.on_quit") {
		t.Fatalf("player should use manifest on_quit, not message payload: %s", shell.Body.String())
	}
	page := get(t, h, "GET", "/play/local/index.html", nil, "")
	body := page.Body.String()
	if !strings.Contains(body, devRuntimeMarker) {
		t.Fatalf("dev runtime marker missing: %s", body)
	}
	for _, want := range []string{"window.vapour", "vapour:quit", "options.onExit", "instance.Quit"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dev runtime missing %q: %s", want, body)
		}
	}
}

func TestMobilePreviewInjectsConfiguredRuntimeHints(t *testing.T) {
	maxDpr := 1.5
	w := get(t, Handler(Options{FS: site(), Player: true, Mobile: true, MobileOrientation: "landscape", MobileRenderScale: 0.5, MobileMaxDPR: &maxDpr}), "GET", "/play/local/index.html?__vapour_mobile=1", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("mobile page status: %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"orientation":"landscape"`) || !strings.Contains(body, `"render_scale":0.5`) || !strings.Contains(body, `"max_dpr":1.5`) {
		t.Fatalf("missing configured runtime settings: %s", body)
	}
	if !strings.Contains(body, "dpr=dpr*s.render_scale") {
		t.Fatalf("expected downscale multiplication not found: %s", body)
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

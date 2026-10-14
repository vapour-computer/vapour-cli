// Package devserver serves a web export on localhost for `vapour test`.
//
// The optional FPS overlay is injected into index.html while serving. The build folder is
// never modified, so published builds can never contain it.
package devserver

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

//go:embed fps.js
var fpsJS []byte

const (
	fpsPath    = "/__vapour/fps.js"
	reportPath = "/__vapour/report"
	scriptTag  = `<script src="` + fpsPath + `"></script>`
)

// Report is the frame summary the overlay sends when the page is closed.
type Report struct {
	Frames  int     `json:"frames"`
	AvgFPS  float64 `json:"avgFps"`
	Low1FPS float64 `json:"low1Fps"`
	WorstMS float64 `json:"worstMs"`
}

// Options configures the server.
type Options struct {
	// FS is the web export. It must contain index.html.
	FS fs.FS
	// Telemetry injects the FPS overlay into index.html.
	Telemetry bool
	// OnReport is called with the summary when a page with the overlay is closed. May be nil.
	OnReport func(Report)
}

func init() {
	// Make sure browsers get the right type for Wasm and engine data files.
	_ = mime.AddExtensionType(".wasm", "application/wasm")
}

// Handler returns the HTTP handler. Wasm games need cross-origin isolation for threads
// (SharedArrayBuffer), so the isolation headers are always sent.
func Handler(o Options) http.Handler {
	files := http.FileServer(http.FS(o.FS))
	mux := http.NewServeMux()

	if o.Telemetry {
		mux.HandleFunc(fpsPath, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write(fpsJS)
		})
		mux.HandleFunc(reportPath, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || !sameOrigin(r) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			var rep Report
			if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&rep); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if o.OnReport != nil {
				o.OnReport(rep)
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			serveIndex(w, o)
			return
		}
		files.ServeHTTP(w, r)
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Embedder-Policy", "require-corp")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, o Options) {
	b, err := fs.ReadFile(o.FS, "index.html")
	if err != nil {
		http.Error(w, "index.html not found in the build", http.StatusNotFound)
		return
	}
	if o.Telemetry {
		b = inject(b)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

// inject adds the overlay script before </head>, else before </body>, else at the end.
func inject(html []byte) []byte {
	s := string(html)
	lower := strings.ToLower(s)
	for _, marker := range []string{"</head>", "</body>"} {
		if i := strings.LastIndex(lower, marker); i >= 0 {
			return []byte(s[:i] + scriptTag + s[i:])
		}
	}
	return []byte(s + scriptTag)
}

// sameOrigin rejects cross-site posts: the Origin header, if present, must match the Host.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}

// Listen opens a loopback-only listener. Port 0 picks a free port.
func Listen(port int) (net.Listener, error) {
	return net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
}

// Serve runs the handler on l until it is closed.
func Serve(l net.Listener, h http.Handler) error {
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	err := srv.Serve(l)
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

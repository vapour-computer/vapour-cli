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
	"path"
	"strconv"
	"strings"
	"time"
)

//go:embed fps.js
var fpsJS []byte

const (
	fpsPath          = "/__vapour/fps.js"
	reportPath       = "/__vapour/report"
	scriptTag        = `<script src="` + fpsPath + `"></script>`
	playPrefix       = "/play/local/"
	devRuntimeMarker = `data-vapour="dev-runtime"`
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
	// Player serves a local player shell with an iframe using production-like sandbox/allow attributes.
	Player bool
	// Mobile enables phone-like shell sizing/hints for --player previews.
	Mobile bool
	// MobileOrientation mirrors vapour.toml [mobile].orientation.
	MobileOrientation string
	// MobileRenderScale mirrors vapour.toml [mobile].render_scale.
	MobileRenderScale float64
	// MobileMaxDPR mirrors vapour.toml [mobile].max_dpr.
	MobileMaxDPR *float64
	// OnQuit mirrors vapour.toml [runtime].on_quit for local player behavior.
	OnQuit string
}

func normalizeOnQuit(value string) string {
	v := strings.TrimSpace(strings.ToLower(value))
	switch v {
	case "home", "reload", "stay":
		return v
	default:
		return "store"
	}
}

func init() {
	// Make sure browsers get the right type for Wasm and engine data files.
	_ = mime.AddExtensionType(".wasm", "application/wasm")
}

// Handler returns the HTTP handler. Wasm games need cross-origin isolation for threads
// (SharedArrayBuffer), so the isolation headers are always sent.
func Handler(o Options) http.Handler {
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
		if o.Player {
			servePlayer(w, r, o)
			return
		}
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			serveIndex(w, o)
			return
		}
		serveBuildFile(w, r, o, strings.TrimPrefix(path.Clean(r.URL.Path), "/"))
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Embedder-Policy", "require-corp")
		h.Set("Cross-Origin-Resource-Policy", "cross-origin")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

func servePlayer(w http.ResponseWriter, r *http.Request, o Options) {
	if r.URL.Path == "/" || r.URL.Path == "/index.html" {
		mobileClass := ""
		mobileScript := ""
		quitMode := normalizeOnQuit(o.OnQuit)
		playerScript := `<script>(function(){const mode='` + quitMode + `';const play=document.getElementById('play');const frame=document.getElementById('vapour-frame');const ended=document.getElementById('quit-panel');const replay=document.getElementById('quit-replay');const back=document.getElementById('quit-back');const start=()=>{frame.focus();frame.contentWindow&&frame.contentWindow.focus();};play.addEventListener('click',start);replay.addEventListener('click',()=>{const src=frame.getAttribute('src')||'';const clean=src.split('#')[0].split('?')[0];frame.setAttribute('src',clean+'?__vapour_reload='+Date.now());ended.style.display='none';});back.addEventListener('click',()=>location.assign('/'));addEventListener('message',(e)=>{if(e.source!==frame.contentWindow||!e.data||e.data.type!=='vapour:quit')return;if(mode==='store'||mode==='home'){location.assign('/');return;}if(mode==='reload'){replay.click();return;}if(mode==='stay'){ended.style.display='block';return;}location.assign('/');});})();</script>`
		if o.Mobile {
			mobileClass = " mobile"
			orientation := o.MobileOrientation
			if orientation == "" {
				orientation = "any"
			}
			mobileScript = `<script>window.__vapourMobilePreview=true;(function(){const gate=document.getElementById('rotate-gate');const update=()=>{if(!gate)return;const portrait=matchMedia('(orientation: portrait)').matches;if("` + orientation + `"==="landscape")gate.style.display=portrait?'flex':'none';else if("` + orientation + `"==="portrait")gate.style.display=portrait?'none':'flex';else gate.style.display='none';};addEventListener('orientationchange',update);addEventListener('resize',update);update();})();</script>`
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		iframeSrc := playPrefix + "index.html"
		if o.Mobile {
			iframeSrc += "?__vapour_mobile=1"
		}
		_, _ = io.WriteString(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head><body class="`+mobileClass+`"><style>html,body{margin:0;height:100%;background:#0b1020;color:#e5e7eb;font-family:system-ui,sans-serif}.wrap{height:100%;display:flex;flex-direction:column}.gate{padding:10px}.stage{flex:1;display:flex;position:relative}.mobile .stage{max-width:430px;max-height:932px;margin:auto}#rotate-gate{position:absolute;inset:0;background:rgba(0,0,0,.8);color:#fff;display:none;align-items:center;justify-content:center;z-index:5;text-align:center;padding:16px}#quit-panel{position:absolute;right:12px;bottom:12px;display:none;z-index:7;background:rgba(0,0,0,.8);border:1px solid rgba(255,255,255,.25);border-radius:10px;padding:10px}iframe{border:0;width:100%;height:100%}</style><div class="wrap"><div class="gate"><button id="play">Play</button></div><div class="stage"><div id="rotate-gate">Rotate device to landscape</div><iframe id="vapour-frame" src="`+iframeSrc+`" sandbox="allow-scripts allow-same-origin allow-pointer-lock allow-forms allow-popups allow-popups-to-escape-sandbox allow-modals allow-downloads allow-orientation-lock allow-presentation" allow="fullscreen; gamepad; autoplay; cross-origin-isolated; keyboard-lock; clipboard-read; clipboard-write; xr-spatial-tracking; screen-wake-lock; accelerometer; gyroscope; web-share"></iframe><div id="quit-panel"><div style="margin-bottom:8px">Game ended</div><button id="quit-replay">Play again</button> <button id="quit-back">Back</button></div></div></div>`+playerScript+mobileScript+`</body></html>`)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/play/local") {
		http.NotFound(w, r)
		return
	}
	rel := strings.TrimPrefix(r.URL.Path, "/play/local/")
	if rel == "" || rel == "/" {
		rel = "index.html"
	}
	serveBuildFile(w, r, o, rel)
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

func serveBuildFile(w http.ResponseWriter, r *http.Request, o Options, rel string) {
	root := o.FS
	rel = strings.TrimPrefix(path.Clean("/"+rel), "/")
	f, err := root.Open(rel)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "file does not support seeking", http.StatusInternalServerError)
		return
	}
	ctype, cenc := contentHeadersForPath(rel)
	if strings.HasSuffix(strings.ToLower(rel), ".unityweb") {
		if sniffed := sniffUnitywebEncoding(rs); sniffed != "" {
			cenc = sniffed
		}
	}
	if (rel == "index.html" || strings.HasSuffix(strings.ToLower(rel), "/index.html")) && r.Method != http.MethodHead {
		html, err := io.ReadAll(rs)
		if err == nil {
			text := string(html)
			if !strings.Contains(text, devRuntimeMarker) {
				text = injectIntoHead(text, devRuntimeScript())
			}
			if o.Mobile && r.URL.Query().Get("__vapour_mobile") == "1" {
				orientation := o.MobileOrientation
				if orientation == "" {
					orientation = "any"
				}
				renderScale := o.MobileRenderScale
				if renderScale <= 0 {
					renderScale = 1
				}
				maxDpr := "null"
				if o.MobileMaxDPR != nil {
					maxDpr = strconv.FormatFloat(*o.MobileMaxDPR, 'f', -1, 64)
				}
				mobileRuntime := `<script>window.__VAPOUR_RUNTIME_SETTINGS={"mobile":{"orientation":"` + orientation + `","render_scale":` + strconv.FormatFloat(renderScale, 'f', -1, 64) + `,"max_dpr":` + maxDpr + `}};(function(){const s=window.__VAPOUR_RUNTIME_SETTINGS.mobile||{};let dpr=window.devicePixelRatio||1;if(typeof s.render_scale==='number'&&s.render_scale>0)dpr=dpr*s.render_scale;if(typeof s.max_dpr==='number'&&s.max_dpr>0)dpr=Math.min(dpr,s.max_dpr);try{Object.defineProperty(window,'devicePixelRatio',{configurable:true,get:()=>dpr});}catch{}})();</script>`
				text = injectIntoHead(text, mobileRuntime)
			}
			w.Header().Set("Content-Type", ctype)
			w.Header().Set("Cache-Control", "no-store")
			_, _ = io.WriteString(w, text)
			return
		}
		_, _ = rs.Seek(0, io.SeekStart)
	}
	w.Header().Set("Content-Type", ctype)
	if cenc != "" {
		w.Header().Set("Content-Encoding", cenc)
	}
	http.ServeContent(w, r, rel, info.ModTime(), rs)
}

func sniffUnitywebEncoding(rs io.ReadSeeker) string {
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return ""
	}
	buf := make([]byte, 512)
	n, _ := rs.Read(buf)
	_, _ = rs.Seek(0, io.SeekStart)
	buf = buf[:n]
	if len(buf) >= 2 && buf[0] == 0x1f && buf[1] == 0x8b {
		return "gzip"
	}
	if bytesContains(buf, []byte("UnityWeb Compressed Content (brotli)")) {
		return "br"
	}
	return ""
}

func bytesContains(haystack, needle []byte) bool {
	if len(needle) == 0 || len(haystack) < len(needle) {
		return false
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if string(haystack[i:i+len(needle)]) == string(needle) {
			return true
		}
	}
	return false
}

func contentHeadersForPath(rel string) (contentType, contentEncoding string) {
	lower := strings.ToLower(rel)
	if strings.HasSuffix(lower, ".br") {
		return contentTypeForPath(strings.TrimSuffix(lower, ".br")), "br"
	}
	if strings.HasSuffix(lower, ".gz") {
		return contentTypeForPath(strings.TrimSuffix(lower, ".gz")), "gzip"
	}
	if strings.HasSuffix(lower, ".unityweb") {
		if strings.HasSuffix(lower, ".wasm.unityweb") {
			return "application/wasm", ""
		}
		if strings.HasSuffix(lower, ".js.unityweb") {
			return "text/javascript; charset=utf-8", ""
		}
		if strings.HasSuffix(lower, ".json.unityweb") {
			return "application/json", ""
		}
		return "application/octet-stream", ""
	}
	return contentTypeForPath(lower), ""
}

func contentTypeForPath(path string) string {
	if strings.HasSuffix(path, ".symbols.json") {
		return "application/json"
	}
	switch ext := strings.ToLower(strings.TrimPrefix(pathpkgExt(path), ".")); ext {
	case "html", "htm":
		return "text/html; charset=utf-8"
	case "js", "mjs":
		return "text/javascript; charset=utf-8"
	case "wasm":
		return "application/wasm"
	case "json":
		return "application/json"
	case "css":
		return "text/css; charset=utf-8"
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	case "svg":
		return "image/svg+xml"
	case "ico":
		return "image/x-icon"
	case "mp3":
		return "audio/mpeg"
	case "ogg":
		return "audio/ogg"
	case "wav":
		return "audio/wav"
	case "webm":
		return "video/webm"
	case "mp4":
		return "video/mp4"
	case "woff":
		return "font/woff"
	case "woff2":
		return "font/woff2"
	case "ttf":
		return "font/ttf"
	case "otf":
		return "font/otf"
	case "xml":
		return "application/xml"
	case "webmanifest":
		return "application/manifest+json"
	case "gif":
		return "image/gif"
	case "flac":
		return "audio/flac"
	case "m4a":
		return "audio/mp4"
	case "glb":
		return "model/gltf-binary"
	case "gltf":
		return "model/gltf+json"
	default:
		return "application/octet-stream"
	}
}

func pathpkgExt(p string) string {
	if i := strings.LastIndexByte(p, '.'); i >= 0 {
		return p[i:]
	}
	return ""
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

func injectIntoHead(html, script string) string {
	lower := strings.ToLower(html)
	if i := strings.Index(lower, "<head>"); i >= 0 {
		insertAt := i + len("<head>")
		return html[:insertAt] + script + html[insertAt:]
	}
	if i := strings.Index(lower, "</head>"); i >= 0 {
		return html[:i] + script + html[i:]
	}
	return script + html
}

func devRuntimeScript() string {
	return `<script ` + devRuntimeMarker + `>(function(){if(window.__vapourQuitRuntime)return;window.__vapourQuitRuntime=true;let quitSent=false;const quit=(code=0)=>{if(quitSent)return;quitSent=true;try{parent.postMessage({type:'vapour:quit',code:Number(code)||0},'*');}catch{}};window.vapour=Object.assign({},window.vapour||{},{quit});const hookUnity=()=>{if(typeof self.createUnityInstance!=='function'||self.createUnityInstance.__vapourQuitWrapped||self.createUnityInstance.__vapourWrapped)return;const original=self.createUnityInstance;const wrapped=function(canvas,config,onProgress){const result=original.call(this,canvas,config,onProgress);Promise.resolve(result).then((instance)=>{if(instance&&typeof instance.Quit==='function'&&!instance.Quit.__vapourQuitWrapped&&!instance.Quit.__vapourWrapped){const quitOriginal=instance.Quit.bind(instance);const wrappedQuit=(...args)=>{quit(0);return quitOriginal(...args);};wrappedQuit.__vapourQuitWrapped=true;instance.Quit=wrappedQuit;}}).catch(()=>{});return result;};wrapped.__vapourQuitWrapped=true;self.createUnityInstance=wrapped;};const hookGodot=()=>{const Engine=self.Engine;if(!Engine||!Engine.prototype||Engine.prototype.__vapourQuitWrapped||Engine.prototype.__vapourWrapped)return;const original=Engine.prototype.startGame;if(typeof original!=='function')return;Engine.prototype.startGame=function(...args){if(args[0]&&typeof args[0]==='object'){const options=args[0];const originalOnExit=options.onExit;options.onExit=(...exitArgs)=>{quit(exitArgs[0]);if(typeof originalOnExit==='function')return originalOnExit(...exitArgs);};}return original.apply(this,args);};Engine.prototype.__vapourQuitWrapped=true;};hookUnity();hookGodot();const t=setInterval(()=>{hookUnity();hookGodot();},100);addEventListener('load',()=>setTimeout(()=>clearInterval(t),10000));})();</script>`
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

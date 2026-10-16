package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"github.com/vapour-computer/vapour-cli/internal/devserver"
	"github.com/vapour-computer/vapour-cli/internal/localprofile"
	"github.com/vapour-computer/vapour-cli/internal/project"
	"github.com/vapour-computer/vapour-cli/internal/zipdir"
)

func newTest() *cobra.Command {
	var dir string
	var port int
	var noTelemetry, noOpen, noProfile, playerMode, mobilePreview, noPerfHints bool
	var profilerBin string
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Run your web build locally in the browser, with an FPS overlay",
		Long: `Test serves your web export (build.dir in vapour.toml) on 127.0.0.1 and opens it in your browser.

The FPS overlay (FPS, frame time, 1% low, worst frame) is added only while testing locally. It is injected
while serving, so your build folder is never changed and "vapour publish" never includes it. Nothing is sent
over the network; with the Linux profiler installed it also attaches vapour-profiler to the browser. A summary is printed here when you close the game tab. Toggle the overlay with Ctrl+Shift+F.

Use --player to preview in a production-like iframe shell (sandbox/allow). Add --mobile to enable a phone-style shell preview with --player.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if mobilePreview && !playerMode {
				return fmt.Errorf("--mobile requires --player")
			}
			out := cmd.OutOrStdout()
			m, err := project.Load(dir)
			if err != nil {
				return err
			}
			if err := m.Validate(); err != nil {
				return err
			}
			emitPerfHints(out, dir, m, noPerfHints)
			fsys, closeFS, err := openBuild(filepath.Join(dir, m.Build.Dir))
			if err != nil {
				return err
			}
			defer closeFS()

			telemetry := !noTelemetry
			mobileOrientation, mobileRenderScale, mobileMaxDPR := readMobilePreviewSettings(dir)
			reportPosted := make(chan struct{})
			var reportOnce sync.Once
			var bridgeMu sync.Mutex
			var bridge *profilerBridge
			h := devserver.Handler(devserver.Options{
				FS:                fsys,
				Telemetry:         telemetry,
				Player:            playerMode,
				Mobile:            mobilePreview,
				OnQuit:            m.Runtime.OnQuit,
				MobileOrientation: mobileOrientation,
				MobileRenderScale: mobileRenderScale,
				MobileMaxDPR:      mobileMaxDPR,
				OnReport: func(r devserver.Report) {
					fmt.Fprintf(out, "Session: %d frames, avg %.0f fps, 1%% low %.0f fps, worst frame %.0f ms\n",
						r.Frames, r.AvgFPS, r.Low1FPS, r.WorstMS)
					reportOnce.Do(func() { close(reportPosted) })
				},
				OnRecords: func(batch devserver.RecordsBatch) {
					bridgeMu.Lock()
					b := bridge
					bridgeMu.Unlock()
					if b == nil {
						return
					}
					_ = b.append(batch)
				},
			})
			l, err := devserver.Listen(port)
			if err != nil {
				return fmt.Errorf("cannot listen on 127.0.0.1:%d: %w", port, err)
			}
			url := "http://" + l.Addr().String() + "/"
			fmt.Fprintf(out, "Testing %s v%s at %s\n", m.Project.Title, m.Project.Version, url)
			if telemetry {
				fmt.Fprintln(out, "FPS overlay on (local test only). Close the game tab for a summary; Ctrl+C to stop.")
			} else {
				fmt.Fprintln(out, "FPS overlay off. Ctrl+C to stop.")
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			go func() {
				<-ctx.Done()
				_ = l.Close()
			}()
			profiled := false
			if !noOpen && !noProfile {
				sess, err := localprofile.Start(url, localprofile.Options{ProfilerBin: profilerBin, Out: cmd.ErrOrStderr()})
				if sess != nil {
					if b, e := newProfilerBridge(sess.DashboardURL); e == nil {
						bridgeMu.Lock()
						bridge = b
						bridgeMu.Unlock()
					}
					defer sess.Close()
					profiled = true
					go func() {
						select {
						case <-sess.Done():
							select {
							case <-reportPosted:
							case <-time.After(2 * time.Second):
							}
							if path, err := saveProfilerReport(dir, sess.DashboardURL); err != nil {
								fmt.Fprintf(out, "Profiler JSON report not saved: %v\n", err)
							} else {
								fmt.Fprintf(out, "Profiler JSON report: %s\n", path)
							}
							_ = l.Close()
						case <-ctx.Done():
						}
					}()
					fmt.Fprintln(out, "eBPF profiler attached to the game browser (CPU and memory, browser plus its processes).")
					fmt.Fprintln(out, "Capture started automatically; open the profiler tab to watch it. The profiler session lasts 5 minutes. Close the browser to stop.")
				}
				if err != nil {
					fmt.Fprintf(out, "Profiler not started: %v\nContinuing with the FPS overlay only.\n", err)
				}
			}
			if !noOpen && !profiled {
				if err := openBrowser(url); err != nil {
					fmt.Fprintf(out, "Could not open a browser (%v). Open %s yourself.\n", err, url)
				}
			}
			return devserver.Serve(l, h)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "game folder containing vapour.toml")
	cmd.Flags().IntVar(&port, "port", 0, "port on 127.0.0.1 (default: a free port)")
	cmd.Flags().BoolVar(&noTelemetry, "no-telemetry", false, "do not add the FPS overlay")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "do not open a browser (this also skips the profiler)")
	cmd.Flags().BoolVar(&noProfile, "no-profile", false, "do not start the eBPF profiler")
	cmd.Flags().BoolVar(&playerMode, "player", false, "serve a production-like player shell with iframe sandbox/allow")
	cmd.Flags().BoolVar(&mobilePreview, "mobile", false, "phone-style shell preview (requires --player)")
	cmd.Flags().BoolVar(&noPerfHints, "no-perf-hints", false, "suppress performance hint warnings")
	cmd.Flags().StringVar(&profilerBin, "profiler", "", "path to vapour-profiler (default: $VAPOUR_PROFILER, next to vapour, then PATH)")
	return cmd
}

// openBuild returns the web export as a filesystem: a folder, or a .zip file read in place.
func openBuild(path string) (fs.FS, func(), error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, nil, fmt.Errorf("build path %q not found: export your game for Web first", path)
	}
	if st.IsDir() {
		if err := zipdir.Check(path); err != nil {
			return nil, nil, err
		}
		return os.DirFS(path), func() {}, nil
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, nil, fmt.Errorf("build %q is not a valid zip: %w", path, err)
	}
	if _, err := fs.Stat(zr, "index.html"); err != nil {
		zr.Close()
		return nil, nil, fmt.Errorf("%q has no index.html at its top level", path)
	}
	return zr, func() { zr.Close() }, nil
}

func openBrowser(url string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	return c.Start()
}

func readMobilePreviewSettings(dir string) (orientation string, renderScale float64, maxDPR *float64) {
	renderScale = 1
	b, err := os.ReadFile(filepath.Join(dir, project.File))
	if err != nil {
		return "any", renderScale, nil
	}
	var raw struct {
		Mobile struct {
			Orientation string   `toml:"orientation"`
			RenderScale *float64 `toml:"render_scale"`
			MaxDPR      *float64 `toml:"max_dpr"`
		} `toml:"mobile"`
	}
	if err := toml.Unmarshal(b, &raw); err != nil {
		return "any", renderScale, nil
	}
	orientation = raw.Mobile.Orientation
	if orientation == "" {
		orientation = "any"
	}
	if raw.Mobile.RenderScale != nil && *raw.Mobile.RenderScale > 0 {
		renderScale = *raw.Mobile.RenderScale
	}
	return orientation, renderScale, raw.Mobile.MaxDPR
}

func profilerReportURL(dashboardURL string) (string, error) {
	u, err := url.Parse(dashboardURL)
	if err != nil {
		return "", err
	}
	u.Path = "/api/report"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func profilerRecordsURL(dashboardURL string) (string, error) {
	u, err := url.Parse(dashboardURL)
	if err != nil {
		return "", err
	}
	u.Path = "/api/records"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func profilerStatusURL(dashboardURL string) (string, error) {
	u, err := url.Parse(dashboardURL)
	if err != nil {
		return "", err
	}
	u.Path = "/api/status"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func profilerToken(dashboardURL string) (string, error) {
	u, err := url.Parse(dashboardURL)
	if err != nil {
		return "", err
	}
	q, err := url.ParseQuery(u.Fragment)
	if err != nil {
		return "", err
	}
	token := q.Get("token")
	if token == "" {
		return "", fmt.Errorf("missing dashboard token")
	}
	return token, nil
}

type profilerBridge struct {
	recordsURL string
	statusURL  string
	token      string
	client     *http.Client
	originMS   float64
	hasOrigin  bool
	lastFrame  float64
	lastMetric float64
	mu         sync.Mutex
}

func newProfilerBridge(dashboardURL string) (*profilerBridge, error) {
	recordsURL, err := profilerRecordsURL(dashboardURL)
	if err != nil {
		return nil, err
	}
	statusURL, err := profilerStatusURL(dashboardURL)
	if err != nil {
		return nil, err
	}
	token, err := profilerToken(dashboardURL)
	if err != nil {
		return nil, err
	}
	return &profilerBridge{
		recordsURL: recordsURL,
		statusURL:  statusURL,
		token:      token,
		client:     &http.Client{Timeout: 5 * time.Second},
	}, nil
}

func nowMS() float64 {
	return float64(time.Now().UnixNano()) / 1_000_000
}

func (p *profilerBridge) ensureOrigin() {
	if p.hasOrigin {
		return
	}
	req, err := http.NewRequest(http.MethodGet, p.statusURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	resp, err := p.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var payload map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&payload); err != nil {
		return
	}
	t := 0.0
	if metrics, ok := payload["metrics"].(map[string]any); ok {
		if tm, ok := metrics["timeMs"].(float64); ok {
			t = tm
		}
	}
	p.originMS = nowMS() - t
	p.hasOrigin = true
}

func (p *profilerBridge) relativeTime(epochMS, last float64) float64 {
	t := epochMS - p.originMS
	if t < 0 {
		t = 0
	}
	if t < last {
		t = last
	}
	return t
}

func (p *profilerBridge) append(batch devserver.RecordsBatch) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(batch.Frames) == 0 && batch.Metrics == nil {
		return nil
	}
	p.ensureOrigin()
	if !p.hasOrigin {
		p.originMS = nowMS()
		p.hasOrigin = true
	}
	lines := make([]string, 0, len(batch.Frames)+1)
	for _, frame := range batch.Frames {
		timeMS := p.relativeTime(frame.EpochMS, p.lastFrame)
		p.lastFrame = timeMS
		row, err := json.Marshal(map[string]any{
			"type":       "frame",
			"timeMs":     timeMS,
			"durationMs": frame.DurationMS,
			"spike":      frame.Spike,
		})
		if err != nil {
			return err
		}
		lines = append(lines, string(row))
	}
	if batch.Metrics != nil {
		timeMS := p.relativeTime(batch.Metrics.EpochMS, p.lastMetric)
		p.lastMetric = timeMS
		row, err := json.Marshal(map[string]any{
			"type":   "metrics",
			"timeMs": timeMS,
			"values": batch.Metrics.Values,
		})
		if err != nil {
			return err
		}
		lines = append(lines, string(row))
	}
	body := strings.Join(lines, "\n")
	req, err := http.NewRequest(http.MethodPost, p.recordsURL, bytes.NewBufferString(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/x-ndjson")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("append records: %s: %s", resp.Status, string(msg))
	}
	return nil
}

func saveProfilerReport(projectDir, dashboardURL string) (string, error) {
	reportURL, err := profilerReportURL(dashboardURL)
	if err != nil {
		return "", err
	}
	token, err := profilerToken(dashboardURL)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodGet, reportURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("%s: %s", resp.Status, string(body))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 60<<20))
	if err != nil {
		return "", err
	}
	outDir := filepath.Join(projectDir, ".vapour", "test-output")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	outPath := filepath.Join(outDir, "report.json")
	if err := os.WriteFile(outPath, body, 0o644); err != nil {
		return "", err
	}
	return outPath, nil
}

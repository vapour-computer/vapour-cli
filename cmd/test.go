package cmd

import (
	"archive/zip"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/vapour-computer/vapour-cli/internal/devserver"
	"github.com/vapour-computer/vapour-cli/internal/localprofile"
	"github.com/vapour-computer/vapour-cli/internal/project"
	"github.com/vapour-computer/vapour-cli/internal/zipdir"
)

func newTest() *cobra.Command {
	var dir string
	var port int
	var noTelemetry, noOpen, noProfile bool
	var profilerBin string
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Run your web build locally in the browser, with an FPS overlay",
		Long: `Test serves your web export (build.dir in vapour.toml) on 127.0.0.1 and opens it in your browser.

The FPS overlay (FPS, frame time, 1% low, worst frame) is added only while testing locally. It is injected
while serving, so your build folder is never changed and "vapour publish" never includes it. Nothing is sent
over the network; with the Linux profiler installed it also attaches vapour-profiler to the browser. A summary is printed here when you close the game tab. Toggle the overlay with Ctrl+Shift+F.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			m, err := project.Load(dir)
			if err != nil {
				return err
			}
			if err := m.Validate(); err != nil {
				return err
			}
			fsys, closeFS, err := openBuild(filepath.Join(dir, m.Build.Dir))
			if err != nil {
				return err
			}
			defer closeFS()

			telemetry := !noTelemetry
			h := devserver.Handler(devserver.Options{
				FS:        fsys,
				Telemetry: telemetry,
				OnReport: func(r devserver.Report) {
					fmt.Fprintf(out, "Session: %d frames, avg %.0f fps, 1%% low %.0f fps, worst frame %.0f ms\n",
						r.Frames, r.AvgFPS, r.Low1FPS, r.WorstMS)
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
					defer sess.Close()
					profiled = true
					go func() {
						select {
						case <-sess.Done():
							time.Sleep(500 * time.Millisecond) // let the page's final report arrive
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

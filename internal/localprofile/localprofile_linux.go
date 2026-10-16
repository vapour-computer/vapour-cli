//go:build linux

// Package localprofile starts the game in its own browser instance and attaches the standalone
// vapour-profiler to it, for `vapour test`. Everything is local: the browser gets a throwaway
// profile, and the profiler listens on loopback only.
package localprofile

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Supported reports whether profiling can run on this OS.
const Supported = true

// browsers are tried in order. They must be Chromium-family: the profiler dashboard targets Chromium.
var browsers = []string{"chromium", "chromium-browser", "google-chrome-stable", "google-chrome", "brave", "brave-browser", "microsoft-edge-stable"}

// Options configures Start.
type Options struct {
	// ProfilerBin is the vapour-profiler binary. Empty means $VAPOUR_PROFILER, then next to the
	// vapour binary, then PATH.
	ProfilerBin string
	// Out receives profiler errors. May be nil.
	Out io.Writer
}

// Session is a running browser plus its profiler. Close it when done.
type Session struct {
	browser    *exec.Cmd
	profiler   *exec.Cmd
	browserBin string
	profileDir string
	browserEnd chan struct{}
	// DashboardURL is the profiler dashboard URL, including its access token.
	DashboardURL string
}

// Done is closed when the browser exits (for example, the user closes the window).
func (s *Session) Done() <-chan struct{} { return s.browserEnd }

func findProfiler(explicit string) (string, error) {
	for _, c := range []string{explicit, os.Getenv("VAPOUR_PROFILER")} {
		if c != "" {
			if _, err := os.Stat(c); err != nil {
				return "", fmt.Errorf("profiler %q: %w", c, err)
			}
			return c, nil
		}
	}
	// Next to the vapour binary, so a profiler placed beside it just works.
	if self, err := os.Executable(); err == nil {
		beside := filepath.Join(filepath.Dir(self), "vapour-profiler")
		if st, err := os.Stat(beside); err == nil && !st.IsDir() {
			return beside, nil
		}
	}
	if p, err := exec.LookPath("vapour-profiler"); err == nil {
		return p, nil
	}
	return "", errors.New("vapour-profiler not found: pass --profiler /path/to/vapour-profiler, set VAPOUR_PROFILER, put it next to the vapour binary, or put it on PATH")
}

func findBrowser() (string, error) {
	for _, b := range browsers {
		if p, err := exec.LookPath(b); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no Chromium-family browser found (looked for %s)", strings.Join(browsers, ", "))
}

// Start opens gameURL in a dedicated browser instance, attaches the profiler to that browser and
// all its child processes, and opens the profiler dashboard as a second tab.
func Start(gameURL string, o Options) (*Session, error) {
	profilerBin, err := findProfiler(o.ProfilerBin)
	if err != nil {
		return nil, err
	}
	browserBin, err := findBrowser()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "vapour-test-browser-")
	if err != nil {
		return nil, err
	}
	s := &Session{browserBin: browserBin, profileDir: dir, browserEnd: make(chan struct{})}

	// A private profile makes this a separate browser process we can track. Without it the browser
	// would hand the URL to an already-running instance and exit, leaving no PID to attach to.
	s.browser = exec.Command(browserBin, s.browserArgs(gameURL)...)
	s.browser.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := s.browser.Start(); err != nil {
		s.Close()
		return nil, fmt.Errorf("start browser: %w", err)
	}
	go func() {
		_ = s.browser.Wait()
		close(s.browserEnd)
	}()
	select {
	case <-s.browserEnd:
		s.Close()
		return nil, errors.New("browser exited immediately")
	case <-time.After(1500 * time.Millisecond):
	}

	s.profiler = exec.Command(profilerBin, "dashboard", "--pid", strconv.Itoa(s.browser.Process.Pid), "--tree", "--autostart")
	s.profiler.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if o.Out != nil {
		s.profiler.Stderr = o.Out
	}
	stdout, err := s.profiler.StdoutPipe()
	if err != nil {
		s.Close()
		return nil, err
	}
	if err := s.profiler.Start(); err != nil {
		s.Close()
		return nil, fmt.Errorf("start profiler: %w", err)
	}
	urls := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			if line := strings.TrimSpace(sc.Text()); strings.HasPrefix(line, "http://") {
				select {
				case urls <- line:
				default:
				}
			}
		}
	}()
	go func() { _ = s.profiler.Wait() }()

	select {
	case s.DashboardURL = <-urls:
	case <-time.After(5 * time.Second):
		s.Close()
		return nil, errors.New("profiler did not print a dashboard URL (is port 6767 already in use?)")
	}
	// Same profile directory, so Chromium opens this as another tab of the running instance.
	if err := exec.Command(browserBin, s.browserArgs(s.DashboardURL)...).Start(); err != nil {
		return s, fmt.Errorf("open dashboard: %w (open %s yourself)", err, s.DashboardURL)
	}
	return s, nil
}

func (s *Session) browserArgs(url string) []string {
	return []string{"--user-data-dir=" + s.profileDir, "--no-first-run", "--no-default-browser-check", url}
}

// Close stops the profiler and the browser and removes the throwaway profile. Safe to call twice.
func (s *Session) Close() {
	stop := func(c *exec.Cmd) {
		if c == nil || c.Process == nil {
			return
		}
		_ = syscall.Kill(-c.Process.Pid, syscall.SIGTERM)
	}
	stop(s.profiler)
	stop(s.browser)
	select {
	case <-s.browserEnd:
	case <-time.After(3 * time.Second):
		if s.browser != nil && s.browser.Process != nil {
			_ = syscall.Kill(-s.browser.Process.Pid, syscall.SIGKILL)
		}
	}
	if s.profileDir != "" {
		_ = os.RemoveAll(s.profileDir)
		s.profileDir = ""
	}
}

package cmd

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runSplit(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := newRoot()
	var out bytes.Buffer
	var errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), errOut.String(), err
}

func TestLLMHelpExplainsGuideOutput(t *testing.T) {
	out, _, err := runSplit(t, "llm", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"stdout", ".claude/vapour-guide.md", ".opencode/vapour-guide.md", "not automatically loaded"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q", want)
		}
	}
}

func TestLLMFreshFetchWritesCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	body := llmHeader + "\nVersion: 2026-10-16\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("ETag", "\"etag-v1\"")
		io.WriteString(w, body)
	}))
	defer srv.Close()

	out, errOut, err := runSplit(t, "llm", "--url", srv.URL)
	if err != nil {
		t.Fatalf("llm fetch: %v", err)
	}
	if errOut != "" {
		t.Fatalf("unexpected stderr: %q", errOut)
	}
	if out != body {
		t.Fatalf("stdout mismatch\nwant: %q\ngot:  %q", body, out)
	}

	cache := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "vapour", "llm.txt")
	etag := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "vapour", "llm.etag")
	b, _ := os.ReadFile(cache)
	if string(b) != body {
		t.Fatalf("cached body mismatch: %q", b)
	}
	e, _ := os.ReadFile(etag)
	if strings.TrimSpace(string(e)) != "\"etag-v1\"" {
		t.Fatalf("etag mismatch: %q", e)
	}
}

func TestLLMNotModifiedUsesCache(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	cacheDir := filepath.Join(cacheHome, "vapour")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cached := llmHeader + "\nVersion: cached\n"
	if err := os.WriteFile(filepath.Join(cacheDir, "llm.txt"), []byte(cached), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "llm.etag"), []byte("\"etag-cache\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != "\"etag-cache\"" {
			t.Fatalf("missing If-None-Match: %q", r.Header.Get("If-None-Match"))
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	out, _, err := runSplit(t, "llm", "--url", srv.URL)
	if err != nil {
		t.Fatalf("llm 304: %v", err)
	}
	if out != cached {
		t.Fatalf("expected cached output, got %q", out)
	}
}

func TestLLMOfflineFallback(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	out, errOut, err := runSplit(t, "llm", "--offline")
	if err != nil {
		t.Fatalf("llm offline fallback: %v", err)
	}
	if !strings.Contains(out, llmHeader) {
		t.Fatalf("fallback output missing header: %q", out)
	}
	if !strings.Contains(errOut, "embedded llm.txt fallback") {
		t.Fatalf("fallback note missing: %q", errOut)
	}
	if !strings.Contains(errOut, "offline mode") {
		t.Fatalf("fallback reason missing: %q", errOut)
	}
}

func TestLLMFallbackWithoutCacheOnHTTPError(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, "down")
	}))
	defer srv.Close()

	out, errOut, err := runSplit(t, "llm", "--url", srv.URL, "--refresh")
	if err != nil {
		t.Fatalf("expected fallback, got error: %v", err)
	}
	if !strings.HasPrefix(out, llmHeader) {
		t.Fatalf("fallback output missing header: %q", out)
	}
	if !strings.Contains(errOut, "503") {
		t.Fatalf("fallback reason missing status: %q", errOut)
	}
}

func TestLLMFallbackWithoutCacheOnBadContent(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "not a vapour llm guide")
	}))
	defer srv.Close()

	out, errOut, err := runSplit(t, "llm", "--url", srv.URL, "--refresh")
	if err != nil {
		t.Fatalf("expected fallback, got error: %v", err)
	}
	if !strings.HasPrefix(out, llmHeader) {
		t.Fatalf("fallback output missing header: %q", out)
	}
	if !strings.Contains(errOut, "must start with") {
		t.Fatalf("fallback reason missing content error: %q", errOut)
	}
}

func TestLLMEmbeddedFallbackHasHeader(t *testing.T) {
	if err := validateLLMBody(llmFallback); err != nil {
		t.Fatalf("embedded fallback invalid: %v", err)
	}
}

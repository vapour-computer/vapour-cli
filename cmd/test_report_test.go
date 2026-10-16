package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vapour-computer/vapour-cli/internal/devserver"
)

func TestProfilerReportURL(t *testing.T) {
	u, err := profilerReportURL("http://127.0.0.1:6767/#token=abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u != "http://127.0.0.1:6767/api/report" {
		t.Fatalf("got %q", u)
	}
}

func TestProfilerURLs(t *testing.T) {
	rec, err := profilerRecordsURL("http://127.0.0.1:6767/#token=abc123")
	if err != nil {
		t.Fatalf("records url error: %v", err)
	}
	if rec != "http://127.0.0.1:6767/api/records" {
		t.Fatalf("records got %q", rec)
	}
	st, err := profilerStatusURL("http://127.0.0.1:6767/#token=abc123")
	if err != nil {
		t.Fatalf("status url error: %v", err)
	}
	if st != "http://127.0.0.1:6767/api/status" {
		t.Fatalf("status got %q", st)
	}
}

func TestProfilerTokenMissing(t *testing.T) {
	if _, err := profilerToken("http://127.0.0.1:6767/"); err == nil {
		t.Fatal("expected error")
	}
}

func TestProfilerToken(t *testing.T) {
	tok, err := profilerToken("http://127.0.0.1:6767/#token=abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "abc123" {
		t.Fatalf("got %q", tok)
	}
}

func TestProfilerReportURLNeverNeedsToken(t *testing.T) {
	if _, err := profilerReportURL("http://127.0.0.1:6767/"); err != nil {
		t.Fatalf("unexpected parse failure: %v", err)
	}
}

func TestProfilerBridgeSendsFramesOnly(t *testing.T) {
	var posted string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"metrics":{"timeMs":0}}`))
		case "/api/records":
			body, _ := io.ReadAll(r.Body)
			posted = string(body)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	b, err := newProfilerBridge(ts.URL + "/#token=test")
	if err != nil {
		t.Fatalf("bridge: %v", err)
	}
	err = b.append(devserver.RecordsBatch{Frames: []devserver.FrameRecord{{DurationMS: 16.7, EpochMS: 1000, Spike: false}}})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if posted == "" {
		t.Fatal("expected records POST")
	}
	for _, line := range strings.Split(strings.TrimSpace(posted), "\n") {
		var v map[string]any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			t.Fatalf("invalid json line: %v", err)
		}
		if v["type"] != "frame" {
			t.Fatalf("unexpected record type: %v", v["type"])
		}
	}
}

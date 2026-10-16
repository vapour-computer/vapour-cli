package cmd

import "testing"

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

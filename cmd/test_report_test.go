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

func TestProfilerReportURLMissingToken(t *testing.T) {
	if _, err := profilerReportURL("http://127.0.0.1:6767/"); err == nil {
		t.Fatal("expected error")
	}
}

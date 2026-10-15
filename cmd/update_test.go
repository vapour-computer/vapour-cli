package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/cobra"
)

func fakeArchive(t *testing.T, body string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "LICENSE", Mode: 0o644, Size: 2, Typeflag: tar.TypeReg})
	tw.Write([]byte("hi"))
	tw.WriteHeader(&tar.Header{Name: "vapour", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
	tw.Write([]byte(body))
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestInstallReleaseReplacesBinaryAndChecksVerification(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	archive := fakeArchive(t, "new-binary")
	sum := sha256.Sum256(archive)
	name := fmt.Sprintf("vapour_9.9.9_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	badSum := hex.EncodeToString(make([]byte, 32))
	checksum := hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			w.Write(archive)
		case "/sums":
			fmt.Fprintf(w, "%s  %s\n", checksum, name)
		case "/badsums":
			fmt.Fprintf(w, "%s  %s\n", badSum, name)
		}
	}))
	defer srv.Close()

	c := &cobra.Command{}
	c.SetContext(context.Background())
	rel := &releaseInfo{Tag: "v9.9.9"}
	rel.Assets = append(rel.Assets, struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	}{name, srv.URL + "/a"}, struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	}{"checksums.txt", srv.URL + "/sums"})

	self := filepath.Join(t.TempDir(), "vapour")
	os.WriteFile(self, []byte("old"), 0o755)
	if err := installReleaseForPlatform(runtime.GOOS, runtime.GOARCH, c, rel, self); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(self); string(b) != "new-binary" {
		t.Fatalf("binary = %q", b)
	}

	// A wrong checksum must leave the existing binary alone.
	rel.Assets[1].URL = srv.URL + "/badsums"
	os.WriteFile(self, []byte("old"), 0o755)
	if err := installReleaseForPlatform(runtime.GOOS, runtime.GOARCH, c, rel, self); err == nil {
		t.Fatal("expected checksum error")
	}
	if b, _ := os.ReadFile(self); string(b) != "old" {
		t.Fatalf("binary changed despite bad checksum: %q", b)
	}
}

func TestInstallReleaseWindowsUnsupported(t *testing.T) {
	c := &cobra.Command{}
	if err := installReleaseForPlatform("windows", "amd64", c, &releaseInfo{Tag: "v1.2.3"}, "C:/vapour.exe"); err == nil {
		t.Fatal("expected windows unsupported error")
	} else if got := err.Error(); got != "self-update is not supported on Windows; download the zip from the releases page" {
		t.Fatalf("error = %q", got)
	}
}

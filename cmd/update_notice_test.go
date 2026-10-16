package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
)

func noticeCommand(t *testing.T, name string, stderr *bytes.Buffer) *cobra.Command {
	t.Helper()
	root := &cobra.Command{Use: "vapour", Version: Version}
	root.SetErr(stderr)
	root.InitDefaultVersionFlag()
	cmd := &cobra.Command{Use: name}
	cmd.SetErr(stderr)
	cmd.SetContext(context.Background())
	root.AddCommand(cmd)
	return cmd
}

func TestMaybeCheckForUpdatePrintsAndCaches(t *testing.T) {
	oldVersion := Version
	Version = "1.2.3"
	t.Cleanup(func() { Version = oldVersion })

	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("CI", "")
	t.Setenv("VAPOUR_NO_UPDATE_CHECK", "")

	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"v1.2.4"}`))
	}))
	defer srv.Close()
	t.Setenv("VAPOUR_RELEASES_URL", srv.URL)

	var errOut bytes.Buffer
	cmd := noticeCommand(t, "publish", &errOut)
	maybeCheckForUpdate(cmd)
	maybeCheckForUpdate(cmd)

	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
	if !strings.Contains(errOut.String(), "vapour 1.2.4 is available (you have 1.2.3). Run: vapour update") {
		t.Fatalf("missing notice in %q", errOut.String())
	}
}

func TestMaybeCheckForUpdateSkipsInCIOrOptOut(t *testing.T) {
	oldVersion := Version
	Version = "1.2.3"
	t.Cleanup(func() { Version = oldVersion })

	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("VAPOUR_RELEASES_URL", "http://127.0.0.1:1")

	var errOut bytes.Buffer
	cmd := noticeCommand(t, "publish", &errOut)

	t.Setenv("CI", "1")
	maybeCheckForUpdate(cmd)
	t.Setenv("CI", "")
	t.Setenv("VAPOUR_NO_UPDATE_CHECK", "1")
	maybeCheckForUpdate(cmd)

	if errOut.Len() != 0 {
		t.Fatalf("unexpected output: %q", errOut.String())
	}
}

func TestMaybeCheckForUpdateSkipsForUpdateAndVersionFlag(t *testing.T) {
	oldVersion := Version
	Version = "1.2.3"
	t.Cleanup(func() { Version = oldVersion })

	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("CI", "")
	t.Setenv("VAPOUR_NO_UPDATE_CHECK", "")

	var updateErr bytes.Buffer
	updateCmd := noticeCommand(t, "update", &updateErr)
	maybeCheckForUpdate(updateCmd)
	if updateErr.Len() != 0 {
		t.Fatalf("unexpected output for update command: %q", updateErr.String())
	}

	var versionErr bytes.Buffer
	cmd := noticeCommand(t, "publish", &versionErr)
	f := cmd.Root().Flags().Lookup("version")
	if f == nil {
		t.Fatal("missing version flag")
	}
	f.Changed = true
	maybeCheckForUpdate(cmd)
	if versionErr.Len() != 0 {
		t.Fatalf("unexpected output with --version: %q", versionErr.String())
	}
}

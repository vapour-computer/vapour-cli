package cmd

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const releasesAPI = "https://api.github.com/repos/vapour-computer/vapour-cli/releases"

// releasesURL can be overridden (tests, mirrors) with VAPOUR_RELEASES_URL.
func releasesURL() string {
	if u := os.Getenv("VAPOUR_RELEASES_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return releasesAPI
}

type releaseInfo struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func newUpdate() *cobra.Command {
	var checkOnly bool
	var version string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update vapour-cli to the latest release",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			rel, err := fetchRelease(cmd, version)
			if err != nil {
				return err
			}
			latest := strings.TrimPrefix(rel.Tag, "v")
			if latest == strings.TrimPrefix(Version, "v") && version == "" {
				fmt.Fprintf(out, "Already up to date (%s).\n", Version)
				return nil
			}
			fmt.Fprintf(out, "Current: %s\nAvailable: %s\n", Version, rel.Tag)
			if checkOnly {
				fmt.Fprintln(out, "Run `vapour-cli update` to install it.")
				return nil
			}
			self, err := os.Executable()
			if err != nil {
				return err
			}
			if self, err = filepath.EvalSymlinks(self); err != nil {
				return err
			}
			if err := installRelease(cmd, rel, self); err != nil {
				return err
			}
			fmt.Fprintf(out, "Updated to %s (%s).\n", rel.Tag, self)
			return nil
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "only report whether a newer version exists")
	cmd.Flags().StringVar(&version, "version", "", "install this release tag (e.g. v0.2.0) instead of the latest")
	return cmd
}

func httpGet(cmd *cobra.Command, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "vapour-cli/"+Version)
	client := &http.Client{Timeout: 5 * time.Minute}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, res.Status)
	}
	return res, nil
}

func fetchRelease(cmd *cobra.Command, version string) (*releaseInfo, error) {
	url := releasesURL() + "/latest"
	if version != "" {
		if !strings.HasPrefix(version, "v") {
			version = "v" + version
		}
		url = releasesURL() + "/tags/" + version
	}
	res, err := httpGet(cmd, url)
	if err != nil {
		return nil, fmt.Errorf("could not look up the release: %w", err)
	}
	defer res.Body.Close()
	var rel releaseInfo
	if err := json.NewDecoder(res.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("could not read the release: %w", err)
	}
	if rel.Tag == "" {
		return nil, fmt.Errorf("release has no tag")
	}
	return &rel, nil
}

func installRelease(cmd *cobra.Command, rel *releaseInfo, self string) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("self-update is not supported on Windows; download the zip from the releases page")
	}
	archive := fmt.Sprintf("vapour_%s_%s_%s.tar.gz", strings.TrimPrefix(rel.Tag, "v"), runtime.GOOS, runtime.GOARCH)
	var archiveURL, sumsURL string
	for _, a := range rel.Assets {
		switch a.Name {
		case archive:
			archiveURL = a.URL
		case "checksums.txt":
			sumsURL = a.URL
		}
	}
	if archiveURL == "" || sumsURL == "" {
		return fmt.Errorf("release %s has no build for %s/%s", rel.Tag, runtime.GOOS, runtime.GOARCH)
	}

	sums, err := httpGet(cmd, sumsURL)
	if err != nil {
		return err
	}
	sumsBody, err := io.ReadAll(io.LimitReader(sums.Body, 1<<20))
	sums.Body.Close()
	if err != nil {
		return err
	}
	want := ""
	for _, line := range strings.Split(string(sumsBody), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[1] == archive {
			want = f[0]
		}
	}
	if want == "" {
		return fmt.Errorf("checksums.txt has no entry for %s", archive)
	}

	res, err := httpGet(cmd, archiveURL)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	fmt.Fprintf(cmd.OutOrStdout(), "Downloading %s ...\n", archive)

	// The new binary goes next to the old one so the final rename stays on one filesystem.
	tmp, err := os.CreateTemp(filepath.Dir(self), ".vapour-update-*")
	if err != nil {
		return fmt.Errorf("cannot write next to %s (try sudo, or reinstall with install.sh): %w", self, err)
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	if err := extractBinary(io.TeeReader(res.Body, h), tmp); err != nil {
		tmp.Close()
		return err
	}
	// Drain the rest of the archive so the checksum covers every byte.
	if _, err := io.Copy(io.Discard, io.TeeReader(res.Body, h)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", archive, got, want)
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), self)
}

// extractBinary copies the "vapour" file out of a .tar.gz stream into dst.
func extractBinary(r io.Reader, dst io.Writer) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("archive has no vapour binary")
		}
		if err != nil {
			return err
		}
		if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == "vapour" {
			_, err := io.Copy(dst, io.LimitReader(tr, 512<<20))
			return err
		}
	}
}

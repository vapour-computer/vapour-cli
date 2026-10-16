package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	updateNoticeTTL     = 24 * time.Hour
	updateNoticeTimeout = 2 * time.Second
)

type updateNoticeCache struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
}

func maybeCheckForUpdate(cmd *cobra.Command) {
	if cmd == nil {
		return
	}
	if os.Getenv("CI") != "" || os.Getenv("VAPOUR_NO_UPDATE_CHECK") == "1" {
		return
	}
	if cmd.Name() == "update" {
		return
	}
	if f := cmd.Root().Flags().Lookup("version"); f != nil && f.Changed {
		return
	}
	current, ok := parseReleaseVersion(Version)
	if !ok {
		return
	}
	latest, err := latestReleaseVersion(cmd.Context())
	if err != nil || latest == "" {
		return
	}
	if compareReleaseVersions(latest, current) <= 0 {
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "vapour %s is available (you have %s). Run: vapour update\n", latest, current)
}

func latestReleaseVersion(ctx context.Context) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cachePath, err := updateNoticeCachePath()
	if err != nil {
		return "", err
	}
	cache, ok := readUpdateNoticeCache(cachePath)
	if ok && time.Since(cache.CheckedAt) < updateNoticeTTL {
		return cache.Latest, nil
	}

	latest, err := fetchLatestReleaseVersion(ctx)
	if err != nil {
		if ok {
			return cache.Latest, nil
		}
		return "", err
	}
	writeUpdateNoticeCache(cachePath, updateNoticeCache{CheckedAt: time.Now(), Latest: latest})
	return latest, nil
}

func fetchLatestReleaseVersion(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, updateNoticeTimeout)
	defer cancel()
	base := strings.TrimRight(os.Getenv("VAPOUR_RELEASES_URL"), "/")
	if base == "" {
		base = releasesAPI
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "vapour-cli/"+Version)

	res, err := (&http.Client{Timeout: updateNoticeTimeout}).Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s/latest: %s", base, res.Status)
	}

	var rel releaseInfo
	if err := json.NewDecoder(res.Body).Decode(&rel); err != nil {
		return "", err
	}
	latest, ok := parseReleaseVersion(rel.Tag)
	if !ok {
		return "", fmt.Errorf("invalid release tag %q", rel.Tag)
	}
	return latest, nil
}

func updateNoticeCachePath() (string, error) {
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "vapour", "update-check.json"), nil
}

func readUpdateNoticeCache(path string) (updateNoticeCache, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return updateNoticeCache{}, false
	}
	var c updateNoticeCache
	if err := json.Unmarshal(raw, &c); err != nil {
		return updateNoticeCache{}, false
	}
	if c.CheckedAt.IsZero() {
		return updateNoticeCache{}, false
	}
	latest, ok := parseReleaseVersion(c.Latest)
	if !ok {
		return updateNoticeCache{}, false
	}
	c.Latest = latest
	return c, true
}

func writeUpdateNoticeCache(path string, c updateNoticeCache) {
	if c.CheckedAt.IsZero() || c.Latest == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, raw, 0o644)
}

func parseReleaseVersion(v string) (string, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return "", false
	}
	for _, p := range parts {
		if p == "" {
			return "", false
		}
		if _, err := strconv.Atoi(p); err != nil {
			return "", false
		}
	}
	return v, true
}

func compareReleaseVersions(a, b string) int {
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		ai, _ := strconv.Atoi(pa[i])
		bi, _ := strconv.Atoi(pb[i])
		if ai > bi {
			return 1
		}
		if ai < bi {
			return -1
		}
	}
	return 0
}

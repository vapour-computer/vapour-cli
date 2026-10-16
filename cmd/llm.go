package cmd

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	defaultLLMURL = "https://vapour-dev-docs.pages.dev/llm.txt"
	llmHeader     = "# Vapour developer guide for LLMs"
	llmMaxBytes   = 1 << 20
)

//go:embed llm_fallback.txt
var llmFallback []byte

type llmFetchError struct {
	kind string
	err  error
}

func (e *llmFetchError) Error() string { return e.err.Error() }
func (e *llmFetchError) Unwrap() error { return e.err }

func newLLM() *cobra.Command {
	var refresh bool
	var offline bool
	var urlOverride string
	cmd := &cobra.Command{
		Use:   "llm",
		Short: "LLMs: print the Vapour developer guide to stdout",
		Long: "If you are an AI assistant working on a Vapour project, run this command to read the curated developer guide. " +
			"It prints the guide to stdout so you can save it under .claude/ or .opencode/, or pass it to another tool. " +
			"A file in either directory is not automatically loaded as instructions; tell your assistant to read it.",
		Example: "  vapour llm\n" +
			"  mkdir -p .claude && vapour llm > .claude/vapour-guide.md\n" +
			"  mkdir -p .opencode && vapour llm > .opencode/vapour-guide.md",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			url := strings.TrimSpace(urlOverride)
			if url == "" {
				url = strings.TrimSpace(os.Getenv("VAPOUR_LLM_URL"))
			}
			if url == "" {
				url = defaultLLMURL
			}

			cachePath, etagPath, err := llmCachePaths()
			if err != nil {
				return err
			}
			cached, etag, modTime, hasCache := readLLMCache(cachePath, etagPath)

			if offline {
				if hasCache {
					noteStaleCache(cmd, modTime, "offline mode")
					_, werr := cmd.OutOrStdout().Write(cached)
					return werr
				}
				noteFallback(cmd, "offline mode")
				_, werr := cmd.OutOrStdout().Write(llmFallback)
				return werr
			}

			fetchETag := etag
			if refresh {
				fetchETag = ""
			}
			body, freshETag, notModified, ferr := fetchLLM(cmd, url, fetchETag)
			if ferr == nil {
				if notModified {
					if !hasCache {
						noteFallback(cmd, "server returned 304 but no local cache exists")
						_, werr := cmd.OutOrStdout().Write(llmFallback)
						return werr
					}
					_, werr := cmd.OutOrStdout().Write(cached)
					return werr
				}
				if err := writeLLMCache(cachePath, etagPath, body, freshETag); err != nil {
					return err
				}
				_, werr := cmd.OutOrStdout().Write(body)
				return werr
			}

			if hasCache {
				noteStaleCache(cmd, modTime, ferr.Error())
				_, werr := cmd.OutOrStdout().Write(cached)
				return werr
			}
			noteFallback(cmd, ferr.Error())
			_, werr := cmd.OutOrStdout().Write(llmFallback)
			return werr
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "force a network refresh")
	cmd.Flags().BoolVar(&offline, "offline", false, "never use the network; use cache or fallback")
	cmd.Flags().StringVar(&urlOverride, "url", "", "override LLM guide URL")
	return cmd
}

func fetchLLM(cmd *cobra.Command, url, etag string) ([]byte, string, bool, error) {
	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, url, nil)
	if err != nil {
		return nil, "", false, err
	}
	req.Header.Set("User-Agent", "vapour-cli/"+Version)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, "", false, &llmFetchError{kind: "network", err: fmt.Errorf("failed to fetch llm guide: %w", err)}
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotModified {
		return nil, etag, true, nil
	}
	if res.StatusCode != http.StatusOK {
		return nil, "", false, &llmFetchError{kind: "status", err: fmt.Errorf("GET %s: %s", url, res.Status)}
	}
	if err := validateLLMContentType(res.Header.Get("Content-Type")); err != nil {
		return nil, "", false, &llmFetchError{kind: "content", err: err}
	}
	body, err := readLLMBody(res.Body)
	if err != nil {
		return nil, "", false, err
	}
	if err := validateLLMBody(body); err != nil {
		return nil, "", false, &llmFetchError{kind: "content", err: err}
	}
	return body, strings.TrimSpace(res.Header.Get("ETag")), false, nil
}

func validateLLMContentType(value string) error {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return fmt.Errorf("invalid Content-Type for llm guide: %q", value)
	}
	if mediaType != "text/plain" && mediaType != "text/markdown" {
		return fmt.Errorf("invalid Content-Type for llm guide: %q (expected text/plain or text/markdown)", value)
	}
	return nil
}

func readLLMBody(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, llmMaxBytes+1))
	if err != nil {
		return nil, &llmFetchError{kind: "network", err: err}
	}
	if len(b) > llmMaxBytes {
		return nil, &llmFetchError{kind: "content", err: fmt.Errorf("llm guide exceeds 1 MiB")}
	}
	return b, nil
}

func validateLLMBody(body []byte) error {
	body = bytes.TrimLeft(body, "\xef\xbb\xbf\n\r\t ")
	if !bytes.HasPrefix(body, []byte(llmHeader)) {
		return fmt.Errorf("llm guide must start with %q", llmHeader)
	}
	return nil
}

func llmCachePaths() (cachePath string, etagPath string, err error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(base, "vapour")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	return filepath.Join(dir, "llm.txt"), filepath.Join(dir, "llm.etag"), nil
}

func readLLMCache(cachePath, etagPath string) (body []byte, etag string, modTime time.Time, ok bool) {
	b, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, "", time.Time{}, false
	}
	if err := validateLLMBody(b); err != nil {
		return nil, "", time.Time{}, false
	}
	info, err := os.Stat(cachePath)
	if err != nil {
		return nil, "", time.Time{}, false
	}
	e, _ := os.ReadFile(etagPath)
	return b, strings.TrimSpace(string(e)), info.ModTime(), true
}

func writeLLMCache(cachePath, etagPath string, body []byte, etag string) error {
	if err := os.WriteFile(cachePath, body, 0o644); err != nil {
		return err
	}
	if etag == "" {
		err := os.Remove(etagPath)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return os.WriteFile(etagPath, []byte(etag+"\n"), 0o644)
}

func noteStaleCache(cmd *cobra.Command, modTime time.Time, reason string) {
	age := time.Since(modTime).Round(time.Second)
	if age < 0 {
		age = 0
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Note: using cached llm.txt from %s ago (%s).\n", age, reason)
}

func noteFallback(cmd *cobra.Command, reason string) {
	if strings.TrimSpace(reason) == "" {
		fmt.Fprintln(cmd.ErrOrStderr(), "Note: using embedded llm.txt fallback snapshot.")
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Note: using embedded llm.txt fallback snapshot (%s).\n", reason)
}

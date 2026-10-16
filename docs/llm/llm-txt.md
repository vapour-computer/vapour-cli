# `vapour llm` (`cmd/llm.go`)

Purpose: print the curated assistant guide to stdout so developers can pipe it into coding assistants without waiting for a CLI release.

## Source and overrides

- Default URL: `https://vapour-dev-docs.pages.dev/llm.txt`
- `--url <url>` overrides per run
- `VAPOUR_LLM_URL` overrides by environment (unless `--url` is set)

## Fetch contract

- Timeout: 10 seconds
- Max body size: 1 MiB
- Allowed content types: `text/plain` or `text/markdown`
- Body must start with: `# Vapour developer guide for LLMs`

Invalid content type/body/size is rejected.

## Cache and ETag

- Cache path: `os.UserCacheDir()/vapour/llm.txt`
- ETag path: `os.UserCacheDir()/vapour/llm.etag`
- Fetch uses `If-None-Match` when an ETag exists
- `304 Not Modified` serves cached `llm.txt`

## Offline/fallback behavior

- `--offline`: never uses network; serves cache if present, otherwise embedded fallback
- Embedded fallback uses `//go:embed` (`cmd/llm_fallback.txt`) for first run with no network
- On stale-cache or fallback use, command prints a stderr note (cache age/reason)

## Why this exists

The guide can update via docs deploy (`book/llm.txt`) without requiring a new `vapour-cli` binary release.

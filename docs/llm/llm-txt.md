# `vapour llm` (`cmd/llm.go`)

Purpose: print the curated assistant guide to stdout so developers can pipe it into coding assistants without waiting for a CLI release.

The root help labels `llm` as a guide for LLMs and explicitly says it writes to stdout. `vapour llm --help` suggests reading the output directly or saving it under `.claude/` or `.opencode/` for an assistant to read. Files saved in those directories are not automatically loaded as instructions; point the assistant at the file. For example, `mkdir -p .opencode && vapour llm > .opencode/vapour-guide.md` saves the guide without replacing an existing `AGENTS.md`.

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
- Any fetch failure with no cache (network, HTTP status, bad content type, bad header/body, size cap) falls back to embedded copy and prints the reason to stderr
- On stale-cache or fallback use, command prints a stderr note (cache age/reason)

## Keeping fallback in sync

- `scripts/sync-llm.sh` downloads `book/llm.txt` from `vapour-dev-docs` `main` into `cmd/llm_fallback.txt`
- Script validates required first-line header before replacing the file

## Why this exists

The guide can update via docs deploy (`book/llm.txt`) without requiring a new `vapour-cli` binary release.

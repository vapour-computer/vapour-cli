# Vapour CLI: agent guide

`vapour` is a Go CLI (cobra) for [Vapour](https://vapour.ink): create a web-game project, build it, test it locally,
and publish it (store page + web build). Read by Claude Code (via `CLAUDE.md`), Codex and opencode.

## Commands

```sh
go build ./... && go vet ./... && go test ./...   # what CI runs, plus: test -z "$(gofmt -l .)"
go run . --help                                   # command tree
```

Releases: push a `v*` tag; `.github/workflows/release.yml` runs goreleaser. Users install with `install.sh`.
`main` is protected: work on a branch and open a PR.

## Layout

- `main.go`, `cmd/`: one file per command; `cmd/root.go` registers them and resolves the API client.
- `internal/`: `api`, `config`, `devserver`, `enginebuild`, `localprofile`, `media`, `project`, `scaffold`, `storepage`, `zipdir`.
- `pkg/delivery`: experimental chunked-upload protocol.

## LLM docs: keep them current

`docs/llm/` explains how every Vapour feature works. **Any PR that adds or changes a command, flag, env var,
`vapour.toml` field or user-visible behavior must add or update the matching `docs/llm/` file in the same PR**
(the PR template has a checkbox for it). Write facts from the code, not from memory.

| File | Covers |
|---|---|
| [docs/llm/overview.md](docs/llm/overview.md) | architecture, packages, API/token resolution, env vars |
| [docs/llm/auth.md](docs/llm/auth.md) | `login`, `logout`, `whoami`, credentials file |
| [docs/llm/create.md](docs/llm/create.md) | `create` and scaffold templates |
| [docs/llm/manifest.md](docs/llm/manifest.md) | every `vapour.toml` field and its validation |
| [docs/llm/build.md](docs/llm/build.md) | `build` (Godot, Unity, Bevy exports) |
| [docs/llm/test.md](docs/llm/test.md) | `test`: local server, FPS overlay, profiler |
| [docs/llm/store.md](docs/llm/store.md) | `store check/push`, `store/page.json`, media rules |
| [docs/llm/publish.md](docs/llm/publish.md) | `publish`, zip checks, experimental chunks |
| [docs/llm/llm-txt.md](docs/llm/llm-txt.md) | `vapour llm` fetch/cache/fallback design |
| [docs/llm/release.md](docs/llm/release.md) | goreleaser, `install.sh`, CI, branch rules |

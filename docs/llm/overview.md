# Overview

`vapour` talks to the Vapour API (contract v1) to create projects, set store pages and upload builds. Everything is
driven by a per-game `vapour.toml` (see [manifest.md](manifest.md)).

## Typical flow
`vapour login` → `vapour create` (or `vapour template`) → `vapour build` (or export manually) → `vapour test` → `vapour publish --version <semver> --notes "..."`.

For assistant context in-terminal: `vapour llm` (fetches docs `llm.txt`, caches with ETag, has embedded fallback).

## Packages (`internal/` unless noted)
| Package | Role |
|---|---|
| `api` | HTTP client: `Me`, `CreateProject`, `PutStore`, `Publish`; typed `api.Error` with a `Code` (e.g. `version_exists`) |
| `config` | credentials file, API/token resolution |
| `project` | `vapour.toml` load/save/validate, `Slugify` |
| `scaffold` | writes starter folders from embedded templates (`templates/{godot,unity,bevy,other,_store}`) |
| `storepage` | loads/validates `store/page.json` + `template.md`, builds the upload bundle |
| `media` | image/video validation by content sniffing |
| `zipdir` | checks a web export folder and zips it |
| `enginebuild` | `vapour build` adapters |
| `perfcheck` | perf guidance warnings shared by `build`, `test`, `publish`, and `perf` |
| `devserver` | local server for `vapour test`, FPS overlay injection |
| `localprofile` | Linux-only: private browser + `vapour-profiler` |
| `pkg/delivery` | experimental chunked upload |

## Resolution rules (`cmd/root.go`, `internal/config`)
- API base: `--api` flag > `VAPOUR_API` > `api` in saved credentials > `https://vapour-api-637456171586.asia-south1.run.app`.
- Token: `VAPOUR_TOKEN` (for CI) > saved credentials. Commands needing auth fail with "not logged in".
- Credentials file: `$XDG_CONFIG_HOME/vapour/credentials` or `~/.config/vapour/credentials`.

## Env vars
`VAPOUR_API`, `VAPOUR_TOKEN`, `VAPOUR_PROFILER` (profiler binary), `VAPOUR_NO_UPDATE_CHECK`, `VAPOUR_RELEASES_URL` (release endpoint override), `VAPOUR_LLM_URL` (`vapour llm` source override), `XDG_CONFIG_HOME`, `XDG_CACHE_HOME`, `GODOT`, `UNITY_EDITOR` (build).

## Conventions
- Errors: returned from `RunE`; `cmd.ErrorMessage` prints `error: ...` plus the docs URL
  (`https://vapour-dev-docs.pages.dev/`). Commands set `SilenceUsage/SilenceErrors`.
- Almost every command takes `--dir` (game folder, default `.`).
- `build`, `test`, and `publish` can print perf findings; `--no-perf-hints` suppresses those warnings.
- Validation mirrors server rules so failures happen before any upload.

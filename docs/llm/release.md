# Releases and CI

- CI (`.github/workflows/ci.yml`, push to main + PRs): `gofmt -l` clean, `go vet ./...`, `go test ./...`.
- Release: push a `v*` tag → `release.yml` runs goreleaser (`.goreleaser.yaml`): linux/darwin/windows × amd64/arm64,
  archives `vapour_<version>_<os>_<arch>.tar.gz` (zip on Windows) and `checksums.txt` as GitHub Release assets.
  Version is injected via `-X .../cmd.Version`.
- Before release, run `scripts/sync-llm.sh` so embedded `cmd/llm_fallback.txt` matches `vapour-dev-docs` `book/llm.txt`.
- Install: `curl -fsSL https://raw.githubusercontent.com/vapour-computer/vapour-cli/main/install.sh | sh`. Detects
  OS/arch, finds latest tag via the `/releases/latest` redirect, verifies SHA-256, installs to `/usr/local/bin` or
  `~/.local/bin`. Env: `VAPOUR_VERSION`, `VAPOUR_INSTALL_DIR`. Windows is not supported by the script.
- `main` is protected (`.github/rulesets/protect-main.json`: PR required). `.githooks/pre-push` blocks direct pushes
  (`git config core.hooksPath .githooks`; override once with `ALLOW_MAIN_PUSH=1`).

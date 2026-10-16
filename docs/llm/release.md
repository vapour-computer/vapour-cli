# Releases and CI

- CI (`.github/workflows/ci.yml`): pull requests only, with `gofmt -l` clean, `go vet ./...`, `go test ./...`.
- Release (`.github/workflows/release.yml`):
  - triggers on pushes to `main` (except docs-only changes under `**/*.md` and `docs/**`), manual `workflow_dispatch`, and `v*` tag pushes;
  - runs the same Go test steps in a `test` job, then `release` needs `test`;
  - fetches tags, derives the next semver tag from the latest `vX.Y.Z` (default base `v0.1.0` when none exist), and bumps:
    - `major` if any commit subject since the previous tag contains `[major]`;
    - else `minor` if any contains `[minor]`;
    - else `patch`;
    - manual `workflow_dispatch` input overrides automatic bump;
  - skips tag creation when `HEAD` already has a `v*` tag (idempotent reruns);
  - creates and pushes an annotated tag for branch/manual runs, then runs goreleaser in the same job;
  - runs `bash scripts/sync-llm.sh || echo 'llm sync skipped'` when script exists, before goreleaser;
  - runs goreleaser as `release --clean --skip=validate` because `sync-llm.sh` can refresh tracked fallback text without committing.
- Tag pushes (`v*`) still run release job; in that path the workflow does not create a second tag.
- Goreleaser (`.goreleaser.yaml`): linux/darwin/windows × amd64/arm64, archives `vapour_<version>_<os>_<arch>.tar.gz`
  (zip on Windows), writes `checksums.txt`, changelog sorted ascending, and injects runtime version via
  `-X github.com/vapour-computer/vapour-cli/cmd.Version={{.Version}}`.
- Update notice (`cmd/update_notice.go`): outside CI and unless `VAPOUR_NO_UPDATE_CHECK=1`, commands except `update`
  and `--version` check GitHub latest release at most once per 24h (cache file in user cache dir), timeout 2s,
  never fail command, and print one stderr line when newer release exists.
- Install: `curl -fsSL https://raw.githubusercontent.com/vapour-computer/vapour-cli/main/install.sh | sh`. Detects
  OS/arch, finds latest tag via the `/releases/latest` redirect (must stay on github.com), verifies SHA-256,
  validates archive contents, installs to `/usr/local/bin` or `~/.local/bin`. Env: `VAPOUR_VERSION`,
  `VAPOUR_INSTALL_DIR`. Windows is not supported by the script.
- `vapour update` only trusts official GitHub release API/asset hosts by default. Overriding
  `VAPOUR_RELEASES_URL` requires `VAPOUR_ALLOW_UNTRUSTED_RELEASES=1`.
- `main` is protected (`.github/rulesets/protect-main.json`: PR required). `.githooks/pre-push` blocks direct pushes
  (`git config core.hooksPath .githooks`; override once with `ALLOW_MAIN_PUSH=1`).

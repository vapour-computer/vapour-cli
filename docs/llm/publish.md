# `vapour publish` (`cmd/publish.go`, `internal/zipdir`, `pkg/delivery`)

Flags: `--dir`, required `--version`, exactly one of `--notes` or `--notes-file`, `--dry-run`, `--skip-store`,
`--no-perf-hints`, `--experimental-chunks`.

Order: load + validate manifest → load store bundle (unless `--skip-store`) → resolve release notes
(`--notes-file` or `--notes`, non-empty after trimming; ≤5000 chars) → pack the build → (stop here on `--dry-run`) → register
project if no id → push store page → upload build. Store goes first so rejected media doesn't use up a version.

Build packing (`zipdir`): a folder must contain top-level `index.html` and no native binaries
(`.exe .dll .so .dylib .app .msi`); it is zipped (skips `.git`, `.DS_Store`, `Thumbs.db`) to a temp file. A `.zip` is
uploaded as is. Limit 512 MiB. Uploaded as `build.zip`.

Unless suppressed by `--no-perf-hints`, command output also includes warnings from `internal/perfcheck`.

A version can be published once: API error `version_exists` becomes "pass a new --version".
Success prints the game URL.

`--experimental-chunks`: opt-in content-addressed chunk protocol (`pkg/delivery/README.md`); the deployed API doesn't
implement it yet. Default remains ZIP.

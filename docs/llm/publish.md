# `vapour publish` (`cmd/publish.go`, `internal/zipdir`, `pkg/delivery`)

Flags: `--dir`, `--version` (override `project.version`), `--notes`, `--notes-file`, `--dry-run`, `--skip-store`,
`--experimental-chunks`.

Order: load + validate manifest → load store bundle (unless `--skip-store`) → resolve release notes
(`--notes-file` > `--notes` > `store.notes`; ≤5000 chars) → pack the build → (stop here on `--dry-run`) → register
project if no id → push store page → upload build. Store goes first so rejected media doesn't use up a version.

Build packing (`zipdir`): a folder must contain top-level `index.html` and no native binaries
(`.exe .dll .so .dylib .app .msi`); it is zipped (skips `.git`, `.DS_Store`, `Thumbs.db`) to a temp file. A `.zip` is
uploaded as is. Limit 512 MiB. Uploaded as `build.zip`.

A version can be published once: API error `version_exists` becomes "bump version in vapour.toml (or pass --version)".
Success prints the game URL.

`--experimental-chunks`: opt-in content-addressed chunk protocol (`pkg/delivery/README.md`); the deployed API doesn't
implement it yet. Default remains ZIP.

# `vapour publish` (`cmd/publish.go`, `internal/zipdir`, `pkg/delivery`)

Flags: `--dir`, required `--version`, exactly one of `--notes` or `--notes-file`, `--dry-run`, `--skip-store`,
`--no-perf-hints`, `--experimental-chunks`.

Order for default ZIP publishing: load + validate manifest → load store bundle (unless `--skip-store`) → resolve release notes
(`--notes-file` or `--notes`, non-empty after trimming; ≤5000 chars) → pack the build → scan the ZIP → sign it and write
`.vapour/build.zip.attestation.json` → verify the signed ZIP locally → (stop here on `--dry-run`) → register project if no id → push store page → upload
build and attestation. Store goes first so rejected media doesn't use up a version. The server verifies the ZIP, then an
admin reviews the version before it goes live.

Build packing (`zipdir`): a folder must contain top-level `index.html` and no native binaries
(`.exe .dll .so .dylib .app .msi`); it is zipped (skips `.git`, `.DS_Store`, `Thumbs.db`) to a temp file. A `.zip` is
uploaded as is. `build.dir` is resolved through symlinks and must stay inside the game folder. Limit 512 MiB.
Uploaded as `build.zip`.

`vapour scan [zip-or-folder]` runs the same static checks without publishing. `--dir` selects a game (no argument
uses `build.dir`); `--json` emits findings. Errors stop upload, but passing is not an antivirus guarantee.
`vapour verify [zip]` checks the Ed25519 attestation signature and, when a ZIP is supplied, its SHA-256. By default it
looks for `<zip>.attestation.json` then the game's `.vapour/build.zip.attestation.json`; `--attestation` overrides this.
`--remote` checks the saved project's server verification record and requires login. Without a ZIP, remote verification
does not check local ZIP bytes. `--version` selects the version (default `project.version`); `--json` emits a result.

Unless suppressed by `--no-perf-hints`, command output also includes warnings from `internal/perfcheck`.

A version can be published once: API error `version_exists` becomes "pass a new --version".
Success prints the game URL.

`--experimental-chunks`: its dry run still scans, but real chunk uploads are blocked until this protocol can provide
a signed attestation that the API can verify. Use ZIP publishing for uploads. The deployed API does not implement the
chunk protocol yet. Upload chunk URLs must be HTTPS and on an allowlisted public host.

The API requires an attestation on every ZIP upload and verifies its Ed25519 signature, key identity, ZIP SHA-256 and
size before writing the build. Missing or forged attestations fail with `verification_required` or `verification_failed`.
The CLI's local check does not replace the server's check.

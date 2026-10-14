# `vapour test` (`cmd/test.go`, `internal/devserver`, `internal/localprofile`)

Serves `build.dir` (folder or .zip read in place) on `127.0.0.1` and opens it in the browser.

Flags: `--dir`, `--port` (0 = free port), `--no-telemetry` (no FPS overlay), `--no-open` (also skips profiler),
`--no-profile`, `--profiler PATH` (else `$VAPOUR_PROFILER`, next to the vapour binary, then PATH).

- Always sends cross-origin isolation headers (COOP/COEP/CORP) so Wasm threads (SharedArrayBuffer) work, and `no-store`.
- FPS overlay (`internal/devserver/fps.js`): injected into `index.html` at serve time (before `</head>`), never written
  to the build folder, so `publish` cannot include it. Toggle with Ctrl+Shift+F. On tab close it POSTs a summary to
  `/__vapour/report` (same-origin only, body capped at 4 KiB); the CLI prints frames, avg fps, 1% low, worst frame.
- Profiler (Linux only, `localprofile_linux.go`): launches a Chromium-family browser (chromium, chrome, brave, edge)
  with a throwaway `--user-data-dir`, runs `vapour-profiler dashboard --pid <browser> --tree --autostart` (dashboard
  port 6767), and stops when the browser exits. If the profiler or a browser is missing it prints why and continues
  with the overlay only. Other OSes use `localprofile_other.go` (unsupported stub).
- Nothing leaves the machine.

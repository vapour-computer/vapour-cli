# `vapour test` (`cmd/test.go`, `internal/devserver`, `internal/localprofile`)

Serves `build.dir` (folder or .zip read in place) on `127.0.0.1` and opens it in the browser.
`build.dir` is resolved through symlinks and must stay inside the game folder.

Flags: `--dir`, `--port` (0 = free port), `--no-telemetry` (no FPS overlay), `--no-open` (also skips profiler),
`--no-profile`, `--profiler PATH` (else `$VAPOUR_PROFILER`, next to the vapour binary, then PATH), `--no-perf-hints`.

- Always sends cross-origin isolation headers (COOP/COEP/CORP) so Wasm threads (SharedArrayBuffer) work, and `no-store`.
- FPS overlay (`internal/devserver/fps.js`): injected into `index.html` at serve time (before `</head>`), never written
  to the build folder, so `publish` cannot include it. Toggle with Ctrl+Shift+F. On tab close it POSTs a summary to
  `/__vapour/report` (same-origin only, body capped at 4 KiB); the CLI prints frames, avg fps, 1% low, worst frame.
  It also POSTs periodic frame batches to `/__vapour/records`; `vapour test` forwards those to
  `vapour-profiler /api/records` so saved `report.json` includes frame/FPS data.
- The served player shell injects a dev runtime script (`data-vapour="dev-runtime"`) and a quit panel. `runtime.on_quit`
  controls local quit behavior (`store|home|reload|stay`).
- Mouse/pointer behavior for published builds comes from `zipdir` pointer-lock injection: click-to-capture, optional
  Escape-on-loss (`pointer_lock = "pause"`), touch-skip paths, and startup spike filtering in movement handling.
- Profiler (Linux only, `localprofile_linux.go`): launches a Chromium-family browser (chromium, chrome, brave, edge)
  with a throwaway `--user-data-dir`, runs `vapour-profiler dashboard --pid <browser> --tree --autostart` (dashboard
  port 6767), and stops when the browser exits. At session end, the CLI fetches `/api/report` from the local profiler
  (after the overlay's final records batch) and saves `.vapour/test-output/report.json`, then prints that path. If the profiler or a browser is missing it prints
  why and continues with the overlay only. Other OSes use `localprofile_other.go` (unsupported stub).
- Nothing leaves the machine.

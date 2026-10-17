# Vapour: platform gaps and guardrails

Source: `vapour-poc/ENGINE-COMPAT.md`, `REPORT.md`, `heavy-game-testing.md`.
`vapour-cli` now carries guardrails (`internal/guardrails`) that run in `build`, `test` and `publish`.
A `block` finding stops the command unless its code is listed under `[guardrails] allow` in `vapour.toml`.

## Can't be fixed by Vapour: guardrails in the CLI

| Limit | Guardrail code | Level | Notes |
|---|---|---|---|
| Godot C# has no web export | `godot_csharp` | block | `.csproj`/`.sln` in a Godot project |
| Godot Forward+/Mobile have no web export | `godot_renderer` | warn | warn only: Godot falls back if `rendering_method.web=gl_compatibility` is set |
| Native plugins (Unity `Assets/Plugins/<platform>`) | `native_plugins` | warn | managed or `.jslib` replacement needed |
| Native executables in the build (`.exe .dll .so ...`) | existing `zipscan` | block | already enforced |
| Raw TCP/UDP/ENet sockets | `raw_sockets` | warn | heuristic source scan; game must use WebSocket/WebRTC |
| Files over 512 MiB, build over 1 GiB expanded | `file_too_large`, `build_too_large` | block | lifted by `--experimental-chunks` |
| Engine `other` (Emscripten C++, Java) | `engine_other` | info | no adapter yet |

Not covered by a guardrail yet:
- [ ] Godot `variant/extensions_support` / GDExtension native libs in the source tree (block).
- [ ] Unity IL2CPP-incompatible code: `System.Threading.Thread`, `Process`, `System.IO.Pipes`, `DllImport` (warn).
- [ ] Bevy: crates that need native-only features (`bevy_renet` is flagged; add `tokio` net, `rodio` on some targets).
- [ ] Memory: warn when runtime.memory_mb is unset on builds above 500 MiB.

## Fixable or partly fixable: work for the platform

### P0: make default exports load (`vapour-infra`)
- [ ] Send `Content-Encoding` and the inner MIME type for `*.br` / `*.gz` (`handlers.rs` `content_type_for_path`, `serve_play_file`).
- [ ] Lower-case extensions before matching; add missing MIME types (`.pck .data .ktx2 .glb .webm ...`).
- [ ] Mirror the MIME/encoding table in `vapour-cli/internal/devserver`, `dev-cdn.mjs`, POC `sw.js`.
- [ ] Unity scaffold `VapourBuild.cs`: Brotli + decompression fallback + hashed names.
- [ ] Cross-origin isolation: full-page navigation into `/play`, default COEP `require-corp`, fix `DeviceCheck.tsx`.
- [ ] Version-pinned play URLs (`/play/{slug}/v/{version}/`), immutable cache, 24 h grace for old versions, `delete_prefix` pagination fix.
- [ ] `Content-Length`, `Accept-Ranges`, ETag, Range and HEAD on `/play`.

### P1: isolation and player behaviour
- [ ] Per-game origin `{slug}.<play-domain>`. Fixes shared saves, service-worker clashes, absolute paths, JS on API origin. Then drop the `absolute_paths`, `pwa_service_worker`, `user_saves` guardrails.
- [ ] Widen iframe `sandbox` / `allow` (popups, modals, downloads, clipboard, keyboard-lock, xr). Then drop `external_apis`.
- [ ] Click-to-play gate, iframe focus, fullscreen target the iframe.
- [ ] Per-engine ready signal instead of `load` + 400 ms.
- [ ] Injected `/__vapour/runtime.js` (progress, errors, consume `vapour:control` so mobile controls work).
- [ ] Threads: honour `[runtime] threads` and `coep` from `vapour.toml` end to end; Safari stays unsupported with `credentialless`.

### P2: publish robustness and toolchain
- [ ] Drop `__MACOSX/`, `.DS_Store`, `Thumbs.db` before wrapper stripping.
- [ ] Entry HTML detection (depth <= 2, single `*.html`); store the chosen entry.
- [ ] CLI: store already-compressed files without deflate.
- [ ] Signed direct-to-bucket uploads for files over 512 MiB or bodies over 32 MiB (Infra PR #2); retire the HTTP/2 staging bridge afterwards.
- [ ] Godot template: `export_presets.cfg` with `thread_support=false`, PWA off, VRAM compression for desktop + mobile.
- [ ] Bevy template: real `Cargo.toml`, `getrandom_backend="wasm_js"`, wasm-bindgen version check, Trunk `--public-url ./`.
- [ ] `vapour test --player` with production sandbox attributes and the same header/MIME table as prod.
- [ ] CDN in front of the play bucket; edge compression for `.wasm`/`.js`/`.pck`.

### Larger efforts
- [ ] Multiplayer: WebSocket/WebRTC transport story and a hosted dedicated server option (needed for games like Unitystation).
- [ ] New engine adapters: Emscripten (C++), then evaluate TeaVM/GWT for Java.
- [ ] Godot C#: revisit when Godot ships a web export for it; remove the `godot_csharp` block then.
- [ ] Multi-GB games: chunked delivery with OPFS cache (CLI PR #2, Web PR #5, Infra PR #2).

## Contradictions to resolve
- [ ] `perfcheck` tells Godot users to set `thread_support=true`, but the platform guidance is threads off. Align the two.
- [ ] Unity docs say Gzip + fallback works however it is served; production never sends `Content-Encoding`.
- [ ] Design docs describe a separate play domain and a sandbox without `allow-same-origin`; the code does the opposite.
- [ ] Cover image limit is 5 MiB in one doc and 2 MiB in another.

## Candidate games to test the platform
- [ ] Pick one famous open-source game per engine that satisfies the guardrails (single-player, no sockets, under 500 MiB). Unity Open Project "Chop Chop" is the leading Unity candidate; Godot 4 and Bevy candidates still to be found.

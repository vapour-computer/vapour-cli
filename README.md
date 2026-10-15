# vapour-cli

Command-line tool for [Vapour](https://vapour.ink): log in, create a game project, publish your web export.

## Install

macOS and Linux (amd64, arm64):

```sh
curl -fsSL https://raw.githubusercontent.com/vapour-computer/vapour-cli/main/install.sh | sh
```

This downloads the latest release from GitHub, verifies its checksum, and installs `vapour` to `/usr/local/bin` (or `~/.local/bin` if that isn't writable). Pin a version with `VAPOUR_VERSION=v0.2.0`, or choose the directory with `VAPOUR_INSTALL_DIR`.

Or download a binary (including Windows) from the [releases](https://github.com/vapour-computer/vapour-cli/releases) page, or build it:

```sh
go install github.com/vapour-computer/vapour-cli@latest
```

## Use

```sh
vapour login --token vpr_...          # create a token on your Vapour dashboard
vapour create "My Game" --engine godot
vapour template godot "My Other Game" # clone org template, remove .git, initialize Vapour files
cd my-game
# export your game for Web into build/web/ (needs an index.html)
vapour publish
```

### Update

`vapour-cli update` installs the latest release over the running binary (checksum verified). `--check` only reports, `--version v0.2.0` picks a release. Not available on Windows.

### Test locally

```sh
vapour test                  # serve build.dir on 127.0.0.1 and open it, with an FPS overlay
vapour test --no-telemetry   # same, without the overlay
vapour test --port 8080 --no-open
```

`vapour test` serves your web export with the cross-origin isolation headers Wasm threads need. The FPS overlay
(FPS, frame time, 1% low, worst frame; toggle with Ctrl+Shift+F) exists only here: it is injected while serving, never
written into your build folder, so `vapour publish` cannot include it. It listens on loopback only and sends nothing over
the network; closing the game tab prints a frame summary in the terminal.

On Linux, `vapour test` also opens the game in its own Chromium-family browser (private temporary profile) and attaches
[vapour-profiler](https://github.com/vapour-computer/vapour-profiler) to that browser and all its processes. The profiler
dashboard opens as a second tab and CPU and memory capture starts automatically. Closing the browser, or Ctrl+C,
stops everything and deletes the temporary profile. Point the CLI at the profiler with `--profiler PATH` or
`VAPOUR_PROFILER`, or place it next to the `vapour` binary; if it or a Chromium-family browser is missing, `vapour test` says so and carries on with the FPS overlay.
Use `--no-profile` to skip it.

`vapour create --from <git-url>` clones a sample game instead of scaffolding. `vapour publish --dry-run` checks and
zips your build without uploading. In CI, set `VAPOUR_TOKEN` instead of running `login`.

`vapour template <engine> <name>` clones official template repos (`bevy`, `godot`, `unity`) from GitHub
(default org: `vapour-computer`), removes `.git`, then writes `vapour.toml` and `store/` files.
Override the org with `--org your-org`.

### Store page: text, images, banner, trailer

`vapour create` also writes a `store/` folder:

| File | Purpose |
|---|---|
| `store/template.md` | The text of your game's page (Markdown). Each `## ` heading is a section. |
| `store/page.json` | A JSON template: tagline, category, tags, details, links, media files and the section layout. |
| `store/page.schema.json` | JSON Schema, so your editor can check and autocomplete `page.json`. |

Put images and the trailer in `store/`, name them under `media` in `page.json`, then:

```sh
vapour store check      # validate text, JSON and media without uploading
vapour store push       # update only the store page
vapour publish --notes "What changed"   # store page + a new version of your build
```

Banner: PNG/JPEG/WebP, at least 960x300 and 2:1 or wider, 4 MiB. Cover: at least 460x215, 2 MiB. Up to 8 screenshots (at least
640x360, 4 MiB each). Trailer: MP4 or WebM, 30 MiB. Files are checked by content, not by name. Each version can be published once:
bump `version` in `vapour.toml` for the next release. Players see only published games.

### vapour.toml

```toml
[project]
id = "..."          # filled in by `vapour create`
title = "My Game"
slug = "my-game"
engine = "godot"    # godot | unity | bevy | other
version = "0.1.0"

[build]
dir = "build/web"   # web export folder, or a .zip file

[store]
page = "store/page.json"       # JSON template: details, media files, layout
content = "store/template.md"  # Markdown text of the store page
notes = ""                     # optional: Markdown release notes for this version
```

### Phones: orientation and touch controls

```toml
[mobile]
orientation = "landscape"   # any | landscape | portrait
# HTML shown over the game while a phone is held the wrong way (max 4000 characters).
# It is rendered in a sandbox with scripts off, so styling works but JavaScript does not.
rotate_message = "<h2>Rotate your phone</h2><p>This game plays in landscape.</p>"
# rotate_message_file = "store/rotate.html"   # or read it from a file in the game folder

[controller]
mode = "keyboard-map"       # keyboard-map | dedicated (alias: action-map)

[[controller.controls]]
id = "move"
type = "joystick"           # button | dpad | joystick
x = 0.23                    # centre of the control, 0..1 of the screen
y = 0.77
w = 0.30
h = 0.30
up = "w"
down = "s"
left = "a"
right = "d"
```

`vapour controller init|check|preview` writes, validates and draws the layout. `preview` prints a landscape
screen in the terminal with each control labelled; `preview --html` writes a page you can open. The layout
is uploaded with `vapour publish`.

Credentials live in `~/.config/vapour/credentials` (mode 0600). Set `--api` or `VAPOUR_API` to point at another server.

## Build from source

`vapour build` exports Godot 4, Unity WebGL or Bevy using locally installed tools.
See [engine build configuration and prerequisites](internal/enginebuild/README.md).
Add `.vapour/` to your project's ignore file; it contains generated boot metadata.

## Develop

```sh
go test ./...
```

Documentation: https://vapour-dev-docs.pages.dev/

MIT licensed.

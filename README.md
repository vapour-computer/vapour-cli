# vapour-cli

Command-line tool for [Vapour](https://vapour.ink): log in, create a game project, publish your web export.

## Install

Download a binary from the [releases](https://github.com/vapour-computer/vapour-cli/releases) page, or build it:

```sh
go install github.com/vapour-computer/vapour-cli@latest
```

## Use

```sh
vapour login --token vpr_...          # create a token on your Vapour dashboard
vapour create "My Game" --engine godot
cd my-game
# export your game for Web into build/web/ (needs an index.html)
vapour publish
```

`vapour create --from <git-url>` clones a sample game instead of scaffolding. `vapour publish --dry-run` checks and
zips your build without uploading. In CI, set `VAPOUR_TOKEN` instead of running `login`.

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

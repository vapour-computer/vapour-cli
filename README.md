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
cover = "cover.png" # optional
```

Credentials live in `~/.config/vapour/credentials` (mode 0600). Set `--api` or `VAPOUR_API` to point at another server.

## Develop

```sh
go test ./...
```

MIT licensed.

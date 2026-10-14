# `vapour.toml` (`internal/project/manifest.go`)

```toml
[project]
id = ""            # filled in on registration
title = "My Game"  # required
slug = "my-game"
engine = "godot"   # required: godot | unity | bevy | other
version = "0.1.0"  # required, ^[0-9A-Za-z][0-9A-Za-z.+_-]*$

[build]
dir = "build/web"            # required: web export folder or a .zip
cover = ""                   # legacy cover image; prefer media.cover in store/page.json
command = "auto"             # auto | custom | none  (see build.md)
args = []                    # argv for command = "custom"
boot = []                    # globs of files needed to start the game
split_warn_mib = 1024        # warn about files above this size

[store]                      # all optional, relative to the game folder
page = "store/page.json"
content = "store/template.md"
notes = ""                   # release notes (Markdown) for this version
```

`Manifest.Validate()` (used by `publish`, `test`, `build` paths) checks: title non-empty, engine in `Engines`,
version matches the regex, `build.dir` non-empty. `Load` errors with a hint if no `vapour.toml` exists.
`Save` rewrites the file (used to store the project id), so comments are not preserved.

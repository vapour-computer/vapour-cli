# `vapour.toml` (`internal/project/manifest.go`)

```toml
[project]
id = ""            # filled in on registration
title = "My Game"  # required
slug = "my-game"
engine = "godot"   # required: godot | unity | bevy | other
platform = "desktop" # optional: desktop | native-mobile | port
version = "0.1.0"  # required, ^[0-9A-Za-z][0-9A-Za-z.+_-]*$

[build]
dir = "build/web"            # required: web export folder or a .zip
cover = ""                   # legacy cover image; prefer media.cover in store/page.json
command = "auto"             # auto | custom | none  (see build.md)
args = []                     # argv for command = "custom"
boot = []                     # globs of files needed to start the game
split_warn_mib = 1024         # warn about files above this size
pointer_lock = "auto"        # auto | pause | off

[runtime]
threads = false
webgpu = false
coep = "require-corp"        # require-corp | credentialless
entry = "index.html"         # relative safe path only
on_quit = "store"            # store | home | reload | stay

[mobile]
orientation = "any"          # any | landscape | portrait
render_scale = 1.0            # 0.25..1.0
max_dpr = 2.0                 # > 0
rotate_message = ""
rotate_message_file = ""      # set only one rotate_message source

[controller]
mode = "keyboard-map"        # keyboard-map | dedicated (alias action-map)

[store]                       # all optional, relative to the game folder
page = "store/page.json"
content = "store/template.md"
notes = ""                   # ignored by publish; use --notes/--notes-file instead
```

`Manifest.Validate()` (used by `publish`, `test`, `build`) enforces:
- required project title/engine/version/build.dir
- platform enum validation
- controller schema validation
- pointer lock enum (`auto|pause|off`)
- runtime (`coep`, `entry`, `on_quit`)
- mobile (`orientation`, `render_scale`, `max_dpr`)

`Load` errors with a hint if no `vapour.toml` exists.
`Save` rewrites the file (used to store project id), so comments are not preserved.

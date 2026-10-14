# Engine exports

`vapour build --dir .` reads `vapour.toml` and invokes installed tools. It never
installs an editor, export template, Rust target, or license. Build output must
contain `index.html`. `.vapour/build.json` records file sizes and boot flags for
future chunk-publish integration; current ZIP publish does not consume it.

```toml
[build]
command = "auto" # auto (default), custom, none
dir = "build/web"
split_warn_mib = 1024
# boot = ["index.html", "*.js", "*.wasm", "*.pck"]
# command = "custom"
# args = ["my-exporter", "--web"] # executable/argv, never a shell string
```

Godot 4: set `GODOT` or put `godot` on PATH. Supply a preset named `Web` on the
Web platform and matching templates. The editor checks templates during export.
Import runs first; exporter failure stops the build. Use Compatibility for Web.
HTML, JavaScript, WASM and PCK files form the default boot set.

Unity: set `UNITY_EDITOR` or put `Unity` on PATH. `vapour create --engine unity`
includes `Assets/Editor/VapourBuild.cs`; existing projects can copy that script.
Enable scenes in Build Settings and install WebGL support. The batch invocation
calls `Vapour.Build.Perform` (also available as the `Vapour/Build` editor menu).
Addressables absence produces a warning, not automatic configuration or bundle
building. CI licensing is the developer's responsibility. HTML, JS, WASM, data
and precompressed files are in the boot set.

Bevy: install the wasm32-unknown-unknown Rust target and a wasm-bindgen CLI
matching the project's wasm-bindgen dependency. A single binary root package is
required; use custom mode for workspaces. Runs Cargo release export, wasm-bindgen
web glue and optional wasm-opt, writes index.html and copies assets/. Assets are
lazy in boot metadata (the engine may still request them immediately).

Boot overrides use Go filepath.Match globs on slash-relative paths; `*` does not
cross directories and `**` is not a recursive wildcard. Add nested globs explicitly.
Large-file warnings link engine split-pack guides. A 50 GiB delivery quota does
not remove WASM address-space or engine whole-file loading limits.

Tests run real subprocesses against fixture executables for all three adapters,
plus metadata, warnings and failure paths. These do not certify actual Unity or
Bevy builds; exporter/target availability must be reported separately.

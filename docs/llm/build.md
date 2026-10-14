# `vapour build` (`cmd/build.go`, `internal/enginebuild`)

Exports a Godot 4, Unity or Bevy project to a web build in `build.dir`, using tools already installed. It never
installs editors, templates, Rust targets or licenses. Output must contain `index.html`. Flag: `--dir`.

`build.command`: `auto` (default; by `project.engine`), `custom` (run `build.args` as argv, never a shell string),
`none`. It also writes `.vapour/build.json` (file sizes + boot flags) and warns about files over `split_warn_mib`.

- **Godot**: `GODOT` env or `godot` on PATH; needs an export preset named `Web`. Import runs first. Use Compatibility renderer.
- **Unity**: `UNITY_EDITOR` or `Unity` on PATH; calls `Vapour.Build.Perform` from `Assets/Editor/VapourBuild.cs`
  (written by `vapour create --engine unity`). WebGL support module required.
- **Bevy**: needs `wasm32-unknown-unknown` target and a matching wasm-bindgen CLI; single binary package
  (use `custom` for workspaces). Runs cargo release, wasm-bindgen, optional wasm-opt, writes `index.html`, copies `assets/`.

`boot` globs use Go `filepath.Match` on slash-relative paths; `*` does not cross directories, `**` is not special.
Details: `internal/enginebuild/README.md`.

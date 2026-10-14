# `vapour create <name>` / `vapour template <engine> <name>` (`cmd/create.go`, `cmd/template.go`, `internal/scaffold`)

Creates `./<slug>/` where slug = `project.Slugify(name)` (lowercase `[a-z0-9-]`, max 48 chars, "game" if empty).
Fails if the folder exists and is non-empty.

`create` flags: `--engine godot|unity|bevy|other` (default godot), `--from <git-url>` (shallow clone, `.git` removed,
store files still added), `--offline` (skip server registration; `publish`/`store push` register it later and write the id).

`template` command is a thin wrapper around `create`: it maps `engine` to a GitHub template repo URL and then uses
the same project initialization path. Default org is `vapour-computer`; override with `--org`.

Repo mapping:
- `bevy` → `https://github.com/<org>/vapour-template-bevy.git`
- `godot` → `https://github.com/<org>/vapour-template-godot.git`
- `unity` → `https://github.com/<org>/vapour-template-unity.git`

Order: validate → register project with the API (unless `--offline`) → write files → save `vapour.toml`
(version `0.1.0`, `build.dir = "build/web"`, `store.page = store/page.json`, `store.content = store/template.md`).
Registering first means nothing is left on disk if the server refuses.

Templates: `internal/scaffold/templates/<engine>/` (README + engine starter files) and the shared `_store/store/`
(`page.json`, `page.schema.json`, `template.md.tmpl`, README).

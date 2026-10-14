# Store page: `vapour store check|push` (`cmd/store.go`, `internal/storepage`, `internal/media`)

- `store check [--dir]`: validates everything locally, prints sizes and warnings. No upload.
- `store push [--dir]`: registers the project if it has no id, then uploads the store page without publishing a build.
- No `[store]`/`build.cover` configured → "No store page configured".

## Files
`store/template.md` (Markdown description, max 20000 chars; `## ` headings define anchors; text before the first is `intro`)
and `store/page.json` (unknown keys are an error; schema at `store/page.schema.json`).

page.json: `version` (must be 1); `game.tagline` (≤140), `game.category` (one of `storepage.Categories`:
action adventure arcade casual horror platformer puzzle racing retro rpg shooter simulation sports strategy other),
`game.tags` (≤5, each 1-24 chars of `[a-z0-9 -]`); `media.banner|cover|screenshots[≤8]|video` (paths relative to page.json);
`sections` (1-12; types `media`, `markdown` (needs unique `anchor` `[a-z0-9-]{1,40}`, optional `title` ≤60), `release_notes`
(`limit` 1-20), `details`, `links`; all but markdown at most once); `details` (≤20; label 1-40, value 1-200);
`links` (≤6; https URL ≤300 chars). Sent layout JSON must be ≤16 KiB. Release notes ≤5000 chars.

## Media (`internal/media`, type detected from bytes, not extension)
Banner: png/jpeg/webp ≤4 MiB, 960x300 to 8192x4096, aspect ≥2:1. Cover: ≤2 MiB, 460x215 to 4096². Screenshot: ≤4 MiB,
640x360 to 8192². Video: mp4/webm ≤30 MiB. All store media in one request must stay under 30 MiB (API limit 32).
`PutStore` is a multipart request: `metadata` fields plus `banner`, `cover`, `screenshot`, `video` parts.

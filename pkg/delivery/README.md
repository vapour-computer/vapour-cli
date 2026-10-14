# Experimental delivery v1

Opt in with `vapour publish --experimental-chunks`. Default publishing remains ZIP.
The local POC implements this protocol; the deployed API does not yet implement it.
Identity encoding only: zstd negotiation/decoder integration is planned.

If present, `.vapour/build.json` from the separate engine-build feature supplies
boot flags, with file-size freshness checks. Otherwise defaults cover Godot and
Unity startup files while Bevy assets stay lazy. Engine-build is optional for
compilation and existing pre-exported folders remain usable.

SHA-256 identifies raw chunk bytes. Manifests contain `schema: 1`, `version`, and
`files` with safe relative `path`, `size`, `boot`, and ordered `{hash,size}` chunks.
POST `/projects/{id}/uploads` returns `{id,missing:[{hash,url,offset}]}`. PUT
remaining bytes to each URL using `Upload-Offset`. Rerunning publish negotiates
remaining offsets. POST `/projects/{id}/uploads/{id}/finalize` commits the manifest.
Four uploads run in parallel; API bearer tokens never accompany object PUTs.

Limits: 50 GiB logical game size, 4 GiB/file, 8 MiB maximum chunk. Unique chunks
are spooled to temporary disk; provision enough disk for the export. This is
not yet a production GCS resumable-session client (that needs Content-Range
negotiation), automatic retry, zstd, quota prompts or garbage collection.

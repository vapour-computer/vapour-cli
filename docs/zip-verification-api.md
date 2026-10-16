# Zip verification and version approval: server contract

The CLI (`vapour scan`, `vapour verify`, `vapour publish`) implements the client side. This is what the Vapour API
(`vapour-infra`) does with it. **The CLI scan is a convenience for honest developers; anyone can bypass a local tool.
The server is the guard, so it never trusts the attestation's scan result.**

## 1. Publish upload

`POST /projects/{id}/publish` (multipart) accepts an optional part:

| Part | Type | Content |
| --- | --- | --- |
| `attestation` | `application/json`, at most 64 KiB | The attestation below, produced by the CLI |

The upload is **accepted at once**. Verification then runs in the background (section 2). A malformed `attestation`
part (not JSON, unknown fields, over 64 KiB, repeated) fails immediately with HTTP 400 `invalid_upload`. A request
without the part is accepted and ends up `unverified` (older CLIs).

### Attestation (version 1)

```json
{
  "version": 1,
  "sha256": "9d1e…64 hex chars…",
  "size": 12345678,
  "entries": 143,
  "scanner": "vapour-zipscan/1",
  "scan": { "result": "pass", "findings": [] },
  "signed_at": "2026-10-16T12:00:00Z",
  "key_id": "6f0b1c2d3e4f5a6b",
  "public_key": "<base64 ed25519 public key, 32 bytes>",
  "signature": "<base64 ed25519 signature, 64 bytes>"
}
```

**Signed bytes:** the ASCII string `vapour-zip-attestation-v1\n` followed by the canonical JSON of the attestation with the
`signature` field removed. Canonical JSON: object keys sorted by Unicode code point at every level, no whitespace,
UTF-8, no HTML escaping (`<`, `>` and `&` are written as is), U+2028 and U+2029 written as ` ` and ` `,
integers without a fraction. `key_id` is the first 16 hex characters of the SHA-256 of the raw 32-byte public key.

## 2. Verification (asynchronous)

On upload the server stores the zip, stores the attestation verbatim, computes the SHA-256 of the bytes it received,
writes a `pending` verification record and publishes a job to a queue (in-process by default, Google Cloud Pub/Sub by
configuration). A worker then:

1. recomputes the hash and size of the stored zip and runs the server's own scan (same finding codes as the CLI);
2. if an attestation exists: parses it, checks `key_id`, verifies the ed25519 signature, and compares `sha256` and `size`;
3. applies trust on first use to the signing key: the first valid key for a developer is remembered, a different key
   later is rejected (`unknown_key`) until the platform team resets it.

Result per version:

| Status | When |
| --- | --- |
| `pending` | queued or running |
| `verified` | attestation valid, hash and size match, server scan has no error finding |
| `rejected` | any check failed, or the server scan found an error finding (even with no attestation) |
| `unverified` | no attestation, and the server scan passed |

A `rejected` version is taken offline automatically (play files removed, version never approvable) and the developer
must publish a new version number. `reason` is one of `bad_attestation`, `unsupported_version`, `bad_key`, `bad_key_id`,
`bad_signature`, `hash_mismatch`, `size_mismatch`, `unknown_key`, or a scan code (`zip_bomb_ratio`, `native_executable`, …).

Server limits: 5,000 files, 1 GiB unpacked in total, 512 MiB for one file, 512 MiB for the zip.

## 3. Reading the result

`GET /projects/{id}/versions/{version}/verification` (owner only):

```json
{ "status": "verified", "sha256": "…", "size": 12345678, "key_id": "6f0b1c2d3e4f5a6b",
  "verified_at": "2026-10-16T12:00:05Z", "reason": null, "findings": [] }
```

`vapour verify --remote` compares `sha256` with the attestation and requires `status == "verified"`.
`GET /projects` also returns `zip_verification` (`status`, `sha256`, `key_id`, `verified_at`) and
`latest_release_review` (`version`, `status`, `note`) for the newest version of each game.

## 4. Admin approval

Every version of every game must be approved by an admin before the public can see or play it. Versions published
before this rule are grandfathered as approved. A new version never replaces the live version until approved.

- Admin = a Firebase session (never a personal access token) whose verified email is in `ADMIN_EMAILS`.
- `GET /admin/versions?status=pending|approved|rejected|all` lists every release of every game with its review and
  verification.
- `POST /admin/games/{game_id}/versions/{version}/approve` (`note`, `accept_unverified`) and
  `POST /admin/games/{game_id}/versions/{version}/reject` (`note`, required).
- A version can only be approved when its verification is `verified`, or `unverified` with `accept_unverified: true`.
  `pending` returns 409 `verification_not_ready`, `rejected` returns 409 `verification_rejected`.

## 5. Interoperability test

Generate an attestation with `vapour publish --dry-run`, read `.vapour/build.zip.attestation.json`, rebuild the signed
bytes from the rules above and verify with the embedded public key. The CLI repo keeps a fixture of this at
`internal/zipsign/testdata/`.

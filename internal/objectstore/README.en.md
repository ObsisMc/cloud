# internal/objectstore: Revision object-store client

[English](README.en.md) | [中文](README.md)

`internal/objectstore` is cloud's own S3-compatible object-store client, and it serves the Revision
path alone: it issues the **single-key, single-method, time-limited** upload grant for one delivery
attempt (Cloud Revision D2/D3) and verifies an uploaded object's **existence, size and SHA-256** by
HEAD before Cloud registers a Revision (D4 step 2). It decides no business semantics — who gets a
grant, when, and what a failed verification means all belong to `internal/core`; this package gets
signing and verification right.

Signing is a self-contained AWS Signature Version 4 (SigV4) implementation rather than a new
dependency: the delivery path needs exactly two operations (presign one PUT, sign one HEAD), and the
repository prefers the standard library over a new module for that.

## Files

- `objectstore.go`: `New`/`Config` validation (endpoint, region, bucket, path style, credential
  files), `PresignPut` (returns a `core.UploadGrant`), `Verify` (HEAD + size + decoded
  `x-amz-checksum-sha256`), the object-key and bucket-name injection boundaries, and credential-file
  reading (size cap, whitespace trimming).
- `sigv4.go`: the canonical-request construction and signing — the presigned PUT signs `host` alone
  (payload `UNSIGNED-PAYLOAD`), the HEAD signs
  `host;x-amz-checksum-mode;x-amz-content-sha256;x-amz-date` (empty-body payload digest); per-segment
  URI encoding that keeps `/`, query parameters sorted by name, and digest normalization across hex
  case.
- `objectstore_test.go`: the offline unit tests described below.

## Dependencies and callers

- Depends on: the standard library (`net/http`, `net/url`, `crypto/hmac`, `crypto/sha256`,
  `encoding/base64`, `encoding/hex`) and `internal/core` — **only** for the `core.UploadGrant` value
  type.
- Called by: `cmd/server` (constructs the client and installs it as
  `store.RevisionObjects`/`store.RevisionUploadTTL` when the `object_store` section is configured);
  `internal/core` through the `RevisionObjects` interface (`PresignPut`/`Verify`) it declares for
  itself.
- Direction: the edge is one-way, `internal/objectstore → internal/core`; `internal/core` does **not**
  import this package. The interface is declared at the consuming boundary, as the repository
  requires, so there is no cycle.
- Credentials enter configuration as **file paths only** (`access_key_id_file`,
  `secret_access_key_file`); once read, the values live only inside this process — never persisted,
  logged, or returned in a grant. The `credentials` struct has no `String` method and this package
  never formats it.

## Invariants

- **A grant is a capability**: one object key, one method, one lifetime. What the URL does not name,
  the Node cannot do. The signature covers `host` alone, so the grant binds no request header — the
  uploader's own `x-amz-checksum-sha256` is enforced by the store's checksum validation and,
  independently, by `Verify`.
- **Nothing from the wire is trusted**: `Verify`'s `size`/`sha256` come from the Node's report, so
  both are shape-checked before any request is made. A missing object, a differing digest and an
  unreachable endpoint all fail, and none is distinguished — for the delivery they are one thing
  (D1: the delivery fails and D5 retries it).
- **No checksum is a failure**: the HEAD carries `x-amz-checksum-mode: ENABLED`. A store that returns
  no digest — one that never received a checksum and so cannot vouch for the content — fails the
  verification instead of passing it on existence alone.
- **Startup validation is separate from reachability**: a malformed endpoint, an invalid bucket name
  or an unreadable credential file fails `New` and refuses to start; an unreachable endpoint or a
  not-yet-created bucket does **not** (D1) — it surfaces as a failed verification, which D5's retry
  and give-up window already handle.
- **The grant lifetime is bounded**: a non-positive TTL is refused, and a TTL above S3's own ceiling
  (seven days) is clamped before signing, so a configuration error cannot masquerade as an
  unexplained 403 at the Node.
- **Credentials do not leak**: `PresignPut` returns a URL and an object key and no credential; the
  tests assert that neither the URL nor the `Authorization` header contains the secret.

## Known limitations

- **A presigned PUT cannot sign the checksum header.** D2's grant is issued *before* the Node computes
  the digest, while real AWS S3 requires every `x-amz-*` header to be signed, so this URL may be
  refused by AWS S3; MinIO is lenient about it. The implementation follows the approved text and says
  so inline in `sigv4.go`. Fixing it means changing when the grant is issued or moving to a POST
  policy — an ADR change, not a local one.
- **Not exercised against a real S3/MinIO.** The MinIO image pull failed during this slice
  (`docker.io` connection reset), so the package has deterministic `httptest`-double tests only: they
  prove the shape, the binding and the failure judgements of the requests, not that a real store
  accepts the signatures. The ADR's local-MinIO integration test is still owed, and the evidence rows
  record that honestly as Partial/Missing rather than letting a unit test stand in for it.

## Tests

- Unit tests are fully offline and deterministic: a stopped clock, real temporary credential files
  and an `httptest` S3 double. They cover the grant's shape (path, method, lifetime, query
  parameters, signature form), the signature's binding to the object key and its reproducibility, both
  edges of the TTL, the object-key injection boundary, `public_endpoint` applying to grants while the
  HEAD uses `endpoint`, the ten `Verify` verdict branches (including no checksum, a non-base64
  checksum and a wrong-length one), an unreachable endpoint, fourteen `New` configuration errors, and
  the credential-file trimming rule.
- The interaction with PostgreSQL and the delivery state machine lives in
  `integration/agent_run_delivery_test.go`.

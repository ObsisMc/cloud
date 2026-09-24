# Observations on existing Cloud code

Recorded per the migration rule "do not self-refactor Cloud". Nothing here is changed by this migration; it is
left as-is, and any improvement is out of scope.

1. **`position` needs a float helper.** `core.Object` offers `S/N/B/O` but no `float64` accessor
   ([store.go:20-53](../../internal/core/store.go#L20-L53)). `double precision` columns decode via
   `row_to_json` as Go `float64`. The issue code therefore does its own `position` read/write (a small local
   helper in `issues.go`) rather than adding an accessor to the shared `Object` type — keeping the change additive.

2. **`updated_at` is not universal.** Only `operations` carries `updated_at` today
   ([0001_core.sql:78](../../internal/core/migrations/0001_core.sql#L78)). `issues` adds its own `updated_at`
   maintained explicitly in the `UPDATE` statements; there is no global trigger and none is added.

3. **`inputSchema("status")` is hard-coded to "active/disabled".** It serves tenant-membership status. The issue
   routes reuse the word `status` with a different enum, so the OpenAPI generator must disambiguate on the path —
   a one-line branch, not a fix to the shared mapping.

4. **`readPublic`'s default list filters by `owner_user_id`.** Cloud resources are single-owner; the board is
   intentionally tenant-shared, so the issue list bypasses the owner filter. This is the first shared (non-owner-
   scoped) public resource in Cloud; the composite creator FK still enforces that the creator is a member of record.

5. **Idempotency is mandatory for POST/DELETE.** Carried over verbatim to create/move/delete. A board UI (or the
   demo) must mint a fresh opaque key per write; this differs from Multica and is called out in
   [08-differences.md](08-differences.md) rather than "fixed" in the core.

6. **`optionalField("version")` only marks version optional on `PUT`.** Move is a `POST` that also carries `version`.
   Leaving the existing rule untouched makes the OpenAPI body mark move's `version` required — defensible since a
   move always needs the current version for optimistic concurrency; absence still yields `428` (a documented
   precondition), not a 400.

7. **`camel()` lowercases the first letter and uppercases the rest after `_`** (e.g. `min` stays `min`, `baseCommitId`)
   ([store.go:135](../../internal/core/store.go#L135)). The `SELECT min(position)` aggregate projects to the key `min`;
   issue code reads `top["min"]`, not a made-up alias.

8. **Single advisory lock for everything** (`pg_advisory_xact_lock(67420911)`, [store.go:177](../../internal/core/store.go#L177)).
   Issues join this lock; appropriate for phase one and requires no special handling.

9. **`version(o,v)` demands `v>0` (428 otherwise), `0` is reserved for "new"** (see `putMember`
   [public.go:160](../../internal/core/public.go#L160)). Issues follow the same convention: updates/moves/deletes
   require `version >= 1`; create never sends a version.

10. **No `deleted_at`-aware ordering is standardized**; every query spells `deleted_at IS NULL` explicitly. Issue
    queries follow that spelling.

11. **The issue dispatch case swallows every nested path.** `public.go`'s `case r.IssueID != "" || strings.HasSuffix(r.Path, "/issues")`
    matches all `/issues/:iid/…` sub-routes, so comment/label/subscriber writes must be dispatched *inside* that case
    (by `strings.Contains(r.Path, "/comments")` etc.), not as sibling cases. Symmetrically, in `readPublic` the
    nested GETs (`/comments`, `/subscribers`, `/labels` with `IssueID != ""`) must be listed **before** the
    `case r.IssueID != ""` fallthrough. This is a fragility of suffix/contains-based dispatch, not a real router.

12. **OpenAPI's `strings.Contains(r.Path, "/issues")` is a second trap.** The generator classifies any path
    containing `/issues` as the Issue resource; all new sub-resource schema branches had to be inserted **before**
    that `Contains` check (and before the hard-coded 7-value `status` enum in `inputSchema`), or
    `/issues/:iid/comments` would be documented as an Issue.

13. **`validField` is a closed type system.** Non-string JSON body fields 400 by default; `ids` (array of strings),
    `filter`/`properties` (object), and `position` (integer) each needed a new `case`, and `status` — previously
    defaulting to the string branch — is unaffected but the enum moved out of the DB into `resolveStatus`.

14. **The contract test enforces `additionalProperties:false`.** `integration/contract_test.go` validates every
    response against the published schemas, so adding `number`/`properties`/`labels` to `Issue` (and the four new
    resources) was not optional — a missing schema field fails the whole suite with an "OpenAPI response mismatch",
    caught only at integration time.

15. **Gin (httprouter) static segments beat `:param`.** `/issues/batch` needs no special-casing: the literal
    `batch` segment is matched before `:iid`, so `c.Param("iid")` is simply empty for the batch route. Verified with
    a single request rather than assumed.

16. **Migrations can't seed runtime entities.** Tenants are created at runtime, so the canonical status columns are
    seeded lazily (on first read/write of a tenant's statuses) via `ON CONFLICT DO NOTHING`, not in `0006`. The seed
    positions (`0..6`) deliberately reproduce wave 1's hardcoded `CASE` order so the board stays identical.
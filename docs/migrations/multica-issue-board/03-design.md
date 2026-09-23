# Issue Board Migration — Design (minimal-invasive)

## 1. Domain mapping (Multica → Ora Cloud)

| Multica concept | Ora Cloud mapping | Decision |
| --- | --- | --- |
| `workspace` (org with members) | `tenant` (org with `tenant_memberships`) | Reuse; board scope = `tenant_id`. |
| `member` / `user` (creator, assignee) | `user` (`users.id`) | Reuse. Creator = authenticated user from `identity()`; assignee = a `users.id`. |
| `project_id` (lightweight issue grouping) | *(none)* | **Do not bind.** Cloud `project` is a dev-environment repository with a provisioning lifecycle — different semantics. Issues are tenant-only for the MVP (documented in [00-overview.md](00-overview.md)). |
| `issue` | new `issues` table + `core` functions | Re-implement (no copy). |
| `status` text key + catalog table (`issue_status`) | `issue_statuses` table (key + name + category + color + icon + `position` + `is_system`), `issues.status` = text key matching a format pattern | Wave 2 adds a simplified catalog (see §3). |
| `priority` (`urgent/high/medium/low/none`) | `text CHECK` of 5 values | Identical. |
| `position float64` (lexorank within `workspace+status`) | `double precision`, ordered within `(tenant_id, status)` | Identical technique. |
| `revision bigint` (`expected_revision`) | `version bigint` (+ `version(o,v)` precondition) | Reuse Cloud's optimistic-concurrency convention (428/409). |
| `number` (per-workspace seq) | `number bigint` (per-tenant `max(number)+1`) | Wave 2; see §6.1. |
| `metadata` / custom properties | `properties jsonb NOT NULL DEFAULT '{}'` | Wave 2; arbitrary KV object. |
| sqlc queries / services / handlers | raw SQL inside `core.transact` | Follow Cloud's "one short advisory-locked transaction" style. |
| WebSocket realtime | *(none)* | Deferred; HTTP refetch. |

## 2. Scope decision: shared board, simple creator

- An issue is **visible to every active member of the tenant** (matching Multica's workspace-shared board). Reads
  filter `tenant_id` only, **not** `owner_user_id`.
- `creator_user_id` = the authenticated user (from `identity()`), stored with a structural FK to
  `tenant_memberships(tenant_id,user_id)` so a non-member creator cannot exist.
- `assignee_user_id` = an optional `users.id`; must be an active user (tenant memberships are not required to be an
  assignee — Multica allows assigning any member; we validate "active user exists" and defer membership hardening).

## 3. Status model

Wave 1 used a fixed 7-value `CHECK`. Wave 2 replaces it with a **tenant status catalog**
(`issue_statuses`), seeded lazily with the same 7 canonical columns so the board is identical on
first read:

```
backlog(0) → todo(1) → in_progress(2) → in_review(3) → blocked(4) → done(5) → cancelled(6)
```

- `issue_statuses` columns: `key` (unique per tenant), `name`, `description`, `category`
  (`unstarted`/`started`/`done`/`closed`), `color`, `icon`, `is_system`, `position` (column order).
- `issues.status` is now a free-format text key matching `^[a-z0-9][a-z0-9_]{0,31}$` (the 0005
  `CHECK(status IN (…))` was dropped in favour of a format check, so custom keys are allowed).
- **Lazy seeding**: the first read/write of a tenant's statuses inserts the 7 canonical rows via
  `ON CONFLICT(tenant_id,key) DO NOTHING` (positions `0..6`). This cannot be done in the migration
  because tenants are created at runtime.
- **Custom columns** are appended after the highest `position`; `is_system=false`.
- **Archival** is a soft delete; system columns refuse archival (`409 system_status_required`), and
  an archived key can no longer be set on an issue (`resolveStatus` → 400 `invalid_status`).
- Board ordering now uses `LEFT JOIN issue_statuses … ORDER BY COALESCE(s.position,1000), position,
  id` — the canonical seed positions reproduce the old `CASE` order exactly; unknown/archived
  statuses sort last.
- Priority: `urgent/high/medium/low/none`, default `none`.

## 4. Ordering / move algorithm (re-implemented from `issue_move.go` + `issueposition.go`)

Position is a `double precision`, ascending within one `(tenant_id, status)` column.

- **New / status-changed-with-no-anchor** → top of column = `MIN(position)-1` (empty column → `0`).
- **Move with anchors** (`beforeId`, `afterId`): both keys must be **present** (empty string = none).
  1. An anchor is resolved to its `position` within the **tenant** (no status filter), matching Multica's
     workspace-wide resolution; nonexistent/foreign anchor → 404.
  2. `before && after` → `before + (after-before)/2`; require `after > before` and the midpoint to be strictly
     between (float precision exhausted → 409 `position_conflict`).
  3. `before` only → `before + 1`.
  4. `after` only → `after - 1`.
  5. neither → keep the current `position` (unless a `status` change re-ranks to the top of the new column).
- A `status` move without anchors re-ranks to the top of the destination column (a deliberate, safe clarification —
  see [08-differences.md](08-differences.md)).

## 5. API design (minimal board surface)

Paths (all public, tenant-scoped; `:iid` is the issue id):

| Method | Path | Fields | Response @status |
| --- | --- | --- | --- |
| GET | `/api/v1/tenants/:tid/issues` | — | `{items:[Issue…], nextCursor:""}` @200 |
| POST | `/api/v1/tenants/:tid/issues` | `title`(req), `description`, `status`, `priority`, `assigneeUserId`, `parentIssueId` | `{resource:Issue}` @200 |
| GET | `/api/v1/tenants/:tid/issues/:iid` | — | `Issue` @200 |
| PUT | `/api/v1/tenants/:tid/issues/:iid` | `title`, `description`, `status`, `priority`, `assigneeUserId`, `parentIssueId`, `version`(428 precondition) | `Issue` @200 |
| POST | `/api/v1/tenants/:tid/issues/:iid/move` | `status`, `beforeId`, `afterId`, `version`(428 precondition) | `Issue` @200 |
| DELETE | `/api/v1/tenants/:tid/issues/:iid` | `version`(428 precondition) | `Issue` @200 (soft-deleted) |

Semantics:

- **Create** defaults `status=todo`, `priority=none`; validates title (non-empty ≤200), description (≤20000, may be
  empty), status/priority enums, assignee (active user) and parent (existing non-deleted issue in same tenant).
  New issue is placed at the top of its column.
- **Update** is a partial update: a field present in the body overwrites; `title` must remain non-empty; `status`
  change without anchors re-ranks to the top of the new column; `assigneeUserId`/`parentIssueId` present-but-empty
  clears the column to `NULL`. `version` precondition required (428 if absent, 409 on mismatch).
- **Move** changes `status` and/or `position` via anchors/column-top; `version` precondition required.
- **Delete** soft-deletes (`deleted_at=now()`), removing it from the board; `version` precondition required.
- **Idempotency**: Cloud requires `Idempotency-Key` on POST/DELETE ([public.go:38](../..//internal/core/public.go#L38)).
  Issue create/move (POST) and delete (DELETE) inherit this; update (PUT) is version-guarded instead. This is a
  deliberate difference from Multica — see [08-differences.md](08-differences.md).

Error codes (Cloud `*Fault`): `invalid_input` / `invalid_status` / `invalid_priority` (400),
`assignee_not_found` / `parent_not_found` / `not_found` (404), `version_required` (428), `version_conflict` /
`position_conflict` / `idempotency_conflict` (409).

## 6. Wave-2 feature designs

### 6.1 Number + custom fields (`properties`)

- `number bigint` is a per-tenant sequence assigned at create via `COALESCE(MAX(number),0)+1`,
  computed inside the same advisory-locked transaction as the insert, so it is race-free. Soft
  deletes keep the row, so numbers never reuse/gap.
- `UNIQUE(tenant_id, number)` backstops the in-app computation.
- `properties jsonb NOT NULL DEFAULT '{}'` is an untyped KV store: the API accepts any JSON object
  (`validField("properties")` requires a map) and stores it verbatim. No schema validation —
  matches "custom fields" without committing to a typed field system.

### 6.2 Comments

- `issue_comments` rows are tenant + issue scoped; `author_user_id` is the authenticated user,
  structurally bound to `tenant_memberships(tenant_id, author_user_id)` so a comment author is
  always a member of record.
- `body` is `CHECK(length(body) BETWEEN 1 AND 20000)`; create/update trim and re-check.
- Comments soft-delete; `version` precondition on update/delete.

### 6.3 Labels

- Tenant-scoped `labels` with a partial unique index `UNIQUE(tenant_id,name) WHERE deleted_at IS
  NULL` — so a soft-deleted label's name can be reused.
- `issue_labels` is a plain join table `(issue_id,label_id)` PK; attach is `ON CONFLICT DO NOTHING`
  (idempotent), detach deletes the join row.
- The `Issue` wire object **embeds** `labels` as an array (`attachLabels`), so the board list carries
  each card's labels via one grouped query per list (no per-issue N+1).

### 6.4 Subscribers

- `issue_subscribers(issue_id,user_id,tenant_id)`; `user_id` must be an active tenant member
  (composite FK + behavioural `subscriberUser` check).
- Subscribe/unsubscribe both return the user and are idempotent (`ON CONFLICT DO NOTHING` / delete).

### 6.5 Search (`?q=`)

- `GET /issues?q=term` filters `title ILIKE $n OR description ILIKE $n`; the term is escaped
  (`\`, `%`, `_`) so user input is matched literally, then wrapped in `%…%`.
- Applies to the board list and to grouped views (both call `issueListItems`).

### 6.6 Batch update

- `POST /issues/batch` body `{ids:[uuid…] (1..100), status?, priority?, assigneeUserId?}` applies one
  patch to every listed issue atomically (single advisory-locked transaction).
- Unlike single-update, no per-issue `version` is required — the batch is a bulk administrative
  action; a deliberate simplification (see [08-differences.md](08-differences.md)).

### 6.7 Grouped views

- `GET /issue-groups?by=status|priority|assigneeUserId` → `{groups:[{key, items:[Issue…]}]}`.
- Bucketing is done in Go over the same ordered `issueListItems` result; unassigned buckets to the
  key `unassigned`.

### 6.8 Saved views

- `issue_views(tenant_id, owner_user_id, name, filter jsonb)`; owner-scoped (a user only sees their
  own), `filter` is a validated JSON object (arbitrary KV; matching is not applied server-side yet —
  it is stored for the client to interpret).

## 7. Expected NEW files

| File | Purpose |
| --- | --- |
| `internal/core/migrations/0005_issues.sql` | `issues` table + indexes (see [04-database.md](04-database.md)). |
| `internal/core/migrations/0006_issue_extensions.sql` | Wave-2 tables + `issues` ALTERs (see [04-database.md](04-database.md)). |
| `internal/core/issues.go` | `issue`, `issueList`, `issueListItems`, `likePattern`, `createIssue`, `updateIssue`, `moveIssue`, `deleteIssue`, `batchUpdate`, `issueGroups`, position/status helpers. |
| `internal/core/issue_statuses.go` | Status catalog: `seedIssueStatuses`, `resolveStatus`, `statusCatalogList`, `createIssueStatus`, `updateIssueStatus`, `deleteIssueStatus`. |
| `internal/core/issue_comments.go` | `commentList`, `createComment`, `updateComment`, `deleteComment`. |
| `internal/core/issue_labels.go` | `labelList`, `createLabel`, `updateLabel`, `deleteLabel`, `attachLabels`, `issueLabelList`, `attachLabel`, `detachLabel`. |
| `internal/core/issue_subscribers.go` | `subscriberList`, `subscribe`, `unsubscribe`. |
| `internal/core/issue_views.go` | `viewList`, `createView`, `updateView`, `deleteView`. |
| `integration/issues_test.go` | Wave-1 end-to-end CRUD + move + persistence. |
| `integration/issue_extensions_test.go` | Wave-2 end-to-end tests (catalog/comments/labels/numbers/properties/search/batch/views/groups/subscribers). |
| `cmd/demo-issue-board-web/` | Browser Kanban UI on the real HTTP API (wave-2 aware: numbers, labels, comments, search, groups, custom columns, subscribe). |
| `docs/migrations/multica-issue-board/*.md` | This analysis/design/testing set + `FINAL.md` + `10-extensions.md`. |

## 8. Expected MODIFIED files (minimal, additive)

| File | Change | Why it cannot be a new file |
| --- | --- | --- |
| `internal/api/router/router.go` | Add the 30 issue routes; pass `IssueID`/`CommentID`/`LabelID`/`StatusID`/`ViewID`/`q`/`by` into `PublicRequest`; extend `validField` (`ids` array, `filter`/`properties` object, `position` integer). | The route allowlist and generic handler are central; there is no plugin hook for new paths. |
| `internal/core/public.go` | Add `CommentID`/`LabelID`/`StatusID`/`ViewID`/`Query`/`GroupBy` to `PublicRequest`; add GET + write dispatch cases (including the nested `/comments`/`/labels`/`/subscribers` sub-dispatch inside the issue case). | `Public` is the single public dispatch point. |
| `internal/contract/openapi.go` | Add `Issue`/`Comment`/`Label`/`IssueStatus`/`IssueView` schemas; branch `responseSchema`/`inputSchema`/`optionalField`/`isList` for issue + extension routes. | OpenAPI is generated from the allowlist; new routes otherwise fall through to the wrong `Project`/`status` schema. |

No other existing Cloud file changes. Nothing in `store.go`, migrations `0001–0004`, the simulator, or `go.mod` is
touched. There is no core state-machine / Project / Tenant / User model change and no new dependency.

## 9. Rollback

Deleting the new files (both migrations, the `issue_*` core files, the two test files, the demo, and this doc set)
and reverting the three modified files returns the code to its pre-migration state. The DB schema is rolled back by
dropping the new tables (in reverse-dependency order) and removing the `0006_issue_extensions.sql` +
`0005_issues.sql` rows from `schema_migrations`. Migration 0006 does `ALTER` the `issues` table (drop the status
CHECK, add `number`/`properties`), so a full rollback is the inverse of 0006 then 0005 — see
[04-database.md](04-database.md).
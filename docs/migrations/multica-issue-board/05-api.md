# API — issue endpoints (reference)

Derived from Multica's actual board calls, re-exposed in Ora Cloud's public, tenant-scoped HTTP surface.
Base path prefix: `/api/v1/tenants/{tid}`. All six routes are public (`Action == ""`), so they require the
gateway service credential + a caller-bound user token, and tenant membership.

## Wire object `Issue`

```
id, tenantId, creatorUserId, assigneeUserId (nullable), assigneeType, assigneeId (nullable),
parentIssueId (nullable), projectRef (nullable),
title, description, status, priority, position, number, properties, labels,
version, createdAt, updatedAt, deletedAt (nullable)
```

- `status` is a free-format key matching `^[a-z0-9][a-z0-9_]{0,31}$`, resolved against the tenant's
  status catalog (default `todo`).
- `priority ∈ {urgent, high, medium, low, none}` (default `none`).
- `position` is a number (float); ordering is the source of truth for the board.
- `number` is a per-tenant integer sequence (`#1`, `#2`, …).
- `properties` is a JSON object (arbitrary KV); `labels` is an array of `Label` objects.
- **Polymorphic assignee** (`Wave 3A`): `assigneeType ∈ {user, agent, team}` + `assigneeId`. `user` is
  validated against active users; `agent`/`team` are opaque UUID refs this wave (no `ActorResolver` yet).
  `assigneeUserId` is mirror-written when `assigneeType='user'` and kept for backward compatibility.
- `projectRef` (`Wave 3A`): a nullable `uuid`, shape-validated only (existence checked later, when
  `ProjectContextResolver` lands).

## Endpoints

### 1. List board — `GET /api/v1/tenants/{tid}/issues`

Returns the whole board in one page (pagination deferred, per [00-overview.md](00-overview.md)):

```json
{ "items": [ { "id": "…", "status": "todo", "position": 0, … } ], "nextCursor": "" }
```

Order: canonical status (backlog→cancelled), then `position` ascending, then `id`.

### 2. Create — `POST /api/v1/tenants/{tid}/issues`

Headers: `Authorization: Bearer <service>`, `X-Ora-User-Token: <user>`, `Idempotency-Key: <opaque≤200>`.

```json
{ "title": "Fix login", "description": "…", "status": "todo", "priority": "high", "assigneeUserId": "<uuid>", "parentIssueId": "<uuid>" }
```

- `title` required; `status`/`priority` default; `assigneeUserId`/`parentIssueId` optional.
- New issue is placed at the top of its column.
- Response `200`: `{ "resource": { …Issue… } }`.

### 3. Get — `GET /api/v1/tenants/{tid}/issues/{iid}`

Response `200`: `{ …Issue… }`. 404 if absent, foreign, or deleted.

### 4. Update — `PUT /api/v1/tenants/{tid}/issues/{iid}`

Partial update; only fields present in the body change. Example:

```json
{ "title": "Fix login (urgent)", "status": "in_progress", "assigneeUserId": "", "version": 3 }
```

- `title` present → must be non-empty ≤200.
- `description` present → trimmed, ≤20000, may be empty.
- `status` present → valid enum; changing status re-ranks to top of the new column.
- `priority` present → valid enum.
- `assigneeUserId`/`parentIssueId` present → empty string clears (`NULL`), else valid uuid that must exist
  (assignee: active user; parent: non-deleted issue in the same tenant, not a self-reference).
- `version` is a required precondition: absent → `428 version_required`, mismatch → `409 version_conflict`.
- Response `200`: `{ …Issue… }`.

### 5. Move — `POST /api/v1/tenants/{tid}/issues/{iid}/move`

Headers include `Idempotency-Key`.

```json
{ "status": "in_progress", "beforeId": "<uuid>", "afterId": "<uuid>", "version": 3 }
```

- `beforeId`/`afterId` anchor the new position (both keys must be present; empty string = no anchor).
  Resolution is within the tenant (not the column), matching Multica. Result: midpoint / before+1 / after-1 / keep.
- `status` optional; changing status without anchors re-ranks to the top of the destination column.
- Response `200`: `{ …Issue… }`.

### 6. Delete — `DELETE /api/v1/tenants/{tid}/issues/{iid}`

Headers include `Idempotency-Key`; body `{ "version": 3 }`. Soft-deletes (sets `deletedAt`); the issue leaves the
board. Response `200`: `{ …Issue… }` (with `deletedAt` set). Subsequent GET/list return 404/omit it.

### 7. Search — `GET /api/v1/tenants/{tid}/issues?q=term`

Filters the board list to issues whose `title` or `description` contains `term` (case-insensitive `ILIKE`, literal
match — `%`, `_`, `\` are escaped). Same `{items, nextCursor}` shape and ordering as the plain list.

### 8. Batch update — `POST /api/v1/tenants/{tid}/issues/batch`

```json
{ "ids": ["<uuid>", "<uuid>"], "status": "done", "priority": "low", "assigneeUserId": "<uuid>" }
```

- `ids` is required (1..100 valid issue ids); at least one of `status`/`priority`/`assigneeUserId` must be present.
- Applies the patch to every listed issue atomically; no per-issue `version` precondition (see
  [08-differences.md](08-differences.md)).
- Response `200`: `{ "items": [ …Issue… ], "nextCursor": "" }`.

### 9. Grouped view — `GET /api/v1/tenants/{tid}/issue-groups?by=status|priority|assigneeUserId`

Response `200`: `{ "groups": [ { "key": "high", "items": [ …Issue… ] } ] }`. `by` defaults to `status`;
`by=assigneeUserId` buckets unassigned issues under the key `unassigned`. Honors `?q=`.

## Wave-2 endpoints (extensions)

All public, tenant-scoped, dual-JWT + membership-gated, same idempotency/error conventions.

### Status catalog — `/issue-statuses`

| Method | Path | Fields | Response @status |
| --- | --- | --- | --- |
| GET | `/tenants/{tid}/issue-statuses` | — | `{items:[IssueStatus…], nextCursor:""}` @200 (auto-seeds the 7 canonical columns) |
| POST | `/tenants/{tid}/issue-statuses` | `key`(req, `^[a-z0-9][a-z0-9_]{0,31}$`), `name`(req), `description`, `category`, `color`, `icon` | `{resource:IssueStatus}` @200 |
| PUT | `/tenants/{tid}/issue-statuses/{sid}` | `name`, `description`, `category`, `color`, `icon`, `position`, `version`(428) | `IssueStatus` @200 |
| DELETE | `/tenants/{tid}/issue-statuses/{sid}` | `version`(428) | `IssueStatus` @200 (soft delete; `409 system_status_required` for system columns) |

`IssueStatus`: `id, tenantId, key, name, description, category, color, icon, isSystem, position, version, createdAt,
updatedAt, deletedAt`. Duplicate `key` → `409 status_conflict`; bad `key`/`category` → 400.

### Labels — `/labels`

| Method | Path | Fields | Response @status |
| --- | --- | --- | --- |
| GET | `/tenants/{tid}/labels` | — | `{items:[Label…], nextCursor:""}` @200 |
| POST | `/tenants/{tid}/labels` | `name`(req), `color` | `{resource:Label}` @200 (dup name → `409 label_conflict`) |
| PUT | `/tenants/{tid}/labels/{lid}` | `name`, `color`, `version`(428) | `Label` @200 |
| DELETE | `/tenants/{tid}/labels/{lid}` | `version`(428) | `Label` @200 (soft delete + detach from all issues) |

`Label`: `id, tenantId, name, color, version, createdAt, updatedAt, deletedAt`.

### Saved views — `/issue-views`

| Method | Path | Fields | Response @status |
| --- | --- | --- | --- |
| GET | `/tenants/{tid}/issue-views` | — | `{items:[IssueView…], nextCursor:""}` @200 (owner-scoped) |
| POST | `/tenants/{tid}/issue-views` | `name`(req), `filter`(object) | `{resource:IssueView}` @200 |
| PUT | `/tenants/{tid}/issue-views/{vid}` | `name`, `filter`, `version`(428) | `IssueView` @200 |
| DELETE | `/tenants/{tid}/issue-views/{vid}` | `version`(428) | `IssueView` @200 |

`IssueView`: `id, tenantId, ownerUserId, name, filter, version, createdAt, updatedAt, deletedAt`. A user only
reads/writes their own views (`ownerUserId` mismatch → 404).

### Comments — `/issues/{iid}/comments[/{cid}]`

| Method | Path | Fields | Response @status |
| --- | --- | --- | --- |
| GET | `/issues/{iid}/comments` | — | `{items:[Comment…], nextCursor:""}` @200 |
| POST | `/issues/{iid}/comments` | `body`(req, ≤20000) | `{resource:Comment}` @200 |
| PUT | `/issues/{iid}/comments/{cid}` | `body`, `version`(428) | `Comment` @200 |
| DELETE | `/issues/{iid}/comments/{cid}` | `version`(428) | `Comment` @200 (soft delete) |

`Comment`: `id, tenantId, issueId, authorUserId (nullable), authorType, authorId (nullable), parentId (nullable),
seq (number), body, version, createdAt, updatedAt, deletedAt (nullable)`.

- **Threading + shared timeline** (`Wave 3A`): `parentId` roots a reply (same-issue invariant is app-layer, cross-issue
  → 404); `seq` is the per-issue timeline position shared with `issue_activities` (one namespace — see
  [04-database.md](04-database.md)). Comments list in `ORDER BY seq, id`.
- **Author ActorRef** (`Wave 3A`): `authorType ∈ {user, agent, team, system}` + `authorId`. `user` is validated against
  active users and mirror-writes `authorUserId`; `agent`/`team`/`system` leave `authorUserId` NULL (no `ActorResolver`
  yet). `POST /comments` accepts an optional `authorType`/`authorId` (default `user`).

### Issue labels — `/issues/{iid}/labels[/{lid}]`

| Method | Path | Fields | Response @status |
| --- | --- | --- | --- |
| GET | `/issues/{iid}/labels` | — | `{items:[Label…], nextCursor:""}` @200 |
| POST | `/issues/{iid}/labels` | `labelId`(req) | `{resource:Label}` @200 (idempotent attach) |
| DELETE | `/issues/{iid}/labels/{lid}` | — | `Label` @200 (detach) |

### Subscribers — `/issues/{iid}/subscribers`

| Method | Path | Fields | Response @status |
| --- | --- | --- | --- |
| GET | `/issues/{iid}/subscribers` | — | `{items:[User…], nextCursor:""}` @200 |
| POST | `/issues/{iid}/subscribers` | `userId`(req) | `{resource:User}` @200 (idempotent subscribe) |
| DELETE | `/issues/{iid}/subscribers` | `userId`(req) | `{resource:User}` @200 (unsubscribe) |

`userId` must be an active tenant member (`404 user_not_found` otherwise).

## Wave-3A endpoints (collaboration foundation)

All public, tenant-scoped, dual-JWT + membership-gated, same idempotency/error conventions. Implemented in the third
wave (first coding batch) per [12-collaboration-architecture.md](12-collaboration-architecture.md) Step 2. These expose
the issue-owned persistence/API-contract spine only — **no** real `ActorResolver`/`ExecutionDispatcher`/`WorkflowResolver`
yet, so `agent`/`team`/`workflow` refs are opaque UUIDs (shape-validated, not resolved).

### Runs — `/issues/{iid}/runs[/{rid}]`

| Method | Path | Fields | Response @status |
| --- | --- | --- | --- |
| GET | `/issues/{iid}/runs` | — | `{items:[IssueRun…], nextCursor:""}` @200 |
| GET | `/issues/{iid}/runs/{rid}` | — | `IssueRun` @200 (404 if absent/foreign) |
| POST | `/issues/{iid}/runs` | `executorType`(req, `agent\|team\|workflow`), `executorId`(req), `input`(object, optional), `parentRunId`, `retryOfRunId`, `rerunOfRunId`, `delegatedFromRunId`, `fireAt`, `externalExecutionId`, `executionContextRef`, `workflowInvocationRef` | `{resource:IssueRun}` @200 |

`IssueRun`: `id, tenantId, issueId, version, executorType, executorId, externalExecutionId, executionContextRef,
workflowInvocationRef, triggerEvidenceKind, triggerEvidenceRefId, status, parentRunId, retryOfRunId, rerunOfRunId,
delegatedFromRunId, attempt, maxAttempts, input, result, error, failureReason, triggerSummary, queuedAt, dispatchedAt,
startedAt, completedAt, fireAt, leaseExpiresAt, createdAt, updatedAt, deletedAt`.

- `POST` enqueues a run with `status='queued'` and appends a `run.enqueued` activity to the issue timeline.
- **Pending dedup**: a new run for the same `(issueId, executorType, executorId)` while one is `queued`/`dispatched`
  → `409 pending_run_exists`.
- **Terminal status ≠ deleted**: `completed`/`failed`/`cancelled` are history; there is **no** delete-run API
  (`deleted_at` is reserved for future hide/archive). Only `queued`/`dispatched` runs may transition; the state machine
  guard lives in SQL `WHERE`, not a central validator.
- No `runtime_id`/`sandbox_id`/`node_id`/`model_id`/`provider_id`/`pty_session_id` columns — execution internals are
  opaque external refs this wave.

### Context refs — `/issues/{iid}/context-refs[/{crid}]`

| Method | Path | Fields | Response @status |
| --- | --- | --- | --- |
| GET | `/issues/{iid}/context-refs` | — | `{items:[ContextRef…], nextCursor:""}` @200 |
| POST | `/issues/{iid}/context-refs` | `refType`(req, enum), `refId`(req) | `{resource:ContextRef}` @200 |
| DELETE | `/issues/{iid}/context-refs/{crid}` | — | `ContextRef` @200 (hard delete) |

`ContextRef`: `id, tenantId, issueId, refType, refId, createdAt`.
`refType ∈ {parent_issue, run, timeline_message, pull_request, project, workspace, acceptance_criteria}`.

- **Reference, not duplicate** — "reference context, not duplicate context": the issue stores an opaque `refId` pointer,
  never a copy of the target's payload.
- `refId` is a plain UUID (no FK, no existence check this wave); duplicate `(issueId, refType, refId)` → `409
  context_ref_exists`; invalid `refType`/`refId` → 400. Hard delete (join-like), no `version`.

## Errors

Cloud error envelope (`application/json`): `{ "code": "…", "params": {…}, "requestId": "…" }`.

| Status | Codes |
| --- | --- |
| 400 | `invalid_json`, `unknown_field`, `invalid_field_type`, `invalid_input`, `invalid_status`, `invalid_status_key`, `invalid_category`, `invalid_priority`, `invalid_anchor`, `invalid_parent`, `invalid_assignee`, `invalid_label`, `invalid_user`, `invalid_query`, `invalid_group`, `invalid_executor`, `invalid_ref_type`, `invalid_ref`, `idempotency_key_required` |
| 401 | `invalid_service_credential`, `invalid_user_credential` |
| 403 | `membership_required`, `user_disabled` |
| 404 | `not_found`, `assignee_not_found`, `parent_not_found`, `user_not_found` |
| 409 | `version_conflict`, `position_conflict`, `status_conflict`, `label_conflict`, `system_status_required`, `pending_run_exists`, `context_ref_exists`, `idempotency_conflict` |
| 428 | `version_required` |
| 500 | `internal_error` |

## Idempotency (Cloud-style)

POST (create, move) and DELETE require an `Idempotency-Key`. Replaying the same method+path+body with the same key
returns the original response without re-applying; a changed body with the same key returns `409 idempotency_conflict`.
This is a deliberate Cloud invariant carried over to issues (Multica does not require idempotency keys here) — see
[08-differences.md](08-differences.md).
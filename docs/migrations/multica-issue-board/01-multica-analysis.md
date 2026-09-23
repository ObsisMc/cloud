# Multica Issue Board — Analysis

Reference repo: `multica-ai/multica` (local checkout).
All paths below are relative to that repo root. Line/function references reflect the local
checkout; they are documentation, not an API promise.

## 1. Data model

### Issue table

Base creation: `server/migrations/001_init.up.sql` (lines 52–72). The column set grew across
~20 later migrations. The **final** shape is reflected by the sqlc model
`server/pkg/db/generated/models.go:765` (`type Issue struct`):

| Column | Type | Notes |
| --- | --- | --- |
| `id` | uuid PK | `gen_random_uuid()` |
| `workspace_id` | uuid | FK → `workspace(id) ON DELETE CASCADE`. The board's scope. |
| `title` | text | required |
| `description` | text | nullable |
| `status` | text | key into the status catalog (see below) |
| `priority` | text | `urgent/high/medium/low/none` |
| `assignee_type` | text | `member/agent/squad` (nullable) |
| `assignee_id` | uuid | nullable, polymorphic by `assignee_type` |
| `creator_type` | text | `member/agent` |
| `creator_id` | uuid | required |
| `parent_issue_id` | uuid | self-FK, `ON DELETE SET NULL` (sub-issues) |
| `acceptance_criteria` | jsonb | `'[]'` |
| `context_refs` | jsonb | `'[]'` |
| `position` | float (double precision) | fractional ordering |
| `due_date` / `start_date` | date | |
| `created_at` / `updated_at` | timestamptz | |
| `number` | int | per-workspace issue number (migration `020_issue_number`) |
| `project_id` | uuid | nullable; optional project grouping |
| `origin_type` / `origin_id` | | provenance stamping (quick-create / channels) |
| `first_executed_at` | timestamptz | agent lifecycle |
| `metadata` | jsonb | per-issue KV |
| `stage` | int | sub-issue barrier group |
| `properties` | jsonb | custom property values |
| `revision` | bigint | optimistic concurrency (Multica's `expected_revision`) |
| `last_activity_at` | timestamptz | |

### Status catalog

Multica started with a table `CHECK` of the 7 keys (`001_init`) and later (MUL-6243,
migrations `332–340` and later `469–478`) replaced it with a per-workspace **catalog table**
plus a **lifecycle category** model:

- Table `issue_status` (`332_issue_status.up.sql`): `id, workspace_id, key, name, description,
  category, color, is_system, position, archived_at, icon, created_at, updated_at`.
- `issue.status` remains a **text key**; the old `CHECK` was dropped (`337`) and replaced by a
  format `CHECK` (`^[a-z0-9][a-z0-9_]{0,31}$`) + application-layer validation.
- 7 canonical built-in keys, seeded for every workspace (`339_seed_issue_status_catalog.up.sql`):
  `backlog`, `todo`, `in_progress`, `in_review`, `done`, `blocked`, `cancelled`.
- 4 lifecycle categories mapped to 7 behaviors (`469`): `unstarted`(backlog,todo),
  `started`(in_progress,in_review,blocked), `done`(done), `closed`(cancelled).
  `478_issue_status_category_expand` widens the stored category enum to also hold the
  legacy behavior keys.
- A reserved `triage` key (not a board column, not a catalog row) (`477`).
- SQL mirror `issue_effective_status(workspace,status)` replicates Go-side resolution for
  SQL-only paths (`340`, `469`, `477`).

### Ordering

- `position` is a `double precision`, ordered **ASC within `(workspace_id, status)`**.
- Package `server/internal/issueposition/position.go` `NextTopPosition` returns
  `MIN(position)-1` for a column (empty column → `0`), placing a new/status-changed issue at
  the top of a column. The same `MIN(position)-1` policy is inlined in `UpdateIssue` /
  `UpdateIssueStatus` (`queries/issue.sql`).
- Move uses **relative anchors** (see §4).

## 2. Status / category semantics

`server/internal/issuestatus/issuestatus.go`:

- `Canonical()` → the 7 keys in display order `[backlog todo in_progress in_review blocked done cancelled]`.
- `Categories()` → `[unstarted started done closed]`.
- `Effective()` maps a status key to the canonical key whose platform behaviour it carries
  (built-ins are identity; custom terminal → `done`/`cancelled`).
- `CategoryForBehavior()` / `BehaviorsForCategory()` map the 7↔4.
- `Resolve()` is the write-path validation replacing the dropped CHECK (built-ins pass even on
  an unseeded catalog row; `triage` refused).

The board renders columns from the **catalog** (via `GET /api/issue-statuses`); grouping by
"status" uses the 7 canonical keys (custom statuses were a follow-up not yet selectable as
table groups — noted as a known limitation in `handler/issue.go` `validIssueStatuses`).

## 3. API surface

Registered in `server/cmd/server/router.go` (~lines 1881–1983, 2056–2067), served by
`server/internal/handler/*`. Endpoints relevant to the board:

| Method | Path | Handler | Purpose |
| --- | --- | --- | --- |
| GET | `/api/issues` | `ListIssues` (`issue.go:1115`) | filtered list |
| POST | `/api/issues/query` | `QueryIssues` (`issue.go:1101`) | POST twin for large filters |
| GET | `/api/issues/grouped` | `ListGroupedIssues` (`issue.go:1824`) | group by assignee/status |
| POST | `/api/issues/table/groups\|rows\|facets` | `ListIssueTableGroups/Rows/Facets` | table/board view |
| POST | `/api/issues` | `CreateIssue` (`issue.go:2882`) | create |
| GET | `/api/issues/{id}` | `GetIssue` (`issue.go:2296`) | detail |
| PUT | `/api/issues/{id}` | `UpdateIssue` (`issue.go:3419`) | update |
| POST | `/api/issues/{id}/move` | `MoveIssue` (`issue_move.go:34`) | board move |
| DELETE | `/api/issues/{id}` | `DeleteIssue` (`issue.go:3982`) | delete |
| GET/POST/PATCH/DELETE | `/api/issue-statuses[/{id}]` | `issue_status.go` | status catalog CRUD/reorder/archive |
| GET/POST/PATCH/DELETE | `/api/issue-views[/{id}]` | `issue_view.go` | saved views |
| – | `/api/issues/{id}/children`, `/labels`, `/metadata`, `/properties`, `/comments`, `/reactions`, `/subscribers`, `/tasks`, … | | (out of scope) |

### Create

`CreateIssueRequest` (`handler/issue.go:2850`): `title`, `description`, `status`, `priority`,
`assignee_type`, `assignee_id`, `parent_issue_id`, `project_id`, `stage`, `start_date`,
`due_date`, `attachment_ids`, `label_ids`, `origin_type`, `origin_id`, `allow_duplicate`.
Defaults: `status="todo"`, `priority="none"`. Status/key resolution via `resolveIssueStatusKey`,
priority validated against `validIssuePriorities`. Delegates to `IssueService.Create` (atomic
create + rules inside the service).

### Update

`UpdateIssueRequest` (`handler/issue.go:3195`). `PUT` performs a partial update, preserving an
`expected_revision` optimistic check; status change may re-rank to the top of the new column.

### Move (the board operation) — `issue_move.go`

Fields (allowlist `issueMoveFields`): `status`, `assignee_type`, `assignee_id`,
`parent_issue_id`, `project_id`, `before_id`, `after_id`, `expected_revision`.

Behaviour (`issue_move.go:34` and `issueMovePosition:191`):

1. Both `before_id` and `after_id` must be **present** keys (may be JSON `null`).
2. Anchors are resolved to their `position` **within the issue's workspace** (no status filter).
3. New position is derived server-side:
   - `before && after` → `before + (after-before)/2` (midpoint), must be strictly between, else 409.
   - `before` only → `before + 1`.
   - `after` only → `after - 1`.
   - neither → keep current `position`.
4. `status` (and other fields) ride along; the write is delegated to `UpdateIssue` so
   validation/realtime/task side effects stay on one path.

### Response

`IssueResponse` (`handler/issue.go:37`) and `publicapi/v1.Issue` (`server/pkg/publicapi/v1/types.go:34`)
are the authoritative Wire shapes: `id, workspace_id, number, identifier, title, description,
status, status_category, status_name, priority, assignee_type, assignee_id, creator_type,
creator_id, parent_issue_id, project_id, position, stage, start_date, due_date, created_at,
updated_at, revision, last_activity_at, metadata, properties, reactions, attachments, labels`.

## 4. Full call chains

```
Create:   browser → POST /api/issues
          → handler.CreateIssue (issue.go:2882) → resolve status/priority → validate assignee
          → service.IssueService.Create (internal/service/issue.go)
          → sqlc db.Queries.CreateIssue (pkg/db/queries/issue.sql) → PostgreSQL issue table

Read:     browser board → issue statuses + issues
          → GET /api/issue-statuses (handler.ListIssueStatuses)
          → GET /api/issues (handler.ListIssues → ListIssueTableGroups/Rows or ListGroupedIssues)
          → sqlc list queries (issue.sql) → PostgreSQL

Update:   browser → PUT /api/issues/{id} → handler.UpdateIssue (issue.go:3419)
          → updateIssueAtomically (issue.go:3314) → sqlc UpdateIssue → PostgreSQL
          → realtime broadvents (internal/events) — NOT migrated

Move:     browser drag (packages/views/issues/components/board-view.tsx)
          → useUpdateIssue mutation (packages/core/issues/mutations.ts:195)
          → POST /api/issues/{id}/move (handler.MoveIssue, issue_move.go)
          → resolve anchors → compute position → delegate UpdateIssue → PostgreSQL

Delete:   browser → DELETE /api/issues/{id} → handler.DeleteIssue (issue.go:3982) → cascade
```

## 5. Board frontend → API mapping

`packages/views/issues/components/board-view.tsx` (with `board-column.tsx`, `board-card.tsx`):

- Columns for `grouping === "status"` come from `buildGroups` → `visibleStatuses` (the status
  catalog `GET /api/issue-statuses`; `useIssueStatuses(...)`, `packages/core/issue-statuses`).
- Cards = `Issue[]`; ordering by `position` when `sortBy === "position"` (manual sort), else
  `created_at` / `updated_at` / priority.
- Drag drop computes `getMoveUpdates` (status/assignee/project change) + `getMoveAnchors`
  (neighbour `before`/`after` ids) in `packages/views/issues/utils/drag-utils.ts`, then calls
  `useUpdateIssue` → `api.moveIssue(id, {..., before_id, after_id})` (owner of the
  `/move` endpoint) or `api.updateIssue` (`PUT`).

## 6. Realtime

Multica emits issue/comment events over WebSocket (`server/internal/realtime`,
`packages/core/realtime`) and the UI subscribes via TanStack Query cache + WS updaters
(`packages/core/issues/ws-updaters.ts`). **Not migrated** for the Cloud MVP — HTTP refetch is
sufficient; the board is a read of the full list/status set.
# Database — issues migration

## Migration file

`internal/core/migrations/0005_issues.sql` (embedded, checksummed, applied by `cloudctl migrate` /
`Store.Migrate`, run against PostgreSQL 17). Immutable once applied.

## Table `issues`

```sql
CREATE TABLE issues (
 id uuid PRIMARY KEY,
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 creator_user_id uuid NOT NULL,
 assignee_user_id uuid REFERENCES users(id),
 parent_issue_id uuid REFERENCES issues(id) ON DELETE SET NULL,
 title text NOT NULL CHECK(length(title) BETWEEN 1 AND 200),
 description text NOT NULL DEFAULT '',
 status text NOT NULL DEFAULT 'todo' CHECK(status IN ('backlog','todo','in_progress','in_review','blocked','done','cancelled')),
 priority text NOT NULL DEFAULT 'none' CHECK(priority IN ('urgent','high','medium','low','none')),
 position double precision NOT NULL DEFAULT 0,
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 deleted_at timestamptz,
 FOREIGN KEY(tenant_id, creator_user_id) REFERENCES tenant_memberships(tenant_id, user_id)
);
CREATE INDEX issue_board ON issues(tenant_id, status, position, id);
CREATE INDEX issue_list ON issues(tenant_id, id);
```

Notes:

- **Scope** = `tenant_id` (board is tenant-shared; no per-owner filter). `creator_user_id` is constrained by the
  composite FK to `tenant_memberships` so the creator is always an active-relationship member of record; the FK does
  not by itself enforce *active* tenants/users — that is done at read/write time by `membership()` → the same
  pattern Cloud already uses (constraints are structural, authorization is behavioural).
- **Soft delete** via `deleted_at`; board queries add `AND deleted_at IS NULL`.
- **`position` is `double precision`** (runs of `before ± 1` can reach ± large, fractional midpoints approach
  double precision limits; Multica's midpoint-invalid → 409 guard is re-implemented).
- **Parent link** `ON DELETE SET NULL`: deleting a parent leaves children orphaned at top level (matches Multica 001_init).
- `updated_at` is maintained in the `UPDATE ... SET ..., updated_at = now()` statements (Cloud has no global
  updated_at trigger; only `operations` uses it today — this is additive and contained to `issues`).

## Board ordering

Canonical column order and within-column position order:

```sql
ORDER BY CASE status
  WHEN 'backlog' THEN 0 WHEN 'todo' THEN 1 WHEN 'in_progress' THEN 2
  WHEN 'in_review' THEN 3 WHEN 'blocked' THEN 4 WHEN 'done' THEN 5
  WHEN 'cancelled' THEN 6 END, position, id
```

## Migration 0006 — issue extensions

`internal/core/migrations/0006_issue_extensions.sql` (second wave). Adds six tables and alters `issues`.

### New tables

| Table | Key columns | Constraints / indexes |
| --- | --- | --- |
| `issue_statuses` | `tenant_id`, `key`, `name`, `description`, `category` (`unstarted/started/done/closed`), `color`, `icon`, `is_system bool`, `position double`, `version`, timestamps, `deleted_at` | `UNIQUE(tenant_id,key)`; `key ~ '^[a-z0-9][a-z0-9_]{0,31}$'`; `FK tenant_id→tenants`; index `(tenant_id,position,id)`. |
| `issue_comments` | `tenant_id`, `issue_id`, `author_user_id`, `body`, `version`, timestamps, `deleted_at` | `CHECK length(body) BETWEEN 1 AND 20000`; `FK issue_id→issues`; composite `FK(tenant_id,author_user_id)→tenant_memberships`; index `(issue_id,created_at,id)`. |
| `labels` | `tenant_id`, `name`, `color`, `version`, timestamps, `deleted_at` | partial unique `UNIQUE(tenant_id,name) WHERE deleted_at IS NULL`; `FK tenant_id→tenants`; index `(tenant_id,id)`. |
| `issue_labels` | `issue_id`, `label_id`, `created_at` | `PK(issue_id,label_id)`; FKs to `issues` and `labels`. |
| `issue_subscribers` | `issue_id`, `user_id`, `tenant_id`, `created_at` | `PK(issue_id,user_id)`; `FK issue_id→issues`; composite `FK(tenant_id,user_id)→tenant_memberships`. |
| `issue_views` | `tenant_id`, `owner_user_id`, `name`, `filter jsonb`, `version`, timestamps, `deleted_at` | `CHECK jsonb_typeof(filter)='object'`; composite `FK(tenant_id,owner_user_id)→tenant_memberships`; index `(tenant_id,owner_user_id,id)`. |

### `ALTER TABLE issues`

1. `DROP CONSTRAINT issues_status_check` → `ADD CONSTRAINT issues_status_format_check CHECK(status ~ '^[a-z0-9][a-z0-9_]{0,31}$')`
   (frees the status key from the fixed 7-value enum; validity is now enforced against `issue_statuses`).
2. `ADD COLUMN number bigint` → backfill `row_number() OVER (PARTITION BY tenant_id ORDER BY created_at, id)` →
   `SET NOT NULL` → `ADD CONSTRAINT issues_tenant_number_uniq UNIQUE(tenant_id, number)`.
3. `ADD COLUMN properties jsonb NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(properties) = 'object')`.

### Status catalog seeding

The migration cannot seed `issue_statuses` (tenants exist only at runtime), so the 7 canonical columns are inserted
lazily on first read/write of a tenant's statuses: `INSERT … ON CONFLICT(tenant_id,key) DO NOTHING` with
`is_system=true` and `position` `0..6` in board order (`backlog,todo,in_progress,in_review,blocked,done,cancelled`),
categories `unstarted/unstarted/started/started/started/done/closed`. The board ordering becomes:

```sql
LEFT JOIN issue_statuses s ON s.tenant_id=i.tenant_id AND s.key=i.status AND s.deleted_at IS NULL
ORDER BY COALESCE(s.position,1000), i.position, i.id
```

## Migration 0007 — issue collaboration foundation (Wave 3A)

`internal/core/migrations/0007_issue_collaboration.sql` (third wave, first coding batch). Purely additive
over `0001–0006`; edits no applied migration. Implements **Step 2 — the issue-owned foundation** of
[12-collaboration-architecture.md](12-collaboration-architecture.md): polymorphic assignee, comment threading +
author ActorRef + timeline `seq`, `issue_runs`, `issue_activities`, `issue_context_refs`. **No** agent/team/
workflow/sim/runtime/notification/websocket/PR/logs schema lives here.

### `ALTER TABLE issues`

1. `ADD COLUMN assignee_type text NOT NULL DEFAULT 'user' CHECK(assignee_type IN ('user','agent','team'))`.
2. `ADD COLUMN assignee_id uuid` (no FK — resolved by type via `ActorResolver`; the resolver does not exist yet).
3. `ADD COLUMN project_ref uuid` (nullable; shape-only this wave, no existence check until `ProjectContextResolver`).
4. Backfill `UPDATE issues SET assignee_type='user', assignee_id=assignee_user_id WHERE assignee_user_id IS NOT NULL`.
   `assignee_user_id` is kept readable and mirror-written when `assignee_type='user'`.

### `ALTER TABLE issue_comments`

1. `ADD COLUMN parent_id uuid REFERENCES issue_comments(id)` (threading; same-issue invariant is app-layer).
2. `ADD COLUMN author_type text NOT NULL DEFAULT 'user' CHECK(author_type IN ('user','agent','team','system'))`.
3. `ADD COLUMN author_id uuid`.
4. `ADD COLUMN seq bigint`.
5. `ALTER COLUMN author_user_id DROP NOT NULL` — agent/team/system authors leave it NULL; the composite
   `FK(tenant_id, author_user_id)` skips NULLs (MATCH SIMPLE).
6. Backfill `author_type='user', author_id=author_user_id`; then `seq = row_number() OVER (PARTITION BY
   issue_id ORDER BY created_at, id)`; then `seq SET NOT NULL` + `UNIQUE(issue_id, seq)`.

**ActorRef shape:** authors use a single `author_type`/`author_id` pair (uniform with `assignee_type`/
`assignee_id` and `issue_activities.actor_type`/`actor_id`). The frozen design's `author_agent_id`/
`author_team_id` per-actor columns are **not** used — one ActorRef `{type,id}` pair, no per-actor columns.

### New table `issue_runs`

Issue-owned AI work-lifecycle unit (**not** `operations`, **not** `execution_tickets`).

| Group | Columns |
| --- | --- |
| identity | `id uuid PK`, `tenant_id → tenants`, `version bigint CHECK(>0)`, `created_at/updated_at/deleted_at` |
| owner | `issue_id uuid NOT NULL REFERENCES issues(id)` (no `ON DELETE CASCADE` — runs are retained history; issues soft-delete anyway) |
| executor | `executor_type text CHECK(IN 'agent','team','workflow')`, `executor_id uuid NOT NULL` (no FK; opaque ref) |
| external refs | `external_execution_id text DEFAULT ''`, `execution_context_ref uuid`, `workflow_invocation_ref uuid`, `trigger_evidence_kind text DEFAULT ''`, `trigger_evidence_ref_id uuid` |
| state | `status text CHECK(IN 'queued','dispatched','running','completed','failed','cancelled','deferred')` |
| run chain | `parent_run_id/retry_of_run_id/rerun_of_run_id/delegated_from_run_id` self-FKs, `attempt bigint`, `max_attempts bigint` |
| payload | `input jsonb NOT NULL '{}'`, `result jsonb`, `error text`, `failure_reason text`, `trigger_summary text` |
| lifecycle | `queued_at`, `dispatched_at`, `started_at`, `completed_at`, `fire_at`, `lease_expires_at` |

Indexes: `issue_run_list(issue_id, created_at, id)`; partial unique
`issue_run_pending_uniq(issue_id, executor_type, executor_id) WHERE status IN ('queued','dispatched') AND
deleted_at IS NULL` (pending dedup — at most one pending run per executor). **Terminal status ≠ deleted**
(`completed/failed/cancelled` are history; `deleted_at` is for future hide/archive/admin cleanup; no delete-run API).

### New table `issue_activities`

Append-only **Timeline projection** (not an event source): `id`, `tenant_id → tenants`, `issue_id → issues`,
`seq bigint NOT NULL`, `actor_type CHECK(IN 'user','agent','team','system')`, `actor_id uuid`,
`action text CHECK(len 1..200)`, `details jsonb NOT NULL '{}'`, `created_at`. `UNIQUE(issue_id, seq)`;
index `issue_activity_list(issue_id, seq)`. No `version`, no update/delete API.

### New table `issue_context_refs`

Issue-scoped opaque references ("reference context, not duplicate context"): `id`, `tenant_id → tenants`,
`issue_id → issues`, `ref_type text CHECK(IN 'parent_issue','run','timeline_message','pull_request',
'project','workspace','acceptance_criteria')`, `ref_id uuid NOT NULL` (no FK), `created_at`.
`UNIQUE(issue_id, ref_type, ref_id)`; index `issue_context_ref_list(issue_id, ref_type, id)`. Join-like —
hard delete, no `version`.

### Timeline `seq` allocator (Option C)

The next per-issue `seq` is allocated inside the advisory-locked transaction as
`GREATEST(COALESCE(MAX(issue_comments.seq),0), COALESCE(MAX(issue_activities.seq),0)) + 1` — one namespace
shared by comments and activities (so a single issue never reuses a `seq` across the two tables). This is
deliberately **not** a `timeline_seq` column on `issues` (which would leak through `SELECT * FROM issues`
into the Issue response and break the `additionalProperties:false` OpenAPI contract). See
[issue_activities.go](../../../internal/core/issue_activities.go) for the concurrency note if the global
advisory lock is ever removed.

## Rollback

```sql
DROP TABLE IF EXISTS issue_views CASCADE;
DROP TABLE IF EXISTS issue_subscribers CASCADE;
DROP TABLE IF EXISTS issue_labels CASCADE;
DROP TABLE IF EXISTS labels CASCADE;
DROP TABLE IF EXISTS issue_comments CASCADE;
DROP TABLE IF EXISTS issue_statuses CASCADE;
-- then revert the issues ALTERs, then:
DROP TABLE IF EXISTS issues CASCADE;
DELETE FROM schema_migrations WHERE version IN ('0006_issue_extensions.sql','0005_issues.sql');
```

0005 is purely additive. 0006 **does** alter `issues` (drop the status CHECK, add `number`/`properties`); its inverse
is `ALTER TABLE issues DROP CONSTRAINT issues_tenant_number_uniq`, `DROP COLUMN properties`, `DROP COLUMN number`,
restore `issues_status_check`. No other existing table, type, or function is touched.
# FINAL — Multica Issue Board → Ora Cloud

## Result

The Issue Board (Kanban) capability was migrated from **Multica** into **Ora Cloud** in two waves, re-implemented
in Cloud's own architecture (route allowlist → `core.Store.Public` → raw SQL in an advisory-locked transaction),
additive-first. Wave 1 delivered the core board (7 statuses, priorities, fractional position, CRUD + move); wave 2
([10-extensions.md](10-extensions.md)) added the board periphery — status catalog, comments, labels, subscribers,
per-tenant numbers, custom `properties`, search, batch update, saved views, and grouped views. Build is clean, all
unit and integration tests pass, the migrations run on PostgreSQL 17 via Docker, everything is verified over HTTP,
and the schema/data persist across a container restart.

**Subsequent waves (see their own docs):** Wave 3 migrated the formal React frontend into
[`frontend/`](../../../frontend/README.md) ([13-frontend-migration.md](13-frontend-migration.md)); **Wave 3A**
landed the issue-owned collaboration foundation (migration `0007` — polymorphic assignee, comment author actors,
`issue_runs`, `issue_activities`, `issue_context_refs`). Wave 3A's divergences and the 3B/3C plan are the source of
truth in [12-collaboration-architecture.md](12-collaboration-architecture.md) (§36 + §0) and
[06-implementation-log.md](06-implementation-log.md); the record below describes waves 1–2 in detail.

## Architecture

```
HTTP → internal/api/router (allowlist + dual-JWT auth)
     → core.Store.Public (identity → membership → dispatch)
     → internal/core/issues.go + issue_{statuses,comments,labels,subscribers,views}.go
       (raw SQL inside core.transact, pg_advisory_xact_lock)
     → PostgreSQL 17 (`issues` + 6 extension tables, migrations 0005/0006)
```

Issues are **tenant-scoped** (no Cloud `project` binding). Creator = authenticated user (structural FK to
`tenant_memberships`); assignee = optional `users.id`; parent = optional self-FK. Ordering is a fractional
`position` ascending within `(tenant_id, status)`, columns ordered by the tenant's `issue_statuses.position`.

## Added files

- `internal/core/migrations/0005_issues.sql` — `issues` table + `issue_board`/`issue_list` indexes.
- `internal/core/migrations/0006_issue_extensions.sql` — 6 extension tables + `issues` ALTERs.
- `internal/core/issues.go` — issue CRUD + move + search/batch/groups + position/status helpers.
- `internal/core/issue_statuses.go` / `issue_comments.go` / `issue_labels.go` / `issue_subscribers.go` /
  `issue_views.go` — wave-2 sub-resources.
- `integration/issues_test.go` — wave-1 integration tests.
- `integration/issue_extensions_test.go` — wave-2 integration tests.
- `cmd/demo-issue-board/main.go` — self-cleaning end-to-end demo.
- `scripts/demo-issue-board.sh` — demo wrapper (env + Docker readiness).
- `cmd/demo-issue-board-web/` — browser Kanban UI on top of the real HTTP API (single-file `index.html`,
  server-side dual-JWT signing; no CORS, no frontend dependency). Wave 2: card numbers, label badges,
  detail drawer (comments/labels/subscribers/properties), search, group-by, custom-column entry.
- `scripts/demo-issue-board-web.sh` — web demo wrapper (env + Docker readiness + auto-open browser).
- `docs/migrations/multica-issue-board/` — `00-overview` … `10-extensions` + this file.

## Modified files (minimal, additive)

- `internal/api/router/router.go` — +30 routes; new path params; `validField` array/object/integer cases.
- `internal/core/public.go` — `PublicRequest` extension fields + GET/write dispatch (incl. nested sub-dispatch).
- `internal/contract/openapi.go` — `Issue` (+3 fields) / `Comment` / `Label` / `IssueStatus` / `IssueView`
  schemas + response/input/optional/isList branches.
- `api/openapi.json` — regenerated artifact.

No changes to `store.go`, existing migrations `0001–0004`, `go.mod`, the simulator, or the core state machine.
No new dependency.

## Database

`issues(id, tenant_id, creator_user_id, assignee_user_id, parent_issue_id, title, description, status, priority,
position, number, properties, version, created_at, updated_at, deleted_at)` with CHECKs (status format, priority,
title length, version>0), FKs (tenant, creators→membership, assignee→users, parent→issues SET NULL), unique
`(tenant_id,number)`, and two indexes — plus six extension tables (`issue_statuses`, `issue_comments`, `labels`,
`issue_labels`, `issue_subscribers`, `issue_views`) from migration 0006. Applied via `cloudctl migrate` /
`Store.Migrate`; recorded in `schema_migrations`. Verified with `\d issues`; tables and records survive
`docker restart cloud-postgres-1` (pgdata volume).

## Multica → Cloud mapping

| Multica | Cloud |
| --- | --- |
| workspace | tenant |
| member/user creator+assignee | users (creator via identity) |
| project (lightweight) | *(not bound)* |
| issue table + sqlc queries/service/handlers | `issues` table + `core/issues.go` |
| 7 canonical statuses / `issue_status` catalog | simplified `issue_statuses` catalog (lazy-seeded) |
| priority (5) | identical CHECK |
| `position float64` | `double precision` |
| `number` (per-workspace seq) | `number` (per-tenant `max+1`, advisory-lock safe) |
| metadata / custom fields | `properties` jsonb (schema-less) |
| comments / labels / subscribers | implemented (simplified: no reactions/mentions/notifications) |
| saved views / batch / search / grouped views | implemented (views store filter, not applied server-side) |
| `revision`/`expected_revision` | `version` + `version(o,v)` 428/409 |
| `chi → handler → service → sqlc` | `Gin allowlist → core.Store.Public → raw SQL` |
| WebSocket realtime | *(deferred)* |

## API

Core: `/api/v1/tenants/{tid}/issues[/{iid}[/move]]` (+ `?q=` search, `POST /issues/batch`,
`GET /issue-groups?by=…`). Extensions: `/issue-statuses[/{sid}]`, `/labels[/{lid}]`,
`/issue-views[/{vid}]`, `/issues/{iid}/comments[/{cid}]`, `/issues/{iid}/labels[/{lid}]`,
`/issues/{iid}/subscribers`.

Full field/error reference: [05-api.md](05-api.md). OpenAPI regenerated: `api/openapi.json`.

## Tests

- `go test ./internal/... ./cmd/...` — PASS (incl. OpenAPI contract test).
- Integration (Docker PG 17, isolated schema per test): full suite PASS, incl.
  `TestIssueBoardCRUDAndMove`, `TestIssueBoardValidationAndIsolation`, and the five wave-2
  `TestIssue*` extension tests.
- See [07-testing.md](07-testing.md) for exact commands and coverage.

## Manual verification

- `cloudctl migrate` → migrations 0005+0006 applied; `psql \d issues` shows all columns/CHECKs/FKs/indexes.
- `scripts/demo-issue-board.sh` prints board snapshots with fractional positions.
- `scripts/demo-issue-board-web.sh` — browser UI exercises numbers, labels, comments, search, groups,
  custom columns, and subscribe against the real API.
- `docker restart cloud-postgres-1` → migration records and all tables intact.

## Known differences

Summarised in [08-differences.md](08-differences.md): simplified status catalog (no lifecycle/workflow
machinery), schema-less `properties`, no polymorphic assignee, no project binding, Cloud-mandatory
idempotency keys on POST/DELETE, `{resource}`-wrapped create, status-only move re-ranks to column top,
soft delete with explicit child orphan, batch without per-issue version guard, saved-view filters not
applied server-side, subscriptions without notification pipeline.

> Wave 3A later **re-opened** two items above — polymorphic `assignee_type/assignee_id` and nullable
> `project_ref` — see [12-collaboration-architecture.md](12-collaboration-architecture.md) §36 and
> [08-differences.md](08-differences.md) §"Wave 3A divergences".

## Known limitations

- Attachments, issue↔project binding, PR links, WebSocket realtime, bots/squads/Autopilot — all deferred
  (see [00-overview.md](00-overview.md)).
- No pagination (board returns all tenant issues).
- Search is ILIKE substring on title/description only.
- Saved-view `filter` is stored but not executed server-side.
- Assignee is not required to be a tenant member (only "active user"); membership hardening is deferred.
- Float-precision exhaustion (`position_conflict`) is implemented but not stress-tested.
- No automatic reindex/normalization of positions; long edit histories can drift toward precision limits
  (shared with Multica's approach).

## Rollback

Delete the new files (both migrations, `issues.go` + the five `issue_*` core files, both test files, the demo
dirs/scripts, `docs/migrations/multica-issue-board/`) and revert the three modified files (`router.go`,
`public.go`, `openapi.go`, plus the generated `api/openapi.json`). Database (inverse of 0006 then 0005):

```sql
DROP TABLE IF EXISTS issue_views CASCADE;
DROP TABLE IF EXISTS issue_subscribers CASCADE;
DROP TABLE IF EXISTS issue_labels CASCADE;
DROP TABLE IF EXISTS labels CASCADE;
DROP TABLE IF EXISTS issue_comments CASCADE;
DROP TABLE IF EXISTS issue_statuses CASCADE;
ALTER TABLE issues DROP CONSTRAINT IF EXISTS issues_tenant_number_uniq;
ALTER TABLE issues DROP CONSTRAINT IF EXISTS issues_status_format_check;
ALTER TABLE issues DROP COLUMN IF EXISTS properties;
ALTER TABLE issues DROP COLUMN IF EXISTS number;
ALTER TABLE issues ADD CONSTRAINT issues_status_check CHECK(status IN ('backlog','todo','in_progress','in_review','blocked','done','cancelled'));
DROP TABLE IF EXISTS issues CASCADE;
DELETE FROM schema_migrations WHERE version IN ('0006_issue_extensions.sql','0005_issues.sql');
```

Migration 0005 is purely additive; 0006 alters `issues` (status CHECK swap, `number`, `properties`) — its
inverse is included above. No other existing table, constraint, function, or index is touched.
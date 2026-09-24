# Issue Board Migration — Overview

## Migration goal

Migrate the **Issue Board (Kanban)** capability from the reference project **Multica**
(`multica-ai/multica`) into **Ora Cloud** (`ora-space/cloud`), re-implemented in Ora Cloud's
own architecture — additive-first, minimizing changes to existing Cloud code.

**In scope (three waves):**

> **Naming note.** "Wave 3" is overloaded in this doc set and must not be read as one thing:
> here and in [13-frontend-migration.md](13-frontend-migration.md) the third wave is the **frontend**
> migration (relabelled "Frontend wave" below); in
> [12-collaboration-architecture.md](12-collaboration-architecture.md) and
> [progress.md](../../development/onboarding/progress.md), "第三波 / Wave 3" means **Issue
> Collaboration** (3A / 3B / 3C). When writing new docs, spell out which one is meant.

*Wave 1 — core board:*

- An Issue resource (tenant-scoped, matching Multica's workspace-scoped issue).
- The 7 canonical board statuses (`backlog`, `todo`, `in_progress`, `in_review`, `done`,
  `blocked`, `cancelled`) as the initial column set.
- Priority (`urgent`, `high`, `medium`, `low`, `none`).
- Title, description, assignee (a Cloud user), creator, optional parent (sub-issue).
- Fractional `position` ordering within a status column (Multica's move algorithm).
- CRUD + `move` (cross-column status change + within/between-column reordering) over HTTP.

*Wave 2 — board periphery (see [10-extensions.md](10-extensions.md)):*

- Custom status columns: a tenant **status catalog** (`issue_statuses`) with lifecycle
  categories, colors, and archival, lazily seeded with the 7 canonical columns.
- **Comments** (`issue_comments`), **labels** (`labels` + `issue_labels`), **subscribers**
  (`issue_subscribers`).
- Per-tenant **card numbers** (`#42`) and **custom fields** (`properties` jsonb KV).
- **Search** (`?q=`), **saved views** (`issue_views`), **batch update**, **grouped views**
  (`?by=status|priority|assigneeUserId`).

- New PostgreSQL migrations (0005 + 0006) + verification against PostgreSQL 17.
- Integration tests + a runnable browser demo.

*Frontend wave — formal frontend (see [13-frontend-migration.md](13-frontend-migration.md)):*

- Migrate the reference frontend into `frontend/` as Cloud's official web UI (React 19 + TS + Vite +
  Tailwind 4 + TanStack Query), generated API client, dual-JWT via `cmd/ora-web`.
- Issue board/CRUD/status/priority/assignee/labels/comments/subscribers/search/batch/views +
  Wave 3A surfaces (`assigneeType`/`assigneeId`/`projectRef`, comment thread + author actor,
  IssueRun, IssueContextRef) against the real HTTP API.
- Agent/team assignee, project details, execution, realtime, PR, logs — unavailable placeholders,
  not faked.

## Non-goals (still deferred)

- **Attachments / file uploads** — Cloud has no file storage.
- **Issue↔project binding** — Cloud `project` is a dev-environment repository with a lifecycle;
  issues stay tenant-only (see [03-design.md](03-design.md) §Domain mapping).
- **Pull-request linking** — requires an external Git service integration.
- **WebSocket / realtime subscriptions** — Multica broadcasts issue events; HTTP refetch covers
  the current surface.
- **Bots / squad leads / autopilot triggers** — no agent/team/automation foundation exists. Agent and
  Team targets arrive as **fixture adapters** in Wave 3B-1 and Workflow targets in 3B-2; the real
  modules and Autopilot itself stay out of scope
  ([12-collab §37](12-collaboration-architecture.md#37-wave-3b-0--collaboration-interaction-model-frozen)).
- **Rich table/graph views** — the grouped view is a first-class endpoint, but arbitrary saved
  table/graph views remain out.
- Full pagination; the board list returns the tenant's issues in one page.

> **Re-opened after this overview was written:** Wave 3A (migration `0007`) added a **polymorphic
> assignee** (`assignee_type`/`assignee_id`) and a **nullable `project_ref`** on `issues`. The
> non-goals above still hold in substance — issues remain tenant-scoped, `project_ref` is an opaque,
> unresolved reference and **not** an issue↔project binding — but the flat "assignee is a Cloud user"
> and "issues stay tenant-only" phrasings are out of date. See
> [12-collaboration-architecture.md](12-collaboration-architecture.md) §10/§21/§36.

## Sources

- **Reference:** `multica-ai/multica` — Issue model, handlers, services, sqlc queries,
  migrations, and the Web issue board (`packages/views/issues`, `packages/core/issues`).
- **Target:** `ora-space/cloud` — Go/Gin + PostgreSQL, explicit route allowlist +
  `internal/core` dispatch + checksummed embedded migrations.

## Migration principles

1. Additive first: new files, new migration, new routes; no core refactor.
2. Follow Ora Cloud architecture (route allowlist → `core.Store.Public` → raw SQL in a
   short advisory-locked transaction), not Multica's (chi router → handler → service → sqlc).
3. Re-implement behavior, do not copy Multica source.
4. Reuse existing Cloud entities where domain semantics match: `tenant` (≈ Multica workspace),
   `user` (creator/assignee). Do **not** bind to Cloud `project` (a dev-environment repository
   with a lifecycle), whose semantics differ from Multica's lightweight project grouping.
5. Minimal, documented modifications to existing Cloud files.

## Current completeness

Tracked in [06-implementation-log.md](06-implementation-log.md) and [FINAL.md](FINAL.md).
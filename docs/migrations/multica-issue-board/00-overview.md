# Issue Board Migration — Overview

## Migration goal

Migrate the **Issue Board (Kanban)** capability from the reference project **Multica**
(`multica-ai/multica`) into **Ora Cloud** (`ora-space/cloud`), re-implemented in Ora Cloud's
own architecture — additive-first, minimizing changes to existing Cloud code.

**In scope (two waves):**

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

## Non-goals (still deferred)

- **Attachments / file uploads** — Cloud has no file storage.
- **Issue↔project binding** — Cloud `project` is a dev-environment repository with a lifecycle;
  issues stay tenant-only (see [03-design.md](03-design.md) §Domain mapping).
- **Pull-request linking** — requires an external Git service integration.
- **WebSocket / realtime subscriptions** — Multica broadcasts issue events; HTTP refetch covers
  the current surface.
- **Bots / squad leads / autopilot triggers** — Cloud has no agent/squad/automation foundation.
- **Rich table/graph views** — the grouped view is a first-class endpoint, but arbitrary saved
  table/graph views remain out.
- Full pagination; the board list returns the tenant's issues in one page.

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
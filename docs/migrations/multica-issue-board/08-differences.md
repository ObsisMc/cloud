# Known differences — Multica vs. this migration

Deliberate divergences. Multica's exact behaviour is on the left; the Cloud re-implementation on the right, with the
reason. None of these lose board capability for the MVP.

| # | Multica | This migration | Reason |
| --- | --- | --- | --- |
| 1 | Status = **catalog table** `issue_status` (seeded canonical keys + custom statuses, lifecycle categories, icons, archival), `issue.status` validated in-app and mirrored by a SQL function. | **Simplified** `issue_statuses` catalog: key/name/category/color/icon/`is_system`/`position`, lazily seeded with the 7 canonical columns, soft-delete archival, system columns protected. | Wave-2 scope. Keeps the tenant-defined-column capability but drops Multica's richer lifecycle/workflow machinery and SQL-function mirroring. |
| 2 | Issue has `number` (per-workspace seq), `stage`, `metadata`, `properties`, `acceptance_criteria`, `context_refs`, `origin_*`, `first_executed_at`, `last_activity_at`, `start_date`/`due_date`. | `number` (per-tenant `max+1`) + `properties` (untyped jsonb KV). Still no `stage`/`metadata`/`acceptance_criteria`/dates/etc. | Wave-2 scope; only the board-relevant extras are kept. `properties` is intentionally schema-less (no typed custom-field system). |
| 3 | `project_id` (lightweight grouping). | Not present. | Cloud's `project` has different (dev-environment) semantics. |
| 4 | `assignee` is **polymorphic** (`assignee_type` member/agent/squad + `assignee_id`). | Single `assignee_user_id → users.id`. | Cloud has no agent/squad notion; board assignee is a user. |
| 5 | `id` + `revision` (`expected_revision`) for optimistic concurrency. | `id` + `version` with Cloud's `version(o,v)` 428/409 preconditions. | Reuse Cloud's existing convention. |
| 6 | POST/DELETE do **not** require idempotency keys. | POST (create/move) and DELETE require `Idempotency-Key`. | Cloud's public contract requires it for POST/DELETE ([public.go:38](../..//internal/core/public.go#L38)); keeping it is the "don't refactor Cloud" principle. |
| 7 | Create returns the issue directly (200); update/move return the updated issue. | Create returns `{resource: Issue}` (200); read/update/move/delete return the bare `Issue`. | Cloud's resource-wrapping convention for creation responses. |
| 8 | Move with `before_id`+`after_id`; **status change without anchors keeps the current position** (Multica relies on the client always sending anchors or a separate `UpdateIssueStatus` re-rank). | Status change without anchors re-ranks to the **top of the destination column** (`MIN(position)-1`). | Coalesces Multica's two paths (MoveIssue vs UpdateIssueStatus) into one, giving a safe, unsurprising result when a card is dragged into a fresh/empty column with no neighbours. |
| 9 | Rich list/table/graph/grouped query endpoints + WebSocket realtime + comments/labels/subscribers/reactions. | Comments, labels, subscribers, search (`?q=`), batch update, saved views, and a grouped view (`/issue-groups`) are implemented; no table/graph views, no reactions, no realtime. | Wave-2 scope. Grouped view is a first-class endpoint, not a full query/view engine. |
| 10 | `chi` router → handler → service → sqlc(pgx). | Gin allowlist → single `core.Store.Public` → raw SQL in one advisory-locked tx. | Ora Cloud's own architecture. |
| 11 | Reads may be filtered/permissioned by many attributes. | Board list is tenant-wide (all members see all issues); saved views are owner-scoped. | Multica's workspace board is shared; Cloud has no per-issue scoping layer yet. |
| 12 | Delete is a **hard** row erase; `ON DELETE SET NULL` orphans sub-issues. | Delete is a **soft** delete (`deleted_at`), and the handler explicitly nulls children's `parent_issue_id` to reproduce the same orphan-on-delete result. | Cloud soft-deletes resources (universal `deleted_at` convention); the explicit child-null keeps Multica's observable behaviour. |
| 13 | Comments carry reactions/edits history, mentions, attachments. | Plain `body` + author + timestamps; soft delete; no reactions/mentions/attachments. | Wave-2 scope; reactions/mentions need a richer identity/notification system. |
| 14 | Labels are per-workspace with color/description; deletion is hard. | Tenant labels (`name`+`color`); soft delete with a partial unique index so a deleted name can be reused. | Cloud's soft-delete convention. |
| 15 | Subscribers receive realtime event notifications. | Subscription is a stored relationship (read back over HTTP); no notification pipeline. | No event bus / realtime in Cloud. |
| 16 | Batch update may require per-issue revision checks. | Batch (`/issues/batch`) applies a patch atomically **without** per-issue `version` preconditions. | A bulk administrative action; keeps the single-issue optimistic-concurrency story intact while making batch usable. |
| 17 | Saved views/table views apply their filter server-side. | `issue_views.filter` is stored as a JSON object but **not** applied server-side — the client interprets it (currently the demo does not execute filters). | No query-builder engine in Cloud; the filter is an opaque spec for future/consumer use. |

## Behavioural parity kept

- 7 canonical statuses and 5 priorities (same values); the status catalog seeds these same columns in
  board order, so the first-read board is byte-identical to wave 1.
- Fractional `position` with before/after midpoint / before+1 / after-1, and column-top = `MIN-1`.
- Anchor resolution within the whole tenant (not the column).
- Parent link soft-clean (`ON DELETE SET NULL`), title required, defaults `todo`/`none`.
- Soft delete removes the issue from the board; no hard row erasure.
- Per-tenant `number` sequence and free-form `properties`.
- Comment/label/subscriber relationships (simplified: no reactions/mentions/attachments/notifications).
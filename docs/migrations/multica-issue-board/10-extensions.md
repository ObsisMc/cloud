# Second wave — board periphery (extensions)

Summary of the follow-up migration that brought the deferred board-periphery features from Multica
into Ora Cloud. Scope was agreed with the user ("第一档全部 + 第二档核心"): custom status columns,
comments, labels, subscribers, card numbers, custom fields, saved views, search, batch operations,
and grouped views. Explicitly out of scope: attachments, issue↔project binding, PR links, realtime
push, bots/squad leads, Autopilot — see [00-overview.md](00-overview.md).

## What landed

| Area | Deliverable |
| --- | --- |
| Database | `0006_issue_extensions.sql` — 6 new tables + `issues` ALTERs (`number`, `properties`, status format check). |
| Core (new files) | `issue_statuses.go`, `issue_comments.go`, `issue_labels.go`, `issue_subscribers.go`, `issue_views.go`. |
| Core (extended) | `issues.go` — catalog-driven status, `number`/`properties`, label embedding, `?q=` search, `batchUpdate`, `issueGroups`. |
| Dispatch | `public.go` — nested `/comments`/`/labels`/`/subscribers` sub-dispatch + new tenant-level cases. |
| Router | `router.go` — 24 new routes, new path params, `validField` array/object/integer cases. |
| OpenAPI | `Comment`, `Label`, `IssueStatus`, `IssueView` schemas; `Issue` gained `number`/`properties`/`labels`; regenerated `api/openapi.json`. |
| Tests | `integration/issue_extensions_test.go` — 5 end-to-end tests. |
| Demo | `cmd/demo-issue-board-web/` — numbers, label badges, detail drawer (comments/labels/subscribers/properties), search, group-by, custom-column entry. |

## 24 new endpoints

- **Status catalog**: `GET/POST /issue-statuses`, `PUT/DELETE /issue-statuses/{sid}`.
- **Labels**: `GET/POST /labels`, `PUT/DELETE /labels/{lid}`.
- **Saved views**: `GET/POST /issue-views`, `PUT/DELETE /issue-views/{vid}`.
- **Batch / groups**: `POST /issues/batch`, `GET /issue-groups?by=…`.
- **Comments**: `GET/POST /issues/{iid}/comments`, `PUT/DELETE /issues/{iid}/comments/{cid}`.
- **Issue labels**: `GET/POST /issues/{iid}/labels`, `DELETE /issues/{iid}/labels/{lid}`.
- **Subscribers**: `GET/POST/DELETE /issues/{iid}/subscribers`.

Plus enhancements to existing endpoints: `?q=` on `GET /issues`; `POST/PUT /issues` accept
`properties`; `Issue` responses carry `number`/`labels`.

## Key design decisions (recap)

1. **Status catalog is seeded lazily** — the migration cannot seed runtime tenants, so the 7
   canonical columns are inserted on first read/write via `ON CONFLICT DO NOTHING`, with positions
   that exactly reproduce wave 1's hardcoded `CASE` order. Board ordering becomes a `LEFT JOIN` on
   `issue_statuses.position`, unknown/archived statuses last.
2. **`number` is computed in-transaction** — `COALESCE(MAX(number),0)+1` under the existing global
   advisory lock, backstopped by `UNIQUE(tenant_id,number)`; soft deletes never reuse numbers.
3. **`properties` is schema-less jsonb** — validated only as "a JSON object" (`jsonb_typeof`), an
   intentional middle ground between "no custom fields" and a typed field-definition system.
4. **Labels soft-delete with a partial unique index** so a deleted name can be reused; the `Issue`
   wire object embeds `labels` to avoid N+1 on the board list.
5. **Batch update skips per-issue `version`** — a bulk administrative action (deliberate divergence,
   [08-differences.md](08-differences.md) #16).
6. **Saved-view filters are stored, not applied** — no query engine in Cloud; the client interprets
   the `filter` object.

## Verification

- `go build ./...` clean; `go test ./internal/... ./cmd/...` PASS (OpenAPI contract test green after
  regenerating `api/openapi.json`).
- Full integration suite against PostgreSQL 17 (Docker `cloud-postgres-1`) PASS, including
  `TestIssueStatusCatalog`, `TestIssueComments`, `TestIssueLabels`,
  `TestIssueNumbersPropertiesSearchBatch`, `TestIssueViewsGroupsSubscribers`.
- Migration upgrade path unchanged: the `integration/cloud_test.go` pre-upgrade baseline stays at
  `0001–0003`; `Store.Migrate()` auto-applies `0004/0005/0006`.
- Web demo rebuilt and exercises all new endpoints through the real router (dual-JWT, idempotency
  keys, advisory-locked transactions).

## Known limitations introduced

- Saved-view `filter` is not evaluated server-side.
- Batch has no optimistic-concurrency guard (by design).
- Search is `ILIKE` substring on title/description only — no full-text index, no label/number search.
- Subscriptions store intent only; nothing notifies.
- Custom status `category` is stored but not yet used to drive any behaviour (e.g. done-progress).

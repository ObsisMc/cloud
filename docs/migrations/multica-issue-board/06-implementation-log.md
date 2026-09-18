# Implementation log

Chronological record of what was changed and why.

## New files

| File | Contents |
| --- | --- |
| `internal/core/migrations/0005_issues.sql` | `issues` table (tenant-scoped, 7 statuses, 5 priorities, fractional `position`, `version`, soft-delete `deleted_at`, `updated_at`, creator/assignee/parent FKs) + `issue_board` and `issue_list` indexes. |
| `internal/core/issues.go` | Issue functions: `issue`, `positionOf`, `topPosition`, `issueList`, `resolveAssignee`, `resolveParent`, `createIssue`, `updateIssue`, `moveAnchor`, `moveIssue`, `deleteIssue`. |
| `integration/issues_test.go` | `TestIssueBoardCRUDAndMove` and `TestIssueBoardValidationAndIsolation`. |
| `cmd/demo-issue-board/main.go` | Runnable end-to-end demo (schema-scoped, self-cleaning) printing board snapshots. |
| `scripts/demo-issue-board.sh` | Wrapper: env setup → Docker readiness → `go run ./cmd/demo-issue-board`. |
| `docs/migrations/multica-issue-board/*.md` | This analysis/design/test document set + `FINAL.md`. |

## Modified files (minimal, additive)

| File | Change |
| --- | --- |
| `internal/api/router/router.go` | Added 6 issue `Route` entries; passed `IssueID: c.Param("iid")` into the `PublicRequest`. |
| `internal/core/public.go` | Added `IssueID` to `PublicRequest`; added GET (`readPublic`) and write (`Public` switch) dispatch for issues. |
| `internal/contract/openapi.go` | Added `Issue` schema; branched `responseSchema`/`inputSchema`/`optionalField` for issue routes. |
| `api/openapi.json` | Regenerated from source (generated artifact; the `M` status was a pre-existing LF/CRLF artifact). |

## Behaviour decisions (recap of the design)

- Scope = tenant (not Cloud `project`); creator is the authenticated user (structural FK to
  `tenant_memberships`); assignee is an optional active user.
- Create defaults `status=todo`, `priority=none`; new issue lands at top of its column (`MIN(position)-1`).
- Update is partial; `version` precondition (428/409); status change re-ranks to top of new column.
- Move: anchors (`beforeId`/`afterId`) resolve within the tenant; midpoint / `before+1` / `after-1` /
  column-top. `version` precondition; invalid anchor order or exhausted float precision → 409 `position_conflict`.
- Delete: soft delete + explicit orphan of children (`parent_issue_id = NULL`), reproducing Multica's
  hard-delete + `ON DELETE SET NULL` result.

## Verification performed

1. `go build ./...` — clean.
2. `go test ./internal/... ./cmd/...` — all pass (incl. the OpenAPI `TestPublishedOpenAPIIsValidAndCurrent`,
   after regenerating `api/openapi.json`).
3. `cloudctl migrate` against Docker PostgreSQL 17 (`ora`/`ora-local`/`ora` @ 127.0.0.1:55432) — migration
   `0005_issues.sql` applied; `\d issues` confirms table, CHECKs, FKs, and both indexes.
4. Full integration suite (`integration.test.exe` with `TEST_DATABASE_URL` + `REQUIRE_POSTGRES=1`) — PASS,
   including the new `TestIssueBoard*` tests and all pre-existing project/identity/concurrency tests.
5. Demo (`scripts/demo-issue-board.sh` / `go run ./cmd/demo-issue-board`) — board snapshots before/after moves.
6. Persistence across restart — `docker restart cloud-postgres-1`; migration record and `issues` table survive
   (pgdata volume).

## Second wave (extensions)

Chronological record of the board-periphery migration. Full scope and rationale:
[00-overview.md](00-overview.md) and [10-extensions.md](10-extensions.md).

### New files

| File | Contents |
| --- | --- |
| `internal/core/migrations/0006_issue_extensions.sql` | 6 tables (`issue_statuses`, `issue_comments`, `labels`, `issue_labels`, `issue_subscribers`, `issue_views`) + `issues` ALTERs (drop status CHECK → format check; add `number` + backfill + unique; add `properties`). |
| `internal/core/issue_statuses.go` | Status catalog: `statusKeyPattern`, `canonicalStatuses`, `seedIssueStatuses`, `resolveStatus`, `statusCatalogList`, `nextStatusPosition`, `createIssueStatus`, `updateIssueStatus`, `deleteIssueStatus`. |
| `internal/core/issue_comments.go` | `commentList`, `createComment`, `updateComment`, `deleteComment`. |
| `internal/core/issue_labels.go` | `labelList`, `createLabel`, `updateLabel`, `deleteLabel`, `attachLabels`, `issueLabelList`, `attachLabel`, `detachLabel`. |
| `internal/core/issue_subscribers.go` | `subscriberUser`, `subscriberList`, `subscribe`, `unsubscribe`. |
| `internal/core/issue_views.go` | `issueView`, `viewList`, `createView`, `updateView`, `deleteView`. |
| `integration/issue_extensions_test.go` | `TestIssueStatusCatalog`, `TestIssueComments`, `TestIssueLabels`, `TestIssueNumbersPropertiesSearchBatch`, `TestIssueViewsGroupsSubscribers`. |

### Modified files (additive)

| File | Change |
| --- | --- |
| `internal/core/issues.go` | Replaced the fixed status map with `resolveStatus`; `issue()` returns `attachLabels(…)`; `issueList` → `issueListItems` (LEFT JOIN `issue_statuses`, `?q=` ILIKE filter, per-item label attach); `createIssue` computes `number = max+1` and writes `properties`; `updateIssue`/`moveIssue` resolve status via catalog; added `batchUpdate` + `issueGroups` + `likePattern`. |
| `internal/core/public.go` | `PublicRequest` gained `CommentID`/`LabelID`/`StatusID`/`ViewID`/`Query`/`GroupBy`; nested `/comments`/`/subscribers`/`/labels` sub-dispatch inside the issue case; new `issue-statuses`/`issue-views`/`issues/batch`/`labels` cases; `readPublic` ordering updated for nested GETs. |
| `internal/api/router/router.go` | +24 routes; new param bindings; `validField` gained `ids` (array), `filter`/`properties` (object), `position` (integer). |
| `internal/contract/openapi.go` | `Comment`/`Label`/`IssueStatus`/`IssueView` schemas; `Issue` gained `number`/`properties`/`labels`; `isList`/`responseSchema`/`optionalField`/`inputSchema` branched for extensions. |
| `api/openapi.json` | Regenerated (`go run ./cmd/openapi`). |
| `cmd/demo-issue-board-web/main.go` | `/demo/config` now serves the live status catalog + label list + user id from the store. |
| `cmd/demo-issue-board-web/index.html` | Card `#number` + label badges, detail drawer (comments, subscribe, label attach, properties), search box, group-by switcher, new-column entry. |

### Verification performed

1. `go build ./...` — clean.
2. `go test ./internal/... ./cmd/...` — PASS (incl. OpenAPI contract test after regenerating `api/openapi.json`).
3. Full integration suite (`integration.test.exe` + `TEST_DATABASE_URL` + `REQUIRE_POSTGRES=1`) — PASS, including
   the five `TestIssue*` extension tests and all pre-existing project/identity/concurrency tests.
4. Migration `0006_issue_extensions.sql` applies cleanly on top of `0005`; `Store.Migrate()` auto-applies it (the
   upgrade test's hardcoded pre-upgrade baseline of `0001–0003` needed no change).
5. Demo (`scripts/demo-issue-board-web.sh`) rebuilt — numbers, labels, comments, search, groups, custom columns,
   and subscribe all exercise the real HTTP API.
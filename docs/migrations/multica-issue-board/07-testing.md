# Testing

## How to run

Unit tests (no database):

```bash
GOPROXY=https://goproxy.cn,direct go test ./internal/... ./cmd/...
```

Integration tests (require the Docker PostgreSQL):

```bash
# Compile once (App Control blocks go test's temp exe on this machine; GOCACHE/GOTMPDIR pinned into .local):
GOPROXY=https://goproxy.cn,direct \
  GOCACHE="$PWD/.local/gocache" GOTMPDIR="$PWD/.local/gotmp" \
  go test -c -o .local/gotmp/integration.test.exe ./integration

# Run the binary from the integration/ directory (it reads migrations via ../internal/...):
cd integration
TEST_DATABASE_URL='host=127.0.0.1 port=55432 user=ora password=ora-local dbname=ora sslmode=disable' \
REQUIRE_POSTGRES=1 \
../.local/gotmp/integration.test.exe
```

Demo:

```bash
bash scripts/demo-issue-board.sh        # terminal board snapshot demo
bash scripts/demo-issue-board-web.sh    # browser Kanban UI (wave-2 aware)
```

## Coverage

### `integration/issues_test.go`

`TestIssueBoardCRUDAndMove`:

- Create 3 issues; column-top `position` (`0` then `-1`).
- Board list in canonical column order (backlog→cancelled), then position, then id.
- Get single issue.
- Idempotent create replay returns the same id; changed body under the same key → 409.
- Partial update (title+priority) bumps `version`; missing version → 428; stale version → 409.
- Status-only move re-ranks to the destination column top (`MIN-1`).
- Anchored move computes the fractional midpoint (`-1.5`).
- Invalid status / invalid anchor → 400 without mutation.
- Soft delete sets `deletedAt`, removes from list, 404 on later GET.
- Persistence asserted via SQL (`count(*)`) through a separate connection.

`TestIssueBoardValidationAndIsolation`:

- Missing title / invalid status / invalid priority / invalid assignee / unknown field → 400.
- Cross-tenant read → 403 (`membership_required`).
- Parent link create; self-reference update → 400; parental delete orphans the child (SET NULL).

### `integration/issue_extensions_test.go` (second wave)

`TestIssueStatusCatalog`:

- First read lazily seeds the 7 canonical columns (board order, `isSystem`).
- Create a custom column → listed; invalid key / duplicate key → 400/409.
- Rename with `version` precondition (428 when absent); system column archival → `409 system_status_required`.
- Custom column archived → `deletedAt` set; archived key rejected on issue create (400).

`TestIssueComments`:

- Create/list/edit (`version` 428/409)/soft-delete; deleted comment leaves the list.
- Cross-tenant read → 403.

`TestIssueLabels`:

- Label CRUD; duplicate name → `409 label_conflict`.
- Attach → issue GET embeds `labels`; issue label list; detach → removed.
- Rename + delete label → detached from issues and unlisted.

`TestIssueNumbersPropertiesSearchBatch`:

- Per-tenant `number` increments 1,2,…; `properties` round-trips through create + GET.
- `?q=` hits title and description (case-insensitive), misses cleanly.
- Batch update across several issues applies status+priority atomically; invalid id → 400.

`TestIssueViewsGroupsSubscribers`:

- Subscribe/list/unsubscribe; subscriber list returns the user.
- Saved views create/list/update/delete (owner-scoped).
- Grouped view `by=priority` returns ≥2 `groups` buckets.

### Regression

The full integration suite (project lifecycle + durable recovery, identity/concurrency/membership,
stop/admission race, migration upgrade path) still passes with the three modified core files, so the
additive dispatch did not perturb existing routes. The migration upgrade test's pre-upgrade baseline stays at
`0001–0003`; `Store.Migrate()` auto-applies `0004/0005/0006`, so no test change was needed for 0006.

### Not covered (known)

- Concurrency stress on the same issue (the advisory lock serialises mutations; the version precondition is
  covered by the stale-version case, not by a parallel race test).
- Float-precision exhaustion of the midpoint (409 `position_conflict`) — behaviour exists but the extreme
  run-length test is not automated.
- Pagination — intentionally out of scope (board returns all issues).
- Saved-view `filter` execution — stored, not applied (by design, see [10-extensions.md](10-extensions.md)).
- Custom status `position` re-ordering of columns (update path exists; no dedicated board-order assertion).
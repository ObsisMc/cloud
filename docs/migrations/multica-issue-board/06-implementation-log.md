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

## Third wave (3A — issue-owned foundation)

First coding wave of the collaboration migration. Implements exactly **Step 2 — the issue-owned foundation** of
[12-collaboration-architecture.md](12-collaboration-architecture.md) and stops there (no Wave 3B). Purely additive
over `0001–0006`; edits no applied migration.

### New files

| File | Contents |
| --- | --- |
| `internal/core/migrations/0007_issue_collaboration.sql` | `issues` ALTERs (`assignee_type`/`assignee_id`/`project_ref` + backfill), `issue_comments` ALTERs (`parent_id`/`author_type`/`author_id`/`seq` + `author_user_id DROP NOT NULL` + backfill + `UNIQUE(issue_id,seq)`), and 3 new tables: `issue_runs`, `issue_activities`, `issue_context_refs`. |
| `internal/core/issue_runs.go` | `run`, `runList`, `createRun` (enqueue `status='queued'`, pending-executor dedup → 409, appends `run.enqueued` activity). |
| `internal/core/issue_activities.go` | `nextTimelineSeq` (Option-C: `GREATEST(MAX(comments.seq), MAX(activities.seq)) + 1`), `appendActivity`. |
| `internal/core/issue_context_refs.go` | `contextRefList`, `createContextRef`, `deleteContextRef`. |
| `integration/issue_collaboration_test.go` | `TestIssueCollaborationMigrationBackfills`, `TestIssueAssignmentAndProjectRef`, `TestIssueCommentThreadingAndSharedTimeline`, `TestIssueRuns`, `TestIssueContextRefsAndIsolation`. |

### Modified files (additive)

| File | Change |
| --- | --- |
| `internal/core/issues.go` | `assigneeRef` struct + `resolveAssignee`/`resolveProjectRef`; `createIssue`/`updateIssue`/`batchUpdate` write `assignee_type`/`assignee_id`/`assignee_user_id`/`project_ref`. |
| `internal/core/issue_comments.go` | `commentList` orders `seq, id`; `createComment` validates same-issue `parent_id` + allocates timeline `seq`. |
| `internal/core/public.go` | `PublicRequest` gained `RunID`/`ContextRefID`; write dispatch for `/runs` (POST) and `/context-refs` (POST/DELETE); `readPublic` branches for `/runs` and `/context-refs`. |
| `internal/api/router/router.go` | +6 routes (runs GET/POST, run GET single, context-refs GET/POST/DELETE); `RunID`/`ContextRefID` param bindings; `validField` `input` (object) case. |
| `internal/contract/openapi.go` | `IssueRun` + `ContextRef` schemas; `Issue` gained `assigneeType`/`assigneeId`/`projectRef`; `Comment` gained `authorType`/`authorId`/`parentId`/`seq`; `responseSchema`/`inputSchema`/`optionalField` branched for runs/context-refs. |
| `api/openapi.json` | Regenerated (`go run ./cmd/openapi`). |

### Verification performed

1. `go build ./...` — clean.
2. `go test ./internal/... ./cmd/...` — PASS (incl. the OpenAPI contract test after regenerating `api/openapi.json`).
3. Full integration suite (`integration.test.exe` + `TEST_DATABASE_URL` + `REQUIRE_POSTGRES=1`) — PASS, including the
   five new `TestIssue*` collaboration tests and all pre-existing project/identity/concurrency/board tests.
4. `0007_issue_collaboration.sql` applies cleanly on top of `0001–0006`; `Store.Migrate()` auto-applies it. The
   backfill test (`TestIssueCollaborationMigrationBackfills`) seeds legacy rows under a single tx (to satisfy the
   deferred `tenant_admin` constraint trigger) and asserts assignee/author/seq backfill.
5. Formatting: my new/edited files passed `go tool gofumpt -w -extra`. (Pre-existing CRLF-vs-LF full-file diffs on
   `store.go`/`auth.go`/etc. are a repo-wide line-ending artifact, unrelated to Wave 3A and intentionally left alone.)

## Wave 3B-0 (collaboration architecture alignment — docs only)

Pre-coding alignment pass. **No production code, no migration, no frontend change, no OpenAPI
behaviour change.** Its purpose: freeze the collaboration *interaction* semantics so Wave 3B-1 can be
implemented straight from the docs instead of re-deriving product meaning.

### Documents changed

| File | Change |
| --- | --- |
| `migrations/multica-issue-board/12-collaboration-architecture.md` | **New §37** (37.1–37.17) — the frozen interaction model (`@` = target selection; Human Mention / Agent Task / Team Task / Workflow Form modes; AI Assist flow; `Comment ≠ Interaction ≠ IssueRun` cardinality; Timeline projection; pending-run constraint; context persistence; fixture strategy; frontend model; `ConversationTarget` status; 3B-1/3B-2/3B-3 roadmap; open questions). **New §6.4** — the single canonical 14-port inventory (now **15** — 3B-2 added `FormDescriptorProvider`), plus the missing `CollaborationTargetResolver` and the new `CollaborationDirectory`/`InteractionDescriptor`/`ContextBuilder`/`ExecutionObserver`/`InputAssistProvider` port contracts in §6.2. **New §35.1** — step→sub-wave mapping. Debt fixed: the remaining unmarked `sim_*` remnants in §9/§26/§27/§28/§29 (and §9's heading), and the §25 "no target endpoints" wording that conflated *domain APIs* with the *Issues-facing target projection*. |
| `development/agent/architecture.md` | Port list replaced by a pointer to §6.4 (the three docs' lists had drifted); added a "schema-ready but not API-implemented" block and a §37 quick-reference table; new trap 7 (`Comment`/`Run` coupling). |
| `development/agent/api-reference.md` | Marked `Comment.authorType` as schema-ready/API-not and `issue_activities` as persistence-implemented/read-API-not; listed the 3B-1 planned routes. |
| `development/agent/database.md` | New "schema-ready vs. exposed" table for the collaboration tables. |
| `development/onboarding/progress.md` | 3B/3C roadmap rewritten as **3B-0 (done) / 3B-1 / 3B-2 / 3B-3**; frontend status corrected (runs are list-only, no create path); added the frontend gaps 3B-1 must close; timeline/author-actor rows marked accurately. |
| `INDEX.md` | Status updated; source-of-truth rows added for the interaction model (§37) and the port inventory (§6.4). |
| `13-frontend-migration.md` | Corrected the inaccurate "Run created / queued" claim; added a measured "Collaboration-UI starting point" table. |
| `00-overview.md`, `FINAL.md` | "Wave 3 = frontend" relabelled so it no longer collides with "Wave 3 = Issue Collaboration" used by 12-collab/progress. |

### No new documents

Deliberately: the doc system already had one source of truth per topic, so the interaction model went
into the existing collaboration architecture doc (§37) rather than a parallel file.

### Repository governance follow-up (recorded, not acted on)

`specs/` is described by `cloud/AGENTS.md` as "an independent Git repository" reachable via
`git -C specs`, but in the working copy at the time, that separate specs checkout was not present.
So the ADR deliverable required by the ADR-first rule currently had **no version-controlled home** in
this working copy. This was **not** fixed here — no `git init`, no `.gitignore` change, no repository
restructuring. It needs an owner decision.

### Verification performed

Docs-only change: no `go build` / test run was required or performed. Reviewed for: no active doc still
presents `0008`/`sim_*` as the plan; exactly one port list (12-collab §6.4); every "planned" item is
labelled as such; no fabricated API.

## Wave 3B-1 (collaboration interaction foundation)

First real end-to-end `@` collaboration chain, with **no real Agent/Team/Workflow/Runtime**: the
frozen §37 semantics (`@` = target selection; Mention → no run; Agent/Team → Task Mode, task required;
Workflow → 409 `workflow_not_available`) implemented straight from the docs. Migration `0008`
(`issue_interactions` spine) + the consuming-side ports (`CollaborationDirectory`, `ContextBuilder`,
`ExecutionDispatcher`, `ExecutionObserver`) + in-memory fixtures (`FixtureCollaborationDirectory`,
`DeterministicContextBuilder`, `MockExecutionDispatcher`), production default off. Run lifecycle
`queued→dispatched→running→completed(/failed)` flows through the real Issue API → IssueRun →
Dispatcher → adapter → observer → Activity → reply Comment (agent/team `author_type` via internal
path). Timeline read `GET /issues/{iid}/timeline`; interaction spine read
`GET /issues/{iid}/interactions`; `targets[]` on comment create; `GET /collaboration/targets?q=`.

Frontend: `@` Target Picker, Mention/Task modes, Workflow "本阶段不可用", and the Timeline-backed
Activity panel on the real API. Backend build / unit / integration / OpenAPI all green.

### Review & verification performed (pre-freeze pass, 2026-09-20)

Schema/security review (no code change needed):

- `0008_issue_interactions` stores **Issue-domain state only** (`issue_id, comment_id, target_type,
  target_id, mode, task, run_id, tenant_id, created_at`) — no Agent config, Team members/leader,
  Workflow definition, Runtime, Model, Prompt, Sandbox, Node, Process or PTY leakage.
- Cardinality holds: one comment → 0..N interactions → 0..1 initial run per interaction;
  `issue_interactions.run_id` is the *initial run reference*, never `comments.run_id`.
- `targets[]` accepts only `{type,id,task?}` (`validField` + OpenAPI `additionalProperties:false`), so
  a client cannot smuggle `authorType`; agent/team replies are written only through the internal
  `appendReplyComment` path. `ExecutionObserver` is a set of methods on `*Store` with **no HTTP route**.
- Fixtures are wired only in `cmd/ora-web` (documented dev edge server, dedicated `ora_web` schema) and
  the integration harness; `cmd/server` leaves them nil → Unavailable, no crash.

Results (all re-run, not carried over from the earlier summary):

- Backend: `go build ./...` PASS; `go test -count=1 ./internal/... ./cmd/...` PASS;
  `go run ./cmd/openapi` idempotent (hash unchanged) + `internal/contract` byte-check PASS;
  full integration suite against real PostgreSQL 17 PASS.
- Wave 3B-1 integration tests (8) PASS: `TestCollaborationTargetList`,
  `TestHumanMentionPersistsTypedTarget`, `TestAgentTaskRunsThroughMockExecution`,
  `TestTeamTaskRunsThroughMockExecution`, `TestCollaborationTargetValidation`,
  `TestContextBuilderBoundedAndDeterministic`, `TestSecurityCannotImpersonateActorAndFailureLifecycle`,
  `TestPendingRunDedup` (new — covers the frozen pending-run rule, previously untested).
- Frontend under the repo-required Node 24.21.0 (`npm ci`, 631 packages): `typecheck` PASS,
  `test` PASS (28 files / 76 tests, incl. the new `activity-panel.test.tsx` collaboration-UI test),
  `build` PASS, `check:modules` / `check:docs` / `check:dead` / `check:dup` PASS.
- `lint` (9 errors) and `format:check` (16 files) remain red but are **entirely pre-existing debt** in
  files this wave did not author — verified by running the same checks against the `HEAD` revisions.
  Wave 3B-1 introduces **zero** new lint/prettier violations.
- No `sim_*` tables, no mock domain tables created; `0007` untouched; additive forward migration only.

## Wave 3B-2 (workflow interaction design — docs only)

Pre-coding design pass for the Workflow Interaction Shell. **No production code, no migration, no
frontend change, no OpenAPI change.** Purpose: freeze the **Issues-facing** Workflow contract so the
next coding round implements from the docs instead of guessing — while leaving Workflow *internal*
architecture explicitly **UNKNOWN / BLOCKED ON EXTERNAL DESIGN**.

### Decisions frozen (authoritative in 12-collab §38)

| Area | Decision |
| --- | --- |
| Top-level UX | `@Workflow` → `mode=form` → `FormDescriptor` → dynamic form → optional Assist → **explicit Confirm** → `IssueRun`. `selecting ≠ executing`, `AI Assist ≠ execution`. |
| `FormDescriptor` | Issues-facing **rendering** descriptor `{formRef, title?, description?, fields[]}`; field `{key,label,type,required,description?,defaultValue?,placeholder?,options?}`. **Not** a Workflow schema, bound to no schema technology; **not** a capability DSL. |
| Field types | `text` / `textarea` / `number` / `boolean` / `select` / `multi_select` (first version). A `context_ref` selector is deferred. |
| Loading | `InteractionDescriptor.formRef` + a separate read-only `GET /collaboration/forms/{formRef}` — **not** embedded in the picker projection. |
| Versioning | None in v1; `formRef` is opaque and may encode a provider-side version; Confirm re-validates against the **current** descriptor. |
| Interaction state | **No status column** — `run_id IS NULL` = unconfirmed, `run_id != NULL` = confirmed; `issue_runs.status` owns execution. |
| Draft persistence | **Frontend-only** (Option A). No endpoint, no column; `input` is the future additive path. |
| Confirm | The single execution boundary. Idempotency via `Idempotency-Key`; concurrent confirm via a `WHERE run_id IS NULL` compare-and-set → `409 interaction_already_confirmed`. No distributed lock. |
| AI Assist | `InputAssistProvider` returns a **field-level patch** + suggested refs; suggest-only — never creates a run, never writes a permanent `IssueContextRef`. |
| Validation | Frontend = UX only; the Issues API re-validates on Confirm; Workflow domain validation stays with the future service. |
| Form values | Plain `{fieldKey: value}` object; **no** Workflow-specific columns. |
| Workflow output | `IssueActivity` with `actor_type='system'` + run/executor in `details`. **`workflow` is not added to `ActorRef`.** No node/raw logs in the Timeline. |
| Ports | **New `FormDescriptorProvider`** (inventory now 15). Reuse `ExecutionDispatcher` / `ExecutionObserver`; add `ObserveProgress` → `run.progress`. No `WorkflowDispatcher`. |
| API | `GET /collaboration/forms/{formRef}`, `POST /issues/{iid}/interactions/{ixid}/assist`, `POST /issues/{iid}/interactions/{ixid}/confirm`. Never `/workflows/*`. |
| Migration | One additive `0009`: `issue_interactions.input jsonb NOT NULL DEFAULT '{}'`. `0008` untouched. |
| Superseded | `409 workflow_not_available` (3B-1) is superseded by the descriptor-based unavailable model (§38.19). |

### Documents changed

| File | Change |
| --- | --- |
| `migrations/multica-issue-board/12-collaboration-architecture.md` | **New §38** (38.1–38.36) — the frozen Workflow Interaction design. Updated §0 status, §6.4 (added `FormDescriptorProvider` + corrected the stale "none of these interfaces exists yet" note), §37.6 (pointer to §38), §37.13 (new fixtures), §37.16 (3B-2 design frozen), §37.17 (Q3 resolved; Q5/Q6/Q7 partially resolved). |
| `development/onboarding/progress.md` | 3B-2 section rewritten from 🧭 待编码 → **📐 FROZEN DESIGN** with the decision table; header, open-questions and milestone timeline updated. |
| `development/agent/architecture.md` | New "Workflow interaction — quick reference" table; 3B-2 moved from "Planned" to "Design frozen"; 3B-1 marked IMPLEMENTED + VERIFIED. |
| `development/agent/api-reference.md` | New "Planned by Wave 3B-2" route table marked **not implemented**; `workflow_not_available` marked SUPERSEDED. |
| `development/agent/database.md` | Planned `0009` row + `issue_interactions` note (all marked NOT YET WRITTEN). |
| `13-frontend-migration.md` | Frontend component plan pointer for 3B-2. |
| `INDEX.md` | Status updated; port count 14 → 15; new source-of-truth row for §38. |

### Verification performed

Docs-only change: no `go build` / test run was required or performed. Reviewed for: no doc claims
3B-2 is implemented; every planned route/column is explicitly marked as not existing; exactly one port
inventory (§6.4); no `/workflows/*` surface introduced; `0008` unmodified; the Foundation's
IMPLEMENTED + VERIFIED status preserved.

## Wave 3B-2 (workflow interaction shell — implemented)

The design frozen earlier the same day was implemented as specified: `@Workflow → FormDescriptor →
dynamic form → optional AI Assist → Review → Confirm → IssueRun → mock execution → Timeline`. Full
record, deviations and evidence: **12-collab §38.37**.

### Added

| File | What |
| --- | --- |
| `internal/core/migrations/0009_issue_interaction_input.sql` | `issue_interactions.input jsonb NOT NULL DEFAULT '{}'` — the one designed column. |
| `internal/core/form_descriptor.go` | `FormDescriptor`/`FormField`/`FormOption`, descriptor validation, form-value validation, wire rendering, assist filtering, `workflowFormDescriptor` / `formDescriptorByRef`. |
| `internal/collab/collab.go` (extended) | `FixtureFormDescriptorProvider` (Security Review, 6 fields), `MockInputAssistProvider` (deterministic), workflow branch in `MockExecutionDispatcher`. |
| `integration/workflow_interaction_test.go` | 8 tests: unconfirmed interaction/no run; descriptor projection + 404s; provider 503 / malformed 500; assist determinism + zero side effects; confirm validation ×5 + one run + `interaction.input` + run snapshot + provenance + timeline shape + `seq`; idempotent replay + 409; scope/mode guards; assist unavailable. |
| `frontend/src/features/issues/components/{workflow-interaction-composer,dynamic-form-renderer,form-field-renderer,assist-suggestions,confirm-review}.tsx` | The Form Mode composer chain. |
| `frontend/src/features/issues/components/workflow-interaction.test.tsx` | 6 tests: descriptor-only rendering; unsupported type fails closed; required gate; assist apply/ignore without executing; suggested context ref applied only into the confirm payload; explicit Review before Confirm + Back. |

### Changed

| File | Change |
| --- | --- |
| `internal/core/collaboration.go` | `InteractionDescriptor.FormRef`; new `FormDescriptorProvider` + `InputAssistProvider` ports; `ContextInput.InteractionValues`; `ExecutionObserver.ObserveProgress`. |
| `internal/core/issue_interactions.go` | Workflow target now records a `mode='form'`, `run_id=NULL` interaction (the 409 is gone); new `confirmInteraction` (validate → build input → CAS `run_id` → enqueue) and `assistInteraction` (side-effect free). |
| `internal/core/issue_run_lifecycle.go` | `buildRunContext` takes interaction values + applied refs; `activityActor()` routes workflow activities to `system`; `runActivityDetails()`; `ObserveProgress`; workflow branch in `ObserveMessage`. |
| `internal/core/store.go` | `Store.Forms` / `Store.Assist` + transaction shadowing; `execRows` for the compare-and-set. |
| `internal/api/router/router.go`, `internal/core/public.go` | 3 new routes; `:formRef` / `:ixid` params; `values` / `contextRefs` in `validField`; dispatch branches. |
| `internal/contract/openapi.go` | `FormDescriptor`/`FormField`/`FormOption`/`AssistSuggestion`/`ContextRefRef` schemas; `InteractionDescriptor.formRef`; `IssueInteraction.input`; non-UUID `formRef` path param; 503 declared. |
| `cmd/ora-web/main.go`, `integration/cloud_test.go` | Wire the two new fixtures (dev/demo + tests only). |
| `integration/collaboration_interaction_test.go` | The workflow target no longer 409s; the atomicity assertions now expect the accepted comment. |
| `frontend/src/features/issues/{types.ts,api.ts}`, `components/{activity-panel,target-picker}.tsx` | Form types + `useFormDescriptor`/`useAssistInteraction`/`useConfirmInteraction`; composer integration; workflow targets selectable; progress/message activity rendering. |

### Verification performed

- Backend: `go build ./...`, `go vet ./...`, `go test -count=1 ./internal/... ./cmd/...` — PASS;
  `go run ./cmd/openapi` idempotent + `internal/contract` byte-check PASS; `gofumpt -l -extra` clean on
  every touched file (`go run ./cmd/checkformat` still flags only pre-existing CRLF debt in untouched
  files); full integration suite against real PostgreSQL 17 — PASS.
- Frontend (Node 24.21.0): `typecheck` PASS; `test` PASS (29 files / 85 tests); `build` PASS;
  `check:modules` / `check:docs` / `check:dead` / `check:dup` PASS. `lint` (9) and `format:check` (10)
  remain red **only** on pre-existing debt in files this wave did not author — **zero** new violations.
- Live demo smoke through `cmd/ora-web` + real PostgreSQL: full Form Mode flow verified, including
  **0 runs before confirm**, three rejected invalid confirms, exactly one run after confirm, `409` on a
  second confirm, same-key replay returning the same run, `system`-authored workflow activities, and
  **0** workflow-authored entries. Foundation regression re-checked live (mention/task/team).

### Deviations from the approved §38 design

1. Added `409 interaction_not_confirmable` (confirm/assist on a non-`form` interaction) — a distinct
   resource-state conflict rather than an input error.
2. Declared 503 on every route in the OpenAPI document (a port-backed route may answer
   `form_descriptor_unavailable` / `assist_unavailable`; the contract-validating transport rejects
   undeclared statuses).

No other divergence: the descriptor is never persisted, drafts stay frontend-only, no
`WorkflowDispatcher`/`WorkflowContextBuilder`, no `/workflows/*`, `ActorRef` not widened.
### Wave 3B-2a (composer draft model — product review revision)

Four product-review findings drove a UI model change; the backend changed with it.

| Finding | Fix |
| --- | --- |
| A form appeared without the user selecting a workflow | The form is now a **client-side draft**; nothing opens unless a target is staged in this session. |
| An abandoned workflow form still showed in the history | Selecting a workflow writes **nothing**; comment + interaction + run are created only at 确认执行. |
| A human mention had no message box | Every staged target (mention / agent / team) now has its own message box. |
| Targets needed an explicit submit | Each staged row has its own 提交 button; a workflow row is committed by 确认执行. |

Backend consequence: `POST /issues/{iid}/interactions/{ixid}/assist` was replaced by the **stateless**
`POST /issues/{iid}/collaboration/assist` (`{targetId, values}`), because assist must work before any
interaction exists. `internal/core/issue_interactions.go` (`assistWorkflow`), `router.go`,
`public.go`, `openapi.go` and the integration tests were updated; `confirm` is unchanged. Frontend:
new `pending-targets.tsx`; `target-picker.tsx` reduced to picking only; `workflow-interaction-composer.tsx`
became a draft form that creates the interaction at confirm time; `activity-panel.tsx` reordered to
history → comment box → staged targets. Frontend tests rewritten for the draft model (29 files / 86 tests).

Verification: `go build`, `go vet`, unit tests, OpenAPI byte-check and the full integration suite PASS;
frontend `typecheck` / `test` / `build` / all `check:*` scripts PASS with **zero** new lint or prettier
violations; live smoke confirmed 0 interactions and 0 timeline entries before confirm, assist writing
nothing, and the full timeline appearing only after 确认执行.

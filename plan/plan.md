# AgentRunDispatcher Implementation Plan

> Status: **Living design / execution record**  
> Scope owner: **B — Cloud business / orchestration**  
> Current target: **Phase 2 — Workspace Settlement (DESIGN REVIEW)**  
> Update rule: **Every implementation round must read and update this file.**

---

## 1. Purpose

This document is the controlled implementation plan for AgentRunDispatcher and the adjacent B-owned business orchestration.

It has four purposes:

1. Define the implementation sequence before code is written.
2. Record architectural decisions and ownership boundaries that code must follow.
3. Record new gaps or conflicts discovered during implementation before they are silently solved in code.
4. Provide an auditable history of what was planned, what changed, what was implemented, and what remains blocked.

This is a **living document**, but it is not a scratchpad. Any material change to architecture, transaction boundaries, ownership, state transitions, or public behavior must be recorded here before or together with the code change.

---

## 2. Mandatory Agent Working Protocol

Every Agent round must follow this order.

### Before editing code

1. Read this `plan.md` completely.
2. Read the applicable `AGENTS.md` files.
3. Check `git status --short` for every affected repository.
4. Identify the exact plan section being implemented.
5. Confirm that the requested work is inside the current phase scope.
6. If a new architectural conflict is found, stop that affected part and add it to **§13 Open Gaps / Decisions Needed** before attempting a workaround.

### During implementation

The Agent must not silently deviate from this document.

If implementation requires a design change, the Agent must update:

- **Decision Log**
- affected architecture/state/transaction section
- affected test plan
- current phase status

before considering the new design accepted for the round.

### At the end of every round

The Agent must update this file with:

- current phase status
- files changed
- decisions made
- new gaps discovered
- tests added
- gate results
- remaining dependencies
- next recommended phase

The final Agent report must explicitly state:

> `plan.md updated: yes/no`

If `no`, explain why no plan change was necessary.

### Git policy

The Agent must never:

- `git commit`
- `git push`
- create a PR
- stage changes unless explicitly instructed by the architect

When work reaches a stable commit boundary, the Agent only reports that fact. The architect decides whether and when to commit.

---

## 3. Architectural Source of Truth

Priority order:

1. Approved specs / ADRs
2. This `plan.md`
3. Existing repository architecture and established patterns
4. Current implementation

If code contradicts an approved ADR, the ADR wins unless the architect explicitly approves an ADR change.

If this document contradicts an approved ADR, record the conflict in **§13** and do not silently follow this document.

---

## 4. Established Decisions

The following decisions are already considered settled for the current implementation path.

### 4.1 Agent identity

For a real Space Agent run:

- `issue_runs.executor_type = 'agent'`
- `issue_runs.executor_id` is the semantic reference to `space_agents.id`
- no physical FK is added because `executor_id` is polymorphic across agent/team/workflow
- the referenced Agent must be active and belong to the correct tenant/space

### 4.2 Agent run snapshot

At run creation time, authoritative business state snapshots the Agent plugin identity/version into the run input.

Caller-provided input must not override authoritative Agent identity/version fields.

Historical runs must remain pinned even when the installed/desired plugin version changes later.

### 4.3 Project requirement

For the real `@agent` Task Mode path:

- the Issue must have a valid Project
- otherwise reject atomically with `409 issue_project_required`
- no comment, Timeline/activity, interaction, or IssueRun may survive the rejected transaction

Manual-run project requirements are governed by their own API/ADR contract and must not be expanded by inference.

### 4.4 Business/control ownership

B owns business state such as:

- `space_agents`
- Agent IssueRun business fields / phase transitions
- Thread business state

A owns control-plane machinery such as workspace/session execution control.

B→A interactions use named transaction-aware control-plane seams.

A→B interactions use `AgentRunHooks`.

Same PostgreSQL transaction does **not** imply same ownership.

### 4.5 B→A control-plane contracts

The B-side contract contains the equivalent of:

- `CreateRunWorkspace`
- `DeleteRunWorkspace`
- `EnqueueExecutionWork`
- `EnqueueThreadCommand`

Contracts are synchronous and transaction-aware.

The caller owns the transaction.

The A implementation must not open a replacement transaction for these business calls.

Production/default behavior before A is wired must fail closed, not fake success.

### 4.6 Busy semantics

Project busy is **retryable business state**, not a terminal error.

For Dispatcher Slice 1:

- busy must not use the existing `idleProject` panic/409 behavior
- busy must be representable as a normal result
- a busy AgentRun stays queued
- no workspace/provisioning state is committed

### 4.7 Dispatch-loop ownership

Current implementation direction:

**B-owned process/orchestration loop**

Reason:

- it scans B-owned `issue_runs`
- decides whether a queued business run should be dispatched
- calls A through the control-plane seam
- preserves/advances B-owned business state

A remains responsible for the control-plane action itself.

### 4.8 Concurrency baseline

Current Store transactions use a PostgreSQL advisory transaction lock, which serializes existing `transact` mutations across Cloud instances.

Dispatcher code must still use explicit state guards/CAS semantics for clarity, replay safety, and future resilience.

The global lock is **not** permission to hold long transactions.

---

## 5. Current System State

Completed foundations before Dispatcher Slice 1:

- B skeleton / migrations / AgentRun hooks skeleton
- `space_agents` lifecycle
- active-only Agent target resolution
- `issue_project_required` gate for real Agent Task Mode
- plugin identity/version run snapshot
- manual AgentRun identity hardening
- B→A control-plane contract
- fail-closed unavailable control-plane implementation
- deterministic injected control-plane test fake
- A→B `AgentRunHooks`

Known A-side dependencies still incomplete:

- real `CreateRunWorkspace` implementation
- real `DeleteRunWorkspace` implementation
- real `EnqueueExecutionWork` implementation
- real `EnqueueThreadCommand` implementation
- `create_workspace` plugin step
- terminal workspace operation → `RunWorkspaceSettled` wiring

These dependencies do not prevent Slice 1 from being implemented against the established contract/fake.

---

## 6. Target Architecture

High-level path:

```text
Real @agent / manual AgentRun creation
        │
        ▼
IssueRun(status=queued, phase=NULL)
        │
        ├── immediate post-commit dispatch attempt
        │
        └── B-owned ≤10s recovery scan
                 │
                 ▼
        AgentRunDispatcher.Dispatch(runID)
                 │
                 ├── stale/non-agent/not-queued → no-op
                 │
                 ├── project busy → remain queued
                 │
                 └── project idle
                       │
                       ▼
              CreateRunWorkspace(tx, ...)
                       │
                       ├── Busy → remain queued
                       ├── Error → rollback; remain queued
                       └── Accepted
                              │
                              ▼
                   phase=provisioning
                   status=dispatched
```

Later slices extend this architecture:

```text
provisioning
  → workspace terminal callback
  → RunWorkspaceSettled
  → starting
  → session work
  → running
  → delivering
  → releasing
  → done
```

Those later transitions are explicitly out of Slice 1.

---

## 7. Phase Plan

### Phase 0 — Foundations

Status: **DONE / ARCHITECTURALLY REVIEWED**

Includes:

- schema skeleton
- business hooks skeleton
- Agent roster lifecycle
- authoritative Agent resolution
- project gate
- run snapshot
- manual AgentRun hardening
- A seam contracts

### Phase 1 — Dispatcher Slice 1: Claim + Busy + Retry Loop

Status: **DONE / ARCHITECTURALLY REVIEWED**

Goal:

Safely move only eligible queued AgentRuns to provisioning when workspace creation is accepted.

Implementation delivered (see §8 and Slice 1 tests T1–T9):

- real `AgentRunDispatcher.Dispatch` (eligible, busy-safe, CAS-guarded claim)
- non-panicking project-busy predicate
- authoritative handling of `CreateRunWorkspace` Busy result
- queued → provisioning/dispatched guarded transition
- replay/stale protection
- routing for real Space Agent without breaking legacy/team/workflow dispatch
- one-shot bounded queued AgentRun scan
- B-owned ≤10s retry loop with graceful-shutdown integration
- deterministic DB tests without real sleeps

Explicitly excluded:

- `RunWorkspaceSettled`
- provisioning → starting
- session work
- workspace failure cleanup
- Thread
- delivery

### Phase 2 — Workspace Settlement

Phase 2A (B-owned settlement core): **DONE / ARCHITECTURALLY REVIEWED** — see §18 and the Phase 2A round record in §15.

Phase 2B (A-side terminal caller wiring, plugin step): **NOT STARTED** — gated by G-002/G-003.

Goal:

Handle terminal create-workspace evidence inside the A control-plane transaction and call B business hook.

Expected work:

- terminal A→B hook wiring (G-003, Phase 2B — not wired this round)
- `RunWorkspaceSettled`
- ready → starting
- failed/unavailable → releasing/failed
- delete-workspace declaration
- rollback/replay tests

Phase 2A delivered: `RunWorkspaceSettled` settlement core, CAS `provisioning→starting`, `releasing/failed(workspace_unavailable)`, cancel-before-settle `releasing/cancelled(deliveryState=skipped)`, same-transaction `DeleteRunWorkspace` declaration, replay/stale no-ops, T2-1..T2-10 (T2-3 DEFERRED to Phase 2B/G-002).

Blocked in production by A-side terminal wiring and plugin-step correctness (G-002, G-003) — both Phase 2B.

### Phase 3 — Session Start

Status: **PLANNED**

Goal:

Release exactly one AgentSession execution work item from `starting`.

Expected work:

- fixed first prompt generation from run snapshot
- `EnqueueExecutionWork`
- immutable pinned plugin/version use
- exactly-once/replay behavior

### Phase 4 — Thread / Running Lifecycle

Status: **PLANNED**

Includes:

- Thread entries
- takeover hooks
- running transitions
- Thread command control seam
- API/SSE work

### Phase 5 — Delivery / Releasing / Done

Status: **PLANNED**

Includes:

- session end
- delivery settlement
- cleanup
- workspace deletion
- terminal states

---

## 8. Slice 1 Detailed Technical Design

### 8.1 Eligibility

Dispatcher only operates on rows satisfying the equivalent of:

```text
executor_type = 'agent'
status = 'queued'
phase IS NULL
```

Non-agent and already-advanced runs are no-op.

### 8.2 Dispatch transaction

Each dispatch attempt uses one short transaction.

Allowed inside the transaction:

- load run
- validate state
- load Issue/Project identity
- perform non-panicking busy check
- call transaction-aware `CreateRunWorkspace`
- apply guarded B business transition

Forbidden inside the transaction:

- sleep
- retry delay
- long polling
- channel wait
- goroutine synchronization
- external asynchronous workflow waiting

### 8.3 Busy check

Two layers are required.

#### Layer A — database/preflight predicate

A non-panicking predicate checks whether the Project currently has an active operation.

Purpose:

- avoid unnecessary seam calls
- preserve existing business semantics

This predicate must not use `idleProject` if that helper expresses busy via panic/409.

#### Layer B — authoritative seam result

`CreateRunWorkspace` may still return Busy because state can change after precheck.

That result is authoritative.

If Busy:

```text
status remains queued
phase remains NULL
workspace_id remains NULL unless the established contract explicitly states otherwise
transaction commits normally
```

### 8.4 Accepted transition

Only an accepted `CreateRunWorkspace` result may cause:

```text
phase = provisioning
status = dispatched
```

The control-plane declaration and B-side transition must commit atomically in the same transaction.

If the seam returns an error or the phase update fails, the transaction rolls back.

### 8.5 Replay / duplicate calls

The state transition must use an explicit state guard/CAS equivalent.

A second Dispatch after a successful transition must not:

- invoke workspace creation again
- create a duplicate operation
- regress state

Database uniqueness/idempotency rules remain a second line of protection.

### 8.6 Immediate dispatch

After the run-creation transaction commits, a real AgentRun may receive an immediate Dispatch attempt.

This is a latency optimization only.

Correctness must not depend on it.

A process crash after enqueue must be recoverable solely from PostgreSQL by the background scan.

### 8.7 Retry scan

Implement a one-shot function with semantics similar to:

```text
DispatchQueuedAgentRunsOnce(ctx)
```

Responsibilities:

1. fetch a bounded deterministic batch of eligible run IDs
2. finish the scan/read transaction
3. dispatch each run independently

Do not dispatch all runs inside one transaction.

Suggested deterministic order:

```text
created_at, id
```

Use repository precedent for batch size. If no precedent exists, record the chosen bound as an implementation choice in the Decision Log.

### 8.8 Retry loop

The loop repeatedly runs the one-shot scanner with cadence ≤10 seconds.

Reuse existing loop/sleep/shutdown abstractions where practical.

Tests must not wait real wall-clock intervals.

Sleep always occurs outside database transactions.

### 8.9 Error handling

#### Busy

Expected state, not an error.

Do not mark failed.

#### Control-plane unavailable

Retryable infrastructure/dependency condition.

Run remains queued.

Do not mark failed in Slice 1.

Avoid noisy 10-second error flooding; follow existing logger conventions.

#### Unexpected internal error

Rollback transaction.

Run remains queued unless an approved ADR explicitly specifies another outcome.

No automatic terminal state is introduced in Slice 1.

---

## 9. Slice 1 Test Plan

All behavioral tests should use PostgreSQL integration infrastructure unless an existing lower-level test is sufficient for a pure contract helper.

Required tests:

### T1 — Idle Project Accepted

- active Agent
- valid Project
- queued run
- fake `CreateRunWorkspace` → accepted

Expect:

- seam called once
- current transaction passed
- `phase=provisioning`
- `status=dispatched`

### T2 — Busy Project Stays Queued

Seed an active operation for the Project.

Expect:

- run remains queued
- phase remains NULL
- no provisioning transition
- no terminal error

### T3 — Busy Then Retry

First attempt busy.

Complete/remove active operation.

Run one deterministic scan.

Expect accepted dispatch and provisioning transition.

### T4 — Seam-level Busy Race

DB precheck observes idle.

Injected `CreateRunWorkspace` returns Busy.

Expect run remains queued.

### T5 — Seam Error Rollback

Injected seam returns error.

Expect:

- transaction rollback
- run queued
- no partial B transition

### T6 — Replay

First attempt accepted.

Second Dispatch same run.

Expect:

- no second seam call
- state unchanged
- no duplicate declaration

### T7 — Non-Agent Ignored

Queued team/workflow run must not be selected by Agent retry scan.

### T8 — Scan Eligibility / Bound

Only eligible queued AgentRuns are returned by one-shot scan.

Verify deterministic ordering/batch behavior where practical.

### T9 — Immediate Post-Commit Route

If routing can be added without widening the slice:

- create a real AgentRun
- inject accepted control seam
- confirm immediate post-commit Agent dispatch

If this requires unrelated restructuring, defer it and record the reason.

---

## Phase 2 — Workspace Settlement 详细设计（DESIGN REVIEW）

> Round: 2026-09-30 · Status: **Phase 2A implemented / verified** (settlement core); **Phase 2B not started** (A-side caller wiring = G-003, plugin step = G-002).  
> Scope of this line: the design in §2.1–§2.15 was implemented for the **B-owned settlement core** in Phase 2A (white-box, package `core`, no production caller); the A-side call-site wiring and plugin-step-gated `agent_plugin_unavailable` path remain deferred to Phase 2B. Design itself is unchanged; only the round status marker moved from DESIGN REVIEW to implemented.  
> Authoritative ADR source of truth for this section: IssueRun root `0-agent-run-in-disposable-isolated-workspace.md` (D3/D4/D6/D7), plugin-step `20260928-plugin-step-and-run-workspace-release.md` (D1/D2/D4), plugin-marketplace `20260928-node-executes-plugin-installs.md` (D3), controller-integration `20260928-agent-run-executions-thread-and-upload-grants.md` (D6, invariant 7). see §3.

Phase 2 turns terminal create-workspace evidence into the business transition `provisioning → starting` (workspace ready) or `provisioning → releasing` (creation failed, its agent plugin unavailable, or the run was cancelled before any session existed). The single entry point is the A→B hook `RunWorkspaceSettled(t *transaction, run Object, ready bool) error`, invoked from the A-side operation-terminal transaction and running on that caller-owned transaction (controller-integration D6: the hook does not open its own transaction). Phase 2 = settle + business transition + delete-workspace declaration. It does **not** release the session work item (Phase 3, D-010) and does **not** execute the delete operation to completion (Phase 5).

### 2.1 Authoritative Terminal Evidence

- The authority that a run workspace's creation is **ready** or **failed** is the `create_workspace` operation reaching a terminal outcome. In the current A-side engine, that terminal is `UPDATE operations SET step='done', state='succeeded'` (with `result.resourceId`) for success (control.go `advance`), or `state='failed'` (and `blocked` > 30 min, IssueRun D3) for failure.
- Workspace object readiness is `workspaces.observed_state='ready'` plus `admission_open=true` (`openWorkspace`). Per the plugin-step ADR D1 the admission must open only **after** the plugin step; that is not yet true in the current engine (G-002).
- The settle hook must be invoked only at the terminal outcome of a **run-workspace** `create_workspace` — identified by the workspace carrying `issue_run_id NOT NULL`. Non-run create_workspace operations (main / interactive isolated) do **not** call the hook.
- Nothing in the settle decision depends on Controller or Node in-memory "next step": it is a pure function of committed PostgreSQL state (IssueRun D3 "没有任何阶段依赖 Controller 内存中的下一步").

### 2.2 create_workspace completion contract

Current engine (control.go `advance`) completion path:

```text
sandbox → node → clone → openWorkspace → done
```

The plugin step ADR D1 target completion path:

```text
sandbox → node → clone → plugin → done   (admission opens only after plugin)
```

The `plugin` case already exists in `advance`, but only for `install_plugin`/`remove_plugin` operation kinds; it is **not** on the generic `create_workspace`/`create_project`/`start` path. Consequence recorded, not fixed here: under the current engine, “operation succeeded” does **not** imply the run's agent plugin is installed, so the settle-level agent-plugin check (§2.6) is gated behind G-002 and is vacuous until that lands. The settle hook itself is engine-agnostic: it reads the terminal and the plugin-instance table, so it remains correct under both the old and the target step tables.

### 2.3 A→B hook call site

- Caller: the A-side operation terminal transaction (the same transaction that advances the operation to `done`/`failed`), for a `create_workspace` whose workspace has `issue_run_id` set. This matches controller-integration D6: `runWorkspaceSettled` is a business hook called from “运行 Workspace 的 `create_workspace` 进入终态的事务”.
- Transaction: the caller's `*transaction` — A-owned. The hook must not open, commit, or roll back its own transaction.
- Atomicity: the operation terminal write and the hook's business transition commit (or roll back) together. If the hook returns an error the whole transaction rolls back, the Controller does not ack the terminal event, and the Node replays it; the hook must therefore be deterministic on the same event/evidence set (controller-integration D6 invariant: “钩子返回错误时整个事务回滚，Controller 不确认事件，Node 会重放；因此钩子必须对同一事件集合是确定的”).
- Replay: `RunWorkspaceSettled` must behave identically if invoked again with the same committed evidence (idempotent via the phase CAS, §2.8).
- Not-yet-present in code today: `RunWorkspaceSettled` has zero callers (G-003). A real A seam (G-001) plus this call site is what a production run needs; the B-side design and tests use an injected seam that reaches the same hook.

### 2.4 ready transition

`RunWorkspaceSettled(t, run, ready=true)` does:

```sql
UPDATE issue_runs SET phase='starting', version=version+1, updated_at=now()
WHERE id=$1 AND phase='provisioning' AND deleted_at IS NULL
```

- `status` stays `dispatched` (the run is mid-flight, not started — the session begins only with the first Thread event, Phase 4).
- CAS on `phase='provisioning'` is the replay/stale guard.
- No Timeline activity is specified by the ADRs for the internal `provisioning → starting` edge; entering this non-terminal phase is not a `run.*` activity. To keep the timeline unambiguous, the hook does **not** append an activity on the ready path. (If the architect wants a `run.provisioned`-style marker, raise it before implementation.)
- Per D-010, Phase 2 stops at `starting`; the session work item is not enqueued.

### 2.5 failure transition

`RunWorkspaceSettled(t, run, ready=false)` carrying a genuine creation failure (operation `failed`, or `blocked` > 30 min) does (IssueRun D3):

```sql
UPDATE issue_runs SET phase='releasing', status='failed', failure_reason='workspace_unavailable',
                      completed_at=now(), version=version+1, updated_at=now()
WHERE id=$1 AND phase='provisioning' AND deleted_at IS NULL
```

- Append the `run.failed` Timeline activity with `failureReason = workspace_unavailable` (IssueRun D4).
- In the same transaction, declare the run-workspace `delete_workspace` operation via the B→A seam `DeleteRunWorkspace` (idempotent by `(issue_run_id, kind)`). Declaration only; the delete operation's execution (sandbox terminate/cleanup → done) is Phase 5.

### 2.6 plugin failure

- plugin-marketplace D3: if the run workspace's instance of the run's **required agent plugin** is `failed`, the run itself fails with reason `agent_plugin_unavailable` — even though the Agent's `space_agents` row survives (it depends on Space-level selection/aggregate, not on any one workspace instance). Non-agent plugin failures recorded on the workspace do **not** block readiness (plugin-step D2).
- Detection point: inside the settle hook, after the operation reads terminal `done`, resolve the run's required agent plugin (from `space_agents.plugin_id`, version pinned in the run input) and query `workspace_plugin_instances` for that `(workspace_id, source_namespace, identifier)`. If `observed_state='failed'` (or the pinned version is absent) → settle as **not ready** and route to §2.5 with `failure_reason='agent_plugin_unavailable'` and a delete declaration; otherwise settle ready.
- Gate: meaningful only after G-002 (plugin step) is wired; until then no plugin-instance rows exist and the check is vacuous. The hook must therefore treat “no plugin-instance record, but terminal succeeded under a pre-plugin engine” as ready (legacy engine compatibility), and only apply the stricter check once the plugin step exists. This dual behavior is recorded, not enforced in this round.

### 2.7 cancel during provisioning

- Cancel is a request, not an immediate state change (IssueRun D6): the user cancels → Cloud records `issue_runs.cancel_requested_at` and (once a session exists) issues a Thread/end command. When cancellation lands in `provisioning`/`starting` with **no session yet**, Cloud goes straight to release with `cancelled` and `deliveryState = skipped`.
- The settle hook covers the no-session case. Before applying ready/failed, it reads `cancel_requested_at`:

```sql
-- precondition: phase='provisioning'
UPDATE issue_runs SET phase='releasing', status='cancelled', completed_at=now(),
      result = COALESCE(result,'{}') || '{"deliveryState":"skipped"}'::jsonb,
      version=version+1, updated_at=now()
WHERE id=$1 AND phase='provisioning' AND cancel_requested_at IS NOT NULL AND deleted_at IS NULL
```

- Append the `run.cancelled` Timeline activity and, in the same transaction, declare the delete_workspace operation (`deliveryState=skipped` means no delivery before release).
- The write that sets `cancel_requested_at` is owned by a later slice (G-005); Phase 2 only consumes it.

### 2.8 replay / stale callback

- Base mechanism: every mutation is guarded by `WHERE phase='provisioning'`. A duplicate terminal event or a replayed settle after a rollback finds the run no longer in `provisioning` (already `starting`/`releasing`) and is a no-op.
- Concurrency: the global advisory transaction lock serializes Store mutations (baseline), so concurrent terminal take-overs cannot both pass the guard; the CAS is defense-in-depth, matching D-008's principle that correctness lives in each transaction.
- Stale / orphan callbacks (run past provisioning) are ignored.
- Determinism: ready/failed/plugin/cancel are decided only from committed state, so the same terminal evidence always settles the same way — required for Node replay after a hook-error rollback.

### 2.9 workspace_id binding

- The run-workspace idempotency identity is `(issue_run_id, kind='isolated')`, generated by Cloud (B), not via the public idempotency key (plugin-step D4).
- `issue_runs.workspace_id` ↔ `workspaces.issue_run_id` are mutually-unique references (IssueRun D2); migration 0018 added `workspaces.issue_run_id` with a unique constraint.
- The settle hook receives the run `Object` (already carrying `workspace_id`); it does **not** derive the workspace identity from Controller/Node input.
- Public API must return `404` and hide run workspaces (plugin-step D4, invariant 5); that public-surface change is a later slice, but the binding (who owns the id) is fixed now: B owns the `(issue_run_id, kind)` identity; A carries it into the create/delete operation.

### 2.10 delete / cleanup boundary

- Entering `releasing` (any of §2.5/§2.6/§2.7 paths) declares the run workspace's `delete_workspace` via `DeleteRunWorkspace`, idempotent by `(issue_run_id, kind)` (plugin-step D4).
- Phase 2 declares; the delete operation's execution (quiesce → terminate → cleanup, observed_state → `deleted`, effect cleanup per plugin-step D3 no-new-substrate-effect) and the eventual `done` are later slices (Phase 5) and follow the pre-existing delete_workspace control path.
- Audit/soft-delete: `deleteWorkspace` sets `observed_state='deleted'`, retaining references for audit/recovery; run workspaces keep this behavior.
- Public API cannot start/stop/delete a run workspace (404); that enforcement boundary is recorded for a later slice, not implemented here.

### 2.11 transaction model

One transaction per settle:

1. A-side terminal write: the operation's `create_workspace` terminal outcome is fixed in the operation row(s).
2. A→B hook `RunWorkspaceSettled(t, run, ready)` runs on the same transaction:
   - validates the run is a real Agent run in `provisioning` (else no-op / stale);
   - reads `cancel_requested_at`, the agent-plugin instance (once G-002), and decides ready/failed/plugin-unavailable/cancelled;
   - performs the §2.4–§2.7 CAS phase/status/failure_reason UPDATE, appends the relevant Timeline activity, and (releasing paths) calls `DeleteRunWorkspace`.
3. All-or-nothing: a hook error is surfaced as a transaction failure (the existing control-flow boundary converts it), rolling back steps 1–2 together. The Controller does not ack; the Node replays; the hook re-runs deterministically.

The hook returns errors only for genuine failures. Busy, cancelled, plugin-unavailable, and failure are **values** that commit a deterministic transition — they are never returned as errors (matching D-009's busy-is-normal principle and controller-integration D6's hook contract).

### 2.12 concurrency proof

- Writers: the global advisory transaction lock makes the settle transaction the only writer of this operation+run during the settle. Concurrent duplicate terminal take-overs are serialized by the lock.
- CAS: even if somehow two settle calls both read `phase='provisioning'`, only the first `UPDATE ... WHERE phase='provisioning'` advances; the second updates 0 rows and is a no-op. No double transition.
- Stale callback on an already-`starting`/`releasing`/later run: no-op.
- Partial failure cannot commit: the operation terminal write and the business transition (+ delete declaration) are one transaction; a mid-way error rolls back all of it.
- Replay with same evidence: deterministic outcome; dedup guaranteed by the CAS.
- Bound on harm: if the A seam is unwired at settle time (fail-closed), the hook returns a real error → rollback → run stays `provisioning` → the B-owned recovery/(for later slices) retry surfaces it. Phase 2 does not mark the run failed on a missing A seam.

### 2.13 error matrix

| Case (settle evidence) | Decision | Phase | status | failure_reason | delete declared | deliveryState |
|---|---|---|---|---|---|---|
| terminal `succeeded`, agent plugin installed, no cancel | ready | `starting` | `dispatched` | – | no | – |
| terminal `failed` / `blocked` > 30 min | creation failed | `releasing` | `failed` | `workspace_unavailable` | yes | – |
| terminal `succeeded` but run's agent-plugin instance `failed` (G-002 gated) | plugin unavailable | `releasing` | `failed` | `agent_plugin_unavailable` | yes | – |
| `cancel_requested_at` set, no session, still `provisioning` | cancel-before-settle | `releasing` | `cancelled` | – | yes | `skipped` |
| run not in `provisioning` (replay / stale / orphan) | no-op | unchanged | unchanged | – | no | – |
| genuine hook/A-seam error (unwired, conflict) | rollback | stays `provisioning` | unchanged | – | no (rolled back) | – |

- `workspace_unavailable` and `agent_plugin_unavailable` are the two B-side failure reasons Phase 2 can write; both lead to `releasing` + delete declaration. No new error code is introduced by Phase 2.

### 2.14 test plan (Phase 2)

White-box tests must live in package `core` because the seam/hook take the unexported `*transaction`; database is real PostgreSQL with an injected A-side seam, exactly like Slice 1's `agent_run_dispatcher_db_test.go`. The A engine is not required to be real for these tests — the hook is driven by the terminal state the test stages.

| T# | Scenario | Must hold |
|---|---|---|
| T2-1 | Ready | terminal `succeeded`, no cancel → `phase='starting'`, `status='dispatched'`, no activity, no delete |
| T2-2 | Workspace failure | terminal `failed` → `phase='releasing'`, `status='failed'`, `failure_reason='workspace_unavailable'`, `run.failed` activity, delete declared once |
| T2-3 | Plugin failure | agent-plugin instance `failed` (staged) → `releasing`/`failed`/`agent_plugin_unavailable`, delete declared once |
| T2-4 | Hook rollback | injected hook error → whole tx rolled back (operation terminal + phase + delete all absent), run still `provisioning`; replay after fix succeeds |
| T2-5 | Replay ready | settle ready called twice → one transition, second no-op, exactly one declaration |
| T2-6 | Replay failure | settle failure called twice → one `releasing`, one delete declaration, second no-op |
| T2-7 | Stale callback | run already `starting`/`releasing` → settle is a no-op (no double move) |
| T2-8 | Cancel-before-settle | `cancel_requested_at` set, still `provisioning` → `releasing`/`cancelled`, `deliveryState=skipped`, delete declared (IssueRun D6) |
| T2-9 | Workspace identity binding | create/delete idempotent by `(issue_run_id, kind)` — replay of the settle's declare produces one workspace/one operation (deduplication); public-surface 404 is a later slice, not asserted here |
| T2-10 | A/B atomicity | terminal write + business transition + delete declaration commit together (T2-1 proving commit); T2-4 proving rollback leaves all three absent — the pair is the atomicity proof |

Blocked-by-decision note: none. All matrix rows in §2.13 resolve to an approved decision (`IssueRun D3/D6`, `plugin-marketplace D3`, D-010). No `BLOCKED_BY_DECISION` is required for Phase 2's state machine.

#### Test evidence — Phase 2A (2026-09-30)

White-box `TestPhase2A*` suite in `internal/core/agent_run_settle_db_test.go` (package `core`, real PostgreSQL, injected `stubAgentRunControlPlane` extended with `DeleteRunWorkspace` tracking). All drive the hook through the unexported `*transaction` exactly as the A caller would, with `panic(databaseFailure)` on hook error to model A-side rollback.

| T# | Evidence status | Test | Note |
|---|---|---|---|
| T2-1 | COVERED — PASS | `TestPhase2ASettleReady` | `provisioning→starting`, status stays `dispatched`, ready=true + no cancel. |
| T2-2 | COVERED — PASS | `TestPhase2ASettleWorkspaceFailure` | `releasing`/`failed`/`workspace_unavailable`, `run.failed` activity, delete declared once in same tx. |
| T2-3 | DEFERRED_TO_PHASE_2B_G002 | – (not written) | No plugin-step evidence exists (G-002); boolean-only hook cannot distinguish; not expressed, per §18.2.6. |
| T2-4 | COVERED — PASS | `TestPhase2ASettleHookRollback` | Hook error → entire tx rolled back (phase + delete absent), run still `provisioning`; replay after the fix succeeds. |
| T2-5 | COVERED — PASS | `TestPhase2ASettleReplayReady` | Ready settled twice → one transition, second no-op, exactly one delete declaration. |
| T2-6 | COVERED — PASS | `TestPhase2ASettleReplayFailure` | Failure twice → one `releasing`, one deletion, second no-op. |
| T2-7 | COVERED — PASS | `TestPhase2ASettleStaleCallback` | Run already `starting`/`releasing` → settle no-op (no regression, no new delete). |
| T2-8 | COVERED — PASS | `TestPhase2ASettleCancelBeforeSettle` | `cancel_requested_at` set, still `provisioning` → `releasing`/`cancelled`, `result.deliveryState=skipped`, delete declared (IssueRun D6). |
| T2-9 | COVERED — PASS (identity) | `TestPhase2ASettleWorkspaceBinding` | Settlement re-reads/binds authoritative `workspace_id`; does not regenerate or overwrite; delete declaration carries the same workspace id. Replay-deduplication of the delete is asserted via the injected seam count. |
| T2-10 | COVERED — PASS | `TestPhase2ASettleAtomicity` | Terminal write + business transition + delete declaration commit together (T2-1 proves commit; T2-4 proves rollback leaves all three absent). |

Additional coverage: `TestPhase2ASettleInvariantErrors` (nonexistent run / `executor_type != agent` / tenant mismatch → error → rollback, valid run untouched) and `TestPhase2ABusinessHooksDelegateAndFailClosed` (`businessAgentRunHooks` delegates `RunWorkspaceSettled`; every other hook fails closed via `UnavailableAgentRunHooks`).

G-002 gates T2-3 and production plugin-specific settlement; G-003 stays open (production caller unwired). No migration/schema change; the cancel rule writes `result.deliveryState` via JSONB per IssueRun D4 (no `delivery_state` column to add).

### 2.15 remaining A-side dependencies

Before Phase 2 is production-meaningful, the following A-side / cross-owner items must exist (all recorded in §13; none implemented this round):

- G-001 — real `CreateRunWorkspace` / `DeleteRunWorkspace` / `EnqueueExecutionWork` / `EnqueueThreadCommand`.
- G-002 — plugin step on the generic `create_workspace` path; makes the §2.6 agent-plugin gate meaningful.
- G-003 — A-side terminal call site wiring to `RunWorkspaceSettled` (this round designs it; the wiring is the Phase 2 implementation).
- G-005 — the cancel-request write path (`cancel_requested_at`), owned by a later slice.
- controller-integration D6 naming: the business hook is `runWorkspaceSettled` in the ADR and `RunWorkspaceSettled` in `agent_run_hooks.go`; implementation should keep the Go name and document the mapping.

---

## 10. Quality Gates

For every implementation round, run the applicable repository gates.

Current expected Cloud gates:

```bash
export PATH="$HOME/.local/go/bin:$PATH"

gofmt -w <files changed in the round>

go build ./...
go vet ./internal/... ./integration

TEST_DATABASE_URL=<existing> \
REQUIRE_POSTGRES=1 \
go test ./integration -count=1

go test -race -count=1 ./internal/core/... ./integration

go test ./... -count=1

git diff --check
```

For specs changes:

```bash
git -C ../specs diff --check
```

Do not install missing frontend/buf dependencies merely to manufacture a green unrelated gate unless the architect explicitly requests it.

Do not skip, weaken, or timeout-mask failing assertions.

---

## 11. Repository / Commit Discipline

Cloud and specs are independent repositories.

Changes must remain attributable to their repository.

`specs` evidence may only be updated when implementation/tests actually cover the obligation.

Do not mark future slices Covered early.

The Agent never commits.

At a stable boundary, report:

> `COMMIT_BOUNDARY_REACHED`

The architect decides what to commit and in which repository.

---

## 12. Decision Log

Append new decisions; do not erase historical ones without noting supersession.

### D-001 — `executor_id` is a semantic reference

Status: Accepted

Agent `executor_id` stores `space_agents.id`; no physical FK is added to the polymorphic column.

### D-002 — authoritative Agent snapshot wins over caller input

Status: Accepted

Plugin identity/version is resolved server-side at run creation and cannot be spoofed by caller-provided input.

### D-003 — Project busy is a normal retry result

Status: Accepted

Dispatcher must not reuse panic/409 busy behavior.

### D-004 — dispatch loop is B-owned

Status: Accepted for implementation unless contradicted by a newer approved ADR.

The loop makes decisions over B-owned `issue_runs` and invokes A via the transaction-aware control-plane seam.

### D-005 — immediate dispatch is optimization, scan is recovery

Status: Accepted

Queued AgentRuns must be recoverable after process restart without relying on in-memory state.

### D-006 — no new retry timestamp in Slice 1

Status: Accepted

Use fixed periodic scan ≤10 seconds. Do not add a migration for `next_attempt_at`/lease fields in Slice 1.

### D-007 — Dispatcher routing uses historical Agent identity

Status: Accepted (Slice 1 discovery)

Creation-time validation and dispatch-time identity classification are separate concerns. At creation, `ResolveTarget` requires the executor to be an `active` Space Agent in the tenant. At dispatch, the routing discriminator `isSpaceAgentRun` only checks that `issue_runs.executor_id` is a `space_agents` row **across any lifecycle status**. A run created while its Agent was `active` therefore still routes to `AgentRunDispatcher` (Claim + Busy) even if the Agent was later `retired`. This preserves historical runs' execution identity; it does not re-validate the Agent's active status at dispatch time.

### D-008 — retry scan batch bound is an implementation choice, not a contract

Status: Accepted (Slice 1)

The recovery scan is bounded by `agentDispatchBatchSize` (100) and deterministically ordered by `(created_at, id)`. This is a tuning/liveness decision, not an architectural contract: correctness (no double claim, no starvation, replay safety) comes from each per-run `Dispatch` transaction and its CAS guard, not from the scan bound. The bound only controls how many claims one tick attempts.

### D-009 — per-run retry failure isolation

Status: Accepted (Slice 1)

A single queued run's claim failure must not terminate the bounded scan pass. `Dispatch` already keeps a failed run queued and rolls back on error; the scan deliberately does not surface per-run errors. Failure classification:

- `busy` (precheck or seam) → normal result, run stays queued, batch continues; not an error and not logged as one.
- genuine seam/internal failure → run stays queued (rollback), batch continues.
- observability, not silent swallow: scan-level failures surface to the caller; the run's repeated failure is visible via its persistent queued state and the seam's own observability, while per-run transient errors do not flood the loop for an unwired A seam (Unavailable).

### D-010 — Phase 2 `RunWorkspaceSettled(ready=true)` does starting only; the session work item is Phase 3

Status: Accepted for this design round

The final contract (IssueRun D3 table; test-case `Run Phases Advance Only In Evidence Transactions`) pairs starting with releasing exactly one `AgentSession` work item, and the `execution_work` partial-unique invariant allows at most one unreleased work item per run. The control-plane ADR's `runWorkspaceSettled` hook description only lists "→ `starting`" as its business effect — it does **not** force the session `EnqueueExecutionWork` to be emitted in the same settle transaction.

This plan therefore splits the final contract across phases deliberately: **Phase 2** `RunWorkspaceSettled(ready=true)` transitions `phase = starting` only (status stays `dispatched`); the exactly-one session work item release is **Phase 3**. This is a phasing boundary, not a contract conflict — during Phase 2 a run may transiently sit in `starting` with zero released session work, which is safe: admission is gated, nothing starts without a work item, and the dedup/once invariant is not violated (zero ≤ one).

Design recommendation recorded for Phase 3 review: release the session work item inside the *same* settle transaction (extending `RunWorkspaceSettled`) so "exactly one session work released only after ready" is atomic and there is no starting-phase gap window. If the architect prefers a starting-phase scan, that also satisfies the once-invariant; the choice is recorded here, not silently made during implementation.

---

## 13. Open Gaps / Decisions Needed

These are known unresolved or cross-owner items.

### G-001 — Real A control-plane implementation

Owner: A

Needed for production E2E:

- CreateRunWorkspace
- DeleteRunWorkspace
- EnqueueExecutionWork
- EnqueueThreadCommand

B may proceed with deterministic injected stand-ins where the contract is already approved.

### G-002 — create_workspace plugin step

Owner: A

Not yet implemented (confirmed this design round). In `control.go` `advance`, `create_workspace` advances `sandbox → node → clone → openWorkspace → done`; the `plugin` case exists in the switch only for `install_plugin`/`remove_plugin` operation kinds (migration 0015 widened `step`). The approved plugin-step ADR D1 requires `create_workspace = sandbox → node → clone → plugin → done` with admission opening only **after** the plugin step. Until A wires this, a run workspace's "ready" terminal does **not** imply its agent plugin is installed, so Phase 2's settle-level `agent_plugin_unavailable` check (§18.2.6) is vacuous and must be gated behind this gap.

Do not fix this incidentally inside B Dispatcher/Phase 2 implementation.

Status after Phase 2A (2026-09-30): **still open — no implementation this round.** The Phase 2A settle hook maps `ready=false` to the *generic* `workspace_unavailable` only; it deliberately does **not** guess `agent_plugin_unavailable` from a boolean (no authoritative plugin evidence exists to distinguish it). G-002 gates the plugin-failure test T2-3 (DEFERRED_TO_PHASE_2B_G002) and the production `agent_plugin_unavailable` settlement path, both required for Phase 2B.

### G-003 — terminal workspace operation → `RunWorkspaceSettled`

Owner: A/B integration seam

Confirmed this design round: `RunWorkspaceSettled` (in `agent_run_hooks.go`, signature `RunWorkspaceSettled(t *transaction, run Object, ready bool) error`) has **no callers**; `UnavailableAgentRunHooks` fails closed. The control-plane ADR (`controller-integration` D6) specifies the call site as the run workspace's `create_workspace` terminal transaction, running on the caller's `*transaction` with no self-opened transaction. The A-side `advance` terminal write currently has no awareness of `issue_runs`.

Required for Phase 2, not Phase 1.

Status after Phase 2A (2026-09-30): **still open — `RunWorkspaceSettled` remains with zero production callers (implemented, but unwired).** Phase 2A implemented and white-box verified the B-side hook (`businessAgentRunHooks.RunWorkspaceSettled`) and its settlement core; the A-side `advance` terminal call site is Phase 2B. Production callers are intentionally wired only once the real A seam (G-001) and the call site (this gap) exist. `UnavailableAgentRunHooks` keeps all not-yet-phase hooks fail-closed.

### G-004 — future scalability of global advisory lock

Status: Not a Slice 1 blocker

Current implementation serializes Store mutations through a global PostgreSQL advisory transaction lock.

Slice 1 uses this baseline plus explicit state guards. Replacing or narrowing the global lock is a separate architecture task and must not be mixed into Dispatcher implementation without approval.

### G-005 — user-initiated cancel request write path owner

Owner: B / later slice

IssueRun D6 defines cancel as a request (`issue_runs.cancel_requested_at`) whose effect lands at settle (no-session provisioning/starting → releasing/cancelled/skipped). The Phase 2 settle hook (§18.2.7) only *reads* `cancel_requested_at`; the write path that a user's cancel action uses to set it is not yet owned. That cancel-action surface is a later slice (Thread/API work, Phase 4–5) — out of Phase 2 scope, recorded here so Phase 2 does not invent its own cancel-request mechanism.

---

## 14. Change-Control Rules

A change is considered **architectural** if it changes any of:

- ownership A vs B
- transaction boundary
- phase/state transition
- retry semantics
- idempotency strategy
- persistence schema
- public/API error behavior
- executor identity semantics
- control-plane seam shape

For architectural changes, the Agent must:

1. add/update an entry in **Decision Log** or **Open Gaps**
2. explain why current plan cannot be followed
3. identify ADR evidence
4. avoid implementing the disputed alternative until resolved, unless the architect explicitly authorizes the change

For non-architectural implementation choices, the Agent may proceed but must record material choices such as:

- retry batch size
- loop helper naming
- log severity
- exact internal helper placement

---

## 15. Per-Round Update Template

At the end of each Agent round, append or update this section.

### Round: `<name / date>`

**Plan section executed:**

- e.g. Phase 1 / §8.1–§8.4

**Status:**

- Not Started / In Progress / Complete / Blocked

**Files changed:**

- `path`

**Decisions added/changed:**

- D-xxx or None

**New gaps:**

- G-xxx or None

**Tests added/changed:**

- test name → behavior proven

**Gate results:**

- build
- vet
- integration
- race
- full tests
- diff check

**Scope deviations:**

- None, or exact deviation + approval status

**Next planned step:**

- exact next subsection/phase

**Git:**

- nothing staged?
- commit performed? must always be `NO` unless architect explicitly changed the rule

---

### Round: Phase 2 — Workspace Settlement design / 2026-09-30

**Plan section executed:**

- Design round: §7 Phase markers, §12 D-007–D-010, §13 G-002/G-003/G-005, §16 marker, `## Phase 2 — Workspace Settlement 详细设计` §2.1–§2.15.

**Status:**

- Phase 1 `DONE / ARCHITECTURALLY REVIEWED`; Phase 2 `DESIGN REVIEW`. Design complete, verdict `READY_FOR_PHASE_2_IMPLEMENTATION`.

**Files changed:**

- `plan/plan.md` (design only; no Go code, migration, test, server, frontend, or specs behavior changed).

**Decisions added/changed:**

- D-007 (routing uses historical Agent identity), D-008 (scan batch bound is an implementation choice), D-009 (per-run retry failure isolation), D-010 (Phase 2 settle does starting-only; session work item deferred to Phase 3, prefer same-tx).

**New gaps:**

- G-005 (user-initiated cancel-request write path owner, later slice). G-002/G-003 updated with confirmed un-implemented status.

**Tests added/changed:**

- None implemented (design round). Planned Phase 2 tests T2-1..T2-10 in §2.14.

**Gate results:**

- Not run (no code changed). Design-only round.

**Scope deviations:**

- None. No code modified; no commit, push, PR, or stage.

**Next planned step:**

- Architect review of §18; on approval, Phase 2 implementation begins with G-003 call-site wiring (§16 order).

**Git:**

- nothing staged? YES (nothing staged by the Agent).
- commit performed? **NO**.

---

### Round: Phase 2A — B-owned Workspace Settlement Core / 2026-09-30

**Plan section executed:**

- Implemented the Phase 2A slice of `§18 Phase 2 — Workspace Settlement 详细设计`: the B-owned settlement core behind the A→B hook `RunWorkspaceSettled`, per §2.4 (ready→starting), §2.5 (workspace failure→releasing/failed), §2.6 approach row (plugin failure — gated G-002, not produced), §2.7 (cancel-before-settle), §2.8 (replay CAS), §2.9 (stale no-op), §2.10 (workspace identity), §13 (same-transaction delete declaration). Did **not** wire the A-side terminal caller (G-003, Phase 2B) and did **not** implement the create_workspace plugin step (G-002).

**Status:**

- Phase 2A `DONE / ARCHITECTURALLY REVIEWED`; Phase 2B (A-side caller wiring + plugin step) `NOT STARTED`. Ideal next target: `PHASE_2A_DONE` + `READY_FOR_PHASE_2B`.

**Files changed (this round):**

- `internal/core/agent_run_settle.go` (new) — `settleRunWorkspace`, `settleReadyGuard`, `settleFailed`, `settleCancelled`, `declareDelete`, `businessAgentRunHooks` (delegates `RunWorkspaceSettled`; all other hooks fail-closed via `UnavailableAgentRunHooks`).
- `internal/core/agent_run_settle_db_test.go` (new) — white-box `TestPhase2A*` suite (T2-1..T2-10; T2-3 DEFERRED), plus invariant-error and business-hooks fail-closed tests.
- `plan/plan.md` — Phase 2A status, test evidence (§2.14), G-002/G-003 status notes, §16 marker, this round record. No schema, migration, server, frontend, OpenAPI, or specs behavior change.

**Decisions added/changed:**

- None new (implementation follows approved §18 design; no conflict found). Reconfirmed: `deliveryState` cancel rule is written as `result` JSONB key (IssueRun D4) — no schema column added.

**New gaps:**

- None. G-002 gates T2-3 and production plugin-specific settlement (stays open); G-003 stays open (production caller unwired). G-005 unchanged (Phase 4–5).

**Tests added/changed:**

- `TestPhase2A*` suite in `agent_run_settle_db_test.go`. Evidence table in §2.14: T2-1, T2-2, T2-4..T2-10 → COVERED/PASS; T2-3 → DEFERRED_TO_PHASE_2B_G002.

**Gate results (all pass):**

- `gofmt -w` on both new files.
- `go build ./...` — OK.
- `go vet ./...` — OK.
- `TEST_DATABASE_URL=... REQUIRE_POSTGRES=1 go test ./internal/core -count=1` — ok (~3.7s).
- `REQUIRE_POSTGRES=1 go test ./integration -count=1` — ok (~39.5s).
- `go test -race -count=1 ./internal/core/... ./integration` — ok (core 4.4s, integration 110.2s).
- `go test ./... -count=1` — ok (exit 0).
- `git diff --check` — clean.

**Scope deviations:**

- None. No commit, push, PR, or stage.

**Next planned step:**

- Phase 2B (G-003 A-side terminal call-site wiring; G-002 create_workspace plugin step → production `agent_plugin_unavailable` path + un-defer T2-3). Do not begin Phase 3 (session `EnqueueExecutionWork`) in the same round.

**Git:**

- nothing staged? YES (nothing staged by the Agent).
- commit performed? **NO**.

---

## 16. Current Execution Marker

Current phase:

**Phase 2 — Workspace Settlement**

Current round: **PHASE 2A DONE / PHASE 2B NEXT** (settlement core implemented and verified; A-side caller wiring and plugin step deferred).

This round delivered the B-owned Phase 2A settlement core: `RunWorkspaceSettled` (`businessAgentRunHooks`), CAS `provisioning→starting`, `releasing/failed(workspace_unavailable)`, cancel-before-settle `releasing/cancelled(deliveryState=skipped)`, same-transaction `DeleteRunWorkspace` declaration (B→A seam, atomic), replay/stale no-ops, and the white-box `TestPhase2A*` suite (T2-1..T2-10; T2-3 DEFERRED_TO_PHASE_2B_G002). All gates green (`go build ./...`, `go vet ./...`, core + integration (incl. race), `go test ./...`, `git diff --check`). Slice 1 (Phase 1) remains `DONE / ARCHITECTURALLY REVIEWED`.

Phase 2B (next round) order:

1. Wire the A-side create_workspace terminal call site for run workspaces (G-003) into the B hook `RunWorkspaceSettled(t, run, ready)`.
2. (A) implement the create_workspace plugin step (G-002); gate the `agent_plugin_unavailable` settle path on it, un-defer T2-3.
3. Re-run gates and update this document.
4. Stop. Do not begin Phase 3 (session `EnqueueExecutionWork`) in the same round.

The Phase 2 production caller remains unwired (G-003 open): `businessAgentRunHooks` is only exercised by white-box tests this phase, never bound on a production Store.

---

## 17. Definition of Done for Slice 1

Slice 1 is complete only when all are true:

- eligible queued real AgentRun can dispatch against the B→A control seam
- idle + accepted → provisioning/dispatched
- DB busy → queued
- seam Busy race → queued
- seam error → rollback/queued
- duplicate dispatch is harmless
- queued AgentRuns are rediscovered by a bounded ≤10s B-owned loop
- scan/sleep do not create long transactions
- team/workflow/legacy fixture behavior is unchanged
- no new schema is required
- no Slice 2 logic was added
- required PostgreSQL integration tests pass
- race/full gates pass
- `plan.md` has been updated with actual implementation evidence
- nothing was committed/pushed/PR-created by the Agent


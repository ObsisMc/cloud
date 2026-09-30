# AgentRunDispatcher Implementation Plan

> Status: **Living design / execution record**  
> Scope owner: **B — Cloud business / orchestration**  
> Current target: **Phase 4 — Thread / Running Lifecycle（DESIGN REVIEW only，本轮不实现）**
> Current Phase: **4 — Thread / Running Lifecycle（设计轮：3A+3B 已完成，Phase 4 尚未实现）**
> Current Status: **DESIGN REVIEW**
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

Phase 2B (A-side terminal caller wiring, plugin step): **DONE / ARCHITECTURALLY REVIEWED** — see §15 Phase 2B round record.

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

Phase 2B delivered (D-011): the Agent run-workspace `create_workspace` internal `plugin` step (effect-based `plugin_ensure` for the run's pinned agent plugin), the run-instance-only plugin writer (never perturbs the space aggregate / Agent roster), and the A-side `advance` terminal wire to `RunWorkspaceSettled` in the same caller-owned transaction (hook error rolls the whole terminal write back). G-002 and G-003 are CLOSED; T2-3 un-deferred and PASS. Phase 3 (session `EnqueueExecutionWork`) is not begun.

### Phase 3 — Session Start

Status: **Phase 3A IMPLEMENTED (B-owned core); Phase 3B IMPLEMENTED (A-side real seam → PHASE_3_DONE); Phase 4 NOT_STARTED (Thread)**

Design chapter: [`## Phase 3 — Session Start 详细设计`](#phase-3--session-start-详细设计design-review) + D-012..D-018 + G-007..G-011.

Goal:

Release exactly one AgentSession execution work item from `starting`.

Expected work (Phase 3A — B-owned core):

- fixed first prompt generation from run snapshot (G-007)
- `EnqueueExecutionWork` against the approved seam with deterministic injected stand-in (G-001)
- immutable pinned plugin/version use (already proven by snapshot freeze)
- exactly-once/replay behavior (D-012, D-015)

Expected work (Phase 3B — A-side execution persistence + production seam): **IMPLEMENTED — G-003..G-008 CLOSED**

- real A-owned `execution_work` table + partial-unique exactly-once constraint (G-008, D-016)
- production `EnqueueExecutionWork` replacing the Unavailable seam in `cmd/server/main.go` (G-001 execution seam)
- Controller claim read (`agent_work_claim`) — pickup ≠ running (D-017)
- payload-mismatch replay rolls back (D-018); §28 concurrency; §29 conflict

### Phase 4 — Thread / Running Lifecycle

Status: **Phase 4A IMPLEMENTED（A-side dispatch registration，architecturally reviewable）→ NOT_READY_FOR_PHASE_4B_IMPLEMENTATION**；4B（Thread takeover + running）与 4C（Thread API/SSE）未实现。详见 4.15 拆分。

Phase 4A delivered (this round, D-020/D-021, G-012 PARTIAL):

- new A-owned `node_executions` migration (0020): execution_id PK, work_id UNIQUE FK→execution_work, pending partial index by node
- production `agent_work_dispatch` (`agentWorkDispatch`) — Controller supplies execution_id/node/input; A records `node_executions` + fences `execution_work.execution_id` in the same transaction, replay idempotent, different id/node/input → `dispatch_conflict`, never overwrite
- recovery reads `agent_work_get` / `agent_work_pending` (no-lease; C2 crash reuse of the recorded execution)
- claim & dispatch never run the run: `issue_runs.phase` stays `starting`/`dispatched`, thread seq stays 1, no running
- NO Phase 4B machinery: `node_event_receipts`/`TakeOverThreadEvents`/starting→running/thread seq≥2/D-023 seq allocation all forbidden and absent (T4A-16)
- tests T4A-1..T4A-16 (real PostgreSQL) + `TestMigration0020NodeExecutionsAppliesFreshAndUpgrades`

Includes:

- thread_entries seq≥2 continuation + takeover hooks (4B, NOT implemented)
- starting → running with a single authority = committed ThreadEventsTakenOver (D-019, 4B, NOT implemented)
- execution_id registration via RecordDispatch (4A, D-020/D-021 — **IMPLEMENTED**)
- node_event_receipts event identity (4B, D-022, NOT implemented)
- Thread command control seam (4C, NOT implemented)
- API/SSE work (4C, NOT implemented)

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
| T2-3 | COVERED — PASS (Phase 2B, G-002) | `TestPhase2BT2_3PluginFailureSettlesAgentPluginUnavailable` (settle-level) + `TestPhase2BRunInstanceWriterLeavesSpaceAggregateAlone` | Agent run-workspace pinned agent-plugin instance `failed` → `releasing`/`failed`/`agent_plugin_unavailable`, delete declared once; run-scoped failure never perturbs the `space_plugins` aggregate or retires the `space_agents` roster. |
| T2-4 | COVERED — PASS | `TestPhase2ASettleHookRollback` | Hook error → entire tx rolled back (phase + delete absent), run still `provisioning`; replay after the fix succeeds. |
| T2-5 | COVERED — PASS | `TestPhase2ASettleReplayReady` | Ready settled twice → one transition, second no-op, exactly one delete declaration. |
| T2-6 | COVERED — PASS | `TestPhase2ASettleReplayFailure` | Failure twice → one `releasing`, one deletion, second no-op. |
| T2-7 | COVERED — PASS | `TestPhase2ASettleStaleCallback` | Run already `starting`/`releasing` → settle no-op (no regression, no new delete). |
| T2-8 | COVERED — PASS | `TestPhase2ASettleCancelBeforeSettle` | `cancel_requested_at` set, still `provisioning` → `releasing`/`cancelled`, `result.deliveryState=skipped`, delete declared (IssueRun D6). |
| T2-9 | COVERED — PASS (identity) | `TestPhase2ASettleWorkspaceBinding` | Settlement re-reads/binds authoritative `workspace_id`; does not regenerate or overwrite; delete declaration carries the same workspace id. Replay-deduplication of the delete is asserted via the injected seam count. |
| T2-10 | COVERED — PASS | `TestPhase2ASettleAtomicity` | Terminal write + business transition + delete declaration commit together (T2-1 proves commit; T2-4 proves rollback leaves all three absent). |

Additional coverage: `TestPhase2ASettleInvariantErrors` (nonexistent run / `executor_type != agent` / tenant mismatch → error → rollback, valid run untouched) and `TestPhase2ABusinessHooksDelegateAndFailClosed` (`businessAgentRunHooks` delegates `RunWorkspaceSettled`; every other hook fails closed via `UnavailableAgentRunHooks`).

G-002 gates T2-3 and production plugin-specific settlement; G-003 stays open (production caller unwired). No migration/schema change; the cancel rule writes `result.deliveryState` via JSONB per IssueRun D4 (no `delivery_state` column to add).

#### Test evidence — Phase 2B (2026-09-30)

White-box `TestPhase2B*` suites (`agent_run_terminal_db_test.go` settle-level, `agent_run_terminal_advance_test.go` A-side drive) in package `core`, real PostgreSQL, injected `stubAgentRunControlPlane` + `businessAgentRunHooks`. Evidence targets: G-002 (plugin step), G-003 (A→B terminal wiring), T2-3 un-defer, D-011 (run-instance-only writer), snapshot version immutability, and G-003's mandatory rollback-on-hook-error.

| Obligation | Evidence status | Test |
|---|---|---|
| G-002 plug step plans the run's **pinned** snapshot version, never the current roster (§3/§6) | COVERED — PASS | `TestPhase2BPlanUsesPinnedSnapshotVersionImmutable`; `TestPhase2BPluginUpgradeKeepsHistoricalSnapshot` |
| G-002 admission stays closed until the plugin step (G-002); not implied by infra-ready | COVERED — PASS | `TestPhase2BTerminalCreateSettlesRunInSameTx` (asserts admission closed at plugin, opens after success) |
| G-002 run-instance writer never perturbs space aggregate / Agent roster (D-011) | COVERED — PASS | `TestPhase2BRunInstanceWriterLeavesSpaceAggregateAlone` |
| T2-3 plugin failure → `releasing`/`failed`/`agent_plugin_unavailable`, delete declared once | COVERED — PASS | `TestPhase2BT2_3PluginFailureSettlesAgentPluginUnavailable` |
| G-003 terminal create settles the run in the **same** transaction; workspace opens, instance installed@pinned | COVERED — PASS | `TestPhase2BTerminalCreateSettlesRunInSameTx` |
| G-003 hook error rolls the whole terminal write back (no "op committed but run still provisioning") | COVERED — PASS | `TestPhase2BHookErrorRollsBackTerminalWrite` |

No migration/schema change in Phase 2B. G-002 and G-003 both CLOSED at the implementation level; the real A seam (G-001) remains outstanding for production E2E.

### 2.15 remaining A-side dependencies

Before Phase 2 is production-meaningful, the following A-side / cross-owner items must exist (recorded in §13):

- G-001 — real `CreateRunWorkspace` / `DeleteRunWorkspace` / `EnqueueExecutionWork` / `EnqueueThreadCommand`. (Outstanding; only a deterministic injected seam + the B→A declare via `stubAgentRunControlPlane` are in place.)
- G-002 — plugin step on the generic `create_workspace` path (Phase 2B implemented it for the Agent run-workspace only; general create_*/start remain pre-plugin-engine — recorded in D-011/G-006).
- G-003 — A-side terminal call site wiring to `RunWorkspaceSettled` (Phase 2B wired it in the A-owned `advance` terminal transaction).
- G-005 — the cancel-request write path (`cancel_requested_at`), owned by a later slice.
- controller-integration D6 naming: the business hook is `runWorkspaceSettled` in the ADR and `RunWorkspaceSettled` in `agent_run_hooks.go`; implementation keeps the Go name and documents the mapping.

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

## Phase 3 — Session Start 详细设计（DESIGN REVIEW）

本阶段是设计轮次（非实现轮次）。目标与新章节边界如下，所有断言以已批准的 ADR 为准，未曾改动任何
实现代码、schema、迁移或 `specs/`；遇到 ADR 不明确的边界一律记录为 Open Gap，不凭便利猜测。

### §3.1 Entry / Exit Condition

- **Entry（前置状态，来自 IssueRun D3 + Phase 2B 实现）**：`issue_runs.executor_type='agent'`、
  `phase='starting'`、`status='dispatched'`、`workspace_id IS NOT NULL`（run Workspace 已由
  `RunWorkspaceSettled(ready=true)` 打开 admission，并已安装 pinned agent 插件实例）。
- **Exit（本阶段结束点，由 ADR 决定而非便利决定）**：Phase 3 的交付物是 **恰好一次 AgentSession
  `execution_work` 声明 + 该 run 的首条 first-prompt 线程条目**，而 `issue_runs.phase` **保持在
  `starting`**（`status` 保持 `dispatched`），**不前进到 `running`**。
- **为什么 `running` 不在 Phase 3**：IssueRun ADR D3 的阶段表（authoritative for phase machine）明确定义
  `running` 的进入条件是 *"Node 报告会话已开始（首条 Thread 事件或会话开始事件被接管）"*。声明
  execution work 与 Workspace ready 都不构成该证据。`starting→running` 的权威事件是首条
  agent-originated Thread 事件 / session-start 事件的接管，由 `ThreadEventsTakenOver` 钩子驱动——
  该钩子属于计划中的 Phase 4。因此 Phase 3 的语义载荷是「在 `starting` 下恰好放出一个会话工作项」，
  没有把运行移动到 `running`（不变量 2「阶段不经证据不前进」以此为界，见 D-014）。
- **这回答了设计问题 §3（结束点）**：Phase 3 结束于 **Option nowhere-in-running** —— 结束条件是
  execution work 已声明（+ first-prompt 已落），phase 仍 `starting`；`running` 属于 Phase 4。

### §3.2 Execution Work Ownership（声明与物理执行分离）

从 controller-integration D6 的表所有权（不变量 7：每张表单一 writer）与「one-writer-per-table」出发：

- **声明（declaration）**：`EnqueueExecutionWork` 是 B→A 控制 seam，在 B 的调用方事务内同步执行，
  返回 `execution_work.id`。B 提供 AgentSession 输入（pinned plugin id/version + first-prompt
  `initial_turn`）与 target；`execution_work` 行本身由控制面（A）写入。B **绝不直接写
  `execution_work`/`node_executions`**（D6 不变量 7）。
- **物理执行（physical execution）**：Controller 的 `ClaimWork → RecordDispatch → StartAgentSession`
  （controller-session ADR D1）。这属于 A/Controller 职责，**不在 Phase 3 B 侧**。
- **retry 归属（对应 §3.9 与设计问题 §12）**：
  - **B 拥有「声明」的持久重试**：starting 扫描在 cadence 上重试声明；声明廉价且幂等（见 §3.12），
    无需退避，不设置 `failure_reason`（瞬时不可用不是运行失败）。
  - **A 拥有「物理会话启动」的重试**（RecordDispatch 之后到 Node 回话期间的 Node/重连重试），是
    Phase 4 边界，此处记录职责归属，不在 Phase 3 实现。

### §3.3 EnqueueExecutionWork — AgentSession 契约

接 D6 的 `ExecutionInput AgentSession`：`{agent_plugin_id, agent_plugin_version, checkout_execution_id,
git_identity, initial_turn{turn_id, content}}`。seam 签名已存在于 `internal/core/agent_run_control.go`：

```go
EnqueueExecutionWork(t *transaction, run Object, kind string,
    input, target Object, availableAt *time.Time) (string, error) // -> execution_work id
```

- `kind` = `"agent_session"`（本阶段唯一）。`deliver_revision` 属 Phase 5。
- `input` 来自 `AgentSessionWork{AgentPluginID, AgentPluginVersion, InitialTurn}`；其中
  `AgentPluginID`/`AgentPluginVersion` **取自 IssueRun 输入快照**（`issue_runs.input.agentPluginId` /
  `agentPluginVersion`，run-create 时按 IssueRun D1/D6 冻结，绝不重读当前 roster），`InitialTurn`
  来自 first-prompt 渲染（见 §3.6）。
- `target` = `{workspace_id, sandbox_instance_id, node_id}`（运行 Workspace 的当前已连接 sandbox/Node，
  D6 WorkItem target）。B 从 run Workspace 的 `sandbox_instances`/`node_instances` 行派生 target；
  该派生工具尚未生产化（G-010）。
- `availableAt`：本阶段默认立即（nil）。退避/超时语义属后续交付/恢复阶段。

### §3.4 Execution / Session Identity

- **执行身份**：`execution_work.id`（Cloud/A 生成的 uuid）是该 run 唯一 AgentSession 工作项的
  **声明身份**。断言靠 `execution_work` 上的部分唯一（partial-unique）`(run_id, kind='agent_session')`
  —— 每 run 至多一条未释放工作项（D6 不变量）。
- **执行/会话 id**：`execution_id` 由 Controller `RecordDispatch` 写入（A 生成），落在 `execution_work`
  与 `node_executions`，**不在 `issue_runs`**。B 不预分配、不持有 execution id；B 唯一的持久产物是本
  工作项行（run 维度）。
- **会话身份**：一次 AgentSession 对应一条 `agent_session` 工作项；会话与执行身份靠
  `execution_work`/`node_executions` 行承载。B 不另铸 session id；Thread 是 per-run 的
  （`thread_entries(run_id, seq)`）。因此 Phase 3 的持久状态仅两类：`thread_entries` 首条 first-prompt
  条目 +（经 A seam 在同一事务内落下的）`agent_session` 工作项行，两者都归 run 维度。

### §3.5 Immutable Run Snapshot（first-prompt 源）

Thread ADR D3 + IssueRun D1/D6：first-prompt 渲染源是 **run-create 时冻结的 IssueRun 输入快照**
（`issue_runs.input`，由 `buildRunContext`/`ContextInput` + ContextBuilder 生成，含 IssueTitle、
IssueDescription、Task、最近评论 ≤10、contextRefs，snapshotAgentRunInput 再补 agentPluginId/Version）。
因为源已在 run-create 冻结，渲染所得 first-prompt 在重试/重放时**稳定不变**——不实时重读 issue、
评论、profile（正好回答问题「首条 prompt 的不可变性」。快照 freeze 已由 `TestAgentRunSnapshotPinsPluginVersion`
等既有证据覆盖 plugin 维度；first-prompt 文本侧渲染器尚未实现，见 G-007）。

### §3.6 First Prompt Contract（对应设计问题 §8/§9 边界）

按 Thread ADR D3：运行进入 `starting` 时，Cloud 用 IssueRun 输入快照经**固定模板**渲染，
写入 `thread_entries` 一条 `source=system, kind=user_turn` 条目，`turn_id` 由 Cloud 生成，作为
AgentSession 执行输入的一部分（`initial_turn`）下发，**不是单独排队**。本阶段落定：

- **首条 prompt 就是 Thread 的第一条 entry**：`thread_entries(run_id, seq=1)`，`source=system`，
  `kind=user_turn`，`record` 存渲染后内容，`turn_id` 由 Cloud 生成。B 在 Phase 3 写 seq=1（Cloud 作者，
  Cloud 分配 seq）；Node 事件引发的 seq≥2 且 gapless 的分配属于 Phase 4 `ThreadEventsTakenOver`
  （Thread D1/D3）。Phase 4 必须**从 seq=2 续接**（seq=1 已被 first-prompt 保留）——记为接缝约束
  （G-009）。
- **边界结论**：first-prompt 在 `starting` 阶段**写入**（落 thread_entries + 作为 initial_turn 内联进
  AgentSession），但**不触发**任何 Thread API / SSE 流式传输——那需要线程接管（Phase 4）。这正好是
  设计问题 §9 的 **Option B**：不可变 first prompt，内联在 execution payload 中。

### §3.7 Thread Boundary

- **Phase 3（本阶段）**：只写首条 Cloud 作者 user_turn（seq=1 + `initial_turn`）。不实现 Thread API、
  ThreadEventsTakenOver、SessionEnded、DeliverySettled、RunWorkspaceDeleted。
- **Phase 4（下一阶段，`Thread / Running Lifecycle`）**：`ThreadEventsTakenOver` 接管首条 Node 事件、
  分配 gapless seq（从 2 续接）、推进 `thread_state`、并据 D3 把 run 移到 `running`。

### §3.8 State Machine（对应设计问题 §7 Cases A–F）

| Case | 触发 | 结果（均在同一事务、全局 advisory lock 下） |
|---|---|---|
| A. enqueue 正常 | starting run、无已有工作、未取消 | 写 first-prompt seq=1 + seam 声明 agent_session 工作；phase 保持 `starting`、status `dispatched` |
| B. 重复/重放同一 pass | crash 后重放，工作行已存在 | 幂等 no-op：复用已有工作行，不重复写 first-prompt（seq 仍 1），返回已有 work id |
| C. 控制面不可用 | seam 返回错误（G-001 stub/瞬时） | 候选留待下个扫描 pass；phase 保持 starting、无工作、**不设 failure_reason** |
| D. cancel_requested 先至（enqueue 前） | 事务内重读 `cancel_requested_at` 已置位 | **不声明工作、不建会话**；运行由后续 cancel-settle 取 `releasing/cancelled/skipped`（D6） |
| E. workspace/target 无效缺失 | starting 但工作空间/沙盒/Node 行缺失或 Node 未连接 | **不建会话**；精确终态无法在本阶段得到 run Workspace（不可重建），记为边界 G-011（见 §3.17） |
| F. 陈旧/重放终态 | 已有 `agent_session` 工作行的 run 再次进入 pass | once-guard 生效：幂等不做第二份（Covers A/B，见 §3.12） |

设计问题 §7 的「cancel 先至」「重复」「短暂不可用」「workspace 无效」「陈旧重放」全部落入上表；
表内所有单元均有归属（无 `?` 遗留，除 E 的精确终态记 G-011）。

### §3.9 Retry Ownership

见 §3.2。汇总：
- **立即路径（immediate）**：`starting` 授予时（Phase 2 settle）本阶段**不在同一事务**做声明
  （D-015），而是交给下一时机——starting 扫描。为避免长延迟，可选的 notify/触发优化记为 Phase 4+
  不做（此处如实记录恢复轮询是接受的延迟，符合既有 ≤ 轮询 cadence 纪律）。
- **恢复路径（recovery）**：starting 扫描对任何「starting 且无未释放 agent_session 工作」的 run 兜底，
  无论它是 crash 后残留还是新授予。once 由幂等工作行保证。
- **责任边界**：B 只重试「声明」；命中即交由 A/Controller 起会话（Phase 4）。不为瞬时 seam 错误设
  运行级失败原因。

### §3.10 Immediate + Recovery Path（一起，deferral 记录）

选择 **Option B（post-settle starting 扫描）**，理由与 D-015 相同：保持 Phase 2B 已实现的
`RunWorkspaceSettled` 只到 `starting` 的语义不变，Phase 3 成为独立的、可重试的 B 组件；声明的高
可用依赖（A seam 瞬时不可用）被隔离在扫描里，不会拖回一个已成功的 settle。恢复由同一扫描在 cadence
上完成，once 不变。D-010 中「在 settle 事务内同步释放工作项」的建议被本决定**取代**（记于 D-015），
Phase 3 直接采用，不再重新评审。

### §3.11 Transaction Model

- 全部变更在 Store 全局 advisory xact lock（`pg_advisory_xact_lock(67420911)`）下的**单个**事务完成：
  1. 选候选 run（B 扫描）；
  2. 事务内重读 `cancel_requested_at`（D：取消则 no-op）；
  3. 重读是否存在未释放 `agent_session` 工作（once-check）；
  4. 写 first-prompt `thread_entries` seq=1（B 持有，seq 为 Cloud 分配）；
  5. 经 A seam 声明 agent_session 工作（同一 tx 内），获 work id；
  6. 提交；提交后对外可见 WorkAvailable。
- 锁使「cancel 写」与「session-start」串行化，二者不会交错产生一个不该存在的会话（见 §3.13 Cases 1-3）。
- 与 Phase 2 相同的纪律：不把锁、行锁或 advisory lock 跨出数据库工作延持到 HTTP/Git/Node。

### §3.12 CAS / Replay（once 与 A seam 重复结果）

- **once-guard**：`execution_work` 部分唯一 `(run_id, kind='agent_session')` + 扫描 pass 的同事务
  once-check，双保险。重放同一 pass 走 Case B no-op。
- **A seam 重复结果（设计问题「A 返回重复结果」）**：若 seam 报错但实际已提交（ambiguous），用既有
  `(run_id, kind)` 部分唯一行**对账**——重读该 run 的工作行，把它当作同一份，绝不把第二次 enqueue
  视为新工作；因为 B 每 run 只发一次声明且行幂等，ambiguous 被解析为已存在行。这是错误分类矩阵的
  必修项（见 §3.15 row: A ambiguous）。

### §3.13 Cancel Races（对应设计问题 §17 Cases 1–3）

| Case | 时序 | 行为（全局 lock 串行化后） |
|---|---|---|
| 1. cancel 先于 session-start tx | `cancel_requested_at` 已置位被重读 | 不声明工作、不建会话；后续 cancel-settle 取 `releasing/cancelled/skipped`（D6） |
| 2. cancel 与 session-start 并发 | 同一 lock 下串行化 | 谁先提交谁生效；session-start 后至上者若已看到 cancel 就 no-op |
| 3. cancel 后于 session-start 提交 | run 已有会话在 `starting` | 合法性由后续 cancel 处理（D6：有会话的取消是结束会话请求，Phase 4 EndSession）；Phase 3 只需保证「观察到 cancel_requested_at 时绝不建会话」成立 |

### §3.14 Running Authoritative Evidence（对应设计问题 §18）

权威证据 = **Node 报告会话已开始（首条 Thread 事件或 session-start 事件被接管）**（IssueRun D3）。
enqueue 声明与 Workspace ready **都不是** running 证据（D-014）。因此：Phase 3 结束在 `starting`；
`running` 仅由 `ThreadEventsTakenOver`（Phase 4）驱动。任何把运行推进到 `running` 的实现
（在本阶段或经 A seam 旁路）都是对不变量 2 的破坏，测试 T3-11 显式验证这一点。

### §3.15 Error Taxonomy Matrix（对应设计问题「错误分类矩阵」，`?` 全部归属）

| 故障 | 谁重试 | 运行相位/状态 | failure_reason | 归属决定 |
|---|---|---|---|---|
| A enqueue 正常 | — | 保持 starting/dispatched，工作已声明 | 无 | §3.8 A |
| 重复/重放同一 pass | 无（幂等） | 保持 starting，复用工作行 | 无 | §3.8 B |
| 控制面不可用（seam 错误） | B 扫描重试声明 | 保持 starting，无工作 | **无**（瞬时，非运行失败） | §3.8 C |
| A seam ambiguous（报错但已提交） | 对账既有工作行 | 保持 starting，收敛到已存在行 | 无 | §3.12 |
| cancel 先至 enqueue | 无 | 不声明，待 cancel-settle → releasing/cancelled/skipped | 由 cancel 终态记录 | §3.8 D |
| workspace/target 无效缺失 | 无（不可重建 run Workspace） | 不建会话；精确终态 | 边界 G-011 | §3.8 E |
| stale/replay 终态 | 无（once-guard） | 幂等 | 无 | §3.8 F |
| first-prompt 渲染器缺失 | 生产化，非重试 | 阻塞声明 | —— 实现缺口 G-007 | §3.6 |

### §3.16 Test Plan（设计，本轮不实现；T3-n 全部为 `Missing` 直至落地）

| 用例 | 断言 |
|---|---|
| T3-1 | starting 无工作 run：一次 pass 声明恰好一条 agent_session 工作；run 保持 starting/dispatched；写 first-prompt seq=1；phase 不前进 |
| T3-2 | 重放同一 pass（crash 后）：幂等 no-op，不再创建工作，first-prompt 不重复（seq 仍 1） |
| T3-3 | 快照不可变：enqueue 后升级 roster/改 issue，pending first-prompt 与工作输入不变 |
| T3-4 | cancel 先至：不声明工作、不建会话 |
| T3-5 | cancel 并发：两种先后顺序均正确（lock 串行化 + 重读） |
| T3-6 | 控制面不可用：seam 报错 → run 保持 starting、无工作、无 failure_reason，下 pass 重试 |
| T3-7 | 部分唯一 once：尝试二次声明仅一条工作行 |
| T3-8/e | workspace 缺失 in starting：不建会话（边界 G-011） |
| T3-9 | target Node 未连接：不建会话 / target 无效（G-011） |
| T3-10 | settle + session-start 两次 pass 均见到同一 run，工作仅一方声明（once 跨路径） |
| T3-11 | 阶段 CAS：无 takeover 证据绝不推进 running（不变量 2，D-014） |
| T3-12 | first-prompt initial_turn == 渲染快照 == thread_entries seq=1 内容 |
| T3-13 | AgentSession 输入 plugin id/version == pinned 快照版本，非当前 roster |
| T3-14 | 钩子失败回滚：seam 报错（或 first-prompt 写在阻止声明后）→ 整事务回滚，A/B 原子 |

### §3.17 Remaining Gaps（新增，Phase 3 视角）

- G-007 — **first-prompt 渲染器**：Cloud 固定模板把 IssueRun 输入快照渲染为系统 user_turn 尚未实现
  （快照构建已有，渲染模板未生产化）。Phase 3 的 first-prompt 文本落地前必须先生产它。
- G-008 — ~~**execution 身份持久化**：`execution_work`/`node_executions` 表与 `execution_id` 写入是
  A/Controller 职责，尚未实现（是 G-001 的组成部分，单列以明确身份依赖）。~~ **CLOSED（Phase 3B）**：
  A-owned `execution_work` 表 + `execution_work_unregistered_once` partial-unique 恰好一次已落地；`execution_id`
  由 Controller `RecordDispatch` 写入（Phase 3B 仅建行并生成 work `id`，不抢占 `execution_id`）。`node_executions`
  不在 Phase 3B scope（§14：除非 pickup 需要，否则不建表）。
- G-009 — **Phase 4 seq 续接**：`thread_entries` seq=1 被 first-prompt 保留，Phase 4
  `ThreadEventsTakenOver` 分配 seq 必须从 2 续接（接缝约束，跨 phase 记录）。
- G-010 — **target 派生工具**：B 从 run Workspace 的 `sandbox_instances`/`node_instances` 行派生
  D6 WorkItem target 的映射工具未生产化（A seam 目标组成部分）。
- G-011 — **starting 下 workspace/target 无效缺失的精确终态**：不可重建一个 run Workspace；
  精确终态（settle 到 `workspace_unavailable` 或其他）ADR 未明示，本阶段记录为边界待定，不猜。

## Phase 4 — Thread / Running Lifecycle 详细设计（DESIGN REVIEW）

Status：**DESIGN REVIEW（本轮只设计，不实现；无 migration、无 production 代码）**。覆盖从
`execution_work` 已持久化 → Controller claim/dispatch → `execution_id` 登记 → 权威会话开始/接管证据 →
`ThreadEventsTakenOver(...)` → B 侧 `starting → running` → `seq` 从 2 续接。权威依据：IssueRun D3（running
进入条件）、Thread D1/D3/D4、controller-integration D2/D6/D7、Node 协议 D2。Phase 3B 后代码事实见 §16；本
章只定契约，不实现 `ThreadEventsTakenOver`/`starting→running`/`RecordDispatch`/`node_executions`（均本轮
禁止）。核心原则：`execution_work created ≠ running`、`claim ≠ running`、`dispatch ≠ running`，只有权威
接管/会话开始证据能 `starting → running`。

### 4.1 entry state

- 进入本阶段的运行：`issue_runs.executor_type='agent'`、`phase='starting'`、`status='dispatched'`、
  `workspace_id` 指向 run Workspace 且 `workspace_plugin_instances` 已安装（Phase 2B）、快照已放出一个未登记
  `execution_work`（kind='agent_session'，`execution_id IS NULL`）。`thread_entries` 当前仅有 seq=1
  `source='system'`/`kind='user_turn'` 的 first prompt（Phase 3A，G-009 续接约束）。
- `thread_state` 保持 NULL（Phase 3A 未写；首条记录接管的事务才由业务钩子写，见 4.6/4.21）。

### 4.2 exit state

- `running`：`issue_runs.phase='running'`、`status='running'`（IssueRun D3 表格对 running 的 status 列固定为
  `running`——非推测，ADR 明示）。`thread_state` 进入 `active`（首条 Node 记录接管，Thread D4）或 `pending`
  （若首批是带会话开始语义的非记录事件，G 见 4.6）；`thread_entries.seq` 从 2 起的 Node 事件已接管。
- 除 `starting→running` 外的旁路退出（`releasing/cancelled`）仍按 IssueRun D6（cancel）与既有 G-011 边界，
  本轮仅为 cancel race 定义串行化（4.11），不实现。

### 4.3 authoritative running event

唯一 authority：**`ThreadEventsTakenOver` 钩子在其所在接管事务成功提交**（IssueRun D3「首条 Thread 事件或
会话开始事件被接管」；controller-integration D6 hook `threadEventsTakenOver`：分配 seq、写 `thread_entries`、
推进 `thread_state`、把轮次标 `delivered`、置 `running`）。只有该钩子返回 nil、整个接管事务提交，才
`starting → running`。以下**均不构成 running**：`agent_work_claim`（纯读，D-017）、`RecordDispatch`/
`execution_id` 登记（dispatch ≠ session start）、Node 接受 work / 分配 sandbox・进程、Node 发 `StartAgentSession`
（这些是 dispatch 与物理分配，不等同会话开始证据）。候选 A/B/C/D/E/F（见 mandate §4）：选 **E/F（首条
Thread 事件接管 + 钩子成功提交）**，据此明确 `claim/RecordDispatch/dispatch ≠ running`。

### 4.4 RecordDispatch contract

- 目标：新控制面表 `node_executions`（**未迁移**，Phase 4A 需求），`execution_id` 为主键标识。
  `RecordDispatch`（`agent_work_dispatch` 动作，leaseValid 门控，沿用 `clone_dispatch`/`WorkspaceOperationDispatch`
  的先例）把 `execution_work` 设置为已登记：写入 `node_executions(execution_id, kind, operation_id=run_id,
  work_id=execution_work.id, node_id, input, dispatched_epoch, last_event_sequence=0, result=null)`，并回写
  `execution_work.execution_id`（在 `WHERE execution_id IS NULL` 的 fence 下）。
- owner/生成：`execution_id` 由 Controller 生成并在 `RecordDispatch` 携带（clone 先例：caller 提供 execution_id，
  A 校验唯一后登记）；A 负责记录与 fence，不生成执行身份（plan §8、D-012/D-016）。work `id`（`execution_work.id`）
  A 生成、Phase 3B 已定。
- 事务：`RecordDispatch` 在本次控制面请求自己的事务内完成 `node_executions` 插入 + `execution_work.execution_id`
  回写；一次 RPC 一个事务，提交后 Controller 才向 Node 发 `StartAgentSession`（controller-session D1）。不在 B
  事务内发生；B 只在 takeover 时经钩子重读。
- replay/mismatch：`node_id` 必须等于 `target.node_id`；输入 `input` 与 `execution_work.input` 必须一致，
  否则 `CONFLICT`。同一 work 重复 `RecordDispatch` 携带**相同** `execution_id`+节点+输入 ⇒ 幂等成功（返回既有行）；
  携带**不同** `execution_id` 或输入不匹配 ⇒ `dispatch_conflict` invariant error，不覆盖旧 identity（§6）。
- crash：claim 后、`RecordDispatch` 前崩溃 ⇒ 无 `node_executions` 行、`execution_id` 未写，work 仍可被重取。
  `RecordDispatch` 提交后、Node 收到前崩溃 ⇒ row 已登记（durable truth），Node 未启动；Controller 恢复后按
  `node_executions` 查询既有执行并重发原 `StartAgentSession`（controller-session D1 的既有恢复流程），不再二次
  claim；`execution_id` 唯一性 + work `execution_id` 已写使重放幂等。

### 4.5 execution identity

一个 `execution_work` 至多一个权威 `execution_id`。约束三层：
（1）`node_executions.execution_id` PRIMARY KEY / UNIQUE；
（2）`node_executions.work_id` UNIQUE（一个 work 至多一次登记）；
（3）`execution_work.execution_id` 只在 `WHERE execution_id IS NULL` fence 下回写（CAS），重复 `RecordDispatch`
同 work 不同 id 时 second-writer 因 work_id 或 execution_id 占位而 `dispatch_conflict`。因此 repeated
`RecordDispatch`：同 id 幂等成功，异 id invariant error，永不覆盖旧 identity。
评估：现有 `execution_work.execution_id` nullable 无唯一约束，单独不足（不阻止两个不同执行登记到同一 work，
且无 `node_executions`）。**Phase 4A migration requirement**：新增 `node_executions`；`node_executions.execution_id`
唯一，`work_id` 唯一，回写 `execution_work.execution_id` 用 fence。sschema 提案见 4.13。

### 4.6 ThreadEventsTakenOver contract

真实签名（本仓 seam）：`ThreadEventsTakenOver(t *transaction, run, execution Object, events []Object) error`
（agent_run_hooks.go）。当前无 production caller（Unavailable 默认 fail-closed；businessAgentRunHooks 只实现
RunWorkspaceSettled，其余继承 fail-closed）。A 侧 `TakeOverThreadEvents` 尚未实现（无 `node_event_receipts`、无
`node_executions`、无路由）。设计：
- caller：控制面 `TakeOverThreadEvents` gRPC handler（Phase 4B 实现），在收据写入之后、Controller `EventAck`
  之前调用（controller-integration D2/D6）。
- 在哪个事务：在控制面 takeover **同一事务**内调用（收据已写，钩子未提交时不确认）。钩子不开启自己的事务。
- 入参：`run`/`execution`/`events` 仅是 hint/权威证据载体——B 必须重新查询权威 `issue_runs` 行（phase/
  status/cancel_requested_at/workspace_id/executor_type）与 `execution_work`（execution_id 已登记）与
  `node_executions`（execution_id/kind/last_event_sequence），不接受 caller Object 的业务字段（§7、plan §3
  re-read 原则，同 settleRunWorkspace）。
- 何时调用：对每个成功的 event batch（≤64，见 controller-integration D2）调用一次；非逐事件。重放：控制面只把
  **首次接管、序号连续**的事件交给钩子（D6），因此重放不再次调用钩子；若因事务不回滚但批次判定重复，钩子幂等
  依赖 thread_entries 唯一键（4.9）。
- hook error：返回错误 ⇒ 整个接管事务回滚（收据、`node_executions.last_event_sequence`、`thread_entries` 全部
  撤销）；Controller 不确认、Node 重放（D6「钩子返回错误时整个事务回滚」）。mandate §7 原则成立。
- 第一批事件：running 在首条 **Node 记录**接管事务里推进；thread_state 由 Thread D4 由同名事务判定（首条记录 ⇒
  `active`）。「会话开始事件」这些场合若首条是无记录的开始信号，thread_state 为 `pending`——该分支的 Node 事件
  形状在 Node 协议 ADR（D2 仅定义 `ThreadEvent{record}`）与 controller-session ADR（proposed）中**未明示**；本轮
  记录为待决（G 见 4.16），不发明 synthetic 事件，不猜 running 时点的替代语义。

### 4.7 A/B transaction model

一次 takeover 的理想提交边界（对应 mandate §14）：

```
BEGIN caller(controller) transaction
  A: 校验 execution 已登记(execution_id 匹配 work)
  A: 写入 node_event_receipts((execution_id, sequence), 原事件)   // 收据是确认唯一依据
  A: 更新 node_executions.last_event_sequence
  B: ThreadEventsTakenOver(t, run, execution, events)             // 同事务
       re-read issue_runs / execution_work / node_executions
       CAS phase:'starting' AND status:'dispatched' AND cancel_requested_at IS NULL → running/running
       分配 seq，写 thread_entries(seq=2..n)，推进 thread_state，标 delivered
       hook error → panic(databaseFailure) → 整事务回滚
COMMIT
  Controller 对 batch 内序号发 EventAck
```

- 与 D6 一致：收据业务钩子在「收据写入之后」、同事务；业务钩子只在被事务提交后才收到后续（重放不再次调用）。
- 若钩子失败：A 的收据/`last_event_sequence` 与 B 的 running/`thread_entries` 一起回滚（mandate §14 全部回滚）。
- 提交后 `EventAck`（D6 不变量 4：任何 ack 晚于对应提交）。

### 4.8 seq allocation

- `thread_entries` PK `(run_id, seq)`，seq>0，「Cloud 在写入事务中分配、连续」（Thread D1）。seq=1 已被 first
  prompt 保留（Phase 3A），Phase 4 从 2 续接（G-009）。
- 分配机制：ADR D1 只定「Cloud 分配、连续」，未定实现。决策提案（**D-023，未实现**）：在接管事务内以
  `MAX(seq)` 为底、从 2 起逐条 +1 分配；安全性由两层保证——（a）控制面按执行串行、同执行至多一个在途批次
  （controller-session D2 序号恒按顺序到 Cloud），（b）交叉写经全局 advisory lock（`transact`，Phase 1 基线）
  串行化，批次间 `MAX(seq)` 重读在锁内完成。Concurrent batches（不同执行、同运行）不可能同时进入锁而产生重复
  seq。候选 MAX+1/counter/独立 sequence 状态/deterministic-from-order：ADR 未否决任一，选锁内 `MAX(seq)+1`
  因其与既有 `transact` 串行化一致且零新表。不实现计数器表。
- 不修改 seq=1；同 `(run_id, seq)` 冲突即并发错误（唯一键兜底）。

### 4.9 event idempotency / receipt identity

- 权威事件身份：`node_event_receipts(execution_id, sequence)`（D6 收据表，**未迁移**，Phase 4A 需求）——快照
  原事件，是 `EventAck` 确认的唯一依据（root D4）。
- business 重放身份：`thread_entries(node_execution_id, node_sequence)` UNIQUE（Thread D1）——同 Node 事件重复
  接管幂等，不产生新条目；同键不同内容 `CONFLICT`，原条目不变。两层都在 Phase 4 落地（Node 协议 D2 的
  `sequence` 即 `node_sequence`；`node_execution_id` 即 `execution_id`）。
- 顺序：批次首事件必须 = 该执行已接管最大序号 + 1；缺口或内容不同 ⇒ `ABORTED`/`CONFLICT`，不通告 Controller
  （D2）。Controller 收到冲突清队列、等 Node 重放（controller-session D2）。
- event idempotency key 就是 `(execution_id, sequence)`（收据）+ `(node_execution_id, node_sequence)`（条目）；
  **非**靠 `thread_entries(run_id, seq)` 猜重放身份。同名多次到达视为 no-op（同内容）或 `CONFLICT`（异内容）。
- Phase 4 需要 `node_event_receipts`；未迁移前无法保证「收据是确认唯一依据」与 Node 未确认事件重启重放的正确
  对齐（gap，见 4.13/4.16）。

### 4.10 replay / stale matrix

| 到达 | 处理 |
|---|---|
| takeover 于 phase='running'（同 execution） | stale no-op：CAS affected=0，无新条目（同 `(node_execution_id,node_sequence)` 幂等），不倒退 |
| 同 run 不同 execution_id | `CONFLICT`/invariant reject（execution_work.execution_id 已写另一值；work_id 唯一拒绝第二执行） |
| takeover 于 terminal/releasing/done | 不得回到 running：钩子重读 phase ≠ 'starting' ⇒ no-op（钩子不再推进）；不 regression |
| unknown run / execution 未登记 | 钩子以 invariant 错误回滚（同 settleRunWorkspace：不存在即 invariant 而非 no-op），防止 A 事件在无业务接受方时被确认 |
| 载荷不一（同 execution_id 同 sequence 异 record） | `CONFLICT`，原条目与兼容性收据不变 |

### 4.11 cancel race

本轮不实现 cancel API；只定串行化。cancel 写入与 `ThreadEventsTakenOver` 都可能决定 `starting→releasing/
cancelled` vs `starting→running`。规则（沿用 Phase 1/2 基线 + settleRunWorkspace 先例）：
- 同一全局 advisory lock（`transact`）串行化两个事务；钩子内做权威 re-read + CAS。
- 钩子 CAS：`UPDATE issue_runs SET phase='running' ... WHERE phase='starting' AND status='dispatched' AND
  executor_type='agent' AND cancel_requested_at IS NULL`。cancel 先提交 ⇒ `cancel_requested_at` 已置 ⇒
  钩子 CAS affected=0 ⇒ takeover 视为 stale/no-op，运行按 D6 无会话取消进入 `releasing/cancelled`。
- takeover 先提交 ⇒ 运行已是 `running`；后续 cancel 落入 running 语义（D6：取消是请求，会话收尾后结束，
  进入 delivering→releasing），不破坏已接管 Thread。
- winner = 先获得锁并提交者；后到者经权威 re-read 观察到并采取对应语义（hook CAS / settle 先读 cincel）。
  不引入新 status。

### 4.12 crash / restart

对照 mandate §23 五类崩溃，写入避雷：

| Crash | durable truth | 重试归属 | replay 安全? | duplicate Thread? | 错误 running? |
|---|---|---|---|---|---|
| C1 claim 后、RecordDispatch 前 | 仅 `execution_work` 未登记 | Controller 重取（`agent_work_claim` 仍返回该行） | 是（未登记可被不同 controller 取） | 否 | 否 |
| C2 RecordDispatch 提交后、Node 收到前 | `node_executions` 已登记、work.execution_id 已写 | Controller 查询既有执行、重发 `StartAgentSession`（不再 claim） | 是（`node_executions` 幂等） | 否 | 否（dispatch ≠ running） |
| C3 Node 启动后、takeover 发送前 | 事件在 Node 账本、未确认 | Node 重放（最小未确认序号）；Controller 中继 | 是 | 否（未接管即无条目） | 否 |
| C4 takeover 事务中途 | 事务未提交 ⇒ 全部回滚 | Node 重放该批（未 ack） | 是（重放同批再次接管） | 否（回滚后不落条目） | 否 |
| C5 takeover 提交后、ack 返回前 | `thread_entries`+running 已落库 | Cloud 幂等；Node 重放同批被视为已接管（同 node 键）⇒ no-op | 是 | 否（唯一键） | 若 CAS 已运行 running，重放 no-op 不倒退；若未到 CAS，重放正常推进一次 |

重启恢复：Controller restart 经 `GetDispatch`/`ListPendingDispatches`（root D4 恢复读取，覆盖新种类）找回
registered/unregistered work；Node retry takeover 依 `(execution_id, sequence)` 幂等；Cloud restart 从 DB 恢复
全部状态（running/thread 都在库里）；B 不依赖任何 in-memory state。

### 4.13 migration needs

**Required Migration: YES（Phase 4A/4B）**

1. `node_executions`（Phase 4A）：`execution_id text PK`、`kind`（agent_session|deliver_revision）、
   `operation_id`（IssueRun id，语义引用）、`work_id uuid UNIQUE`（→ execution_work.id）、`node_id`、
   `input jsonb`、`result jsonb`、`dispatched_epoch bigint`、`last_event_sequence bigint default 0`。
2. `node_event_receipts`（Phase 4B）：`(execution_id, sequence)` 主键、`event jsonb`（原事件）。
3. 可选：`issue_runs.thread_state` 值集合已含 `pending/active/idle/ending/ended`（0018）无需迁移；`thread_entries`
   schema 已够（0018）；first-takeover 的 `pending` 语义分支依赖 Node 事件形状，若无则可能只需业务写。
本轮不实现任何 migration；以上仅为 plan 记录。

### 4.14 test matrix

本轮只列 T4-1..T4-16（§26），全部 `DESIGNED / MISSING`（不标 Covered——未实现）；其中 T4-4 claim/dispatch 不
推进运行在 Phase 3B 的 T3B-10/11/12 已由真实实现证明（可标该单项 Covered，见 specs），其余 T4 待 Phase 4A/4B 实现。

| ID | 义务 | Phase | 状态 |
|---|---|---|---|
| T4-1 | RecordDispatch 登记同一 work → 一个 execution_id | 4A | DESIGNED / MISSING |
| T4-2 | RecordDispatch 重放（同 id）幂等 | 4A | DESIGNED / MISSING |
| T4-3 | RecordDispatch 异 id → invariant error | 4A | DESIGNED / MISSING |
| T4-4 | claim/dispatch 不推进运行（仍是 starting） | 4A | **Covered**（T3B-10/11/12, Phase 3B） |
| T4-5 | 权威 takeover：starting → running | 4B | DESIGNED / MISSING |
| T4-6 | takeover 重放：无重复条目、无状态倒退 | 4B | DESIGNED / MISSING |
| T4-7 | 错误 execution 被拒 | 4B | DESIGNED / MISSING |
| T4-8 | terminal/stale run 不回到 running | 4B | DESIGNED / MISSING |
| T4-9 | seq 从 2 续接 | 4B | DESIGNED / MISSING |
| T4-10 | 并发 event 批次 seq 单调唯一 | 4B | DESIGNED / MISSING |
| T4-11 | event 重放：同 Node 事件无重复条目 | 4B | DESIGNED / MISSING |
| T4-12 | takeover 事务回滚（hook 错 → A 收据 + B running 一起回滚） | 4B | DESIGNED / MISSING |
| T4-13 | commit 后重启重放安全 | 4B | DESIGNED / MISSING |
| T4-14 | cancel 先提交 + takeover → 不进 running | 4B | DESIGNED / MISSING |
| T4-15 | takeover 先提交 + 后到 cancel → running 取消语义（不破坏） | 4B | DESIGNED / MISSING |
| T4-16 | 无 Phase 5 副作用（无 delivery/release 工作项） | 4B | DESIGNED / MISSING |

### 4.15 sub-phase split

基于代码事实（无 `node_executions`/`node_event_receipts`/`thread_commands`；无 `TakeOverThreadEvents` 路由；钩子无
caller）拆三子阶段：

- **Phase 4A — A-side Dispatch Registration**：`node_executions` 迁移 + `agent_work_dispatch`（RecordDispatch）+
  `execution_work.execution_id` 回写 + `AgentRunControlPlane`（或直接控制面动作）生产化 + replay/mismatch invariant。
  范围：dispatch 到已登记、可被 Node 开始的执行。**不做 running**（D-017：claim/dispatch ≠ running）。
- **Phase 4B — Thread Takeover + starting→running**：`node_event_receipts` 迁移 + `TakeOverThreadEvents`
  控制面路由（首批 ≤64 连续、首事件 = 已接管最大+1）+ `ThreadEventsTakenOver` 钩子（seq≥2、CAS running、
  thread_state、delivered）+ replay/stale 矩阵 + cancel race + 崩溃恢复。
- **Phase 4C — Thread API/SSE**（后续，公开面）：`thread_commands` + `POST .../thread/messages`（幂等）+
  `GET .../thread` + SSE `issue_run.thread_appended` + `EndSession` 命令 + delivery state / idle。Thread D3/D4/D5、
  controller-integration D3/D5。

理由：4A 是纯 A 控制面（登记身份），4B 是 A/B 交接（running 唯一 authority），4C 是公开 API 与命令（与 running
正交）。两两边界可独立验收（migration + 用户态状态推进互不复用），降低评审风险。若只求连贯最小合入，也可
**单 Phase 4（A 登记 + 4B 接管一起）**，但 thread_commands/API 仍必须后置到 4C——两者不同时落地。

### 4.16 open gaps（Phase 4 新增）

- **G-012（新）** `node_executions` / `node_event_receipts` 未迁移、无 `RecordDispatch`/`TakeOverThreadEvents`
  生产路由、`ThreadEventsTakenOver` 无 caller —— Phase 4A/4B 的实现前提，现状全闭门。
- **G-013（新）** `starting→running` 的「会话开始事件」Node 形状未明示：Node 协议 D2 只定义 `ThreadEvent{record}`，
  controller-session ADR 处于 proposed；thread_state 首条即 `active` 还是可 `pending`（非记录开始信号）待定。
  本轮不发明 synthetic 事件。
- **G-014（新）** seq 分配机制 ADR 未定（已提案 D-023：锁内 `MAX(seq)+1`），实现前需批准。
- **G-015（新）** Thread event 量与限速上限（controller-integration「未决」：单运行 event 量上限、`node_event_receipts`
  保留期限）—— Phase 4B 前需给初值或显式延迟。
- G-001（执行 seam CLOSED；**ThreadEventsTakenOver caller / RecordDispatch / EnqueueThreadCommand** 仍 PARTIAL，
  分别由 4B / 4A / 4C 关闭）。G-005（cancel 写路径，后续 slice 拥有；Phase 4 只定 race 语义 4.11）。
  G-009（seq 续接）Phase 4B 关闭。G-011（invalid workspace）**不在 Phase 4 顺手解决**，仅 fail-closed 保真
  （钩子重读 workspace 无效 ⇒ 不推进 running、保持 gap，是否终态仍 G-011 后续）。

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

### D-011 — Phase 2B plugin mechanic: effect-based `plugin_ensure` for the Agent run-workspace, Scope scoped to Agent run workspaces only

Status: Accepted for Phase 2B implementation (recorded per §0/§24 as an ADR-scope resolution, not a silent workaround)

The approved plugin-step ADR (`20260928-plugin-step-and-run-workspace-release.md`, D2/D3) targets a single Node `InstallPlugins` execution that is no longer a Substrate effect, and D1 adds the `plugin` step to every `create_project` / `create_workspace` / `start`. Both are **approved but not implemented** by the current engine, which today executes plugin installs via the legacy `plugin_ensure` Substrate effect (the pre-D3 model `install_plugin`/`remove_plugin` actually use) and drives those effects over the HTTP control path. Phase 2B makes the following scope decision, consistent with plan §2.6 (which treats the settle check as engine-agnostic and tolerates a "pre-plugin-engine" instance):

- **Mechanic (Option A — effect-based):** the Agent run-workspace `create_workspace` gains an internal `plugin` step that plans one `plugin_ensure` effect for the run's **pinned** agent plugin (`issue_runs.input.agentPluginId` / `agentPluginVersion`, snapshotted at run-create per IssueRun D1/D6 — never re-read from the current roster). The effect writes a `workspace_plugin_instances` row for the run workspace. This reuses the implemented plugin machinery; it is **not** a new public operation kind and does not touch `install_plugin` / `remove_plugin` (D4/D5 unchanged).
- **Failure semantics (plan §2.6 + ADR D2 non-blocking):** a single plugin failure does **not** block the Workspace from being infra-ready; the operation still reaches its terminal and the instance records `failed`. The **settlement** enforces the run's plugin requirement: if the pinned agent plugin instance is `failed` (or absent at the pinned version) the run settles `agent_plugin_unavailable` (§2.6, §2.13 matrix), else `ready`.
- **Applicability scope:** Phase 2B adds the `plugin` step only to **Agent run-workspace** `create_workspace` (`workspaces.issue_run_id IS NOT NULL`). Non-Agent `create_workspace` / `create_project` / `start` keep their current `sandbox → node → clone → done` path unchanged this round (Phase 2B §11 regression requirement). The ADR D1 "all create_* / start get a plugin step installing the Space desired set" and D2/D3 "single Node InstallPlugins execution, no Substrate plugin effect" are **deferred** to a later ADR-implementation slice (Open Gap G-006).
- **why not a different choice:** the mandate (§2B, goals) scopes Phase 2B to "complete create_workspace Agent plugin step / terminal evidence" and forbids changing non-Agent terminal semantics (§11) and breaking standalone install/remove (§5); at the same time it explicitly permits reusing the "existing plugin install declaration / effect / instance writeback / failure semantics" and carrying evidence "by existing result/effect representation" (§3/§4/§7). Option A satisfies all of that within the implemented engine. The ADR D2/D3 Node-execution model remains the approved end-state and is recorded (G-006), not silently re-scoped.

### D-012 — execution declaration identity and exactly-once

Status: Accepted for Phase 3 design

The authoritative identity of the exactly-one session work is the `execution_work` partial-unique row
`(run_id, kind='agent_session')` — at most one unreleased item per run (controller-integration D6
invariant). B declares via the `EnqueueExecutionWork` seam (returns `execution_work.id`); the row is
written by the control plane (A) in the caller's transaction, never by B directly (D6 invariant 7, one
writer per table). `execution_id` is Controller-generated at `RecordDispatch` and lives on
`execution_work`/`node_executions`, not on `issue_runs`; B preallocates no execution/session id. Because
the enqueue runs under the global advisory xact lock and uniqueness is A-enforced by the partial-unique
index plus B's same-tx once-check, declaration and uniqueness are atomic — a run can never end up with
two live session works.

### D-013 — first prompt source and immutability

Status: Accepted for Phase 3 design

Thread ADR D3 is authoritative: the first prompt is written by Cloud as a `thread_entries`
`source=system, kind=user_turn` entry (seq=1), `turn_id` generated by Cloud, rendered from the
run-create-frozen IssueRun input snapshot (issue title/description, task, recent comments ≤10, context
refs) via a fixed template, and delivered inline as the AgentSession `initial_turn` — not separately
queued, no Thread API/SSE streaming at `starting` (that is Phase 4 Thread takeover). Immutability holds
because the source snapshot is already frozen at run-create (IssueRun D1/D6): the rendered first prompt
is stable across retries/replays and never re-reads the live issue/comment/profile. The first-prompt text
renderer itself is unimplemented (G-007).

### D-014 — starting→running authoritative evidence

Status: Accepted for Phase 3 design

IssueRun ADR D3 phase table is authoritative: `running` entry requires "Node reports session started —
first Thread event or session-start event taken over". Neither execution-work declaration nor workspace
ready advances the phase; both are `starting` facts. `running` is driven only by the first
agent-originated Thread event / session-start takeover (`ThreadEventsTakenOver`), which belongs to Phase
4. Phase 3 therefore exits with the run **still `starting`/`dispatched`** and never moves it to
`running` without that takeover evidence (invariant 2). T3-11 verifies this.

### D-015 — Phase 3 retry ownership (post-settle starting scan supersedes D-010's settle-coupled release)

Status: Accepted for Phase 3 design; **supersedes the D-010 "release in the same settle transaction" recommendation**

Session-start declaration is retried durably under A/Node unavailability, but the retry must not move the
run or duplicate work. Decision: extend the Phase 1 dispatcher-loop discipline with a separate **starting
scan** owned by B (sibling component, not a silent widening of the queued-claim loop). Each pass: pick a
`starting` non-cancelled run with no unreleased `agent_session` work → re-check cancel in-tx → write
first-prompt seq=1 → enqueue exactly once; the run already has a ready workspace and an immutable input,
so retries are idempotent via the existing work row / partial-unique index. Chosen over Option A
(same-settle-tx declaration) because it keeps the already-implemented Phase 2B `RunWorkspaceSettled`
(→ `starting` only) unchanged, isolates A-seam transient unavailability from a successful settle, and
puts retry naturally in the scan without re-opening the settle terminal. This explicitly replaces D-010's
suggested settle-coupled release, so Phase 3 does not re-litigate it.

### D-016 — `execution_work` is the A-owned authoritative execution identity（Phase 3B, closes G-008）

Status: Accepted (Phase 3B implementation; extends D-012 into the A side)

The authoritative exactly-once execution identity is a real A-owned `execution_work` row, enforced by a real DB
partial-unique index `UNIQUE(run_id) WHERE execution_id IS NULL` (`execution_work_unregistered_once`), declared
by B through the `EnqueueExecutionWork` seam **in the same caller-owned transaction** as the seq=1 first-produce
write and A's `execution_work` row (§11/§12 — the `execution_work` declaration is the same-tx + A-side
authoritative fact; seq=1 is B's own presentation marker, never the authoritative identity §12). `execution_work.id`
is A-generated at declare time; `execution_id` stays Controller/RecordDispatch-owned and is written later by A on
registration — Phase 3B builds the row, it never occupies `execution_id`. Idempotent replay with identical payload
returns the existing row (no 409, §10); exactly-once is the DB constraint, not
SELECT-before-INSERT/advisory-lock/memory-dedupe/seq=1 (§7).

### D-017 — Controller claim is a pure read; pickup ≠ running（Phase 3B）

Status: Accepted (Phase 3B)

`agent_work_claim` (`agentWorkCommand`) returns the oldest eligible un-registered `agent_session` work
(`execution_id IS NULL AND available_at <= clock_timestamp()`, ordered `(available_at, created_at, id)`) as a
pure read within an existing control-lease-validated request; it changes no `issue_runs` phase/status. Claiming
a work item is neither `running` evidence nor a session start (D-014, §17/§18). Physical start is Phase 4 and
requires Thread/session-start takeover evidence; `execution_id` allocation and registration remain the
Controller/RecordDispatch authority outside Phase 3B.

### D-018 — payload-mismatch replay is a conflict that rolls back the shared transaction（Phase 3B）

Status: Accepted (Phase 3B, §29)

A replay of `(run_id, kind)` that already has an un-registered `execution_work` row must be idempotent **only
when the payload matches**. When the re-declared `kind`/`workspace_id`/`input`/`target` differ from the existing
row, `EnqueueExecutionWork` returns an error; the `AgentRunSessionStart` caller panics `databaseFailure` and the
caller-owned transaction rolls back the seq=1 write too — a run can never end up carrying two conflicting first
produces. Declaration is exactly-once (§7/§21); physical execution is at-least-once and owned by Phase 4, which
serializes on the same A row. A 0-affected `ON CONFLICT DO NOTHING` with no existing un-registered row is an
invariant error (the row was registered concurrently), also rolled back.

### D-019 — `starting → running` authority is the committed `ThreadEventsTakenOver` takeover（Phase 4 design）

Status: Accepted (design; implementation deferred to Phase 4B)

Only the business hook `ThreadEventsTakenOver` succeeding inside its caller-owned `TakeOverThreadEvents`
transaction moves a run `starting → running` (IssueRun D3 entry condition「首条 Thread 事件或会话开始事件被接管」；
controller-integration D6 hook `threadEventsTakenOver`). `execution_work` creation, Controller claim
(`agent_work_claim`, D-017), `RecordDispatch`/`execution_id` registration, Node accepting work, Node allocating a
sandbox/process, and Controller sending `StartAgentSession` are all dispatch or physical-allocation facts and are
**never** running evidence. The takeover transaction MUST have already written `node_event_receipts` before the hook
runs, and the hook re-reads authoritative `issue_runs` before its CAS (4.6/4.7).

### D-020 — `execution_id` is Controller-generated; A records and fences it at `RecordDispatch`（Phase 4 design）

Status: Accepted and implemented (Phase 4A, 2026-09-30). Evidence: `agentWorkDispatch` in
`internal/core/agent_run_execution_work.go`; migration `0020_node_executions.sql`; `T4A-1` (first registration),
`T4A-2` (same replay idempotent), `T4A-3` (different execution_id conflict), `T4A-4` (same execution_id reused on a
different work conflict), `T4A-13/14` (concurrent same/different id converge/conflict on one authoritative row);
`TestMigration0020NodeExecutionsAppliesFreshAndUpgrades` (PK(execution_id), UNIQUE(work_id), FK, pending index).
A generates neither `execution_work.id`-names-work nor Controller `execution_id`; A records `node_executions` and
fences `execution_work.execution_id` in the same transaction (T4A-7/8 atomicity).

`execution_id` is generated by the Controller and supplied on `RecordDispatch` (`agent_work_dispatch`), following the
existing clone precedent (caller supplies the execution identity; A validates uniqueness and records it). A persists
it in a new `node_executions` row and writes it back to `execution_work.execution_id` under a `WHERE execution_id IS
NULL` fence. `execution_id` ownership therefore stays Controller/RecordDispatch-owned (plan §8, D-012/D-016); A owns
the recording table and the fencing, never the identity value. `execution_work.id` (A-generated, Phase 3B) and
`execution_id` (Controller-generated, Phase 4A) are distinct: the former names the queued work, the latter the
dispatched execution.

### D-021 — one `execution_work` at most one authoritative `execution_id`（Phase 4 design）

Status: Accepted and implemented (Phase 4A, 2026-09-30). Evidence: as D-020, plus `T4A-5` (node mismatch conflict),
`T4A-6` (immutable-input mismatch conflict leaves stored input unchanged), `T4A-7` (schema UNIQUE(work_id) forbids a
second node_execution for one work), `T4A-8` (caller rollback leaves no node_execution and NULL binding).

### D-022 — `TakeOverThreadEvents` event identity is `(execution_id, sequence)`; entry identity is
`(node_execution_id, node_sequence)`（Phase 4 design）

Status: Accepted (design; implementation deferred to Phase 4A/4B)

The authoritative event receipt identity is `node_event_receipts(execution_id, sequence)` (root D4,
controller-integration D6) — the confirmation basis for `EventAck`; the business entry-remap key is
`thread_entries(node_execution_id, node_sequence)` UNIQUE (Thread D1), with `node_execution_id` = the execution's
`execution_id` and `node_sequence` = the Node event `sequence`. Replay of the same Node event is a no-op (same
content, same key); the same key with different content is `CONFLICT` and the original entry/receipt is unchanged.
This is **not** derived by guessing from `thread_entries(run_id, seq)`; both receipt and entry must be migrated
(`node_event_receipts`) before Phase 4B can guarantee Node-restart replay alignment.

### D-023 — Thread `seq` allocation is `MAX(seq)+1` inside the takeover transaction（Phase 4 decision proposal, unapproved）

Status: Proposed for approval; not implemented

Thread D1 fixes「Cloud 在写入事务中分配、seq 连续」but does not prescribe the mechanism. Proposal: in the
`ThreadEventsTakenOver` takeover transaction, allocate `seq` from 2 upward as `MAX(seq)+1` per inserted entry. Safety
rests on (a) the control plane serializing per execution with at most one in-flight batch (controller-session D2, so
sequences always arrive in order) and (b) cross-run/cross-batch serialization by the existing global advisory lock
(`transact` baseline, Phase 1), so `MAX(seq)` re-reads inside the lock race no other Writer. Chosen over a counter
column / separate sequence state because it adds no table and reuses the established serialization; a
deterministic-from-Node-order allocation is rejected because (seq=1 is a Cloud system turn) it cannot stay gapless
under replay. Approve before Phase 4B implementation.

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

Status after Phase 3 design (2026-09-30): **fully open and decisive for the Phase 3A/3B split.** The
control-plane tables `execution_work`, `node_executions`, `node_event_receipts`, `thread_commands` exist in
**no migration** (0018 `agent_issue_run_skeleton` carries only the business tables), `controlgrpc.ClaimWork`
is only a tenant-clone stub, and `CreateRunWorkspace`/`EnqueueExecutionWork` have no production
implementation (only `UnavailableAgentRunControlPlane` + test fakes). Phase 3's declaration therefore runs
against a deterministic injected stand-in (Phase 3A); the real A seam is Phase 3B / A's own slice. See
§3.A/B split verdict, D-012, G-007/G-008/G-010.

Status after Phase 3B (2026-09-30): **PARTIAL** — the **execution seam** (`EnqueueExecutionWork`) is real and
production-wired: A-owned `execution_work` persistence (`0019_execution_work.sql`), the authoritative
exactly-once row, and the production plane `StoreAgentRunControlPlane` handed to `store.AgentRunControlPlane`
in `cmd/server/main.go` (G-008 CLOSED; see D-016). The remaining three seam methods
(`CreateRunWorkspace`, `DeleteRunWorkspace`, `EnqueueThreadCommand`) still fail closed with "not implemented"
— `CreateRunWorkspace` is owned by the phase that creates the run Workspace (Phase 2A/B used the injected
stub), `DeleteRunWorkspace` by the Phase 5 releasing/delete path, and `EnqueueThreadCommand` by Phase 4 Thread
control. G-001 as a whole therefore stays PARTIAL until those phases land their seam portions.

Status after Phase 4 design (2026-09-30): **PARTIAL (unchanged)** — the Phase 4 design round adds **no**
production caller. The execution seam stays CLOSED; the seam pieces Phase 4 owns remain outstanding: the
`RecordDispatch`/`execution_id` registration path (Phase 4A, D-020/D-021) is not the isolated A seam method but a
control-plane transaction on `node_executions`; `ThreadEventsTakenOver` caller (Phase 4B) is the
`TakeOverThreadEvents` route; `EnqueueThreadCommand` (Phase 4C) stays fail-closed. None of these are closed by
this design round (mandate §30: schema/contract existence ≠ implementation).

### G-002 — create_workspace plugin step

Owner: A

Not yet implemented (confirmed this design round). In `control.go` `advance`, `create_workspace` advances `sandbox → node → clone → openWorkspace → done`; the `plugin` case exists in the switch only for `install_plugin`/`remove_plugin` operation kinds (migration 0015 widened `step`). The approved plugin-step ADR D1 requires `create_workspace = sandbox → node → clone → plugin → done` with admission opening only **after** the plugin step. Until A wires this, a run workspace's "ready" terminal does **not** imply its agent plugin is installed, so Phase 2's settle-level `agent_plugin_unavailable` check (§18.2.6) is vacuous and must be gated behind this gap.

Do not fix this incidentally inside B Dispatcher/Phase 2 implementation.

Status after Phase 2B (2026-09-30): **CLOSED at the implementation level (D-011).** The Agent run-workspace `create_workspace` gains an internal `plugin` step that plans one effect-based `plugin_ensure` for the run's **pinned** agent plugin (`issue_runs.input`, snapshotted at run-create per IssueRun D1/D6 — never re-read from the current roster) and writes a run-instance-only `workspace_plugin_instances` row (see D-011). Admission stays closed until the plugin step; a succeeded install opens the workspace; a failed install records `failed` and the settlement classifies `agent_plugin_unavailable`. Non-Agent `create_workspace` / `create_project` / `start` and standalone `install_plugin` / `remove_plugin` are unchanged. T2-3 un-deferred and PASS. The approved-but-unimplemented ADR D1/D2/D3 (plugin step on every create_*/start as a single Node `InstallPlugins` execution, plugins no longer Substrate effects) remains deferred to a later slice — see G-006.

### G-003 — terminal workspace operation → `RunWorkspaceSettled`

Owner: A/B integration seam

Confirmed this design round: `RunWorkspaceSettled` (in `agent_run_hooks.go`, signature `RunWorkspaceSettled(t *transaction, run Object, ready bool) error`) has **no callers**; `UnavailableAgentRunHooks` fails closed. The control-plane ADR (`controller-integration` D6) specifies the call site as the run workspace's `create_workspace` terminal transaction, running on the caller's `*transaction` with no self-opened transaction. The A-side `advance` terminal write currently has no awareness of `issue_runs`.

Required for Phase 2, not Phase 1.

Status after Phase 2B (2026-09-30): **CLOSED at the implementation level.** The A-side `advance` terminal (`create_workspace` reaches `done`) calls `settleRunWorkspaceOnDone` → `RunWorkspaceSettled` on the same caller-owned transaction; a hook error panics `databaseFailure` so the operation terminal write and the run's business transition roll back together. Replay of the same terminal event is a no-op via the existing phase CAS. Verified by `TestPhase2BTerminalCreateSettlesRunInSameTx` (commit) and `TestPhase2BHookErrorRollsBackTerminalWrite` (rollback). Production callers are still gated by the real A seam (G-001).

### G-004 — future scalability of global advisory lock

Status: Not a Slice 1 blocker

Current implementation serializes Store mutations through a global PostgreSQL advisory transaction lock.

Slice 1 uses this baseline plus explicit state guards. Replacing or narrowing the global lock is a separate architecture task and must not be mixed into Dispatcher implementation without approval.

### G-005 — user-initiated cancel request write path owner

Owner: B / later slice

IssueRun D6 defines cancel as a request (`issue_runs.cancel_requested_at`) whose effect lands at settle (no-session provisioning/starting → releasing/cancelled/skipped). The Phase 2 settle hook (§18.2.7) only *reads* `cancel_requested_at`; the write path that a user's cancel action uses to set it is not yet owned. That cancel-action surface is a later slice (Thread/API work, Phase 4–5) — out of Phase 2 scope, recorded here so Phase 2 does not invent its own cancel-request mechanism.

### G-006 — ADR plugin-step D1/D2/D3 full implementation (deferred)

Status after Phase 2B (2026-09-30): **open — deferred to a later ADR-implementation slice.**

The approved plugin-step ADR (`20260928-plugin-step-and-run-workspace-release.md`) defines, as unimplemented today: D1 the `plugin` step on every `create_project` / `create_workspace` / `start` installing the Space desired-set; D2 execution as a single Node `InstallPlugins` execution reporting per-plugin results; D3 plugin installs no longer Substrate effects (`plugin_ensure`/`plugin_delete` no longer planned). Phase 2B (D-011) deliberately implements only the Agent run-workspace `create_workspace` internal `plugin` step via the existing effect-based `plugin_ensure` machinery (the model the engine actually uses today); it does **not** rework the plugin execution plane, so the ADR D1 general-create/D2 single-execution/D3 no-Substrate-effect reforms remain outstanding. Closing G-006 requires the Cloud control path + controlpb/gRPC contract + Node `InstallPlugins` execution, and is out of Phase 2B scope.

Status after Phase 3 design (2026-09-30): **unchanged — still open; out of Phase 3 scope.**

### G-007..G-011 — Phase 3 session-start gaps (added this design round)

These are Phase 3-specific gaps raised by the Phase 3 design chapter (see §3.17 for full detail). Status
updated after the Phase 3A implementation round:

- **G-007** (first-prompt renderer) — **CLOSED**: `renderAgentInitialTurn` (fixed Cloud template →
  `thread_entries` seq=1 `source=system`,`kind=user_turn`) is productionized and deterministically tested.
- **G-008** (`execution_work`/`node_executions` tables + `execution_id` write) — **CLOSED for `execution_work`**:
  `0019_execution_work.sql` persists the authoritative work row with a real partial-unique exactly-once index
  (D-016); Phase 3B builds the work row and generates `execution_work.id`, and never occupies `execution_id` (which
  stays Controller/RecordDispatch-owned). `node_executions` remains un-migrated and is a **Phase 4A** obligation
  (D-020/D-021), no longer part of G-008 per se.
- **G-009** (`thread_entries` seq=1 reserved by the first prompt; Phase 4 must continue gapless from seq=2) —
  **OPEN → Phase 4B**: seq=1 is preserved (Phase 3A writes only seq=1); Phase 4 allocates seq≥2 in the takeover
  transaction (D-023 proposal, 4.8).
- **G-010** (derive the D6 `EnqueueExecutionWork` `target` from `sandbox_instances`/`node_instances`) —
  **CLOSED with the minimal deterministic `sessionStartTarget`** (`{workspace_id, sandbox_instance_id, node_id}`
  when a live sandbox/connected Node exists).
- **G-011** (terminal for a `starting` run with invalid/missing workspace) — **PARTIAL**: the fail-closed path
  (leave `starting`, no terminal state, retry loop resurfaces) is implemented + tested (`runWorkspaceLive` /
  `TestAgentSessionStartSoftDeletedWorkspaceFailsClosed`); the authoritative choose-path decision remains for a
  later approval (this round chose fail-closed per §6/§11, never `status=failed` for convenience). **Phase 4 does
  not solve this** — the takeover hook re-checks the workspace and fails closed (no `running`), preserving the gap.

### G-012..G-015 — Phase 4 thread/running gaps (added this design round)

- **G-012 — PARTIAL（Phase 4A 已闭合 A 侧登记部分，4B 仍 Missing）** — `node_executions` migrated
  (0020) + production `RecordDispatch` (`agent_work_dispatch`) + `execution_work.execution_id` fence implemented and
  evidence-backed (D-020/D-021, T4A-1..T4A-14, `TestMigration0020NodeExecutionsAppliesFreshAndUpgrades`). Still
  Missing: `node_event_receipts` un-migrated, `TakeOverThreadEvents` route absent, `ThreadEventsTakenOver` caller /
  `EnqueueThreadCommand` fail-closed — all owned by Phase 4B (D-022). (D-020/D-021/D-022.)
- **G-013** — the "session-start event" shape for `starting→running` is unspecified by the approved ADRs: Node
  protocol D2 only defines `ThreadEvent{record}`; the controller-session ADR (which would pin the first-takeover
  semantics and whether `thread_state` may be `pending` on a non-record start signal) is `proposed`. This design
  round does **not** invent a synthetic event and does not guess the alternative semantics (4.3/4.6/4.16).
- **G-014** — Thread `seq` allocation mechanism is not fixed by Thread D1 (only「Cloud 分配、连续」). Proposed
  `MAX(seq)+1` in the takeover transaction (D-023); needs approval before Phase 4B implementation.
- **G-015** — per-run Thread event-count ceilings and `node_event_receipts` retention are unresolved
  (controller-integration「未决」). Phase 4B needs either an initial bound or an explicit defer.

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

### Round: Phase 2B — A-side create_workspace plugin step + terminal wiring / 2026-09-30

**Plan section executed:**

- Implemented §16 marker "PHASE 2B — IMPLEMENTING_PHASE_2B": G-002 (Agent run-workspace `create_workspace` internal `plugin` step) + G-003 (A-side terminal wire to `RunWorkspaceSettled`), per §12 D-011 and the plan §2B.1/§2B.2. Did **not** begin Phase 3 (session `EnqueueExecutionWork`), did **not** implement the delete execution (Phase 5), did **not** touch standalone `install_plugin` / `remove_plugin`.

**Status:**

- Phase 2 `DONE / ARCHITECTURALLY REVIEWABLE`; Phase 2B CLOSED at the implementation level (G-002, G-003). Current → `READY_FOR_PHASE_3_DESIGN`.

**Files changed (this round):**

- `internal/core/agent_run_terminal.go` (new) — identity predicates, `runPinnedAgentPlugin`, `ensureRunPluginInstance`, `writeRunPluginInstance` (run-instance-only writer), `planAgentRunPluginEffect`, `advanceRunWorkspacePlugin`, `settleRunWorkspaceOnDone`.
- `internal/core/control.go` — `advance` clone case routes run-workspaces to `plugin`; new `plugin` branch for run-workspaces (plan the pinned `plugin_ensure`); `done` block calls `settleRunWorkspaceOnDone`; `planPluginEffect` / `effectResult` route run-workspaces to the run-instance-only writer.
- `internal/core/agent_run_terminal_db_test.go` (new) — settle-level G-002/T2-3 suite + helpers.
- `internal/core/agent_run_terminal_advance_test.go` (new) — A-side drive tests for same-tx settle, hook-error rollback, snapshot-immutability, aggregate isolation.
- `plan/plan.md` — Phase 2 DONE marker, evidence tables, G-002/G-003 status, this round record, §16 marker. No schema, migration, server, frontend, OpenAPI, or specs behavior change.

**Decisions added/changed:**

- D-011 was recorded in the prior design round and is now the implemented Phase 2B mechanic (effect-based `plugin_ensure`, run-instance-only writer). No new decision recorded; no conflict found.

**New gaps:**

- None. G-002, G-003 CLOSED; G-001 (real A seam) stays open for production E2E; G-005 unchanged (Phase 4–5); G-006 (ADR D1/D2/D3 general plugin-step execution plane) stays open as recorded.

**Tests added/changed:**

- `TestPhase2BT2_3PluginFailureSettlesAgentPluginUnavailable` — T2-3 un-defer (plugin failure → `agent_plugin_unavailable`, delete once).
- `TestPhase2BPluginInstalledAtPinnedSettlesReady` / `TestPhase2BPluginVersionMismatchSettlesUnavailable` / `TestPhase2BPluginUpgradeKeepsHistoricalSnapshot` / `TestPhase2BPluginMissingInstanceTreatsReady` / `TestPhase2BPluginPendingSettlesUnavailable` — settle-level G-002 semantics + snapshot immutability (§6).
- `TestPhase2BTerminalCreateSettlesRunInSameTx` — A-side drive: terminal create settles the run in the same transaction (G-003 commit side).
- `TestPhase2BHookErrorRollsBackTerminalWrite` — hook error rolls the whole terminal write back (G-003 rollback side).
- `TestPhase2BPlanUsesPinnedSnapshotVersionImmutable` — plan uses the pinned snapshot version, never the upgraded roster.
- `TestPhase2BRunInstanceWriterLeavesSpaceAggregateAlone` — D-011 run-instance-only writer never perturbs the space aggregate / Agent roster.

**Gate results (all pass; full suite below):**

- `gofmt -l` on changed files — clean.
- `go build ./...` — OK.
- `go vet ./internal/... ./integration` — OK.
- `TEST_DATABASE_URL=... REQUIRE_POSTGRES=1 go test ./internal/core -count=1` — ok.
- `REQUIRE_POSTGRES=1 go test ./integration -count=1` — ok.
- `go test -race -count=1 ./internal/core/... ./integration` — ok.
- `go test ./... -count=1` — ok (exit 0).
- `git diff --check` — clean.

**Scope deviations:**

- None. Silently-resolved blocker (not a workaround): the Phase 2B A-side drive scene needed one `tasks` row per live isolated workspace (deferred constraint trigger, migration 0003 — the A-side Controller would have satisfied it); added it to the test scene helper. No behavior change.

**Next planned step:**

- Phase 3 design (session `EnqueueExecutionWork` from `starting`; D-010 recommendation: release the session work item in the same settle transaction). Do not begin Phase 3 implementation in the same round.

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

**Phase 4A DONE — A-side dispatch registration（实现完成，架构可评审；未进入 4B running）**

Current status: **PHASE_4A_DONE / PHASE_4A_ARCHITECTURALLY_REVIEWABLE / NOT_READY_FOR_PHASE_4B_IMPLEMENTATION**
（4A 只落地 A 侧 `RecordDispatch` 执行登记：`execution_work(agent_session) → Controller claim → Controller 分配
execution_id → agent_work_dispatch → node_executions 行 + execution_work.execution_id 栅栏`。`IssueRun.phase` 全程保持
`starting`/`dispatched`，**绝不进入 running**；4B（Thread takeover、starting→running、thread seq≥2、node_event_receipts、
D-023 MAX(seq)+1）本轮**禁止实现/禁止固化**，其取值与本 marker 判断无关，`PHASE_4B = NOT READY`。）

**Phase 4A implementation record (this round — A-side dispatch registration, D-020/D-021):**

1. **migration** `internal/core/migrations/0020_node_executions.sql` (new): A-owned `node_executions` table
   (execution_id text PK, kind('agent_session'|'deliver_revision'), operation_id uuid 语义引用, work_id uuid UNIQUE
   FK→execution_work(id), node_id, input/result jsonb, dispatched_epoch bigint, last_event_sequence bigint DEFAULT 0,
   created/updated)。`node_executions_pending_node (node_id, created_at) WHERE result IS NULL` 支撑 C2 崩溃恢复。
   execution_id 是 Controller 生成的全局权威执行身份（PRIMARY KEY），A 只记录与栅栏、绝不生成（D-020）；work_id
   UNIQUE 在存储层强制一个 work 至多一个执行（D-021）；`last_event_sequence` 是 4B 占位（DEFAULT 0，本轮只建不读，
   D-023 未实现）。4A 不创建 `node_event_receipts`/`thread_commands`。
2. **production first-registration** (`agentWorkDispatch` in `agent_run_execution_work.go`): Controller 提供
   work_id/execution_id/node_id/input/epoch；A 校验（work 存在、kind=agent_session、node==work.target.node_id、
   input==work.input 不可变快照 json 相等），随后**同一事务** INSERT node_executions + fenced UPDATE
   `execution_work SET execution_id=$2 WHERE id=$1 AND execution_id IS NULL`；fence 影响 0 行→`reject(409)` 整体回滚，
   绝不留下 orphan node_executions（§9/§10）。失败一律 panic/reject 使事务回滚，dispatch 由 `submitted` 幂等包装。
3. **replay / invariant-conflict 语义（D-021）**：同一 work 已注册时，读回注册行，execution_id/node_id/input 均一致
   → 幂等成功返回既有行；**不同 execution_id / node / input → `dispatch_conflict`（409），绝不 overwrite /
   last-write-wins**。全局唯一性（同一 execution_id 复用于另一 work）→ 冲突。首次登记路径对"读到未注册期间他方已落
   以便被戳穿的同 execution_id"也做幂等容忍（同 work+node+input→返回既有行），其余→冲突。
4. **recovery reads（C2，无 lease 的纯读）**：`agent_work_get`（按 executionId 返回已注册 dispatch，404 if nil）、
   `agent_work_pending`（列出 result IS NULL 的在途执行，可按 node 过滤），在 control.go 的 clone_get/clone_pending 旁
   提前分支，替换 Controller 复用既有 execution_id 而不重复登记。
5. **run 状态与 owner 纪律**：claim 与 dispatch 都是纯读/登记，`issue_runs.phase/status/thread_state` 与
   `thread_entries` 均不触碰；4A 代码不 UPDATE issue_runs、不 INSERT thread_entries、不 UPDATE thread_state；A 侧
   CreateRunWorkspace/DeleteRunWorkspace/EnqueueThreadCommand 仍 fail-closed（Phase 5/4）。T4A-9/T4A-10/T4A-16 全程
   phase=starting/status=dispatched、thread seq 恒为 1、无 running。
6. **tests（真实 PostgreSQL white-box，`internal/core/node_executions_db_test.go`）T4A-1..T4A-16**：
   T4A-1 FirstRegistration；T4A-2 SameReplay（幂等）；T4A-3 DifferentExecutionIDConflict；T4A-4
   SameExecutionDifferentWorkConflict；T4A-5 NodeMismatchConflict；T4A-6 InputMismatchConflict（既有 input 不变）；
   T4A-7 NoOrphan（schema work_id UNIQUE 强制一 work 一执行，存储层原子性佐证 §9/§10）；T4A-8 CallerRollback（真实
   dispatch 后调用者事务回滚→无 node_execution 残留、execution_id 复位 NULL）；T4A-9 ClaimDoesNotRun；
   T4A-10 RecordDispatchDoesNotRun；T4A-11 CrashC1UnregisteredRecoverable；T4A-12 CrashC2RegisteredRecoverable
   （agent_work_get / agent_work_pending 复用同一 execution）；T4A-13 ConcurrentSameID（幂等收敛）；
   T4A-14 ConcurrentDifferentIDs（恰一冲突、恰一权威注册）；T4A-15 NonAgentRegression（clone/未知 agent_work_ 路由不被
   劫持）；T4A-16 NoPhase4BSideEffects（无 node_event_receipts、thread seq=1、不 running）。
7. **migration 升级测试** `integration.TestMigration0020NodeExecutionsAppliesFreshAndUpgrades`：从 0019 升级应用
   0020 + 重复 Migrate/CheckSchema 幂等，断言 PK(execution_id)、UNIQUE(work_id)、FK(work_id→execution_work)、
   `node_executions_pending_node` 部分索引（result IS NULL）。

**Files changed this round (Phase 4A):**

- `internal/core/migrations/0020_node_executions.sql` (new)。
- `internal/core/agent_run_execution_work.go`: `agentWorkCommand` 新增 `agent_work_dispatch`（submitted 包装）、
  `agent_work_get`、`agent_work_pending`；新增 `agentWorkDispatch`（首次登记 + 幂等重放 + invariant 冲突，D-021）。
- `internal/core/control.go`: 提前分支 `agent_work_get`/`agent_work_pending`（无 lease 纯读，C2 恢复）。
- `internal/core/node_executions_db_test.go` (new): T4A-1..T4A-16。
- `internal/core/agent_run_dispatcher_db_test.go`: `seedDispatchScene` collab workspace slug 每场景唯一（支撑
  单 schema 多 scene 的 T4A-4）。
- `integration/migration_upgrade_path_test.go`: 新增 0020 升级测试。
- `plan/plan.md`, `plan/plan-zh.md`: marker 与决策日志更新（§16）。

**New gaps this round:** none that Phase 4A owns. **G-008 / G-001 execution seam** continue as covered by Phase 3B。
**G-001 stays PARTIAL**（`CreateRunWorkspace`/`DeleteRunWorkspace`/`EnqueueThreadCommand` 仍 fail-closed，归各自
Phase）；**G-012 PARTIAL**（`node_executions` 迁移 + production `RecordDispatch`/execution_id 栅栏已实现，4A 部分
Covered；`node_event_receipts` 未迁移、`TakeOverThreadEvents` 未实现——4B 部分仍 Missing）；**G-013、G-014
unchanged/open**（starting→running 事件形状、D-023 seq 分配均需后续批准与 4B）；G-011 unchanged（fail-closed 已做、
choose-path 待批）。

**Tests added this round (Phase 4A):** 见上列 T4A-1..T4A-16 全文。D-020/D-021 由 T4A-1/2/3/4/5/6/13/14（登记、幂等、
冲突、并发权威）与 0020 迁移测试直接证明；§9/§10 原子性由 T4A-7/8 证明；C2 恢复由 T4A-12 证明；「claim 与登记均不
running」由 T4A-9/10/16 证明；无 4B side-effect 由 T4A-16 证明。

**Gate results (this round):** gofmt/gofumpt clean on all Phase 4A files（`go tool gofumpt -l -extra` 对本次改动文件
无输出）；`go build ./...`；`go vet ./internal/... ./integration`；`go test ./internal/core -count=1`；
`go test ./integration -count=1`；`go test -race -count=1 ./internal/core/... ./integration`；`go test ./... -count=1`；
`git diff --check` clean。`task format:check`/`task lint` 仍报告**既有基线**（皆 committed、未改动文件；
`agent_run_control.go:42/76`、`agent_target.go:47/58`、`agent_run_control_test.go:65`、`agent_run_settle_db_test.go:64`、
`space_agents.go:37`，另 format 门还含 `agent_run_terminal_db_test.go`）；按 mandate §1/§38 不 reformat 这些无关旧文件——
非 4A 回归。4A 新增文件与改动文件在 lint/format 门中零新增 finding。

**Previous round marker (Phase 4 design only, before 4A implementation), preserved:**

**Phase 4 design facts (see the "Phase 4 — Thread / Running Lifecycle 详细设计" chapter):**

1. **running authority** = the committed `ThreadEventsTakenOver` takeover transaction (4.3); `claim`,
   `RecordDispatch`, Node dispatch/StartAgentSession and physical allocation are all ≠ running.
2. **execution_id** = Controller-generated, A records `node_executions` + fences `execution_work.execution_id`
   (4.4/4.5, D-020).
3. **event identity** = `node_event_receipts(execution_id, sequence)` (receipt) + `thread_entries(node_execution_id,
   node_sequence)` UNIQUE (entry); both required migrations for 4B (4.9, D-022).
4. **running transition** = hook CAS `phase='starting' AND status='dispatched' AND cancel_requested_at IS NULL →
   running`, `status='running'` per IssueRun D3, `thread_state` per Thread D4 (4.6/4.7/4.8, 4.11 cancel race).
5. **PHP split** = Phase 4A (A-side dispatch registration) / 4B (Thread takeover + running) / 4C (Thread API/SSE) —
   4C is mandatory-apart, 4A and 4B may merge (4.15).

**Files changed this round (Phase 4 design):** only `plan/plan.md` (+ this §16 echo, the "Phase 4 — Thread /
Running Lifecycle" chapter, D-019..D-023, G-012..G-015) and `plan/plan-zh.md` (mirror). **No** `internal/core`,
**no** migration, **no** OpenAPI/contract, **no** specs test-case evidence changed to `Covered` (all T4-* stay
`DESIGNED / MISSING` except T4-4 which Phase 3B already covers).

Previous rounds: Phase 3A (B-owned Session Start Core) DONE; then this round — **PHASE 3B IMPLEMENTATION —
COMPLETE** (A-side `execution_work` persistence + production `EnqueueExecutionWork` seam). Phase 3B closes
G-008 (authoritative execution identity persistence / exactly-once via a real DB partial-unique constraint) and
the **execution seam portion** of G-001 (the real A `EnqueueExecutionWork` is now wired in production
`cmd/server/main.go`); G-001 as a whole stays PARTIAL because the other A seam methods (`CreateRunWorkspace`,
`DeleteRunWorkspace`, `EnqueueThreadCommand`) still fail closed until their owning phases (2A/2B own-create,
Phase 5 delete, Phase 4 thread command). The run stays `starting`/`dispatched` throughout — `execution_work`
pickup (`agent_work_claim`) is a Controller read, physically starting (`phase=running`) is Phase 4 and requires
takeover evidence (D-014). Phase 3 = "queued execution_work → claimable/dispatchable by Controller".

Phase 3 design (D-012..D-018), as implemented across 3A + 3B:

1. **D-012** — execution identity is the `execution_work (run_id, kind='agent_session')` partial-unique row; B
   never writes control tables; `execution_id` is Controller/RecordDispatch-owned on A rows, not `issue_runs`.
   Phase 3A implements the B-side once-guard for releasing work using `thread_entries(seq=1)` as B's own durable
   declaration marker; **Phase 3B adds the authoritative A-owned `execution_work` row in the same transaction
   (D-016)**, so the DB partial-unique index `execution_work_unregistered_once` is the real exactly-once
   enforcement (G-008 CLOSED, §7/G-008 condition).
2. **D-013** — first prompt is the Thread's first entry (`thread_entries` seq=1, `source=system`,`kind=user_turn`),
   rendered from the immutable run-create input snapshot via a fixed Cloud template, delivered inline as the
   AgentSession `initial_turn`; no Thread API/SSE streaming at `starting`. G-007 CLOSED (renderer productionized:
   `renderAgentInitialTurn`).
3. **D-014** — Phase 3 ends at exactly-one AgentSession work declaration with `issue_runs.phase` **staying
   `starting`**; `running` requires the first Thread/session-start takeover evidence (`ThreadEventsTakenOver`,
   Phase 4), per IssueRun ADR D3. T3-11 guards invariant 2. Phase 3B records **D-017** — Controller pickup
   (`agent_work_claim`) is a pure read that changes no phase; it is neither `running` evidence nor a start.
4. **D-015** — retry lives in a separate post-settle B-owned **starting scan** (sibling loop, exactly-once via the
   idempotent seq=1 marker); supersedes D-010's "release in the same settle transaction". Implemented as
   `scanStartingAgentRuns` + `StartQueuedAgentSessionsOnce` (+ `AgentRunSessionStart.StartSession`), wired as a
   server loop at a 10s cadence (IMPLEMENTATION CHOICE — §3 does not fix the seconds).
5. **D-016** (new, Phase 3B) — `execution_work` is the A-owned authoritative execution identity. B declares it via
   the `EnqueueExecutionWork` seam in the **same caller-owned transaction** as the seq=1 write; A generates the
   `execution_work.id` (`execution_id` stays Controller/RecordDispatch-owned; G-008 condition "A-generated id" is
   the work-row `id`). Replay with an identical payload (`INSERT ... ON CONFLICT (run_id) WHERE execution_id IS
   NULL DO NOTHING` then re-read) is idempotent success returning the existing row; a **payload mismatch** on the
   re-read is an invariant/conflict error that rolls back the whole transaction (D-018) — never silently surface a
   different first produce. The real DB partial-unique index (`execution_work_unregistered_once`) enforces
   exactly-once, not SELECT-before-INSERT, advisory-lock, memory-dedupe, or seq=1 (§7).
6. **D-018** (new, Phase 3B) — payload-mismatch replay semantics. When the identical `(run_id, kind)` already has
   an un-registered execution_work row whose `input`/`target`/`workspace_id`/`kind` differ from the re-declared
   ones, `EnqueueExecutionWork` returns an error; the `AgentRunSessionStart` caller panics `databaseFailure`, and
   the caller-owned transaction rolls back the seq=1 write too — a run can never carry conflicting declarations
   (declaration exactly-once; physical execution at-least-once later in Phase 4, §21).

**Files changed this round (Phase 3B):**

- `internal/core/migrations/0019_execution_work.sql` (new): A-owned `execution_work` table (id/tenant/run
  workspace/kind/'agent_session'|'deliver_revision'/input/target/available_at/execution_id/created/updated),
  `execution_work_unregistered_once` partial unique `ON execution_work(run_id) WHERE execution_id IS NULL`, and
  `execution_work_pickup` partial index `(kind, available_at, created_at) WHERE execution_id IS NULL`.
- `internal/core/agent_run_execution_work.go` (new): `StoreAgentRunControlPlane` — the production A seam
  implementing only the real `EnqueueExecutionWork` (INSERT + ON CONFLICT idempotent re-read with payload-match
  guard; seam is a pure same-transaction call, no new tx/goroutine/post-commit/memory queue, §9/§10); the other
  seam methods fail closed with "not implemented". Plus `agentWorkCommand` (`agent_work_claim`: pure read of the
  oldest eligible, `available_at`-ready, un-registered `agent_session` work, ordered `(available_at, created_at,
  id)`; ownership of the identity is Controller `RecordDispatch`, exercised per the control-lease validation).
- `cmd/server/main.go`: wired `store.AgentRunControlPlane = core.NewStoreAgentRunControlPlane()` after
  `configureCollaboration(...)` — the production Store now uses the real A seam for `EnqueueExecutionWork`.
- `internal/core/control.go`: added the `agent_work_` prefix dispatch branch (`agentWorkCommand`) alongside the
  clone/lease action gates, so a Controller principal can claim work.
- `internal/core/execution_work_db_test.go` (new): real-PostgreSQL white-box suite T3B-1..T3B-14 + the §28
  concurrency (two goroutines → one row) + §29 payload-mismatch replay tests.
- `integration/migration_upgrade_path_test.go`: added `TestMigration0019ExecutionWorkAppliesFreshAndUpgrades` — fresh
  apply + upgrade-from-0018 path both green, table + both indexes present, index predicate contains
  `execution_id IS NULL`, FKs on run/workspace/tenant hold.
- `plan/plan.md`, `plan/plan-zh.md`: markers updated (§16).

**New gaps this round:** none that Phase 3B owns. G-008 CLOSED (real `execution_work` persistence + DB-unique
exactly-once, §22 items all met); **G-001 PARTIAL** — the execution seam (`EnqueueExecutionWork`) is real and
production-wired, the remaining A seam methods (`CreateRunWorkspace`/`DeleteRunWorkspace`/`EnqueueThreadCommand`)
stay fail-closed placeholders owned by their phases; G-010 (target derivation) resolved by the deterministic
`sessionStartTarget` (persisted verbatim into `execution_work.target`, §20); G-011 PARTIAL (fail-closed path done;
authoritative choose-path awaits later approval); G-009 (roster-current/real-agent controller target) remains OPEN,
owned by Phase 4.

**Tests added this round (Phase 3B):** T3B-1 EnqueueOneRow; T3B-2 ReplaySameRow (idempotent, existing id
returned; supersedes the old T3B-6 duplicate-declaration case by folding it in); T3B-3 UniqueGuard (second
un-registered declaration blocked by the DB index); T3B-4 SeamErrorRollsBack; T3B-5 CallerFailureRollsBack;
T3B-7 DirectAReplay (authoritative A-side row, not seq=1); T3B-8 SnapshotImmutable (snake-cased
`agent_plugin_version` frozen, never the current roster); T3B-9 CancelDeclaresNothing; T3B-10 NeverRunning
(phase stays `starting`/`dispatched` through claim); T3B-11/T3B-12 ControllerPickup (a Controller claims the
oldest eligible `agent_session` work via `agent_work_claim`); T3B-13 NonAgentExcluded; §28
ConcurrencyExactlyOnce (two goroutines, one row); §29 PayloadMismatch (mismatched replay rolls back the shared
transaction). G-008's "real-Postgres test, no fake counter" (§22) is met — all evidence is over `execution_work`,
not a test-only counter.

**Gate results (this round):** gofmt clean on all Phase 3B files (exit 0 on `go tool gofumpt -l -extra`
for my changed files); `go build ./...`; `go vet ./...`; `go test ./internal/core -count=1`;
`go test ./integration -count=1`; `go test -race ./internal/core/... -count=1` + `go test -race ./integration
-count=1`; `go test ./... -count=1`; `git diff --check` clean. **Caveat, not caused by Phase 3B:** `task lint`
still reports 7 pre-existing findings, ALL in committed, unchanged files (`agent_run_control.go:42/76`,
`agent_target.go:47/58`, `agent_run_control_test.go:65`, `agent_run_settle_db_test.go:64`, `space_agents.go:37`);
`git diff --name-only` confirms none of them are in this round's diff. They fail under the go.mod-pinned
gofumpt v0.12.0/go1.27.1 + golangci-lint v2, which are stricter than the formatter the committed code was
formatted with (environment/tooling-version drift). Per mandate §1/AGENTS.md I did **not** reformat or otherwise
modify that pre-existing committed code, since these are unrelated, pre-existing findings — not a Phase 3B
regression, and touching them would be an unrelated change to committed files.

Phase 3 design (D-012..D-015), as implemented:

1. **D-012** — execution identity is the `execution_work (run_id, kind='agent_session')` partial-unique row; B
   never writes control tables; `execution_id` is Controller/RecordDispatch-owned on A rows, not `issue_runs`.
   Phase 3A implements the B-side once-guard for releasing work WITHOUT a control table: `thread_entries(seq=1)`
   is B's own durable declaration marker (`INSERT ON CONFLICT DO NOTHING`); G-008 stays OPEN for the real
   execution_work persistence (A side, Phase 3B).
2. **D-013** — first prompt is the Thread's first entry (`thread_entries` seq=1, `source=system`,`kind=user_turn`),
   rendered from the immutable run-create input snapshot via a fixed Cloud template, delivered inline as the
   AgentSession `initial_turn`; no Thread API/SSE streaming at `starting`. G-007 CLOSED (renderer productionized:
   `renderAgentInitialTurn`).
3. **D-014** — Phase 3 ends at exactly-one AgentSession work declaration with `issue_runs.phase` **staying
   `starting`**; `running` requires the first Thread/session-start takeover evidence (`ThreadEventsTakenOver`,
   Phase 4), per IssueRun ADR D3. T3-11 guards invariant 2.
4. **D-015** — retry lives in a separate post-settle B-owned **starting scan** (sibling loop, exactly-once via the
   idempotent seq=1 marker); this supersedes D-010's "release in the same settle transaction". Implemented as
   `scanStartingAgentRuns` + `StartQueuedAgentSessionsOnce` (+ `AgentRunSessionStart.StartSession`), wired as a
   server loop at a 10s cadence (IMPLEMENTATION CHOICE — §3 does not fix the seconds).

**Files changed this round (Phase 3A):**

- `internal/core/agent_run_session_start.go` (new): `AgentRunSessionStart.StartSession` (authoritative in-tx
  re-read, cancel-first §5, thread_entries seq=1 once-guard §14, A/B atomic {seq=1 + EnqueueExecutionWork} §15,
  phase/status untouched §16, G-011 fail-closed workspace-alive check), `renderAgentInitialTurn` (D-013/G-007),
  `scanStartingAgentRuns`, `StartQueuedAgentSessionsOnce`, `sessionStartTarget`.
- `internal/core/agent_run_session_start_db_test.go` (new): white-box PostgreSQL tests.
- `cmd/server/main.go`: wired the Phase 3A recovery loop via `pluginmarket.RunSyncLoop(ctx,
  store.StartQueuedAgentSessionsOnce, 10s, …)` alongside the dispatch loop.
- `plan/plan.md`, `plan/plan-zh.md`: markers updated (§16). No migration, no schema, no OpenAPI/contract change;
  no Store struct or `control.go` change (the session-start accessor builds a fresh `AgentRunSessionStart`).

**New gaps this round:** none. G-007 CLOSED; G-008 (real execution_work) REOPEN/STILL OPEN (A seam, Phase 3B);
G-009 (roster-current/real-agent controller target) OPEN; G-010 (target derivation) resolved by the minimal
deterministic `sessionStartTarget`; G-011 (invalid/missing run Workspace) PARTIAL — fail-closed path implemented +
tested, the authoritative choose-path decision remains for a later approval; G-001 (real A seam) OPEN.

**Tests added this round:** `TestAgentSessionStartOnceDeclaresExactlyOne` (T3-1/10/11/14),
`TestAgentSessionStartReplayIsNoOp` (T3-2/14), `TestAgentSessionStartCascSnapshotImmutable` (T3-3/12),
`TestAgentSessionStartCancelledRunDeclaresNothing` (T3-4, T3-5 serialized), `TestAgentSessionStartSeamRollsBackSeq1`
(T3-6/13), `TestAgentSessionStartSoftDeletedWorkspaceFailsClosed` (T3-8, G-011). T3-7 (partial-unique execution
identity) DEFERRED_TO_PHASE_3B_G008; T3-9 (cross-path settle→session-start at the A boundary) PASS at the B once
boundary, full A-side execution_work persistence is Phase 3B.

**Gate results:** all pass — gofmt clean; `go build ./...`; `go vet ./internal/... ./integration`; `go test
./internal/core -count=1`; `go test ./integration -count=1`; `go test -race ./internal/core/... -run
'TestAgentSessionStart|TestAgentRunDispatch|TestPhase2' -count=1`; `go test ./... -count=1`; `git diff --check`.

**Scope deviations:** none. No commit, push, PR, or stage (staged:no / commit:no / push:no / PR:no).

**Next planned step:** Phase 3B — the real A-side `EnqueueExecutionWork` seam (execution_work persistence,
G-008/G-001) + the Controller session-start hand-off; then Phase 4 (Thread `running` turnover, `ThreadEventsTakenOver`).

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


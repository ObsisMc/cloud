# AgentRunDispatcher Implementation Plan

> Status: **Living design / execution record**  
> Scope owner: **B — Cloud business / orchestration**  
> Current target: **G-032 Implementation Slice — Workspace Delete Give-Up → `done` without lying about deletion success**（已批准的 IssueRun **D8**：唯一放弃条件 = 运行 Workspace 的 Node 状态未知超过 D5 的 `delivery_unreachable_after` / 同一事务 `releasing → done` + `failure_reason = workspace_unavailable` + 业务 `status` 不变 / 残留 Workspace 行与终结失败态 `delete_workspace` operation 如实保留 / 无后台清理机制 / 核心用例四条义务 `Missing` → `Covered`）
> Current Phase: **5 — Delivery / Releasing / Done（删除侧的上限已实现并取证；G-032 = CLOSED）**
> Current Status: **`G032_IMPLEMENTATION_DONE`**（G-032 = `CLOSED`；`G-037` 仍 `OPEN`、`G-034` 仍 `OPEN`、`G-029` 仍 `DEFERRED / NON-BLOCKING`，G-020 / G-021 / G-025 / G-026 / G-027 / G-028 / G-035 / G-036 不变；新增 `cloud/integration/agent_run_release_giveup_test.go`（G032-1..G032-9，真实 PostgreSQL）；`format:check` / `lint`（仅剩 G-028 的既有基线）/ `build` / `test` / `test:race` 全部通过；**未** stage、**未** commit、**未** push、**未**开 PR）
> Previous target: **Revision Completion Slice — object store + verified Revision registration + Project Final Acceptance**（`object_store` 配置 / 对象存储客户端 / `0023_revisions.sql` / `GrantRevisionUpload` 与授权约束 / D4 的两步校验 / 登记与释放同事务 / 重放与冲突 / G-031・G-032・G-033 已批准文本同步 / Revision 核心用例证据 / 全项目 Final Acceptance）
> Previous Phase: **5 — Delivery / Releasing / Done（Revision 半边已实现：对象存储、上传授权、亲自校验后登记；随后跑全项目 Final Acceptance）**
> Previous Status: **`REVISION_COMPLETION_SLICE_DONE` + `PROJECT_FINAL_ACCEPTANCE_PASSED`（G-030 = `CLOSED`；两个 Revision ADR 的已批准契约在 Cloud 侧落地；`format:check` / `lint`（仅剩 G-028 的既有基线）/ `test` 420 PASS・0 FAIL・0 SKIP / `test:race` 0 DATA RACE / `build` 全部通过；新增 G-035 / G-036 / G-037 三项**已披露、未修**的缺口：公开读未投影 D5 的 Revision 元数据、健康检查不报告对象存储、真实 MinIO 上的三项验收义务仍 `Missing`；**未** stage、**未** commit、**未** push、**未**开 PR）**
> Earlier target: **Phase 5 — Revision ADR Decision & Amendment Round**（converge B-1..B-4 + P-1..P-3 into one approvable Revision contract; write the exact amendment proposals for G-031 / G-032; **no production code, no `status` change**）
> Earlier Phase: **5 — Delivery / Releasing / Done（decision & amendment round; the two `proposed` Revision ADRs were amended in place and explicitly marked 待审批）**
> Earlier Status: **`REVISION_ADR_DECISIONS_READY_FOR_HUMAN_APPROVAL`（B-1..B-4 与 P-1..P-3 各有唯一推荐方案且已写入 Cloud Revision ADR 正文；Node Revision ADR 补入最小回显条款；G-031 / G-032 输出精确 amendment 提案（**未**改写 approved ADR）；新增 G-033（IssueRun D5 澄清）与 G-034（放弃后仍可认领的交付工作项）；两个 ADR 的 `status` 仍为 `proposed`——**未**自行批准、**未**实现任何 Revision 生产路径）**
> Earlier target: **Revision ADR Approval Round — G-030 approval-readiness audit + G-029/G-031/G-032 clarification**
> Earlier Status: **`REVISION_ADR_APPROVAL_BLOCKED`（`REVISION_ADR_NOT_READY_FOR_APPROVAL`：B-1..B-4 + P-1..P-3；G-029 = `DEFERRED / NON-BLOCKING`）**
> Earlier target: **Phase 5 Batch 2 — Delivery → Releasing → Done（交付/释放/终态半边 DONE；Revision 登记半边 BLOCKED by G-030）**
> Earlier Status: **`PHASE_5_BATCH_2_BLOCKED`（4A/4B/4C/5B1 已实现；交付执行/结算/重试/放弃/释放/删除终态/`done` 已实现；G-019 = CLOSED；受阻项 = G-030 `proposed` ADR）**
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

Status: **Phase 4A IMPLEMENTED（A-side dispatch registration）→ Phase 4B IMPLEMENTED（Thread takeover / running authority）→ Phase 4C DESIGN DONE（Thread API / SSE / commands / lifecycle；设计轮，未实现）**。
Phase 4B 的 G-013/G-014 由架构决议轮关闭，本轮按该决议落地实现（见 "## Phase 4B — Thread Takeover + `starting→running` 架构决议"）。
Phase 4C 的完整设计见 "## Phase 4C — Thread API / SSE / Thread Commands / Lifecycle 详细设计"（§4C.0–§4C.20）。

Phase 4A delivered (D-020/D-021, G-012 PARTIAL):

- new A-owned `node_executions` migration (0020): execution_id PK, work_id UNIQUE FK→execution_work, pending partial index by node
- production `agent_work_dispatch` (`agentWorkDispatch`) — Controller supplies execution_id/node/input; A records `node_executions` + fences `execution_work.execution_id` in the same transaction, replay idempotent, different id/node/input → `dispatch_conflict`, never overwrite
- recovery reads `agent_work_get` / `agent_work_pending` (no-lease; C2 crash reuse of the recorded execution)
- claim & dispatch never run the run: `issue_runs.phase` stays `starting`/`dispatched`, thread seq stays 1, no running
- NO Phase 4B machinery: `node_event_receipts`/`TakeOverThreadEvents`/starting→running/thread seq≥2/D-023 seq allocation all forbidden and absent (T4A-16)
- tests T4A-1..T4A-16 (real PostgreSQL) + `TestMigration0020NodeExecutionsAppliesFreshAndUpgrades`

Phase 4B delivered (D-019/D-022/D-023/D-024, D-026; this round — implementation):

- migration `0021_node_event_receipts.sql` — `node_event_receipts(execution_id, sequence, event, created_at)`, PK `(execution_id, sequence)`, FK → `node_executions(execution_id)`, `event jsonb` object CHECK, `created_at` index. The only new 4B table; no business-table change, no existing migration touched.
- A side, `internal/core/agent_run_thread_takeover.go` — control action `agent_thread_takeover` (route added in `control.go`): batch classification (1..64 events, strictly ascending gap-free, first ≤ last+1, overlap must replay identically → `receipt_conflict`), receipt inserts, the B hook, then the fenced `last_event_sequence` advance — all in the caller-owned transaction.
- B side, `internal/core/agent_run_thread.go` — `Store.threadEventsTakenOver`: authoritative re-read of `issue_runs`/`node_executions`/`execution_work`/seq=1, `kind` from the `ora-history` type tag against the closed known set, echo dedupe by `turn_id`, `MAX(seq)+1` allocation with `UNIQUE(node_execution_id, node_sequence)` as the business duplicate guard, and the `starting→running` CAS (`thread_state='active'`).
- wiring: `businessAgentRunHooks.ThreadEventsTakenOver` (`agent_run_settle.go`), `NewBusinessAgentRunHooks`, `store.AgentRunHooks` in `cmd/server/main.go` (G-003 seam), `controlgrpc.agentRunService.TakeOverThreadEvents` registered in `controlgrpc/server.go`.
- tests: T4B-1..T4B-19 real-PostgreSQL matrix (`internal/core/agent_run_thread_takeover_db_test.go`, incl. the §42 concurrency case), gRPC acceptance `TestAgentRunThreadTakeoverOverGRPC` / `...GRPCRejections`, migration `TestMigration0021NodeEventReceiptsAppliesFreshAndUpgrades`. T4B-14 (post-takeover cancel) and T4B-18 (event cap) are **DEFERRED** with reasons in §4B.13.
- **G-013 CLOSED** (D-024, now implemented): running authority = first real Node Thread record takeover + committed `ThreadEventsTakenOver`; no synthetic session-start event; echo dedup keeps seq=1 immutable; empty batch rejected
- **G-014 CLOSED / D-023 ACCEPTED** (now implemented): `seq` = `MAX(seq)+1` from 2 inside the takeover transaction; serialization from approved facts (one session execution per run + global advisory lock + PK(run_id,seq)), **not** the proposed controller-session ADR; Node `sequence` ≠ Thread `seq`
- **G-009 CLOSED**: seq=1 preserved, seq≥2 continuous across batches, replay allocates nothing
- **G-015 PARTIAL** (unchanged): initial bounds were given (cap 200,000; retention 30d post-`done`) but have **no approved ADR value**, so the enforcement is deliberately not implemented; the `created_at` index is the hook the future sweep will use
- **G-016 → CLOSED by the Phase 4C design round** (D-4C-01, 2026-10-08): `thread_state='pending'` is materialized by B in the `StartSession` transaction; the 4B-era note below is preserved as history
- **G-016 OPEN / NON-BLOCKING** (as recorded in the 4B round, now superseded): no in-scope writer materializes the literal `thread_state='pending'`; the takeover writes exactly `NULL/pending → active` (see D-026)
- **G-012 → PARTIAL** re-evaluated: the takeover path is now real, but the A-side `ThreadEventsTakenOver` caller still exists only for a Node that Cloud has a Controller for; the production Controller loop remains the open half

Phase 4C designed (D-4C-01..D-4C-12). **This paragraph and the bullets below are preserved as the 4C design
round's record; Phase 4C is now IMPLEMENTED** (`PHASE_4C_COMPLETE`: migration
`0022_thread_api_and_commands.sql`, Thread GET/POST, `/thread/end`, SSE invalidation, thread commands,
`pending/active/idle/ending` lifecycle):

- `thread_state` ownership + `pending` materialization (**G-016 CLOSED**), Thread GET (snapshot/pagination/tail/`before`), Thread POST (ownership, transaction, 7-case idempotency, state acceptance matrix), `EnqueueThreadCommand` contract and the four identities, SSE invalidation + unified `after`, `active`/`idle` authority, `ending` authority + `ended` = `SessionEnded` (Phase 5), cancel/terminal interaction, the 4C migration decision, the 10-scenario concurrency matrix, the API error taxonomy and T4C-1..T4C-34. See `## Phase 4C — …` (§4C.0–§4C.20).
- new gaps: G-017 (tail/`before`/`idleSince` extend approved D5), G-018 (user-end endpoint missing from the ADR), G-019 (`SessionEnded`/`discarded` = Phase 5), G-020 (SSE delivery not guaranteed ⇒ client polling), G-021 (multi-worker command partitioning)

Includes:

- thread_entries seq≥2 continuation + takeover hooks (4B — **IMPLEMENTED**, D-023/D-024)
- starting → running with a single authority = committed ThreadEventsTakenOver (D-019 — **IMPLEMENTED**)
- execution_id registration via RecordDispatch (4A, D-020/D-021 — **IMPLEMENTED**)
- node_event_receipts event identity (4B, D-022 — **IMPLEMENTED**)
- Thread command control seam (4C — **DESIGNED**, not implemented)
- API/SSE work (4C — **DESIGNED**, not implemented)
- Thread lifecycle `pending/active/idle/ending` (4C — **DESIGNED**, not implemented); `ending → ended` + `discarded` remain Phase 5

> **Superseded by later rounds:** the three 4C bullets above are **IMPLEMENTED** as of `PHASE_4C_COMPLETE`
> (migration `0022` + Thread API/SSE/commands/lifecycle); their "DESIGNED, not implemented" wording is the 4C
> design round's history. `ending → ended` and `queued → discarded` are **IMPLEMENTED** as of
> `PHASE_5_BATCH_1_DONE`; delivery / releasing / `done` are **IMPLEMENTED** as of the Phase 5 Batch 2 round
> (`PHASE_5_BATCH_2_BLOCKED`), whose only blocked surface — Revision registration behind G-030's then-`proposed` ADR —
> is **no longer blocked**: both Revision root decisions were approved on 2026-10-08 and the Revision Completion Slice
> implemented the Cloud side, so `PHASE_5_BATCH_2_BLOCKED` is superseded by `REVISION_COMPLETION_SLICE_DONE` +
> `PROJECT_FINAL_ACCEPTANCE_PASSED` (G-030 = `CLOSED`; its derived gaps are G-035 / G-036 / G-037).

### Phase 5 — Delivery / Releasing / Done

Status: **COMPLETE — Batch 1 DONE, Batch 2 DONE, Revision half DONE (`REVISION_COMPLETION_SLICE_DONE` +
`PROJECT_FINAL_ACCEPTANCE_PASSED`; G-030 = `CLOSED`)**

**Batch 1 — `SessionEnded` terminal takeover（DONE, 2026-10-08）**: the session execution's terminal Node event is taken
over through the existing `TakeOverNodeEvent` authority (control action `agent_session_takeover`) and commits, in one
caller-owned transaction: the receipt, the durable terminal result, Thread `ending → ended` (also from
`pending | active | idle`), the remaining `queued` user turns → `discarded`, `running → delivering`, exactly one
unregistered `deliver_revision` work item, and the fenced `last_event_sequence` advance — published after commit as
exactly one `issue_run.thread_changed`. Replay is a no-op; refusals and hook failures leave no trace. Tests:
`integration/agent_run_session_end_test.go`.

**Batch 2 — Delivery → Releasing → Done（DELIVERED except the Revision half, 2026-10-08）**: the `deliver_revision` work
item is claimed and dispatched through the existing shared execution-work predicate; its terminal result is taken over by
`agent_delivery_takeover` (control action, same transaction shape as Batch 1: receipt → result → `DeliverySettled` hook →
fenced sequence). `DeliverySettled` retries a failure with D5's backoff (30s × 2^(n-1), capped at 10 min) **without
creating a new logical delivery**, and gives up per D5 (>2h of continuous failure, or the run Workspace's Node unknown
for >30m). Giving up commits `delivering → releasing`, D4's derived `status` and exactly one `delete_workspace` intent in
one transaction; `RunWorkspaceDeleted` then settles `releasing → done` (replay is a no-op; phases that cannot have a
succeeded delete are refused). Two periodic compensation passes re-drive give-up and delete re-declaration from
PostgreSQL — they are not in-memory queue authorities. Tests: `integration/agent_run_delivery_test.go` (P5-7…P5-16) and
`internal/core/agent_run_release_db_test.go`.
**Revision half — object store + verified Revision registration（DELIVERED, Revision Completion Slice, 2026-10-08）**:
both Revision root decisions were approved by the human on 2026-10-08 (Cloud's and Node's, **independently**), and the
Cloud side now implements the approved contract end to end — optional `object_store` configuration, an object-store
client that signs a single-key `PUT` grant and reads an object back with `HEAD`, migration
`0023_revisions.sql` (`PRIMARY KEY (id)` + `UNIQUE (run_id)` + the shape checks), `GrantRevisionUpload`, and D4's
verification-then-registration: a local input-consistency check that runs **before** any I/O, then the `HEAD` checks,
then the registration in the same takeover transaction as the release. A claim Cloud cannot confirm becomes
`failed{verification_failed}` — Cloud's own verdict, never accepted from the wire — with no `revisions` row and D5's
retry; the same payload replays as a no-op and a different one rolls the whole transaction back. Tests:
`integration/agent_run_delivery_test.go`, `internal/core/agent_run_revision_test.go`,
`internal/controlgrpc/executions_test.go`, `internal/objectstore/*_test.go`, `integration/migration_upgrade_path_test.go`.
The three obligations that need a **real** object store stay `Missing` (no endpoint was reachable this round;
**G-037**), and two presentation/ops surfaces still deviate from the approved text (**G-035** public read does not
project D5's Revision metadata, **G-036** the health check cannot report object storage) — all three are registered,
not silently absorbed.

Includes:

- session end — **Batch 1 DONE**
- delivery settlement — **Batch 2 DONE**
- cleanup — **Batch 2 DONE**
- workspace deletion — **Batch 2 DONE**
- terminal states — **Batch 2 DONE**
- Revision registration / object verification / upload grants — **Revision Completion Slice DONE（G-030 = `CLOSED`）**

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

本轮只列 T4-1..T4-16（§26）。4A 行由 Phase 4A 实现证明，4B 行由 Phase 4B 实现证明，逐行给出证据；T4-14/T4-15
两条依 4B 范围的明确禁令仍为 DEFERRED（4B 无 cancel 写入路径，见 §4B.13 结尾）。

| ID | 义务 | Phase | 状态 |
|---|---|---|---|
| T4-1 | RecordDispatch 登记同一 work → 一个 execution_id | 4A | **Covered**（T4A-1，Phase 4A） |
| T4-2 | RecordDispatch 重放（同 id）幂等 | 4A | **Covered**（T4A-2，Phase 4A） |
| T4-3 | RecordDispatch 异 id → invariant error | 4A | **Covered**（T4A-3/4/5/6，Phase 4A） |
| T4-4 | claim/dispatch 不推进运行（仍是 starting） | 4A | **Covered**（T3B-10/11/12 + T4A-9/10/16） |
| T4-5 | 权威 takeover：starting → running | 4B | **Covered**（T4B-1/2/10 + `TestAgentRunThreadTakeoverOverGRPC`） |
| T4-6 | takeover 重放：无重复条目、无状态倒退 | 4B | **Covered**（T4B-4/15） |
| T4-7 | 错误 execution 被拒 | 4B | **Covered**（T4B-8/19、`...GRPCRejections`） |
| T4-8 | terminal/stale run 不回到 running | 4B | **Covered**（T4B-9/17） |
| T4-9 | seq 从 2 续接 | 4B | **Covered**（T4B-1/11） |
| T4-10 | 并发 event 批次 seq 单调唯一 | 4B | **Covered**（T4B-16） |
| T4-11 | event 重放：同 Node 事件无重复条目 | 4B | **Covered**（T4B-4/5） |
| T4-12 | takeover 事务回滚（hook 错 → A 收据 + B running 一起回滚） | 4B | **Covered**（T4B-14） |
| T4-13 | commit 后重启重放安全 | 4B | **Covered**（T4B-4 + `...OverGRPC` 的 C5 重放段） |
| T4-14 | cancel 先提交 + takeover → 不进 running | 4B | **DEFERRED**（4B 不实现 cancel 写入路径；先到 cancel 侧的既有语义未变） |
| T4-15 | takeover 先提交 + 后到 cancel → running 取消语义（不破坏） | 4B | **DEFERRED**（同上；`cancel_requested_at` 参与 CAS 谓词，见 T4B-9/17） |
| T4-16 | 无 Phase 5 副作用（无 delivery/release 工作项） | 4B | **Covered**（T4B-19） |

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

---

## Phase 4B — Thread Takeover + `starting→running` 架构决议（DESIGN / DECISION ONLY）

Status：**本轮只做架构决议，不实现**（无 migration、无 production 代码、无测试实现、无 OpenAPI/契约改动）。
本决议解析 **G-013**（会话开始权威形状 / `thread_state` 语义）与 **G-014**（Thread `seq` 分配机制 / D-023），
给出 Phase 4B（Cloud 侧 Thread 接管 + `starting→running`）的最小实现范围与测试矩阵 T4B-1..T4B-18。
权威依据**全部取自 `approved` ADR**：IssueRun D3/D6、Thread D1/D2/D3/D4、controller-integration D2/D6、
Node 协议 D2/D3/D4。**controller-session 根 ADR 仍为 `proposed`** —— 本决议在 4B.14 证明 Cloud 侧 4B
**不依赖它**，它只承载 Controller 侧中继循环（属 desktop 的 slice）。

### 4B.1 结论摘要（verdict 前置）

- **G-013 → CLOSED**：`starting→running` 的唯一权威 = **首条真实 Node Thread 记录被 `TakeOverThreadEvents`
  接管、且 `ThreadEventsTakenOver` 钩子在同一事务内成功提交**（IssueRun D3「首条 Thread 事件…被接管」）。
  **不存在** synthetic「session_started」控制事件：Node 只发 `ThreadEvent{record}`（Node 协议 D2），
  `record` 是 `ora-history` 定型记录（Thread D2）。这对应 mandate §4 的选项 **B（首条真实 Thread 记录即开始证据）**。
- **G-013 `thread_state` → CLOSED**：`pending` 的权威语义 = **会话执行已登记、尚无任何 Node 记录被接管**
  （Thread D4 明示）；首条 Node 记录接管 ⇒ `active`。`pending` **不是**「收到无记录的开始信号」，也**不是**
  「命令已排队」（命令排队的展示属 4C）。4B 只写 `pending→active`，不写 `idle/ending/ended`（属 4C/4B 终态钩子）。
- **G-014 → CLOSED；D-023 → ACCEPTED**：Thread `seq` 分配采用 **接管事务内 `MAX(seq)+1`**（从 2 起）。
  串行化证明不依赖 controller-session：见 4B.5。
- **G-015 → PARTIAL（给出初值）**：见 4B.10。
- **G-009 → 由 4B 关闭**（seq 从 2 连续续接）；**G-012 保持 PARTIAL**（4B 部分实现后仍未闭合全部）；
  **G-001 保持 PARTIAL**（`ThreadEventsTakenOver` caller 由 4B 关闭一部分，`Create/DeleteRunWorkspace`、
  `EnqueueThreadCommand` 仍未实现）。
- 已锁定事实（不做二次争论）：(A) running authority 只有已提交的接管；(B) `execution_work→node_executions
  →execution_id`，一 work 至多一 execution_id；(C) `thread_entries` seq=1（`source=system`,`kind=user_turn`）
  不可变、永不重建/覆盖/重编号。

### 4B.2 G-013：会话开始权威的事件形状（选项 A–E 的裁决）

mandate §4 列出 A–E 五个候选；逐项对照 approved ADR：

| 选项 | 内容 | 裁决 | 依据 |
|---|---|---|---|
| A | Node 显式发 `session_started` 事件 | **否决** | Node 协议 D2 只定义 `ThreadEvent{record}`；`record` 是 Ora 会话记录（Thread D2），无 synthetic 控制事件类型。发明新事件类型会超出现有契约。 |
| B | **首条真实 Thread 记录即开始证据** | **采纳** | IssueRun D3「首条 Thread 事件…被接管」；controller-integration D6 钩子 `threadEventsTakenOver` 置 `running`；Thread D4 `active`。 |
| C | `TakeOverThreadEvents` 请求携带 start marker | **否决** | 合约头字段是 `{submission_id, epoch, operation_id, execution_id, events[]}`（controller-integration D2），无 marker；加 marker 需改已发布 `v1` 契约（不变量 1）。 |
| D | execution 级状态足够，不写 Thread 事件 | **否决** | 与 D3/D4 冲突：`running` 与 `active` 都以「记录被接管」为准，不引入 execution 级旁路状态。 |
| E | spec 未定义（维持 gap） | **否决** | 本决议证明 approved ADR 已充分定义，无需维持 gap。 |

**事件形状（权威）**：`TakeOverThreadEvents.events[i] = {sequence, record, turn_id?}`（controller-integration
D2）；`sequence` 是该执行内从 1 起的 Node 序号（Node 协议 D2）；`record` 是 `ora-history` 定型记录 JSON
（Thread D2，单条 ≤256 KiB，JSON object）。**首个成功接管的批次**（事件非空、序号连续）提交即令
`starting→running`。

### 4B.3 G-013：`thread_state` 转换表（4B 与 4C 的边界）

以 Thread D4 为准，明确 4B **拥有**的转换与**留给 4C**的转换：

| 事件 | `thread_state` | 归属 | 备注 |
|---|---|---|---|
| 会话执行已登记、尚无记录 | `pending` | 未定（见 G-016） | Thread D4；Phase 4A 未写，4B 亦未写（§29 只授权写 `pending→active`），当前实际值为 `NULL` |
| 接管到**至少一条真实** Node 记录（echo 除外，见 4B.6/§31） | `active` | **4B（已实现）** | 同一接管事务内 |
| 接管到 `TurnEnded` 且无 `queued` 用户轮次 | `idle` + `idle_since` | 4C | 依赖用户轮次队列（`thread_commands`） |
| 新用户轮次写入 | `active` | 4C | `POST .../thread/messages` |
| 用户「结束」/`idle` 超时/运行被取消 | `ending` | 4C（`EndSession` 命令） | |
| 会话执行终态被接管 | `ended` | 4B/5（`sessionEnded` 钩子） | 终态经 `TakeOverNodeEvent`，非本 4B 范围 |

**4B 只写 `pending→active` 与 `running`**；`idle/ending/ended` 需要 `thread_commands` 与用户轮次队列，
属 4C / 会话终态钩子。这样 4B 与 4C 的提交边界正交、可独立验收。

> **Phase 4C 更新（2026-10-08，不重写本节）**：表中第一行的归属「未定（见 G-016）」已由 **D-4C-01** 关闭——
> `pending` 由 **B** 在 Phase 3A `StartSession` 事务内物化（**不是** 4B 的接管事务），并另加幂等回填迁移；
> `active ⇄ idle`（第三、四行）的权威由 **D-4C-09** 确定为接管事务内的「批次末条记录 + `queued` 检查」；
> `ending`（第五行）由 **D-4C-10/D-4C-11** 确定；`ended`（第六行）保持 **`SessionEnded` / Phase 5**。
> 本节其余内容作为 4B 轮设计记录原样保留。

### 4B.4 G-013：`pending` 的必要性、空批次、echo 首 prompt 去重

- **`pending` 是否必要？必要**。它是「执行已登记、尚无记录」的持久状态（Thread D4），是 `starting` 运行
  的可读表现；写它的时点 = 4B 首批接管前（或会话执行登记时），使「已下发但还没有输出」对前端可见。
  **实现现状（G-016）**：本轮的 4B mandate §29 只授权写 `pending→active` 这一转换，登记时写初值会落在
  A 侧（§11 禁止）或在 4B 事务外新增一个转换（§29 未授权），因此两侧都没写；实际前置值为 `NULL`，
  语义上等同于 `pending`。字面值 `pending` 的写入者仍是待定项，不影响本轮正确性。
  **（Phase 4C 更新，D-4C-01：该待定项已关闭——写者 = B 的 `StartSession` 事务，随 seq=1 同事务物化并断言
  1 行受影响，另加幂等回填；`pending` 的语义澄清随 Thread ADR 修订一并落地，见 G-017。）**
  `pending` **不表示「命令已排队」**（那是 4C 的用户轮次 `queued` 条目状态，落在 `thread_entries` 行上，
  不是 `thread_state`）。
- **空批次（`events` 为空）**：**拒绝**（`ABORTED`），不写收据、不推进 `last_event_sequence`、不调钩子、
  不改变 `phase/thread_state`。理由：运行权威必须来自「接管了一条真实事件」；空调用不构成证据，视为
  协议误用（controller-integration D2 的 `events` 至少 1 个）。
- **echo 首 prompt 去重（锁定事实 C 的保护）**：Phase 3A 已把首 prompt 写成 `thread_entries` **seq=1**
  （`source=system`,`kind=user_turn`,`turn_id` 由 Cloud 生成，Thread D3）。Node 会话以 `initial_turn{turn_id,
  content}` 为输入（controller-integration D1），其记录若携带**同一** `turn_id`（Node 协议 D2 的
  `turn_id?`），即为首 prompt 的 echo。规则：
  - 若被接管事件的 `turn_id == initial_turn.turn_id`：**不分配新 thread `seq`、不插入新条目**（seq=1 已呈现它）；
    仍写收据（Node 必须收到 `EventAck`），钩子不产生业务条目。seq=1 **内容不变、不重编号**（锁定事实 C）。
  - 其余事件（真实 Agent 输出 / 后续轮次的新 `turn_id` / 无 `turn_id` 的记录）：按 4B.5 分配 `seq≥2`，
    `source='node'`，写入 `node_execution_id`/`node_sequence`。
  - **4B 不引入 `delivered` 列**：Thread D3 的「标 delivered」针对 `SubmitUserTurn` 排队的用户轮次
    （需 4C 的 `thread_commands` 与列）；首 prompt 不是排队命令，是会话首个输入，4B 无需标记它。
  该规则同时满足 Thread D1「同一 Node 事件至多一个条目」与锁定事实 C。

### 4B.5 G-014：`seq` 分配机制（D-023 ACCEPTED）与串行化证明

- **裁决**：Thread D1 只定「Cloud 在写入事务中分配、连续」；签名由 D-023 固定为 **接管事务内
  `MAX(seq)+1`，从 2 起逐条递增**（seq=1 被首 prompt 保留，G-009）。**ACCEPT D-023**，无 superseding 决策。
- **串行化证明（不依赖 controller-session）**：
  1. **一个运行同时至多一个会话执行**（controller-integration D1「一个运行同时至多一个会话执行」；
     D-021 一 work 至多一 execution_id；`execution_work_unregistered_once` 部分唯一）。因此同一运行的
     会话接管流唯一。
  2. **所有接管事务经全局 advisory lock 串行**：`TakeOverThreadEvents` 经 `Store.transact`
     （`pg_advisory_xact_lock(67420911)`，Phase 1 基线）执行，锁内完成收据写入 → `MAX(seq)` 重读 → 逐条
     +1 → 提交。任意两次接管（即使未来存在同 run 多执行）不可能同时在锁内计算 `MAX(seq)`。
  3. **PK `(run_id, seq)` 是最终存储层屏障**：任何重复 seq 插入被数据库唯一键拒绝，作为 CAS 兜底。
  因此 `MAX(seq)+1` 在事务内产出连续、无洞、唯一的 seq；重放不会重复分配（4B.7）。
- **为什么不需要计数器（counter）**：用户追加消息的条目也在 `Store.transact` 内写入（Thread D3 的 POST
  事务），与接管共享同一全局锁；`MAX(seq)+1` 对两个来源（Node 记录接管、用户轮次）都成立，二者按时间
  交错但 seq 连续（对话顺序正确，无洞）。因此**不新增 `issue_runs.next_thread_seq` 计数列 / 独立 sequence
  状态表**。仅当未来移除全局锁（G-004）时需重新评估——该替换是独立架构任务，本决议不动。
- **Node `seq` ≠ Thread `seq`（不得混淆）**：`node_event_receipts(execution_id, sequence)` 的 `sequence`
  是 **Node 在该执行内**从 1 起的序号（Node 协议 D2）；`thread_entries.seq` 是 **运行内** Cloud 维护的
  连续序号（run-scoped，从 1）。两套序号空间**不同**：批次连续性检查作用于**收据/Node** 序号空间
  （首事件 = 该执行已接管最大序号 + 1，controller-integration D2），thread `seq` 由 4B.5 独立 `MAX+1`
  计算；二者的映射是逐事件、由接管钩子完成的（4B.7）。**禁止**用 Node `sequence` 直接当 thread `seq`，
  也禁止用 `thread_entries(run_id,seq)` 反推重放身份（D-022）。
- **4C 兼容性**：`seq` 分配集中在 `MAX(seq)+1` 且受全局锁串行，用户轮次（4C 的 `SubmitUserTurn`）插入
  thread 条目天然并入同一分配器；4C 只需在既有分配器上继续（无新机制），4B 不为其预设假设。

### 4B.6 收据语义与批次连续性（缺口 / 重复 / 乱序 / 重叠）

- **收据身份**：`node_event_receipts(execution_id, sequence)` 主键 + 原事件 `event jsonb`（controller-integration
  D6/D-022）。非终态 Thread 事件与终态事件**共用**该表与同一序号空间（Node 协议 D2 不变量 2），是
  `EventAck` 的**唯一依据**（root D4）。
- **批次连续性（A 判定，不交钩子）**：
  - 首事件 `sequence` **必须 = 该执行已接管最大序号 + 1**（= `node_executions.last_event_sequence + 1`）。
  - 批次内 `sequence` 必须**严格连续递增**（`s, s+1, …, s+k-1`）。
  - **缺口 / 乱序（首事件 ≠ max+1）/ 越界** ⇒ `CONFLICT`（或 `ABORTED`），**整批不写**（收据、
    `last_event_sequence`、条目、`phase` 全不动），不确认，由 Node 重放（controller-integration D2）。
  - **重复（同 `(execution_id, sequence)` 且内容相同）** ⇒ 幂等 no-op，跳过（不重复确认语义、不产生第二
    条目）；**同键不同内容** ⇒ `CONFLICT`，原收据与条目不变（D-022）。
  - **重叠批次**（与已接管序号相交）⇒ 相交部分按重复处理（内容同 ⇒ 跳过；内容异 ⇒ `CONFLICT`）；未接管
    的后缀继续，但**首事件必须 = max+1**，否则整体 `CONFLICT`。
- **终态事件**：经 `TakeOverNodeEvent` 接管，必须在其前面所有序号都已接管之后（controller-integration D2/
  不变量 3），保证 Thread 最后记录先于「会话已结束」落库。终态接管属 4B/5 的会话终态钩子（`sessionEnded`），
  不在本 4B 的 `starting→running` 范围内，但**共用** `node_event_receipts`（D-022）。

### 4B.7 批次 → Thread 映射与事务模型（含 C5 重放）

一次接管的提交边界（对应 mandate §14/§15）：

```
BEGIN                           (Store.transact：全局 advisory lock)
  A: 校验 execution 已登记、kind=agent_session、execution_id 匹配 work
  A: 连续性校验：首事件 sequence == last_event_sequence + 1 且批次严格连续
  A: INSERT node_event_receipts(execution_id, sequence, event)   // 收据 = 确认唯一依据
  A: UPDATE node_executions SET last_event_sequence = 最大已接管 sequence
  B: ThreadEventsTakenOver(t, run, execution, events)            // 同事务，不新开事务
       re-read 权威 issue_runs / node_executions（不接受 caller 业务字段）
       CAS phase:'starting' AND status:'dispatched' AND cancel_requested_at IS NULL → running/running
         （affected=0 ⇒ stale/no-op，见 4.10/4B.8）
       遍历事件：turn_id==initial_turn ⇒ 跳过（echo，见 4B.4）
                 否则 seq = MAX(seq)+1 (从 2) → INSERT thread_entries(source='node', node_execution_id, node_sequence)
       thread_state: 若无记录 → 首批接管后置 'active'（Thread D4）
       把（若有）对应 user_turn 轮次标 delivered（4C 才有列；4B 仅结构预留，不建列）
       hook error ⇒ panic(databaseFailure) ⇒ 整事务回滚
COMMIT
  然后 Controller 对批次内序号发 EventAck
```

- **与 D6 一致**：收据写入**之后**、同事务调用钩子；钩子只在首次接管（连续、非重放）时被调用；重放**不
  再次调用**钩子（controller-integration D6）。
- **C5（提交后、ack 丢失）重放安全**：重放批次的首事件 `sequence` ≤ `last_event_sequence` ⇒ 相交部分已
  接管（收据存在、内容相同）⇒ no-op；若整批已接管 ⇒ 整体 no-op，返回已接管最大序号；不新增条目、不
  倒退、不重复跑钩子。**重放不改 `seq` 连续**（`MAX+1` 只在有新事件时执行）。
- **C4（事务中途崩溃）**：事务未提交 ⇒ 收据、`last_event_sequence`、条目、`phase/thread_state` 全部回滚，
  Node 重放该批（未 ack）再次接管。**C2/C3**（claim 后/Node 启动后）见 4.12。
- **`last_event_sequence` 单调**：每次成功接管推进到该批最大序号；只增不减，是恢复重放边界（Node 从
  `last_event_sequence+1` 重放，C3）。

### 4B.8 cancel race 与 invalid workspace 兼容（4B 语义固化）

- **cancel race（沿用 4.11）**：cancel 写入与 `TakeOverThreadEvents` 都可能决定 `starting→releasing/cancelled`
  vs `starting→running`；同一全局锁串行 + 钩子内权威 re-read + CAS（`cancel_requested_at IS NULL`）。
  - cancel **先**提交 ⇒ `cancel_requested_at` 已置 ⇒ 钩子 CAS affected=0 ⇒ 接管视为 **stale/no-op**，运行按
    IssueRun D6 无会话取消进入 `releasing/cancelled`，**不进 running**。
  - takeover **先**提交 ⇒ 运行已 `running`；后续 cancel 落入 running 语义（取消是请求，会话收尾后 delivering
    →releasing），**不破坏已接管 Thread**。
- **invalid/missing workspace（G-011 保真）**：钩子权威重读 `workspace_id`；无效 ⇒ **fail-closed**（不推进
  running、保持 `starting`、不写终态），保留 G-011 gap。不因测试方便写 `status=failed`。

### 4B.9 迁移 proposal（Phase 4B）

**Required Migration: YES（Phase 4B 唯一新表）** —— 仅新增 `node_event_receipts`（4A 的 `node_executions`
已在 0020 落地）：

```sql
CREATE TABLE node_event_receipts (
  execution_id text NOT NULL REFERENCES node_executions(execution_id),
  sequence bigint NOT NULL CHECK (sequence > 0),
  event jsonb NOT NULL,                 -- 原 ThreadEvent（收据 = 确认唯一依据）
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (execution_id, sequence)
);
-- 保留期限/清理与恢复读（4B.10）：按 execution + 时间
CREATE INDEX node_event_receipts_retention ON node_event_receipts(created_at);
```

- `FK(execution_id → node_executions)`：收据只对**已登记执行**存在，防「无执行收据」。
- 不新增 `delivered` 列（4C 的 `thread_commands`/用户轮次投递才需要）。
- `thread_entries`（0018）已足够：PK `(run_id,seq)`、`thread_entries_node_uniq(node_execution_id, node_sequence)`
  已存在；`issue_runs.thread_state` 值集已含 `pending/active`。**4B 无需改业务表**。

### 4B.10 G-015：单运行 event 上限与收据保留（初值，PARTIAL）

给出**初值**（可配置，第一版默认；enforcement 点在 4B 接管路由/派发循环）：

- **单运行（单执行）非终态 event 上限**：默认 **200,000** 条（可配置 `thread_event_cap`）。检查点 =
  `node_executions.last_event_sequence`（连续序号 ⇒ 计数 = 最大序号）；达到上限后新的非终态接管以
  `ABORTED` 拒绝（不静默丢弃），运行保持终态决策由 IssueRun D5 失败路径承担。**无需新列**。
- **`node_event_receipts` 保留期限**：默认 **运行终态（`done`）后 30 天**（可配置 `node_event_receipts_retention`），
  由派发循环按 `created_at` 清理；运行存活期间**不清理**（Node 重放需要收据）。保留期限是默认值，非硬
  契约。
- 状态：**PARTIAL**（给出初值即满足 4B 前置；enforcement 与清理循环随 4B 实现落地并补证据）。

### 4B.11 4C 兼容性

4B 不预设与 4C 冲突的假设：

- **seq**：4B 的 `MAX(seq)+1` 分配器对 4C 的用户轮次条目天然适用（4B.5）。
- **thread_state**：4B 只写 `pending→active`；`idle/ending/ended` 与 `idle_since` 归 4C（4B.3 表）。
- **命令**：`thread_commands`、`EnqueueThreadCommand`、`POST .../thread/messages`、SSE 均不在 4B；4B 只
  预留「标 delivered」的结构位（不建列）。`sessionEnded`（终态）由会话终态钩子承担，非本 4B 路由。
- **公开面**：4B 无 OpenAPI/前端改动（Thread REST/SSE 属 4C）。

### 4B.12 Phase 4B 最小实现范围（minimal scope）

只实现「A 记录 + B 会话开始」的最小闭环，不含 API：

1. **迁移** `node_event_receipts`（4B.9）。
2. **控制路由** `TakeOverThreadEvents`（internal control 动作，leaseValid 门控，`submitted` 幂等包装）：
   校验 execution 已登记且 `kind=agent_session` → 连续性/重复/冲突判定（4B.6）→ 写收据 + 更新
   `last_event_sequence` → 同事务调 `ThreadEventsTakenOver` 钩子（4B.7）→ 提交后返回已接管最大序号。
   空批次拒绝（4B.4）。G-015 cap 检查（4B.10）。
3. **业务钩子** `ThreadEventsTakenOver`（真实化，替换 fail-closed）：权威 re-read → CAS `starting→running`
   → echo 去重 + `seq≥2` 分配 + `thread_entries(node)` 写入 → `thread_state pending→active`。钩子错误
   panic 回滚整事务。
4. **不改**：`issue_runs` 业务列的其它转换、`thread_commands`、公开 API、OpenAPI/前端、`deliver_revision`
   登记分支、Phase 5 释放路径。

### 4B.13 测试矩阵 T4B-1..T4B-19（本轮回填实现证据）

测试文件（全部真实 PostgreSQL）：`internal/core/agent_run_thread_takeover_db_test.go`（T4B-1..T4B-19 主体，
含 §42 并发场景）、`integration/agent_run_thread_takeover_test.go`（gRPC 生产路径验收）、
`integration/migration_upgrade_path_test.go::TestMigration0021NodeEventReceiptsAppliesFreshAndUpgrades`（迁移）。

| ID | 义务 | 依据 | 状态 | 证据 |
|---|---|---|---|---|
| T4B-1 | 首条真实 Node 记录接管 ⇒ `starting→running`（`status=running`、同事务） | IssueRun D3、D6 | **COVERED** | `TestThreadTakeoverFirstRecordRunsTheRun`、`TestAgentRunThreadTakeoverOverGRPC` |
| T4B-2 | `thread_state` `pending→active`（同接管事务） | Thread D4 | **COVERED**（前置字面值见 G-016） | `TestThreadTakeoverActivatesPendingThread`、`TestThreadTakeoverFirstRecordRunsTheRun` |
| T4B-3 | 批次原子 + 顺序：缺口/乱序/首事件≠max+1 ⇒ `CONFLICT`，整批不写 | controller-integration D2 | **COVERED** | `TestThreadTakeoverRejectsGapsAndReordering`、`TestAgentRunThreadTakeoverGRPCRejections` |
| T4B-4 | 重放（同 exec/seq/内容）⇒ 幂等 no-op，无新条目、无倒退、钩子不二次调用 | D6、D-022 | **COVERED** | `TestThreadTakeoverReplayIsIdempotent`、`TestThreadTakeoverOverlapReplayAndForward` |
| T4B-5 | 载荷冲突（同 exec/seq 异 record）⇒ `CONFLICT`，原收据/条目不变 | D-022 | **COVERED** | `TestThreadTakeoverReceiptConflictKeepsOriginal` |
| T4B-6 | thread `seq` 从 2 连续续接（跨多批次 `MAX+1`） | Thread D1、G-009 | **COVERED** | `TestThreadTakeoverContiguousBatch` |
| T4B-7 | echo 首 prompt 去重：`turn_id==initial` 不分配 seq、不新增条目、seq=1 不变 | Thread D3、锁定事实 C | **COVERED** | `TestThreadTakeoverInitialTurnEchoIsDeduped`、`TestAgentRunThreadTakeoverOverGRPC` |
| T4B-8 | 空批次拒绝：无收据、不推进、不钩子、不改变 phase/thread_state | 4B.4 | **COVERED** | `TestThreadTakeoverRejectsWrongExecutionAndEmptyBatch`、`TestAgentRunThreadTakeoverGRPCRejections` |
| T4B-9 | 钩子错误回滚：收据 + `last_event_sequence` + running + 条目同事务整体回滚 | D6、4.7 | **COVERED** | `TestThreadTakeoverHookFailureRollsBackEverything` |
| T4B-10 | C5 提交后 ack 丢失重放安全（幂等、无重复、不倒退） | 4.12、4B.7 | **COVERED** | `TestThreadTakeoverReplayIsIdempotent`、`TestAgentRunThreadTakeoverOverGRPC`（重放段） |
| T4B-11 | `last_event_sequence` 随批次单调推进（只增不减） | Node 协议 D2、4B.6 | **COVERED** | `TestThreadTakeoverContiguousBatch`、`TestThreadTakeoverReplayIsIdempotent` |
| T4B-12 | C2/C3 恢复：Node 从 `last_event_sequence+1` 重放对齐 | 4.12 | **COVERED**（Cloud 侧义务） | `TestThreadTakeoverHookFailureRollsBackEverything`（失败后同批次可重放）、`TestThreadTakeoverRejectsGapsAndReordering` |
| T4B-13 | cancel 先提交 + takeover 后到 ⇒ CAS affected=0、不进 running | IssueRun D6、4.11 | **COVERED** | `TestThreadTakeoverStaleRunIsNotRerun` |
| T4B-14 | takeover 先提交 + 后到 cancel ⇒ running 取消语义、不破坏已接管 Thread | IssueRun D6、4.11 | **DEFERRED** | 4B 无 cancel 写路径（§3/§19 禁全量 cancel API）；本轮仅证明 cancel-first 顺序，后到 cancel 由 4C cancel slice 拥有 |
| T4B-15 | terminal/stale run 不回 running；未登记 execution / 未知 run 被拒（invariant 回滚） | 4.10 | **COVERED** | `TestThreadTakeoverStaleRunIsNotRerun`、`TestThreadTakeoverRejectsWrongExecutionAndEmptyBatch` |
| T4B-16 | 并发批次 seq 单调唯一（同 run 单执行串行） | 4B.5、4.10 | **COVERED** | `TestThreadTakeoverConcurrentBatches`（barrier + 两种允许顺序） |
| T4B-17 | invalid/missing workspace ⇒ fail-closed 不推进 running（G-011 保真） | G-011、4B.8 | **COVERED** | `TestThreadTakeoverInvalidWorkspaceFailsClosed` |
| T4B-18 | G-015 cap：`last_event_sequence ≥ cap` ⇒ 后续非终态接管 `ABORTED` | 4B.10 | **DEFERRED** | 无已批准 ADR 数值；实现 200,000 cap 属未批准行为（§3/§39），G-015 保持 PARTIAL |
| T4B-19 | 无 Phase 5 / 4C 副作用：无 deliver 工作项、不 releasing/done、不写 `thread_commands` | 4.15 | **COVERED** | `TestThreadTakeoverHasNoLaterPhaseSideEffects` |

（T4B-1..T4B-18 为 mandate §33 的编号基线；上表按义务补足至 T4B-19 的「无副作用」项——与既有 T4-16 对齐。
既有 T4-5..T4-16 的 4B 义务由本表细化覆盖，T4-4 仍由 Phase 3B 的 T3B-10/11/12 直接证明。
T4B-14 与 T4B-18 的 DEFERRED 理由都在本轮 mandate §3/§19/§39 的显式禁止清单内，不是遗漏。）

### 4B.14 controller-session `proposed` 依赖说明（未构成 blocker）

- **事实**：`specs/decisions/controller/session/0-controller-relays-agent-sessions.md` 的 frontmatter 为
  **`status: proposed`**。其 D2 描述 Controller 侧中继循环（≤64 事件 / 100 ms 一批、同一执行至多一个在途
  批次、`CONFLICT` 时清队列等），D3/D4/D5 描述命令投递、上传授权、释放证据。
- **G-013 / G-014 是否依赖它**：**不依赖**。G-013 的事件形状（Node 协议 D2）、running 权威（IssueRun D3 +
  controller-integration D6）、`thread_state`（Thread D4）、批次连续性（controller-integration D2）全部
  来自 **approved** ADR；G-014 的串行化证明来自 approved 的「单会话执行/运行」+ 全局锁 + `PK(run_id,seq)`
  （4B.5），**不需要** controller-session D2 的「至多一个在途批次」。Cloud 侧 4B 的正确性对 Controller
  行为是**防御性**的：即使 Controller 乱序/重叠，Cloud 也以 `CONFLICT` 拒绝并等重放（controller-integration
  D2），因此正确性不依赖 controller-session 是否 approved。
- **裁决**：**不**报告 `BLOCKED_ON_CONTROLLER_SESSION_ADR`。controller-session 仍是 **Controller 侧（desktop）
  的独立待批准决策**（其 D1–D5 的 relay/命令/授权/释放行为），属 desktop 的 slice 与后续轮次；它与 Cloud
  侧 4B **不冲突**、不是 4B 的实现前置。若未来 Cloud 侧需要引用其 D2 的具体批处理参数（如 100 ms），
  那些参数应作为 Controller 实现选择，不改 Cloud 契约。

### 4B.15 §36 架构自评清单

> **历史记录（设计轮）**：以下 20 条是 Phase 4B 设计轮的清单，其中第 16–18 条（"未改 production 代码 / 未写
> migration / 未实现测试"）描述的是**设计轮**的事实。Phase 4B 实现轮已交付 migration `0021`、production 接管核心与
> T4B-1..T4B-19；实现轮的对应自评见 §16 的 "Round: Phase 4B — ... implementation"。本节保持原样以免改动历史轮次记录。

1. **running 权威未变**：仅已提交的 `ThreadEventsTakenOver` 接管（D-019/4B.2）——是。
2. `RecordDispatch`/claim/dispatch/物理分配 **≠ running**（D-017/4.3）——是。
3. `execution_work→node_executions→execution_id`，一 work 至多一 execution_id（D-021）——是，未改。
4. `thread_entries` seq=1 **不可变**（锁定事实 C）：echo 只写收据、不重编号（4B.4）——是。
5. 事件身份 `(execution_id, sequence)` 收据 + `(node_execution_id, node_sequence)` 条目（D-022）——是。
6. `seq` 分配 `MAX(seq)+1`（D-023 ACCEPTED），串行化证明不依赖 proposed ADR——是。
7. Node `seq` 与 Thread `seq` **不混淆**（4B.5）——是。
8. `thread_state` 只写 `pending→active`，`idle/ending/ended` 归 4C——是。
9. 空批次 / 缺口 / 乱序 / 重叠的判别在 A 侧，不交钩子（4B.6）——是。
10. 钩子错误 ⇒ 整事务回滚（收据 + running + 条目）（4B.7）——是。
11. C4/C5 重放安全，`last_event_sequence` 单调（4B.7）——是。
12. cancel race 与 invalid workspace 语义固化（4B.8）——是。
13. 4B 迁移仅 `node_event_receipts`，不改业务表（4B.9）——是。
14. G-015 给出初值（cap + retention）（4B.10）——是。
15. 4C 兼容（seq 分配器 / thread_state / 命令 / 公开面）（4B.11）——是。
16. 未改 production 代码——是（本轮仅 plan）。
17. 未写 migration——是。
18. 未实现测试（T4B-* 全 `DESIGNED / MISSING`）——是。
19. 未 stage / commit / push / PR——是（见 §16 Git 段）。
20. 未把 `proposed` ADR 当作 `approved`（controller-session 明确标注，见 4B.14）——是。

---

## Phase 4C — Thread API / SSE / Thread Commands / Lifecycle 详细设计（DESIGN / DECISION ONLY）

> **Round: Phase 4C design / 2026-10-08（纯设计轮）**
>
> 本轮**不改** production 代码、**不建** migration、**不写/不修改**测试、**不改** API/proto/generated 代码、
> **不改** schema；只读代码 / migration / ADR / spec / 已有测试并更新 `plan/plan.md` 与 `plan/plan-zh.md`。
> 因此本节所有条款都是**设计**，**不是已实现**；specs 的证据状态一律保持 `Missing`（§4C.17）。
> **未** stage / commit / push / PR；既有未提交修改原样保留。

### 4C.0 结论摘要（verdict 前置）

1. **G-016 CLOSED**（D-4C-01）：字面值 `thread_state='pending'` 由 **B 自己**在 Phase 3A `StartSession` 事务内
   物化（与 seq=1、`agent_session` 工作项同事务），写谓词 `thread_state IS NULL`；4C migration 附一条幂等回填。
   **拒绝**「A 侧 `RecordDispatch` 登记时写」（违反 controller-integration D6 不变量 7 / §11）与
   「读时派生 pending」（把持久列的含义交给另一个 owner 的 `phase`，DB 无法表达该不变量）。
2. **`ending → ended` 的唯一权威是 `SessionEnded`**（会话终态接管钩子），**属 Phase 5**（D-4C-10）。4C 的
   lifecycle 只到 `ending`；**不得**为 `ended` 造第二条路径，也不得把 `SessionCommandAccepted` 当成终态事实。
3. **SSE 只是失效提示**（D-4C-07/08）：durable replay 的唯一来源是 `thread_entries` + `GET ...?after=`；
   客户端游标**只**由 GET 的响应推进（`nextCursor`/`prevCursor`），SSE 事件只触发 GET。绝不把
   PG notify / SSE 当持久日志，也绝不承认「SSE 不漏事件」。
4. **4C 需要一条 migration**（D-4C-12）：新控制面表 `thread_commands`；`thread_entries.status` 列 + 两个部分索引；
   一条 `thread_state='pending'` 幂等回填。**不动** `issue_runs.thread_state` 的值集（0018 已覆盖五值），
   **不新增计数器**，**不新建第二张 `starting→running` 权威表**。
5. **4B 需要 4 处小改**（§4C.13，本轮**不改**，登记为 4C 实现前置）：echo 去重从「仅首 prompt」推广到
   「任意 Cloud 生成用户轮次」（并置 `queued → delivered`）；补 `idle` 与 `idle → active` 的 `thread_state` 写；
   `StartSession` 物化 `pending`；`SpaceEvent` 增可选 `issueId/runId/lastSeq`。
6. **未决（NON-BLOCKING）**：G-017（tail/`before` 分页是对已批准 Thread D5 的**扩展**，需 ADR 修订或架构师确认）、
   G-018（ADR 缺「用户主动结束」端点）、G-019（Phase 5 依赖：`ended` + `discarded`）、G-020（SSE 不保证送达 ⇒
   客户端须轮询）、G-021（多 Controller worker 的命令分区，ADR 已列为未决）。逐条见 §4C.18。
7. **最终裁定**：`PHASE_4C_DESIGN_DONE / PHASE_4C_ARCHITECTURALLY_REVIEWABLE / READY_FOR_PHASE_4C_IMPLEMENTATION`
   （附上列 NON-BLOCKING OPEN；其中 G-017/G-018 对应的代码落地前需 ADR 修订或架构师确认）。见 §4C.20。

### 4C.1 入口条件与冻结的 Phase 4B 基线

**入口条件（本轮全部满足）**：Phase 4B 已实现且有证据（migration `0021`、A 侧 `agent_thread_takeover`、
B 侧 `ThreadEventsTakenOver`、gRPC `TakeOverThreadEvents`、`cmd/server` 接线、T4B-1..T4B-19 + 端到端 gRPC 验收）；
G-009 / G-013 / G-014 **CLOSED**；G-016 OPEN（本轮关闭）。

**冻结的 4B 基线（4C 不得改变，只允许 §4C.13 列出的补充）**：

1. **running 唯一权威** = `ThreadEventsTakenOver` 已提交事务内被接管的**首条真实 Node Thread 记录**
   （D-019/D-024）。claim / dispatch / 分配 / 登记 / workspace 创建 / synthetic 事件都**不是**权威。
2. **事务顺序**：`BEGIN → pg_advisory_xact_lock → A 侧读 execution → 收据写入 → B 钩子 → fenced
   last_event_sequence 推进 → COMMIT → EventAck`。钩子错误 ⇒ 整事务回滚（含收据与 running），Controller 不 ack。
3. **`seq=1` 不可变**（Phase 3A 的 Cloud 生成首 prompt）；业务条目从 `seq=2` 起；分配 = 接管事务内
   `MAX(seq)+1`（D-023）；**无** `thread_seq = node_sequence + 常数`；**无** counter 表。
4. **首 prompt echo = 只写收据**，不产生条目、不消耗 seq。
5. G-001 / G-011 / G-012 / G-015 / G-016 的状态按本轮结束时的判定（§4C.18）；G-009 / G-013 / G-014 保持 CLOSED。
6. **不得**因 4C 出现第二个 `starting → running` 权威，也不得把 4C 的 `thread_state` 扩展解释为运行阶段权威。

**4C 的范围（mandate §2 的 4C 定义）**：Thread 读取（GET）、用户消息写入（POST）、`EnqueueThreadCommand` 缝、
SSE 失效提示、`pending/active/idle/ending` 生命周期、取消与终态的交互、以及**为以上所需的** migration 设计。
**不在 4C**：`running → delivering → releasing → done`、`SessionEnded`/`DeliverySettled`/`RunWorkspaceDeleted`
钩子、`deliver_revision` 交付登记、`revision` 上传授权、收据 GC。

### 4C.2 D-4C-01：`thread_state` 的 owner 与 `pending` 的物化（**G-016 RESOLVED**）

**决策：物化（materialize），写者 = B，写点 = Phase 3A `StartSession` 事务。**

| 项 | 结论 |
|---|---|
| owner | **B**（`issue_runs.thread_state` 是业务列，thread D1；只有业务转换写它） |
| 写者与写点 | `AgentRunSessionStart.StartSession`：与 `thread_entries seq=1`、`EnqueueExecutionWork(agent_session)` **同一事务**、同一次「首次声明」分支内 |
| 前置条件 | 事务已权威重读该 run：`executor_type='agent' AND phase='starting' AND status='dispatched' AND workspace_id IS NOT NULL AND cancel_requested_at IS NULL AND deleted_at IS NULL`，且 `runWorkspaceLive`（G-011）为真 |
| 写谓词 | `UPDATE issue_runs SET thread_state='pending', version=version+1, updated_at=now() WHERE id=$1 AND executor_type='agent' AND thread_state IS NULL` |
| 影响行数 | 该语句只在 seq=1 的 `INSERT ... ON CONFLICT DO NOTHING` **确实插入**（影响 1 行）的分支执行；此时 `thread_state` 必为 NULL（没有任何先前的 StartSession 提交过），所以**要求影响行数 = 1**，否则返回错误让整事务回滚（与 4B `moved != 1` 同一种硬化） |
| 回滚 | A 缝（`EnqueueExecutionWork`）失败 ⇒ `panic(databaseFailure)` ⇒ `pending` 与 seq=1、工作项一起回滚，不留孤立状态 |
| 重放 | 第二次 `StartSession` 的 seq=1 插入影响 0 行 ⇒ 早退，不写任何东西；`IS NULL` 谓词本身也幂等 |
| 并发 | `Store.transact` 全局 advisory lock 串行 + `IS NULL` 谓词 + `PRIMARY KEY (run_id, seq)` 三重 |
| cancel | `cancel_requested_at IS NOT NULL` 的 run 进不了该事务（WHERE 排除）⇒ 被取消的 run **永远不会**出现 `pending` |
| 终态 | `pending` 只在 `phase='starting'` 可达；`active/idle/ending/ended` 一旦写入后 `IS NULL` 恒假 ⇒ `pending` **不可能**覆盖已推进状态（不构成回退路径） |
| workspace 无效 | `runWorkspaceLive` 为假时整个分支不执行 ⇒ 不写 `pending`（保持 G-011 fail-closed 语义） |
| 与 4B 的关系 | 4B 的 CAS 不读 `thread_state`（它无条件写 `active`），因此「先 pending 后 active」与「从未 pending 直接 active」都成立；**不新增转换**，只是给既有转换补上唯一缺失的前置字面值 |

**语义澄清（相对 Thread D4 的行文）**：Thread D4 写「会话执行**已登记**、尚无记录 → `pending`」，其中「登记」是
A 侧 `RecordDispatch`。本轮把 `pending` 的物化提前到 `StartSession`，即 **`pending` 的精确含义定为
「Cloud 已声明首 prompt 与会话工作项，尚无任何 Node 记录被接管」**（是 D4 那行的**超集**：`StartSession` 先于
`RecordDispatch`）。理由：A 侧不能写业务列（D6 不变量 7），而 B 侧在「登记之后、首批接管之前」没有任何自己的
事务。可观察差异为零：两个时点之间 Thread 仍只有 seq=1、无 Node 记录，前端渲染（输入可用、"等待 Agent 输出"）
完全相同。**该澄清必须随 ADR 修订一并反映**（与 G-017 同批，见 §4C.18）。

**为什么不是这些替代方案**：

| 方案 | 否决理由 |
|---|---|
| A 侧 `RecordDispatch` 登记时写 `pending` | 控制面写业务表，违反 controller-integration D6 不变量 7 / §11；且「跑了 StartSession 但一直没登记」的 run 会永远停在无状态 |
| 读时派生：`phase='starting'` 且 `thread_state IS NULL` ⇒ 对外显示 `pending` | 把一个 DB 可表达的不变量交给每个读者；POST 接受矩阵与 idle 扫描都要各自复制该派生，规则会漂移；`thread_state` 的五值 CHECK 将有一个值不可被持久表达，与已批准 D1/D4 的持久模型静默偏离 |
| 4C 首个接管批次里补写 `NULL → pending → active` | 同一事务内的第二次 `thread_state` 写，既没有可观察中间态，又是自造的转换（Thread D4 没有 `NULL → pending` 这一行） |
| 迁移里给列加 `DEFAULT 'pending'` | 会让 team/workflow run 与「尚未开始」的 agent run 都带上 Thread 状态，破坏 0018 的 `issue_runs_agent_columns` 约束语义 |

### 4C.3 D-4C-02：Thread GET 的快照语义与分页

**路由**：`GET /api/v1/tenants/{tid}/issues/{iid}/runs/{rid}/thread`（Thread D5）。4C 需在 `router.Routes`
注册（`PublicRequest` 已有 `RunID`/`After`/`Limit`；**需新增** `Before` 字段与 `before` 查询参数解析）。

**响应形状**（`items` 恒按 `seq` 升序）：

```
{ "items": [ { seq, source, kind, record, turnId?, status?, createdAt } ],
  "threadState": "pending|active|idle|ending|ended",
  "idleSince":   "…" | null,
  "nextCursor":  "<seq>" | "",
  "prevCursor":  "<seq>" | "" }
```

- `status` **只**在 `source='user'` 时出现（`queued|delivered|discarded`）；`node`/`system` 条目不带该字段。
- **不**暴露 `nodeExecutionId`/`nodeSequence`（D-022 的身份是 Cloud 内部身份，不是公开契约）。

**快照语义**：整次读取在**一个** `Store.transact` 内完成（一次查询 + 一次 run 状态读取），因此 `items` 与
`threadState` 来自同一数据库快照。**必须显式承认一个非对称**：条目 append-only 且 `seq`/`record` 不可变，
列表内容永不改写；而 `threadState` 与各条 `status` 可以前进。因此客户端**不得**把 `threadState` 读作
「光标处的状态」——它只表示响应生成时刻的运行状态。

**分页（本轮裁定，含对 D5 的扩展，见 G-017）**：

| 参数 | 语义 | 校验 |
|---|---|---|
| `after=N` | 返回 `seq > N`，升序，最多 `limit` 条（D5 的原义） | `N` 为十进制非负整数；`after=0` = 从头读 |
| `before=N` | 返回 `seq < N` 中**最新**的 `limit` 条，仍按升序呈现 | `N` 为十进制正整数 |
| 无游标 | **tail 读**：返回最新的 `limit` 条（升序），即 `before` 取「当前 max+1」的语义 | — |
| 同时给 `after` 与 `before` | 拒绝 | `400 invalid_pagination` |
| `limit` | 默认 **200**，`1..500`（D5 的上限 500） | 越界 ⇒ `400 invalid_pagination` |
| 游标非法 | 非十进制整数 | `400 invalid_cursor` |

- `nextCursor` = 传回 `after` 可取下一段（更新）页的 `seq`；`""` 表示已到**当前数据末端**（不表示线程结束）。
- `prevCursor` = 传回 `before` 可取下一段（更旧）页的 `seq`；`""` 表示已到 `seq=1`。
- 实现取 `limit+1` 行判断是否还有更多，再裁剪到 `limit`；`seq` 是 `(run_id, seq)` 主键的一部分 ⇒ 全序、无需次序键。
- **不复用** `page`/`window`：它们的游标是 UUID（`validID` 会拒绝 `seq`）、上限是 100。**也不得**放宽 `window`
  的 100（那会静默改变其它所有列表的契约）。Thread 用自己的读者，但**沿用既有 fault 名**
  （`invalid_pagination`/`invalid_cursor`）以保持错误词汇统一。
- **为什么需要 tail 与 `before`**：主 UI 是对话面板，首屏要最新若干条；只有 `after` 意味着「从 seq=1 向前走」，
  长 Thread（5000 条 / limit 500）要多轮往返才能显示尾部。`before` 与 `after` 对称、同一套 `seq` 词汇，
  不再引入第二种游标类型。`nextCursor`/`prevCursor` 都是**不透明字符串**（客户端原样回传、不得解析；
  编码就是十进制 `seq`，保留可读性以便日志与测试判读）。
- **并发追加**：任何写入都在同一把 advisory lock 内分配 `seq`，且条目不可变 ⇒ 客户端只用
  `after=<自身已见最大 seq>` 前进就**永不漏、永不重**；`seq` 的连续性已由 4B 证明（§4B.5 / D-023）。
- **`idleSince`**：仅在 `thread_state='idle'` 时非空。这是 D5 字面之外的**附加字段**（D5 只点名 `threadState`），
  理由：面板要解释「为什么会话即将结束」并显示倒计时，而 `idle_since` 正是权威依据；附加字段向后兼容，
  但**必须**在 OpenAPI 描述与 ADR 修订中写明（与 G-017 同批）。
- **授权**：与读该 Issue 的评论完全一致（tenant 成员 + 该 Issue 的可读 Space 成员），查询按
  `tenant_id + issue_id + run_id` 三重限定，跨 tenant / 跨 Issue / 软删 run 一律 `404 not_found`（与 `run()` 同）。
- **公开面**：`stripAgentRunSkeleton` 继续把 `threadState`/`idleSince` 等 0018 列从 **run 资源**里剥掉；
  Thread 读者**直接投影**这两列，**不得**顺手把 run 资源的形状改掉（那是一次独立的契约变更）。4C 必须有一条
  测试断言 run 资源的字段集**未变**。
- **终态运行的读取**：`ended` 之后 GET 仍返回完整历史（不可变、不隐藏、不分页降级）；这是「Thread history
  immutability」的直接体现。

### 4C.4 D-4C-03：Thread POST 的条目归属、事务与状态接受矩阵

**路由**：`POST /api/v1/tenants/{tid}/issues/{iid}/runs/{rid}/thread/messages`，**必须**带 `Idempotency-Key`
（Thread D3）。router 的 body 白名单为 `[]string{"content"}`（沿用既有严格解码：未知字段/多值 JSON/类型错误一律
400，服务端身份与 scope 字段由路由而非 body 决定）。

**body**：`{content: [{type: "text", text}]}`；v1 **只接受** `type="text"`，未识别的 block 类型 ⇒
`400 invalid_field_type`（**不静默丢弃**——丢弃等于丢用户内容）。文本合计 ≤ 64 KiB（D3），超限 ⇒
`400 content_too_large`（4C 新增 fault，需进 `internal/contract` + OpenAPI）。

**归属与事务（一个事务，两个写者类，各写各的表）**：

```
Store.Public(POST .../thread/messages)          ← 调用方持有的唯一事务
  ├─ 通用幂等前置（已存在）：require Idempotency-Key；命中且 requestHash 相同 ⇒ 回放原响应并结束
  ├─ 授权：读该 Issue 的成员资格（与评论一致）
  ├─ 权威重读 run（tenant+issue+deleted_at IS NULL+executor_type='agent'）→ 决定接受/拒绝（下表）
  ├─ B 写：seq = MAX(seq)+1；INSERT thread_entries(source='user', kind='user_turn',
  │         record={content:[…]}, turn_id=<Cloud 生成>, status='queued')
  ├─ B 写：CAS issue_runs thread_state → 'active' 且 idle_since=NULL（影响行数必须 = 1）
  ├─ A 缝：EnqueueThreadCommand(t, run, {kind:'SubmitUserTurn', turn_id, content})
  └─ 通用幂等后置（已存在）：INSERT idempotency_records(…, response, status)
提交后：B 发布 SSE 失效提示 issue_run.thread_appended{issueId, runId, lastSeq}；
        A 侧按 D5 发 ThreadCommandAvailable{run_id}（A 自己的提交后义务）
```

- **`turn_id` 由 Cloud(B) 生成**（Thread D3），它是用户轮次的**业务身份**，会被 Node 记录回带（§4C.5）。
  `command_id` 由 A 生成（D-4C-06）。两者都必须出现在命令里，但只有 `turn_id` 进条目。
- **`thread_state` 的写谓词**：`UPDATE issue_runs SET thread_state='active', idle_since=NULL,
  version=version+1, updated_at=now() WHERE id=$1 AND cancel_requested_at IS NULL AND thread_state IN
  ('pending','active','idle')`，**要求影响行数 = 1**；0 行 = 与本次事务内的重读结论矛盾 ⇒ 返回错误整体回滚
  （**不是**伪装成 409 的客户端冲突）。
- **状态接受矩阵**（POST 时权威重读的组合；`cancel_requested_at` 与 `thread_state` 不会出现竞态窗口，因为
  取消路径在**一个**事务内同时写 `cancel_requested_at` 与 `ending`，见 D-4C-11）：

| run / thread 状态 | 结果 |
|---|---|
| run 不存在 / 软删 / 非本 tenant·issue | `404 not_found`（不泄露存在性） |
| `executor_type != 'agent'` | `404 not_found`（非 agent 运行没有 Thread 子资源） |
| `thread_state='pending'`，`cancel IS NULL`，workspace live | **201** 接受 ⇒ thread 变 `active` |
| `thread_state='active'`，`cancel IS NULL`，workspace live | **201** 接受 |
| `thread_state='idle'`，`cancel IS NULL`，workspace live | **201** 接受（清 `idle_since`，回 `active`） |
| `thread_state='pending'\|'active'\|'idle'` 但 workspace 不 live | `409 thread_closed`（G-011：沙盒已不可用，接受命令没有意义，fail closed） |
| `thread_state IN ('ending','ended')` | `409 thread_closed`（D3） |
| `cancel_requested_at IS NOT NULL`（无论 thread_state） | `409 thread_closed`（取消是请求，run 正在走向结束；避免了「命令永远送不出去」） |
| `thread_state IS NULL` 且 `phase='starting'` | **不变量破坏**（D-4C-01 已保证 `pending`）⇒ `500`，**不**当成 409 |
| `phase='running'` 而 `thread_state` 为 NULL/`pending` | **不变量破坏** ⇒ `500` |
| 缺 `Idempotency-Key` | `400 idempotency_key_required`（既有） |
| 同 key 同 body（已提交） | **回放**原响应与状态码（不重算、不看当前 thread 状态） |
| 同 key 异 body（含同 key 换 run：hash 含 path） | `409 idempotency_conflict`（既有） |
| `content` 形状非法 / 未知 block 类型 | `400 invalid_json` / `invalid_field_type` |
| 文本合计 > 64 KiB | `400 content_too_large`（新增） |
| A 缝未接线（`UnavailableAgentRunControlPlane`） | `503 thread_command_unavailable`（新增；见 D-4C-05） |

- **成功响应**：`201` + `{resource: {seq, source:"user", kind:"user_turn", record, turnId, status:"queued",
  createdAt}}`（与既有 POST 的 `{resource: …}` 一致）。
- **不变量**：条目与命令同事务 ⇒ 不存在「命令已可见但条目未提交」或反之；提交前的任何失败（含 A 缝错误）
  都使**两者一起**回滚，并且**幂等记录也不落**，因此客户端用同一 key 重试就是一次干净的首发（不是冲突）。

### 4C.5 D-4C-04：POST 幂等身份矩阵（7 例）

机制：**复用既有 `idempotency_records(tenant_id, user_id, key, request_hash, response, status)`**
（`public.go` 的通用前置/后置），**不新增**每条的幂等列。`request_hash = requestHash(Method, Path, Body)`
——**含 path**，这是下面第 6 例安全的关键。

| # | 场景 | 结果 | 依据/理由 |
|---|---|---|---|
| 1 | 首次请求，key 未见，body 合法，状态接受 | **201** 建条目 + 命令 | 正常路径 |
| 2 | 同 key、同 body，在首次提交**之后**重放 | **同一响应原样回放**（同 `seq`、同 `turnId`） | 通用幂等；不产生第二个条目/命令/事件 |
| 3 | 同 key、同 body，两个请求**同时在飞** | 全局 advisory lock 使其退化为顺序执行；后到者命中幂等记录 ⇒ 与 #2 相同 | 必须用 start barrier 测（4B 并发测试先例）；结果集只允许「一次建 + 一次回放」 |
| 4 | 同 key、**异 body** | `409 idempotency_conflict`，其它什么都没发生 | 既有 fault |
| 5 | **异 key**、同 body（用户确实连发两条一样的话） | **两个**条目、**两个** `turn_id`、两条命令 | 幂等按 key 计，不按内容计；这是正确语义 |
| 6 | 同 key、**异 run**（path 不同） | `409 idempotency_conflict` | hash 含 path ⇒ 不会跨 run 回放第一条的响应；key 的作用域是 `(tenant, user, key)`，**不**静默跨资源复用 |
| 7 | 重放**晚于**状态变化（条目已 `delivered`/`discarded`，或 thread 已 `ending`/`ended`） | **仍回放原响应** | 「同一逻辑请求只有一个结果」；让回放结果依赖后续状态会让客户端重试语义不可判定。**新 key** 在 `ending` 后 ⇒ `409 thread_closed`（矩阵行） |

- **作用域说明**：不同用户在同一 tenant 用同一 key 是两条独立记录（各自 key 空间），因此各自产生条目 ——
  **有意接受**（与本仓所有 POST 一致），并写入 OpenAPI 说明。
- **绝不**把 DB 不变量失败（seq 主键冲突、命令 FK 失败、影响行数 ≠ 1）伪装成 4xx 冲突：一律 500。

### 4C.6 D-4C-05 / D-4C-06：`EnqueueThreadCommand` 契约与四种身份的分离

**D-4C-05：A 拥有 `thread_commands`，B 在**自己的事务内**调用缝。**

| 项 | 结论 |
|---|---|
| 缝签名（已声明） | `EnqueueThreadCommand(t *transaction, run Object, command Object) (string, error)`，返回 `command_id` |
| 谁构造 | **B** 构造业务内容：`{kind: 'SubmitUserTurn'\|'EndSession', run_id, turn_id?/reason?, content?}`（内容归 B：用户文本、结束原因，`reason ∈ user_ended\|idle_timeout\|cancelled`） |
| 谁生成 id | **A 生成 `command_id`**（`thread_commands.id`，D6）；B 不生成 A 行的主键。`turn_id` 由 B 生成并**同时**写进条目与命令体，使「条目 ↔ 命令 ↔ Node 记录」可相关 |
| 事务 | 缝在**调用方事务**内执行，**不**自己开事务、不跨 HTTP/外部 IO；任何 error ⇒ B 的整事务回滚（条目 + 命令 + 幂等记录一起），Controller 侧看不到半成品 |
| 提交后 | A 在**提交后**发 `ThreadCommandAvailable{run_id}`（D5）。B 在提交后发 SSE 失效提示。两者都不得在提交前发 |
| fail-closed | 生产缝未接线时返回错误；在 POST 路径上映射为 `503 thread_command_unavailable`（**可重试**，且因为事务回滚、幂等记录未落，同 key 重试是干净首发）；在 idle 扫描路径上则不做状态转换（run 保持 `idle`，下一 tick 再试）——**绝不**出现「`ending` 已提交但 `EndSession` 命令没写」 |
| 命令不可见窗口 | `StartSession` 已完成但 `RecordDispatch` 未完成时，run 处于 `pending`：POST **仍接受**（矩阵行 3），但命令按 D3 留在 Cloud、`ClaimThreadCommands` **不返回**它。这是 ADR 的既有规则，4C 只负责测试覆盖 |
| 一张表一个写者类 | `thread_commands` 只由控制面代码写（B 只能经这个缝）；`thread_entries`/`issue_runs.thread_state` 只由业务转换写。D6 不变量 7 不变 |

**D-4C-06：四种身份不得混用。**

| 身份 | 生成者 | 载体 | 作用 | 不是什么 |
|---|---|---|---|---|
| `turn_id` | B（Cloud） | `thread_entries.turn_id`；命令体 | **业务身份**：一个用户轮次；Node 记录回带它，Cloud 据此把条目置 `delivered` | 不是命令 id，不是执行 id |
| `command_id` | A | `thread_commands.id` | **投递身份**：Node 按它去重（同一 `command_id` 至多执行一次，Node 协议 D4） | 不是条目的 `seq`，不写进 `thread_entries` |
| `execution_id` | Controller（A 栅栏） | `node_executions.execution_id` | **会话身份**：命令投递登记记 `delivered_execution_id`；一条会话执行一个 Thread 来源 | 不是 run id |
| `seq` | B（Cloud） | `thread_entries.seq` | **顺序身份**（run-scoped，从 1 连续）：分页游标与对话顺序 | 不是 Node 的 `sequence`（执行内，`node_event_receipts`），也**不**下发给 Node |

- 关系：一个用户轮次 ⇒ 恰一个条目（B） + 至多一个命令（A） + 至多一次投递登记（A）。
  一个命令的 `turn_id` 等于对应条目的 `turn_id`；命令的投递目标是该 run 的**当前**会话执行。
- **禁止**用 `seq` 与 Node `sequence` 互推（4B 已固化，4C 不得引入 `seq = sequence + k` 的等价物）。

### 4C.7 D-4C-07：SSE 的权威来源、`after` 统一语义与重连

**必须区分的三件事**（mandate §7）：

| 概念 | 本设计中的实体 | 性质 |
|---|---|---|
| durable log（唯一重放来源） | `thread_entries`（append-only，`PRIMARY KEY (run_id, seq)`） | 持久、权威、可重放 |
| notification mechanism | 进程内 Space event hub（`SpaceEvent`）+ `issue_run.thread_appended` | **易失**、进程内、缓冲 8、无持久化/无重放、单实例（api-boundary ADR，已实现） |
| connection-local delivery | 某条 SSE 连接恰好在连接期间收到的东西 | 可丢、可重、不构成任何承诺 |

**统一 `after` 语义（GET 与 SSE 重连同一个词汇）**：

1. 系统里只有**一个**游标词汇：`seq`。它**只**对 durable log 求值（`after=N` ⇒ `seq > N`）。
2. **客户端游标只由 GET 推进**：唯一能推进游标的响应是 GET 的 `nextCursor`/`prevCursor`/`items`。
   **SSE 事件永远不推进游标**——事件的 `lastSeq` 只说「有数据，去取」，不表示客户端已经拥有它。
3. **SSE 重连 = 客户端重发自己的 GET**：断线期间漏掉的一切由「用自己的游标再 GET 一次」补齐。
   因此流本身**不需要**可恢复，也不需要 `Last-Event-ID` 语义（api-boundary ADR 也未定义它）。
4. `after` 在两种入口下含义完全一致：`after=N` ⇒ 「我已持有 `seq ≤ N` 的全部条目，给我 `> N` 的」。
   `before` 对称（D-4C-02）。**不**给 SSE 引入第二套游标，也**不**让 SSE 承担 `after` 的解释权。

**为什么不把 GET 的语义搬进 SSE（例如 `?after=` 的 Thread 专用流 + 服务端重放）**：那等于让服务端再实现一个
durable 读者（重复 GET 的分页/上限/游标规则），并且会诱导客户端把流当日志——正是 Thread D5 与 api-boundary
ADR 明确禁止的用法。若将来要做「可恢复的流式投递」，必须另行定义 `Last-Event-ID == seq` 与有界重放窗口
（本轮明确**不在** 4C）。

**公开事件形状**：`issue_run.thread_appended{issueId, runId, lastSeq}`，发布在 Space 事件流上（客户端按
`issueId`/`runId` 过滤）。`lastSeq` = 提交时刻该 run 的 `max(seq)`。**实现注记**：现有 `SpaceEvent` 只有
`{type, spaceId, projectId?, version?}`，因此 4C 需**新增可选字段** `issueId`/`runId`/`lastSeq`
（空值省略，既有事件的 JSON 保持逐字节不变——`project.created` 已有 `projectId` 的先例）。

### 4C.8 D-4C-08：SSE 的顺序、重复与丢失

| 维度 | 结论 |
|---|---|
| 顺序 | 不承诺。事件在提交**之后**发布，因此并发提交的两次发布可能乱序到达，客户端可能看到 `lastSeq` 从 7 回到 5。**这不是 bug**：客户端只把事件当触发，游标由 GET 推进，GET 读的是严格有序的 durable log。**禁止**从「事件顺序」推断 Thread 顺序 |
| 重复 | 允许且无害。重复事件 = 再触发一次幂等 GET；服务端不做去重，客户端可自行合并 |
| 丢失 | **数据不丢**（log 持久 + GET 权威）；**通知可能丢**（缓冲 8、慢订阅者、进程重启、单实例）。因此客户端在「长时间收不到事件」时必须靠**轮询**收敛：本设计规定前端需有重连 + 周期轮询（建议 ≥ 30s，或在窗口重新获得焦点时）作为兜底。**不得**宣称「每次 append 都会送达一次通知」 |
| 多实例 | 进程内 hub 单实例（api-boundary ADR 已限定 slice 1）：跨实例的订阅者收不到失效提示 ⇒ 只能靠轮询。记为 G-020（依赖，非本轮阻塞） |
| 状态变化无新条目 | 仅 `status`（`queued → delivered/discarded`）或 `thread_state` 变化、`max(seq)` 未变时，Cloud **仍**发布 `issue_run.thread_appended`（`lastSeq` 与上次相同）。理由：复用 ADR D5 点名的事件类型，payload 恒为「提示」而非事实，客户端无需分支。**替代方案**（改名 `issue_run.thread_updated` 或新增类型）因偏离 ADR 字面被否决，若 ADR 修订可再议 |
| 客户端刷新已加载区间 | 前进分页（`after`）只能发现**新**条目；更旧条目的 `status` 翻转必须靠**重读已加载区间**（如 `after=<firstSeq-1>&limit=<窗口大小>`）。**禁止**把状态变化做成新条目（会重复内容、破坏「一轮条目」），也**禁止**让事件携带该事实 |
| 终态之后 | 事件可能停止（会话结束后不再有 append）。客户端靠轮询 + `threadState` 收敛；GET 在 `ended` 后仍返回完整历史 |

### 4C.9 D-4C-09：`active` / `idle` 生命周期的权威

**转换表（写者 / 权威 / 事务）**：

| 转换 | 权威（证据） | 写者与事务 | 幂等 / 重放 | 终态性 |
|---|---|---|---|---|
| （无）→ `pending` | Phase 3A 会话声明（seq=1 + `agent_session` 工作项） | B：`StartSession` 事务（D-4C-01） | `IS NULL` 谓词；seq=1 影响 0 行 ⇒ 早退 | 可前进 |
| `pending` → `active` | 首条**真实** Node 记录被接管（D-019/D-024，**已实现**） | B：`ThreadEventsTakenOver` 的 `starting→running` CAS | 重放 `taken==0` ⇒ 早退 | 可前进 |
| `active`/`pending`/`idle` → `active` | 新用户轮次写入（D3） | B：POST 事务（D-4C-03） | 幂等由 POST 的 key 保证；CAS 要求 1 行 | 非终态（振荡） |
| `idle` → `active` | 接管到**非** `turnEnded` 的记录 | B：`ThreadEventsTakenOver` 追加写（§4C.13 第 2 项） | 重放 `taken==0` ⇒ 早退 | 非终态 |
| `active` → `idle` + `idle_since` | 接管到 `turnEnded` 且**提交时无 `status='queued'` 的用户轮次** | B：同一接管事务 | 同上；`idle_since = now()`（**数据库时间**） | 非终态 |
| `pending`/`active`/`idle` → `ending` | 用户结束 / `idle` 超窗 / 取消 | B：POST-end、idle 扫描、cancel 三条路径，各自单事务 | CAS `thread_state IN ('pending','active','idle')` ⇒ 恰好一次 | 近终态（不可回 `active`） |
| `ending` → `ended` | **`SessionEnded`**（会话终态接管钩子） | **Phase 5**，本周不实现 | 终态事件重放幂等 | **终态（吸收态）** |

**`idle` 的判定细节（必须写清，避免实现走偏）**：

- 判定发生在**接管事务内**、条目写入之后；条件是
  `NOT EXISTS (SELECT 1 FROM thread_entries WHERE run_id=$1 AND source='user' AND status='queued')`。
  有排队轮次 ⇒ Agent 还会执行它 ⇒ **保持** `active`。
- **批次末条记录决定**：一批里若 `turnEnded` 之后还有别的记录，最终态是 `active`；只有**该批次最后一条被接管的
  内容记录是 `turnEnded`**（且无 queued 轮次）才落 `idle`。实现必须记录「最后一条被接管记录的 kind」，
  不能只判断「批里出现过 `turnEnded`」。
- 门控：仅当 run 处于活跃会话（`phase='running'` 且 `thread_state ∈ ('pending','active','idle')` 且
  `cancel_requested_at IS NULL`）才写 `thread_state`；否则**跳过状态转换但仍追加条目**（4B 的
  「ack 过的记录不得静默丢弃」语义保持）。跳过是**设计允许**的 0 行；若事前重读判定为「可写」而 CAS 影响 0 行，
  则是**不变量破坏**⇒ 返回错误回滚（与 4B `moved != 1` 同一种硬化）。
- `idle_since` 只在 `idle` 有意义：进入 `active`（用户轮次或新记录）与进入 `ending` 时都必须置 NULL。
- Agent 首轮异常结束也走同一条路（接管到 `turnEnded` ⇒ `idle`），**不自动结束**（Thread D4 的理由保持）。

### 4C.10 D-4C-10：`ending` / `ended` 的权威与 `SessionEnded` 依赖

1. **`ending` 的三个触发**（都写 `thread_state='ending'` + 清 `idle_since`，并在**同一事务**里放出
   **恰好一条** `EndSession{reason}`）：(a) 用户主动结束（`user_ended`）；(b) `idle` 超窗（`idle_timeout`）；
   (c) run 被取消且 `phase='running'`（`cancelled`）。
2. **`idle_timeout` 的扫描器归 B**（与 Phase 3A 的 starting 重试循环同族的派发循环任务）：
   读 `thread_state='idle' AND idle_since IS NOT NULL AND idle_since + <window> < now() AND
   executor_type='agent' AND cancel_requested_at IS NULL AND deleted_at IS NULL`，逐 run 开**短事务**：
   重读 → CAS `idle → ending`（影响行数必须 1，0 行 = 别的 tick 赢了 ⇒ 跳过）→ `EnqueueThreadCommand(EndSession{idle_timeout})`。
   窗口 `issue_runs.thread_idle_timeout` = **进程配置项**（点号命名的配置 key，默认 15 分钟），**不是**列：
   ADR 称其为「Cloud 配置」，且没有任何按 run 变化的证据；判定与比较**全部用数据库时间**（Thread D4 不变量 5）。
   扫描节奏必须远小于窗口（与 3A 的 10s 同量级，属实现选择，D-008 的先例）。
3. **`ended` 的权威 = `SessionEnded`**（会话终态 `TakeOverNodeEvent` → 钩子 `sessionEnded`，
   controller-integration D6）：Thread → `ended`、仍 `queued` 的用户轮次 → `discarded`、run → `delivering`
   并放出交付工作项。该钩子**未实现**，属 **Phase 5（delivery 切片）**。
   **4C 只到 `ending`；不得**以「`EndSession` 命令已被 Node 受理」或「会话执行结果已登记」当成 `ended` 的权威
   （`SessionCommandAccepted` 是**回复**，不是序列事件，也不是状态，Node 协议 D2）。
4. **用户主动结束的端点**：ADR D4 有该转换但**没有**定义 API 形状 ⇒ **G-018（OPEN / NON-BLOCKING）**。
   推荐形状：`POST .../runs/{rid}/thread/end`（`Idempotency-Key` 必填；`ending|ended` ⇒ `409 thread_closed`；
   成功 202 + `threadState`）。**必须**与「取消 run」区分：结束 Thread 只放 `EndSession{user_ended}`，
   run 照常进入交付；取消 run 写 `cancel_requested_at`，只影响最终 `status`（D-4C-11）。
5. **`ending` 是近终态**：不可回 `active`；`ending` 之后 POST 一律 `409 thread_closed`。
   **`ended` 是吸收态**：没有任何 4C/Phase 5 转换从 `ended` 出发。
6. **API/SSE 可见性**：`threadState` 在 GET 与每次失效提示的重取里可见；`pending`/`active`/`idle` 时输入可用，
   `ending`/`ended` 时输入禁用（前端按状态渲染）。

### 4C.11 D-4C-11：取消与终态的交互（与 4B 的 cancel-first 规则一致）

| 场景 | 结果 |
|---|---|
| POST 时 `cancel_requested_at` 已置 | `409 thread_closed`（接受矩阵），**不写**条目/命令 |
| 取消时 `phase='provisioning'\|'starting'` | 按 IssueRun D4/D6 直接进 `releasing`/`cancelled`，`deliveryState=skipped`：**不写 `ending`、不发 `EndSession`**（此时会话输出还不构成 Thread 内容，交付不欠）。**不违反** 4B 的「cancel-first 接管不进入 running」——4B 保持原样：被取消的 run 不因接管而进 `running` |
| 取消时 `phase='running'` | 一个事务内：写 `cancel_requested_at` + CAS `thread_state → 'ending'`（`IN ('pending','active','idle')`）+ `EndSession{cancelled}`。因此**没有**「已取消但仍是 `active`」的可观察窗口（接受矩阵里那条组合不可能出现） |
| 取消时 Thread 已是 `ending` | 只写 `cancel_requested_at`：**不**再放第二条 `EndSession`，**不**改写原因。最终 `status` 由 `cancel_requested_at` 在结算（Phase 5）决定——这是 IssueRun 的规则，不是 4C 的 |
| 未投递的命令（`queued` 条目）在会话结束时 | 在 `SessionEnded` 事务中标 `discarded`（**Phase 5 写**）；4C 提供列与谓词，并保证「`ending` 之后不再产生新的 `queued`」 |
| 重试（run 已终态后重发同一 POST） | 同 key ⇒ 回放原响应（幂等矩阵 #7）；新 key ⇒ `409 thread_closed` |
| 接管与 POST 竞争 | 全局 advisory lock ⇒ 全序；两种提交顺序都合法：先 POST ⇒ 接管看到 `queued` ⇒ 保持 `active`；先接管 ⇒ 落 `idle`，随后 POST 置 `active` 并清 `idle_since`。测试必须断言**完整允许结果集**，不是最常见结果 |
| 接管与取消竞争 | 两种顺序都不得产生第二个 running 权威：取消先 ⇒ 4B 的 `cancel_requested_at IS NULL` 门控使 running CAS 跳过（条目仍追加）；接管先 ⇒ run 已 `running`，取消随后走 `ending` 分支 |
| 终态运行的 GET / SSE 重放 | GET：返回完整不可变历史；SSE：事件可能已停 ⇒ 轮询兜底 |
| Thread history 不可变 | 任何 4C 转换都**不写** `record`，**不重编号** `seq`；唯一可变的列是 `status`，且单向 `queued → delivered | discarded`。`thread_state` 除 `active ⇄ idle` 振荡外无回退，`pending` 只前进，`ending`/`ended` 吸收 |

### 4C.12 D-4C-12：schema 决策（**需要 4C migration**，本轮只设计）

**结论：需要一条新 migration（编号 0022，接 0021）。** 逐项理由：

| 需要 | 内容 | 理由 |
|---|---|---|
| 新控制面表 `thread_commands` | `id uuid PK`（= `command_id`）、`run_id uuid NOT NULL REFERENCES issue_runs(id)`、`kind text CHECK (kind IN ('SubmitUserTurn','EndSession'))`、`body jsonb NOT NULL CHECK (jsonb_typeof(body)='object')`、`created_at timestamptz NOT NULL DEFAULT now()`、`delivered_at timestamptz`、`delivered_execution_id text`；`CHECK ((delivered_at IS NULL) = (delivered_execution_id IS NULL))`；索引 `(run_id, created_at) WHERE delivered_at IS NULL` | controller-integration D6 点名的表，当前**不存在**；「至少投递一次」需要未投递扫描索引。`run_id` 的 FK 沿用 `execution_work.run_id REFERENCES issue_runs(id)` 的先例（0019） |
| 新列 `thread_entries.status` | `text CHECK (status IN ('queued','delivered','discarded'))`，**且 `CHECK ((source='user') = (status IS NOT NULL))`**；索引 `(run_id) WHERE source='user' AND status='queued'` | `idle` 判定与「会话结束时清理排队轮次」都需要一个**持久**的轮次状态。`discarded` 无法从任何别的表派生（它是会话结束时 Cloud 的决定）⇒ 必须落列。**替代方案**（由「是否存在带同 `turn_id` 的收据」派生 `delivered`）被否决：那让业务读取控制面表，且派生不出 `discarded` |
| 一条幂等回填 | `UPDATE issue_runs SET thread_state='pending' WHERE executor_type='agent' AND thread_state IS NULL AND EXISTS (SELECT 1 FROM thread_entries te WHERE te.run_id = issue_runs.id AND te.seq = 1)` | 使 D-4C-01 的物化对**已存在**的 run 也成立，避免 4C 之后仍存在「已声明首 prompt 但 `thread_state` 为 NULL」的 run。谓词与代码路径的判据一致（seq=1 存在 = StartSession 已跑过），可重复执行、安全重试；migration 内 DML 有先例（0010/0016） |
| 索引 `issue_runs (idle_since) WHERE thread_state='idle'` | 支撑 idle 超窗扫描 | 避免全表扫描；与 A 侧无关 |

**明确「不做」的部分**：

- **不改** `issue_runs.thread_state` 的值集（0018 已含 `pending|active|idle|ending|ended`），**不改** `idle_since`。
- **不新增** `issue_runs.next_thread_seq` 之类的计数列或独立 sequence 表：`MAX(seq)+1` 在全局锁内已足够（D-023，§4B.5），
  且用户轮次与 Node 接管共享同一把锁。
- **不新增** `thread_entries` 的幂等列：POST 幂等交给既有 `idempotency_records`（D-4C-04）。
- **不改** `node_event_receipts`（收据 GC/cap 仍 G-015，未决）。
- **不给** `issue_runs.thread_state` 加 `DEFAULT`（见 D-4C-01 的否决表）。
- **不建**任何 `starting→running` 的等价表/列（4B 的唯一权威不变）。
- `thread_commands` 的 `delivered_at`/`delivered_execution_id` 是**单行一次写**（`AND delivered_at IS NULL` +
  影响行数校验），不使用 trigger；重复投递登记幂等由该谓词承担。**取舍**：不保留重复投递的历史（ADR 认可以
  第一次登记为准；Node 去重使重复投递无副作用）。

### 4C.13 4C 实现所需的 4B 改动（**实现前置**，本轮不改代码）

1. **echo 去重推广**（`internal/core/agent_run_thread.go`）：现谓词是 `turnID == initialTurnID`（只有首 prompt）。
   4C 起，`turn_id` 命中任意 **Cloud 生成的用户轮次**（`thread_entries` 中 `source='user'` 的行）都必须
   **不产生新条目、不分配 seq**，并把该行 `status` 从 `queued` 置为 `delivered`（ADR D3：「Node 执行某个用户轮次时
   写入的用户消息记录携带同一 turn_id；Cloud 接管到它时把对应 user_turn 条目标为 delivered」）。收据照写。
   否则用户消息会在 Thread 里重复出现两次（Cloud 一份 + Node echo 一份），且那条 echo 记录会被当成 `active` 证据。
2. **`thread_state` 的 4C 分支**：接管事务内，除既有 `starting→running` CAS（写 `active`）之外，还要
   (a) 对已 `running` 的 run，`idle → active`（新记录到达）；(b) 批次末条为 `turnEnded` 且无 `queued` 轮次时落
   `idle` + `idle_since=now()`。二者都在门控内（§4C.9），0 行只有在事前判定为「可写」时才是错误。
3. **`StartSession` 物化 `pending`**（D-4C-01）：在 seq=1 插入成功后、且同一事务内。
4. **`SpaceEvent` 扩展**：新增可选 `issueId`/`runId`/`lastSeq`（`omitempty`），既有事件 JSON 不变。
5. **控制面实现**（A）：`StoreAgentRunControlPlane.EnqueueThreadCommand`（写 `thread_commands` + 返回
   `command_id`）、`ClaimThreadCommands`（纯读、按 run 分组、组内创建顺序、返回会话 `execution_id` 与目标 Node、
   要求 lease epoch）、`RecordThreadCommandDelivered`（CAS `delivered_at IS NULL` + 幂等）、`Watch` 的
   `ThreadCommandAvailable{run_id}`，以及 gRPC `AgentRunService` 的对应方法。
6. **公开面**：router 注册 `GET .../thread`、`POST .../thread/messages`（+ 若要 G-018 的 `POST .../thread/end`）；
   `PublicRequest` 增 `Before`；`internal/contract` + `api/openapi.json` + `task frontend:generate`（生成物不得手改）；
   `content_too_large` / `thread_closed` / `thread_command_unavailable` 三个新 fault 进稳定错误契约。
7. **派发循环**：idle 超窗扫描（§4C.10 第 2 项）与 `cmd/server` 接线。
8. **不动**：`node_event_receipts`、`execution_work`、`node_executions`、4B 的批次分类/收据/`last_event_sequence`
   栅栏、`starting→running` 的唯一权威。

### 4C.14 事务 / 锁 / 并发矩阵（10 场景）

**前提**：所有 Cloud 写入都经 `Store.transact` ⇒ `pg_advisory_xact_lock(67420911)`（Phase 1 基线）。因此 4C 的
**所有**并发场景都退化为「同一把锁内的全序」，需要证明的不是「不会并发」而是「任意顺序下的结果都在允许集内、
且每个非法中间态都不可观察」。**本轮不新增任何锁，不改变锁的粒度或顺序**；若将来 G-004 移除全局锁，则
4C 的这些转换需要 per-run 锁或基于 CAS 的排序——该替换属独立架构任务，记录为依赖。

| # | 场景 | 串行化后的允许结果集 | 必须拒绝/禁止的结果 |
|---|---|---|---|
| 1 | POST ‖ POST（同 run，异 key） | 两条条目，`seq` 连续（`MAX+1` 两次）；两条命令；两次 SSE 发布 | `seq` 重复、条目互换顺序、只落一条 |
| 2 | POST ‖ 接管批次（同 run） | 两种顺序都合法：POST 先 ⇒ 接管看到 `queued` ⇒ 保持 `active`；接管先（末条 `turnEnded`）⇒ `idle`，POST 随后 ⇒ `active` + `idle_since=NULL` | `idle` 与 `queued` 同时成立却停在 `idle`；`seq` 冲突；条目丢一条 |
| 3 | POST ‖ POST（同 key 同 body） | 恰好一条条目 + 一条命令；两次响应相同（一次建、一次回放） | 两条条目 / 两条命令 / 两次 201 都"新建" |
| 4 | 接管 ‖ 接管（同 run） | 4B 已证：一批成功、另一批要么补序成功、要么 `CONFLICT`；`seq` 无重复（T4B-16） | 第二个 running 权威、`seq` 重复、`idle` 判定双写 |
| 5 | idle 扫描 ‖ POST（thread 为 `idle`） | 扫描先 ⇒ `ending` + `EndSession{idle_timeout}`，POST ⇒ `409 thread_closed`；POST 先 ⇒ `active`，扫描 CAS 影响 0 行 ⇒ **不发** `EndSession` | 既 `ending` 又接受了新轮次；两条 `EndSession`；`EndSession` 与转 `active` 同时提交 |
| 6 | idle 扫描 ‖ idle 扫描（两 tick / 两实例） | 恰好一条 `EndSession`（CAS 只允许一个 tick 从 `idle` 迁出） | 重复 `EndSession`、重复计时 |
| 7 | cancel ‖ 接管批次 | 取消先 ⇒ running CAS 被 `cancel_requested_at IS NULL` 门控跳过（条目仍追加，4B 语义保持）；接管先 ⇒ 已 `running`，取消随后走 `ending` 分支 | 取消后仍进 `running`（违反 4B）；running 与 `ending` 双权威 |
| 8 | cancel ‖ POST | 取消先 ⇒ POST `409 thread_closed`；POST 先 ⇒ 条目已建，取消随后 `ending`；该轮次可能已被 Node 执行（`delivered`）或未执行（Phase 5 置 `discarded`），两者都合法 | 取消后仍接受新轮次；已接受轮次凭空消失（条目不可删） |
| 9 | GET ‖ 任意写 | 同一把锁内的读 ⇒ 一致快照；分页前进永不漏/重 | 撕裂页、缺 `seq`、跳号 |
| 10 | `SessionEnded`（Phase 5）‖ POST | 依赖：`ending` 提交后 POST 一律被接受谓词拒绝 ⇒ Phase 5 事务里不会再有新的 `queued` 行冒出 | 4C 侧保证：**不得**存在「`ending` 之后仍能建 `queued` 条目」的窗口 |

**不新增全局锁的证明义务**：以上每一条的排序都由**既有**全局锁提供，且每个状态转换各自带 CAS 谓词与影响行数
校验（「0 行 = 有人先动了」在事前判定为可写时视为不变量破坏）。因此本轮**不引入** per-run 锁、行锁升级或
`SELECT ... FOR UPDATE`。

### 4C.15 API 错误分类（与 4B 一致，绝不把 DB 不变量失败伪装成冲突）

| HTTP | fault | 出现处 | 语义 |
|---|---|---|---|
| 400 | `idempotency_key_required` | POST 缺 key | 既有 |
| 400 | `invalid_json` / `invalid_field_type` | body 形状/未知 block 类型/未知字段 | 既有（严格解码） |
| 400 | `content_too_large` | 文本合计 > 64 KiB | **4C 新增** |
| 400 | `invalid_pagination` | `limit` 越界、`after` 与 `before` 同时给 | 既有 fault 名，Thread 复用 |
| 400 | `invalid_cursor` | 游标不是十进制整数 | 既有 fault 名，Thread 复用 |
| 404 | `not_found` | run 缺失/软删/非本 tenant·issue/非 agent | 既有（不泄露存在性） |
| 409 | `idempotency_conflict` | 同 key 异 hash（含换 run） | 既有 |
| 409 | `thread_closed` | `thread_state ∈ {ending,ended}`，或 `cancel_requested_at` 已置，或 workspace 不 live | ADR D3 点名 |
| 503 | `thread_command_unavailable` | A 缝未接线/不可用（事务已回滚，重试安全） | **4C 新增**，可重试 |
| 500 | 内部错误（`databaseFailure` 家族，稳定 Fault 只给通用 code） | 影响行数 ≠ 1、`seq` 主键冲突、命令 FK 失败、`thread_state` 与 `phase` 组合不可能 | **绝不**映射为 4xx |

- **不沿用** 4B 的内部/控制面 fault（`takeover_conflict`、`sequence_gap`、`receipt_conflict`、`empty_thread_batch`、
  `invalid_takeover`）：它们是 internal control / gRPC 的错误词汇，不是公开 API 的。
- 公开响应**不得**出现 SQL、栈、约束名；`thread_closed` 必须能由客户端区分「已结束/已取消/沙盒不可用」之外的
  处置动作（三者都只需停止发送），因此不为三者各造一个 code。

### 4C.16 测试设计矩阵 T4C-1..T4C-34（**设计**，本轮不写测试；全部 `DESIGNED / MISSING`）

证据预期：`integration`（真实 PostgreSQL + 真实 HTTP）为验收层；`internal/core` 白盒（真实 PostgreSQL + 注入
A/B seam）为控制核心层；并发项必须用 start barrier 并断言**完整允许结果集**。**本轮不实现任何测试**。

| ID | 义务 | 层 | 依据 |
|---|---|---|---|
| T4C-1 | `pending` 在 `StartSession` 事务内物化，且与 seq=1、工作项同生共死 | core 白盒 | D-4C-01 |
| T4C-2 | `StartSession` 重放（seq=1 影响 0 行）不再写 `thread_state`；`IS NULL` 谓词幂等 | core 白盒 | D-4C-01 |
| T4C-3 | A 缝失败 ⇒ `pending` 与 seq=1 一起回滚 | core 白盒 | D-4C-01 |
| T4C-4 | 被取消 / workspace 不 live 的 run 永不出现 `pending` | core 白盒 | D-4C-01 |
| T4C-5 | migration 0022 全新库 + 从 0021 升级；回填只对「有 seq=1」的 agent run 生效、可重复执行 | 迁移测试 | D-4C-12 |
| T4C-6 | GET 免游标 = tail 读（最新 `limit` 条、升序）；`prevCursor` 可继续向旧翻页 | 集成 | D-4C-02 |
| T4C-7 | `after=N` 严格 `> N`；`after=0` = 从头；`nextCursor=""` 表示到达当前末端（非线程结束） | 集成 | D-4C-02 |
| T4C-8 | `before=N` 取更旧页且恒升序；与 `after` 同时给 ⇒ `400 invalid_pagination` | 集成 | D-4C-02 |
| T4C-9 | `limit` 默认 200 / 上限 500；越界 ⇒ `400 invalid_pagination`；非整数游标 ⇒ `400 invalid_cursor` | 集成 | D-4C-02 |
| T4C-10 | `threadState`/`idleSince` 只由 Thread 响应暴露；**run 资源字段集不变**（`stripAgentRunSkeleton` 语义保持） | 集成 | D-4C-02 |
| T4C-11 | 跨 tenant / 跨 Issue / 软删 run ⇒ `404`；非成员 ⇒ 拒绝，且与评论授权一致 | 集成 | D-4C-02 |
| T4C-12 | 分页前进在并发追加下不漏不重（写入者与读者交错，`seq` 无洞） | 集成（并发） | D-4C-02 |
| T4C-13 | POST 接受矩阵逐行（`pending`/`active`/`idle` 接受；`ending`/`ended`/已取消/workspace 不 live ⇒ `409 thread_closed`） | 集成（表驱动） | D-4C-03 |
| T4C-14 | POST 成功后条目 `queued` + 命令同事务存在；`thread_state='active'`、`idle_since=NULL` | 集成 | D-4C-03 |
| T4C-15 | A 缝失败 ⇒ 条目、命令、幂等记录**一起**回滚；同 key 重试是干净首发（非冲突） | core 白盒 | D-4C-03/05 |
| T4C-16 | 幂等矩阵 7 例逐条（含 #5 异 key 同 body ⇒ 两条、#6 同 key 异 run ⇒ `409`、#7 终态后回放原响应） | 集成（表驱动） | D-4C-04 |
| T4C-17 | 同 key 并发的完整允许结果集（一次建 + 一次回放） | 集成（并发 + barrier） | D-4C-04 |
| T4C-18 | `turn_id` 由 Cloud 生成并在响应可见；条目与命令携带同一 `turn_id` | 集成 | D-4C-06 |
| T4C-19 | 接管的 echo（`turn_id` 命中 `source='user'` 行）不新增条目、不分配 seq、把该行置 `delivered` | core 白盒 | D-4C-13-1 |
| T4C-20 | `queued` 条目在接管 echo 前一直保持 `queued`；`delivered` 不被回退 | core 白盒 | D-4C-06 |
| T4C-21 | `EnqueueThreadCommand` 在调用方事务内执行；失败整体回滚；`command_id` 由 A 生成并返回 | core 白盒 | D-4C-05 |
| T4C-22 | 会话执行未登记时 POST 仍成功，但 `ClaimThreadCommands` 不返回该命令；登记后返回 | core 白盒 | D-4C-05 |
| T4C-23 | `RecordThreadCommandDelivered` 幂等（重复登记不改变行），登记后 `ClaimThreadCommands` 不再返回 | core 白盒 | D-4C-05 |
| T4C-24 | `ThreadCommandAvailable` 只在提交后发出；回滚不留信号 | core 白盒 | D-4C-05 |
| T4C-25 | SSE 失效提示只在提交后发布；回滚不发；payload 含 `issueId`/`runId`/`lastSeq` 且 `lastSeq` = 提交时 `max(seq)` | 集成 | D-4C-07/08 |
| T4C-26 | 只有状态变化（无新条目）时仍发布提示，且 `lastSeq` 与上次相同 | 集成 | D-4C-08 |
| T4C-27 | 事件重复/乱序到达对客户端收敛无影响（游标只由 GET 推进） | 集成 | D-4C-08 |
| T4C-28 | 接管末条为 `turnEnded` 且无 `queued` ⇒ `idle` + `idle_since`（数据库时间）；有 `queued` ⇒ 保持 `active` | core 白盒 | D-4C-09 |
| T4C-29 | 批次末条为 `turnEnded`、其后又有记录 ⇒ 最终 `active`；`idle` 不由「批中出现过 turnEnded」触发 | core 白盒 | D-4C-09 |
| T4C-30 | `idle` 状态下接管新记录 ⇒ `active` 且 `idle_since=NULL`；stale/取消 run 追加条目不写 `thread_state` | core 白盒 | D-4C-09 |
| T4C-31 | idle 扫描：超窗只放一条 `EndSession{idle_timeout}`；两 tick 也只一条；窗口内 POST 使之 `active` 且不发命令 | core 白盒 + 集成 | D-4C-10 |
| T4C-32 | 取消：`starting` 直进 `releasing`（无 `ending`/无 `EndSession`）；`running` ⇒ `ending` + `EndSession{cancelled}`；已 `ending` 不重复发 | core 白盒 | D-4C-11 |
| T4C-33 | 并发 10 场景（§4C.14）逐条断言完整允许结果集 | core 白盒 / 集成（barrier） | §4C.14 |
| T4C-34 | 终态运行的 GET 返回完整不可变历史；`record` 永不改写、`seq` 永不重编号 | 集成 | D-4C-11 |

**明确不覆盖（保持 `Missing`，不进本轮矩阵）**：`ending → ended`、`discarded` 的写入、交付/释放/`done`
（Phase 5）；收据 GC / cap（G-015）；多 Controller worker 的命令分区（G-021）；多实例 SSE（G-020）。

### 4C.17 specs 证据纪律（mandate §13）

- 本轮**不**把 Thread GET / POST / SSE / `EnqueueThreadCommand` / `idle` / `SessionEnded` / 交付 / Phase 5 中的
  **任何**义务标记为 `Covered`。
- `specs/test-cases/cloud/thread/durable-thread.md`：既有三组义务（条目连续、用户轮次、空闲结束）保持 `Missing`
  不变；**新增两组** `Missing` 义务——「Thread Reads Are Snapshots Addressed By Sequence Cursors」
  （GET 快照/游标/limit/并发追加/状态翻转）与「Thread Invalidation Notices Are Hints And Never The Log」
  （通知形状与提交序 / 丢通知不丢数据 / 重复乱序无害 / 客户端重连与轮询），并在开头登记本节的设计轮状态。
- `specs/test-cases/cloud/controller-integration/agent-run-executions-and-thread.md`：既有义务状态不变
  （「批次原子与顺序」`Covered`、「终态顺序」`Partial`、「命令至少投递一次」「授权不持久化」`Missing`）；
  **新增两行** `Missing` 义务（条目与命令同事务原子 + `503` 回滚可重试；投递身份与轮次身份分离），并登记 4C 设计轮状态。
- 依据：`Covered` 需要直接证据，而本轮**没有任何实现**；`Partial` 亦不适用（4C 尚无代码）。已批准的 Thread ADR
  与本设计共同构成"义务已固定、证据未产生"的状态。写入 specs 的仅是**义务与状态**（文档），不含任何测试代码、
  实现或 ADR 状态变更；位于 `proposed` 的 controller-session ADR **未被触碰**，其核心测试用例也**未**新增（§0.3）。
- 4B 已 `Covered` 的行**不因 4C 设计而改变**；`SessionEnded` 相关的「终态顺序」保持 `Partial`（Phase 5 未实现）。

### 4C.18 未决项 / 依赖（不假装 closed）

| ID | 状态 | 内容 | WHY | WHAT IS NEEDED TO CLOSE |
|---|---|---|---|---|
| G-016 | **CLOSED（本轮，D-4C-01）** | 字面 `thread_state='pending'` 的写入者 | 已选定 B/`StartSession` + 幂等回填，并给出被否决方案的理由 | 已闭；4C 实现时按 T4C-1..T4C-5 取证 |
| G-017 | **OPEN / NON-BLOCKING / DEPENDENCY** | tail 读、`before` 游标、响应里的 `idleSince` 都是对已批准 Thread D5 字面的**扩展**；`pending` 的时点澄清（D-4C-01）也改动 D4 的措辞 | D5 只定义了 `after`/`limit`（升序），未定义无游标默认、更旧分页与 `idleSince`；实现若带着未记录的扩展落地，就是「静默偏离已批准 ADR」 | 修订 Thread ADR D4/D5（补：`pending` 的物化时点与含义、无游标 = tail、`before`、`idleSince`、`nextCursor`/`prevCursor`），或架构师显式确认该扩展；**对应代码在修订/确认前不应合并** |
| G-018 | **OPEN / NON-BLOCKING** | 「用户主动结束」的**端点形状**在 ADR 中缺失（D4 有该转换，无 API） | 没有端点 ⇒ `ending` 的 `user_ended` 触发在 4C 不可达；但 idle 超窗与取消两条触发不依赖它 | 决定 route/verb/状态码/幂等（推荐 `POST .../runs/{rid}/thread/end`，202 + `thread_closed` 冲突），并在 OpenAPI 落地 |
| G-019 | **CLOSED（Phase 5 Batch 1 + Batch 2，2026-10-08）** | `ending → ended` 与 `queued → discarded` 的写者是 `SessionEnded`（会话终态钩子） | 终态事实来自 Node 的终态事件接管；4C 若自造路径将产生第二个终态权威 | 已闭：Batch 1 实现 `SessionEnded` 与 `ending → ended`/`discarded`/`running → delivering`；Batch 2 实现交付执行、`DeliverySettled`、D5 重试/放弃、`releasing`、`RunWorkspaceDeleted`、`done`。Revision 登记的成功分支因 ADR 为 `proposed` 而受阻，**单独**登记为 G-030 |
| G-020 | **OPEN / NON-BLOCKING / DEPENDENCY** | SSE 失效提示**不保证送达**（进程内 hub、缓冲 8、单实例、可能乱序/重复） | api-boundary ADR 已把 slice 1 限定为单实例、无重放；把它当可靠投递会诱导客户端把流当日志 | slice 1 由客户端「重连 + 周期轮询」兜底（属前端义务）；多实例需换成 broker 并另行决策 |
| G-021 | **OPEN / NON-BLOCKING** | 多 Controller worker 时 `thread_commands` 的投递分区 | controller-integration ADR 自己列为未决；4C 的缝与表不改变该问题 | 另行决策（按 run 分区/租约归属），4C 只需保证「至少投递一次 + 登记幂等」在单 worker 下成立 |
| G-012 | **PARTIAL（不变）** | 生产 Controller 环（真正调用 `TakeOverThreadEvents`/`ClaimThreadCommands`） | 4C 只设计 Cloud 侧 | 生产 Controller 实现 |
| G-015 | **PARTIAL（不变）** | 收据 cap / retention enforcement | 值仍无 ADR 依据 | 批准的 ADR 值 + 实现与证据 |
| G-001 / G-004 / G-011 | **不变** | 真实 A 控制面 / 全局锁可扩展性 / starting 无效终态的 choose-path | 与 4C 无交叉 | 各自独立轮次 |

> **ADR Approval Round 更新（2026-10-08）**：**G-017** 与 **G-018** 的修订提案已就绪 —
> 状态改为 **`OPEN / READY FOR APPROVAL`**（A3 与 G-018 endpoint proposal，全文见 §15 的
> `### Round: Phase 4C ADR Approval Round …`）。**未获批准前不得实现**，ADR 文件未被修改。
> 其余行（G-016 CLOSED、G-020、G-021、G-012、G-015、G-001/G-004/G-011）**状态不变**。
>
> **Phase 5 Batch 2 更新（2026-10-08）**：**G-019 = `CLOSED`**（Batch 1 的 Thread 终态半边 + Batch 2 的交付/释放/删除终态半边
> 均已实现并有直接证据）；本表新增 **G-029**（D4 的回复注释无批准字段路径）、**G-030**（Cloud Revision ADR 仍为 `proposed`
> ⇒ Revision 登记/对象校验/`GrantRevisionUpload` 受阻，本轮的**唯一** mandatory blocker）、**G-031**（D6 的幂等身份拼写
> `(issue_run_id, kind)` 与实现的 `(workspace_id, kind)` 偏差）、**G-032**（`releasing` 侧删除失败无放弃上限），
> 四者状态均为 `OPEN / NON-BLOCKING`。**G-020 / G-021 / G-025 / G-026 / G-027 / G-028 状态未变**（G-027 仍为
> `OPEN / DOCUMENTATION CLARIFICATION`，ADR 未被修改；G-028 仍为 `PRE-EXISTING BASELINE` 且是本仓 `task lint` 的唯一剩余项）。
> Phase 5 Batch 2 判定 **`PHASE_5_BATCH_2_BLOCKED`**（0 项交付侧 regression，1 项由 `proposed` ADR 定义的受阻面）。
>
> **Revision Completion Slice 更新（2026-10-08）**：两个 Revision 根决策经人类逐项批准（`status: approved`）后，
> 本表 **G-030 = `CLOSED`**（Revision 登记/对象校验/`GrantRevisionUpload` 已实现并有直接证据）；
> **G-031 = `CLOSED`**（批准的 Option B 已写入两个 approved ADR 与核心用例，实现与批准文本逐字一致，**未**改 schema）；
> **G-032** 的 IssueRun D8 已批准并同步进 ADR 与核心用例，**实现仍待做**（**未**批准任何修复方式）；`G-033` 的 D5 澄清
> 已批准并落地；**新登记 G-035 / G-036 / G-037**（公开读未投影 D5 的 Revision 元数据、健康检查不报告对象存储、
> 真实 MinIO 上的三项义务仍 `Missing`）。**G-020 / G-021 / G-025 / G-026 / G-027 / G-028 状态未变**
> （G-025 的文档债因 `0023` 落地扩为 `0018`–`0023`；G-028 仍是 `task lint` 的唯一剩余项）。
> 判定 **`REVISION_COMPLETION_SLICE_DONE` + `PROJECT_FINAL_ACCEPTANCE_PASSED`**；`PHASE_5_BATCH_2_BLOCKED` 被取代。
>
> **Final Completion Batch 更新（2026-10-08，收口轮）**：人类批准 **A2 / A3 / A4 / G-018** 并已落地 —— 本表
> **G-017 = `CLOSED BY A3`**、**G-018 = `CLOSED`**（route/verb/状态码/幂等按本表推荐形状实现并进入 OpenAPI）；
> G-023（A4）与 G-024（A2）在同批关闭（见 §13 的 G-022..G-024 段）。**G-019 仍归 Phase 5**、**G-020 / G-021 仍 OPEN**
> （本批新增的 `thread_changed` 与 `thread_appended` 走同一条进程内 hub，多实例与分区问题一字未变）；
> G-012 / G-015 / G-001 / G-004 / G-011 **状态不变**。Phase 4C 判定 **`PHASE_4C_COMPLETE`**（0 项 BLOCKER）。

### 4C.19 §17 架构自评清单（26 项）

1. 未修改/新增 production 代码 —— 是（本轮只改 `plan/plan.md`、`plan/plan-zh.md` 与 specs 文档）。
2. 未新增/修改 migration —— 是（D-4C-12 只是**设计**，未创建文件）。
3. 未新增/修改测试代码 —— 是（T4C-* 全为设计，标 `DESIGNED / MISSING`）。
4. 未修改 API/proto/generated 代码 —— 是。
5. 未修改 schema —— 是。
6. G-016 已解决且给出理由（含被否决方案）—— 是（D-4C-01，§4C.2）。
7. `pending` 未被伪造为已实现/已覆盖 —— 是（CLOSED 的是**决策**，不是实现；证据义务列为 T4C-1..T4C-5）。
8. 未引入第二个 `starting→running` 权威 —— 是（§4C.1 冻结，§4C.13 只做 `thread_state` 补充）。
9. `seq=1` 不可变未被动摇 —— 是（§4C.1、D-4C-02 的 history immutability）。
10. Node `sequence` 与 Thread `seq` 未混淆 —— 是（D-4C-06 身份表）。
11. B 未拥有 A 的执行身份 —— 是（`command_id` 由 A 生成，D-4C-05/06）。
12. ownership 明确（`thread_state`/`thread_entries`/`thread_commands`/SSE 事件）—— 是（D-4C-01/03/05/07）。
13. caller-owned transaction 明确（缝不开事务、在调用方事务内）—— 是（D-4C-05）。
14. POST 幂等矩阵完整（7 例）—— 是（D-4C-04）。
15. SSE 的 durable replay 有明确来源（`thread_entries` + GET）—— 是（D-4C-07）。
16. 不会永久丢失事件（数据不丢；通知可丢且有兜底）—— 是（D-4C-08）。
17. `after` 语义在 GET 与 SSE 重连统一 —— 是（D-4C-07，游标只由 GET 推进）。
18. `idle`/`ending`/`ended` 的权威明确 —— 是（D-4C-09/10；`ended` = `SessionEnded` = Phase 5）。
19. 取消/终态与 4B 的 cancel-first 语义一致 —— 是（D-4C-11 表格第 2 行）。
20. schema 决策明确（需要 migration，逐项列出）—— 是（D-4C-12）。
21. 测试设计矩阵完整（含真实 PostgreSQL 并发）—— 是（T4C-1..T4C-34，§4C.16）。
22. 未过早标记 `Covered` —— 是（§4C.17）。
23. 两个 plan 文件都更新 —— 是（`plan.md` 本节 + §7/§12/§13/§15/§16；`plan-zh.md` §12/§13/§15 镜像）。
24. 未覆盖既有未提交修改、未删除/还原工作区改动 —— 是（未执行任何 git 破坏性命令）。
25. 未执行 `git add`/`commit`/`push`/PR/`reset`/`restore`/`checkout .`/`clean`/`stash` —— 是。
26. 未把 `proposed` ADR 当作已批准 —— 是（controller-session ADR 仍 `proposed`，本设计**不依赖**它：
    D-023 的串行化证明只用了已批准事实；4C 的串行化同样只用全局锁 + PK，见 §4C.14）。

### 4C.20 最终裁定

**`PHASE_4C_DESIGN_DONE / PHASE_4C_ARCHITECTURALLY_REVIEWABLE / READY_FOR_PHASE_4C_IMPLEMENTATION`**

- 附 **NON-BLOCKING OPEN**：G-017（D5 的 tail/`before`/`idleSince` 扩展与 D4 的 `pending` 时点澄清 —— 需 ADR 修订
  或架构师确认，**对应代码在修订/确认前不应合并**）、G-018（用户结束端点形状）、G-019（Phase 5 的
  `SessionEnded`/`discarded`）、G-020（SSE 通知不保证送达 ⇒ 客户端轮询）、G-021（多 worker 命令分区）。
- **无 BLOCKER**：G-016 已 CLOSED；4C 不依赖 `proposed` 的 controller-session ADR；4B 的冻结基线未被改动。
- 本轮**未**进入实现：没有 production 代码、migration、测试或 API 变更，也没有任何 git 写操作。

---

## Phase 4C Readiness — Architecture Review & Implementation Slice Plan（REVIEW / DESIGN ONLY，2026-10-08）

> 本轮是 **Architecture Review + Implementation Readiness**：只核验、只登记、只切分。**未改** production 代码、
> migration、proto、generated 或测试实现；**未改**任何 ADR 文件或其 `status`。§4C.0–§4C.20 的设计记录保持原样。

### 4R.0 结论摘要

**`READY_FOR_PHASE_4C_IMPLEMENTATION_SLICE_1`** —— Slice 1（migration 0022 + `pending` 物化）**不依赖**任何未决项，
可直接开工。其余切片按 §4R.5 的依赖序推进；**S2b 需要先关闭 G-017**，**S7 需要先关闭 G-018**，**S6 的「仅状态变化也发布」
需要 G-023**，**S1 的 `thread_entries.status` 需要 G-022**（S1 可先按 §4R.4 的迁移安全要求落地，但 ADR 修订须同批）。

### 4R.1 核验（只读）

**事实来源**：`plan/plan.md`、`plan/plan-zh.md`；`specs/decisions/cloud/thread/0-durable-agent-thread-with-user-turns.md`
（`status: approved`）；`specs/decisions/cloud/controller-integration/20260928-agent-run-executions-thread-and-upload-grants.md`
（`status: approved`）；4B production（`internal/core/agent_run_thread.go`、`agent_run_thread_takeover.go`、
`agent_run_session_start.go`、`internal/controlgrpc/agentruns.go`）；migration 0018–0021；
`proto/ora/cloud/internal/v1/agent_runs.proto`、`signals.proto`。

**只读 git**：`cloud` 有 4B 轮的未提交修改（6 modified：`cmd/server/main.go`、`integration/migration_upgrade_path_test.go`、
`internal/controlgrpc/server.go`、`internal/core/agent_run_settle.go`、`internal/core/control.go`、
`internal/core/node_executions_db_test.go`；6 untracked：`integration/agent_run_thread_takeover_test.go`、
`internal/controlgrpc/agentruns.go`、`internal/core/agent_run_thread.go`、`agent_run_thread_takeover.go`、
`agent_run_thread_takeover_db_test.go`、`migrations/0021_node_event_receipts.sql`）。`specs` 有 3 modified。
`cluster` 根与 `desktop` 干净。**本轮未触碰上述任何文件的历史，未执行任何 git 写操作。**

**本轮新确认的事实**（首次核验，直接决定切片计划）：

1. **proto 已完成**：`agent_runs.proto` 已声明 `ClaimThreadCommands` / `RecordThreadCommandDelivered`，
   以及 `ThreadCommand{command_id, run_id, execution_id, target, oneof SubmitUserTurn|EndSession}`、
   `SubmitUserTurn{turn}`、`EndSession{reason}`；`signals.proto` 已有 `ThreadCommandAvailable{run_id}`。
   ⇒ **Phase 4C 不需要任何 proto 改动**（与 controller-integration D3「proto 已合并」一致）。
2. **`thread_entries`（0018）没有 `status` 列**；`issue_runs.thread_state` / `idle_since` 已存在（值集
   `pending|active|idle|ending|ended`）；`thread_commands` 表**不存在**。
3. `SpaceEvent` 只有 `{type, spaceId, projectId?, version?}`（`internal/core/hub.go`）；`PublicRequest` 有
   `After`/`Limit` 但**没有 `Before`**（`internal/core/public.go`）；router 无任何 thread 路由
   （`internal/api/router/router.go`，runs 路由在 :70–:72）。
4. `Store.transact` 的 `pg_advisory_xact_lock(67420911)` 覆盖全部读写（`internal/core/store.go:267`）。
5. `runWorkspaceLive(t, wid, runID)` 存在（`internal/core/agent_run_session_start.go:69`），语义为
   「workspace 属于该 run 且 `deleted_at IS NULL`」——D-4C-03 的接受谓词可直接复用它。

### 4R.2 G-017 决议：已批准 / 扩展 的逐条切分

对照 Thread **D5/D4 的字面文本**（不读设计意图）：

| 设计条款 | ADR 字面依据 | 判定 |
|---|---|---|
| `GET .../runs/{rid}/thread` 路由 | D5 点名 | **已批准** |
| `after={seq}` | D5 点名 | **已批准** |
| `limit`，上限 500 | D5 点名（上限 500） | **已批准** |
| 响应含条目 + `threadState` | D5 点名 | **已批准** |
| 按 `seq` 升序 | D5 点名 | **已批准** |
| 事件 `issue_run.thread_appended{issueId, runId, lastSeq}` | D5 **逐字点名这三个字段** | **已批准**（设计轮的「给 `SpaceEvent` 加三个可选字段」是实现缺口，不是新决策） |
| 事件只是失效提示、权威经 REST 重取 | D5 + api-boundary | **已批准** |
| 输入框在 `pending|active|idle` 可用 | D5 点名 | **已批准** |
| `pending` 由 `StartSession` 物化 | D4 表格行 1 | **语义已批准；写入时点是澄清**（见下） |
| 无游标 = **tail 读** | D5 未定义无游标行为 | **扩展 ⇒ 需 ADR 修订** |
| `before=N` | D5 只定义 `after` | **扩展 ⇒ 需 ADR 修订** |
| 响应字段 `idleSince` | D5 只说「条目与 `threadState`」 | **扩展 ⇒ 需 ADR 修订** |
| `nextCursor`/`prevCursor` | D5 未定义响应游标字段 | **扩展 ⇒ 需 ADR 修订** |
| 仅 `status`/`thread_state` 变化也发 `thread_appended` | D5 说「**条目写入**事务提交后…发布」 | **扩展 ⇒ 需 ADR 修订（G-023）** |
| POST 在 `cancel_requested_at` 已置时拒绝 | D3 说「`pending|active|idle` 时接受」 | **扩展 ⇒ 需 ADR 修订（G-024）** |
| `thread_entries.status` 列 | D1 **逐项列举 `thread_entries` 的列，无 `status`** | **扩展 ⇒ 需 ADR 修订（G-022）** |

**`pending` 的写入时点（澄清，非扩展）**：D4 行 1 的「会话执行已登记」有两种读法——(a) Cloud 在 `StartSession`
声明 `agent_session` 工作项（Phase 3A）；(b) Controller 经 `RecordDispatch` 登记 `execution_id`（Phase 4A）。
设计轮选 (a)。**可观察差异仅在于 `pending` 何时可见**：(a) 在 `phase='starting'` 一开始即可见；(b) 会留下一段
`NULL` 窗口。两者都不改变任何转换集合，也不产生第二个写者。**判定：澄清**，但**必须**随 G-017 的 ADR 修订一并
写明，否则 D4 的措辞与实现长期不一致。

**结论**：G-017 不是一个事项，而是 **4 项响应形状扩展 + 1 项措辞澄清 + 2 项本轮新发现（G-023/G-024）**。
在修订落地前，**只有「已批准子集」可直接实施** ⇒ 切片 S2 拆成 S2a / S2b。

### 4R.3 G-018 决议：用户主动结束 Thread 的 API 契约

Thread D4 **已批准**该转换与命令（「用户点击『结束』… ⇒ `ending`，放出 `EndSession{reason}`」，
`reason ∈ user_ended|idle_timeout|cancelled`）；controller-integration D3 已批准 `EndSession{command_id, run_id, reason}`
的线上形状。**缺的只是公开端点形状**。以下契约作为**待批准的 ADR 补充**（本轮不改 ADR 文件）：

| 项 | 契约 |
|---|---|
| Endpoint / method | `POST /api/v1/tenants/{tid}/issues/{iid}/runs/{rid}/thread/end` |
| 请求体 | **无**（空 body）。**不**接受 `reason`（服务端固定 `user_ended`）；未知字段沿用严格解码 ⇒ `400` |
| 幂等 | **必须** `Idempotency-Key`，与 POST messages 同一机制、同一 `idempotency_records` 表；同 key 重放 ⇒ 回放原响应（即使 thread 已 `ending`） |
| 权限 | 与该 Issue 的读/评论权限一致（活动 Space 成员）；跨 tenant / 跨 Issue / 软删 run ⇒ `404 not_found` |
| 接受条件 | `thread_state ∈ {pending, active, idle}` ∧ `cancel_requested_at IS NULL` ∧ workspace live |
| 成功 | **`202 Accepted`** + `{threadState: "ending"}`（不返回 201：没有创建用户可见资源） |
| `EndSession` 命令 | **同一事务**内 `enqueueThreadCommand(t, run, EndSession{reason: 'user_ended'})`，**恰好一条** |
| 与 cancel 的竞态 | 结束 ≠ 取消。结束只放 `EndSession{user_ended}`，run 照常交付；取消写 `cancel_requested_at`（D-4C-11）。全局锁 ⇒ 全序：取消先 ⇒ 结束得 `409 thread_closed`；结束先 ⇒ 取消时已是 `ending` ⇒ **不**再放第二条 `EndSession`，只写 `cancel_requested_at` |
| 与 idle 超窗的竞态 | 两条路径都 CAS `thread_state IN ('pending','active','idle') → 'ending'`，**影响行数必须 = 1**；0 行 = 别方先赢 ⇒ 本路径**不发** `EndSession`：对用户返回 `409 thread_closed`，对扫描器静默跳过 |
| 错误 | 缺 key `400 idempotency_key_required`；形状非法 `400 invalid_json`；`ending|ended` / 已取消 / workspace 不 live ⇒ `409 thread_closed`；不存在 ⇒ `404 not_found`；缝未接线 ⇒ `503 thread_command_unavailable`；不变量破坏（影响行数 ≠ 1 而事前判定可写）⇒ `500` |
| `ending → ended` | **仍归 Phase 5**（`SessionEnded`）；本轮与整个 4C 都**不实现** |
| 测试义务 | T4C-32（三分支）、T4C-33（场景 5/6/7/8）+ **新增 T4C-35**（202 / 409 / 幂等回放 / 与 cancel 及 idle 的两种提交顺序） |

**`user_ended` 触发在 S7 之前不可达**；S1–S6 不依赖它。

### 4R.4 D-4C-01..D-4C-12 复核

| 决策 | 复核结论 |
|---|---|
| **D-4C-01** `pending` 物化 | **一致**；写入时点属澄清（§4R.2）。**新增实现要求**：migration 0022 在加 `thread_entries.status` 的 `CHECK ((source='user') = (status IS NOT NULL))` 之前，必须显式处理既有 `source='user'` 行——当前**不可能存在**（4B 无用户条目写者，seq=1 是 `source='system'`），但迁移**不得**依赖「碰巧没有数据」：要么先 `UPDATE ... SET status='queued' WHERE source='user'` 再校验，要么把该前提写成迁移注释并附一条断言查询 |
| **D-4C-02** GET | **部分已批准**；扩展部分见 G-017 |
| **D-4C-03** POST | **一致**，但「`cancel_requested_at` 已置 ⇒ 拒绝」与 D3 字面「`pending|active|idle` 时接受」冲突 ⇒ **G-024**。该冲突是**真实可达**的：取消发生在 `provisioning|starting` 时 run 直进 `releasing` 而**不**写 `ending`（IssueRun D4/D6、D-4C-11），此时 `thread_state` 可能仍是 `pending` 而 `cancel_requested_at` 已置 |
| **D-4C-04** 幂等 | **一致**（根 D3「重传返回原结果」）；不新增列 |
| **D-4C-05** 缝 | **逐字符合** controller-integration D6 的 `enqueueThreadCommand(t, run, command) → command_id` 与「函数都在调用方的事务内执行」 |
| **D-4C-06** 身份 | **一致**（proto 已把 `command_id` 与 `turn_id` 分开；`execution_id` 由 Controller 生成） |
| **D-4C-07** SSE 来源 | **一致**（D5 逐字给出事件三字段）；「只有 GET 推进游标」与 D5「事件只是失效提示」一致 |
| **D-4C-08** 序/重/丢 | 「仅状态变化也发布」与 D5 字面冲突 ⇒ **G-023**；其余（不承诺顺序、重复无害、数据不丢通知可丢、客户端轮询兜底）与 D5/api-boundary 一致 |
| **D-4C-09** active/idle | **一致**；「批次末条记录决定」是对 D4「接管到 `TurnEnded`」在批量下的**实现级澄清**，不改变转换集合 |
| **D-4C-10** ending/ended | **一致**；端点形状缺 ⇒ G-018；`ended` = `SessionEnded` = **Phase 5 保持** |
| **D-4C-11** cancel/terminal | **一致**（IssueRun D4/D6）；`starting` 取消直进 `releasing` 且无 `ending` 是 IssueRun 已批准规则，**不违反** 4B 的 cancel-first |
| **D-4C-12** 迁移 | `thread_commands` **逐列符合** D6（`id uuid`/`run_id`/`kind`/`body jsonb`/`created_at`/`delivered_at`/`delivered_execution_id`）；`thread_entries.status` 是 D1 列清单之外的**扩展** ⇒ **G-022**；`pending` 回填与 `idle_since` 索引为实现级 |

**未发现**：第二个 `starting→running` 权威、A/B ownership 越界、caller-owned transaction 违背、advisory lock 变更、
`seq` 连续性破坏、4B 冻结基线漂移。**已发现 3 项须先登记再改设计的事项（G-022/G-023/G-024）**，按 mandate §4
登记为 Gap，**不静默修改设计**。

### 4R.5 Implementation Slice Plan（7 切片，各自可独立验收）

依赖：`S1 → {S2a, S3}`；`S3 → S4`；`S1 → S5`；`S5 → S6`；`S3 → S7`。S2b 需 G-017，S7 需 G-018。

**S1 — Migration 0022 + `pending` 物化**

- **Scope**：迁移 0022（`thread_commands` 表 + FK/CHECK/部分索引；`thread_entries.status` 列 + CHECK + 部分索引；
  `pending` 幂等回填；`issue_runs (idle_since) WHERE thread_state='idle'` 部分索引）；`StartSession` 在同一事务内物化 `pending`。
- **Files**：`internal/core/migrations/0022_thread_api_and_commands.sql`（新）、`internal/core/agent_run_session_start.go`（改）、
  `integration/migration_upgrade_path_test.go`（加 0022 fresh+upgrade）、`internal/core/agent_run_session_start_db_test.go`（T4C-1..4）。
- **Migration 影响**：**是**，0022。必须「全新库 + 从 0021 升级」双绿、可重复执行、回填幂等；`status` 的 CHECK 按 §4R.4 的
  安全要求处理既有行。
- **Transaction contract**：`StartSession` 既有事务；`UPDATE issue_runs SET thread_state='pending' WHERE id=$1 AND thread_state IS NULL`，
  **断言影响行数 = 1**；seq=1 影响 0 行 ⇒ 早退（**不**写 `thread_state`）；A 缝错误 ⇒ 整体回滚。
- **Ownership contract**：B 写业务表 `issue_runs`；`thread_commands` 在本切片只创建、不写入（A 拥有）。
- **T4C**：T4C-1、2、3、4、5。
- **Regression**：全部 T4B-*；`TestThreadTakeoverActivatesPendingThread`（以字面 `'pending'` 预置）；`TestMigration0021*`；
  `stripAgentRunSkeleton` 的 run 资源形状不变。
- **Exit criteria**：双迁移路径绿；`StartSession` 提交后 `thread_state='pending'`；running 权威与 `seq` 分配**零变化**。

**S2a — Thread GET（仅已批准子集）**

- **Scope**：`GET .../runs/{rid}/thread?after={seq}&limit={n}`，返回 `{items, threadState}`，按 `seq` 升序，`limit` 上限 500。
- **Files**：`internal/api/router/router.go`、`internal/core/public.go`（dispatch + `readPublic` 分支）、
  `internal/core/thread_read.go`（新）、`internal/contract/*`、`api/openapi.json` + `frontend/src/api`（生成物，不得手改）。
- **Migration 影响**：无。**Transaction contract**：单次 `Store.transact` 只读，无写。
- **Ownership contract**：只读 B 表（`thread_entries` + `issue_runs`）。
- **T4C**：T4C-7、T4C-9（`after`/`limit` 部分）、T4C-10、T4C-11、T4C-12。
- **Regression**：run 资源字段集不变；既有 `page`/`window` 语义不变（**不得**复用或放宽）。
- **Exit criteria**：`after`/`limit` 契约与错误码正确；授权与评论一致；run 资源逐字节不变。
- **依赖**：无（G-017 未决不影响该子集）。

**S2b — GET 扩展（tail / `before` / `idleSince` / 游标）**

- **Scope**：无游标 = tail 读；`before={seq}`；响应 `idleSince`；`nextCursor`/`prevCursor`；`PublicRequest.Before` 与
  `before` 查询参数解析；`400 invalid_pagination`（`after`+`before` 并存、`limit` 越界）/`400 invalid_cursor`。
- **前置**：**G-017 的 ADR 修订或架构师确认已落地**；未落地**不实现**。
- **Files**：`internal/core/public.go`、`internal/core/thread_read.go`、`internal/api/router/router.go`、OpenAPI + 生成物。
- **T4C**：T4C-6、T4C-8、T4C-9（游标部分）。
- **Exit criteria**：双向翻页可用；游标不透明；扩展已记入 ADR。

**S3 — `thread_commands` 控制面 + A 缝（无公开 API）**

- **Scope**：`EnqueueThreadCommand` 真实实现（写 `thread_commands`、返回 `command_id`）；`ClaimThreadCommands`
  （纯读、按 run 分组、组内创建序、要求 lease epoch、返回会话 `execution_id` 与目标 Node）；`RecordThreadCommandDelivered`
  （CAS `delivered_at IS NULL` + 影响行数校验 + 重复登记幂等）；`Watch` 的 `ThreadCommandAvailable{run_id}`；
  gRPC 三个方法的映射与接线。
- **Files**：`internal/core/agent_run_control_plane*.go`（新/改）、`internal/controlgrpc/agentruns.go`（改）、
  `internal/controlgrpc/server.go`、`cmd/server/main.go`。
- **Migration 影响**：无（表在 S1）。**Proto**：**无需改动**（§4R.1 事实 1）。
- **Transaction contract**：缝在**调用方事务**内执行、不自开事务、不做外部 IO；claim 纯读；登记为单行一次写 + 影响行数校验。
- **Ownership contract**：A 拥有 `thread_commands`；B 只能经缝写入。
- **T4C**：T4C-21、22、23、24。
- **Regression**：`execution_work`/`node_executions`/收据路径不变；4B 的 `agent_work_*` 不变。
- **Exit criteria**：「至少投递一次 + 登记幂等」成立；会话执行未登记时命令不返回；`ThreadCommandAvailable` 只在提交后发出。

**S4 — Thread POST**

- **Scope**：`POST .../thread/messages`（body 白名单与严格解码、64 KiB、接受矩阵、条目 + `thread_state` CAS + A 缝同事务、幂等）。
- **前置**：S3。
- **Files**：`internal/api/router/router.go`、`internal/core/public.go`、`internal/core/thread_post.go`（新）、
  `internal/contract/*`（`content_too_large`、`thread_closed`、`thread_command_unavailable`）、OpenAPI + 前端生成物。
- **Migration 影响**：无。**Transaction contract**：单一 `Store.transact`；条目与命令同生共死；幂等记录同事务。
- **Ownership contract**：B 写 `thread_entries` 与 `issue_runs.thread_state`；A 经缝写 `thread_commands`。
- **T4C**：T4C-13..18（G-024 落地后同步接受矩阵行）。
- **Regression**：run 资源不变；通用幂等前置/后置行为不变。
- **Exit criteria**：201/404/409/503 分类正确；缝失败整体回滚且同 key 重试是干净首发；`turn_id` 由 Cloud 生成并可见。

**S5 — 接管 echo→`delivered` + `active`/`idle` 生命周期**

- **Scope**：echo 谓词从「仅首 prompt」推广到**任意** Cloud 生成的用户轮次（不新增条目、不分配 seq、把该行置 `delivered`）；
  `idle` 判定（批次**末条**被接管记录为 `turnEnded` 且无 `status='queued'` 用户轮次 ⇒ `idle` + `idle_since=now()`）；
  `idle → active`（新记录到达，清 `idle_since`）。
- **Files**：`internal/core/agent_run_thread.go`。
- **Migration 影响**：无。**Transaction contract**：既有接管事务；CAS 影响行数硬化（事前判定可写而 0 行 ⇒ 回滚）。
- **Ownership contract**：B 写 `thread_entries`（仅 `status` 单向）与 `issue_runs.thread_state`。
- **T4C**：T4C-19、20、28、29、30。
- **Regression**：全部 T4B-*（尤其 echo-only 批次**不**进 running；stale/cancel/无效 workspace 仍写条目但跳过 CAS）。
- **Exit criteria**：4B 冻结基线**零变化**；`delivered` 不回退；`idle_since` 用数据库时间；门控不满足时仍追加条目。

**S6 — SSE 失效提示**

- **Scope**：`SpaceEvent` 新增可选 `issueId`/`runId`/`lastSeq`（`omitempty`，既有事件 JSON 逐字节不变）；
  提交后发布 `issue_run.thread_appended`。
- **前置**：S5（发布点存在）；「仅状态变化也发布」需 **G-023** 落地，未落地时**只对「有新条目」的提交发布**。
- **Files**：`internal/core/hub.go`、发布点（`internal/core/public.go`、`internal/core/agent_run_thread.go`）、
  `internal/api`（SSE 序列化）。
- **Migration 影响**：无。**Transaction contract**：发布**只在提交后**；回滚零事件。
- **Ownership contract**：B 的提交后副作用；不写任何表。
- **T4C**：T4C-25、T4C-26（受 G-023 约束）、T4C-27。
- **Regression**：`space-api-and-events.md` 的 `Covered` 用例不回归；`project.created` 等既有事件负载不变。
- **Exit criteria**：事件形状与提交序正确；被拒绝的写入零事件。

**S7 — `ending`：用户结束端点 + idle 超窗扫描**

- **Scope**：`POST .../thread/end`（§4R.3 契约）；idle 超窗派发循环 + 配置项 `issue_runs.thread_idle_timeout`（默认 15 分钟，
  按**数据库时间**判定）；三条 `ending` 路径各自单事务且**恰好一条** `EndSession`。
- **前置**：S3（缝）、S5（`idle`/`idle_since` 存在）、**G-018 批准**。
- **Files**：`internal/api/router/router.go`、`internal/core/thread_end.go`（新）、`internal/core/agent_run_dispatch.go`
  或同级（扫描循环）、`cmd/server/main.go`。
- **Migration 影响**：无（`idle_since` 索引在 S1）。
- **Transaction contract**：每条路径单事务；CAS `thread_state IN ('pending','active','idle') → 'ending'` + 影响行数 = 1 +
  同事务 `enqueueThreadCommand(EndSession{reason})`。
- **Ownership contract**：B 写 `thread_state`；A 经缝写命令。
- **T4C**：T4C-31、32、33、34 + **T4C-35**（新增，结束端点）。
- **Regression**：POST 的接受矩阵（`ending` 后一律 409）；4B cancel-first 语义。
- **Exit criteria**：三触发各自恰好一条 `EndSession`；两 tick 不重复；`ending` 后 POST 一律 `409 thread_closed`；
  **`ending → ended` 仍未实现**（Phase 5）。

### 4R.6 依赖与风险

| # | 依赖 / 风险 | 影响切片 | 处置 |
|---|---|---|---|
| 1 | **G-017**（GET 扩展需 ADR 修订） | S2b | 先取修订或架构师确认；S2a 不受阻 |
| 2 | **G-018**（用户结束端点形状） | S7 | 本轮的 §4R.3 契约即提案；批准后落地 |
| 3 | **G-022**（`thread_entries.status` 不在 D1 列清单） | S1 | 迁移可先落地，但 ADR 修订须同批，否则为静默偏离 |
| 4 | **G-023**（无新条目也发事件，与 D5 字面冲突） | S6 | 未落地前只对「有新条目」的提交发布 |
| 5 | **G-024**（POST 在 `cancel_requested_at` 已置时拒绝，与 D3 字面冲突） | S4 | 修订 D3 的接受谓词；矩阵行同步 |
| 6 | **G-019**（`ended`/`discarded` = Phase 5） | S5、S7 | 4C 只到 `ending`；**不得**自造第二条终态路径 |
| 7 | **G-020**（SSE 通知不保证送达） | S6 | 客户端重连 + ≥30s 轮询（前端义务） |
| 8 | **G-021**（多 worker 命令分区） | S3 | 只保证单 worker 下「至少一次 + 登记幂等」 |
| 9 | 迁移 CHECK 与既有行 | S1 | 按 §4R.4 的安全要求处理；不得依赖「碰巧没有数据」 |
| 10 | S2 分两次改响应形状 | S2a/S2b | 若 G-017 快速确认，可直接做完整 S2 |
| 11 | idle 扫描与全局锁 | S7 | 逐 run **短事务**，扫描节奏远小于窗口；不在扫描里做外部 IO |
| 12 | 4B 未提交修改与 4C 改动同文件 | S1、S5 | `agent_run_thread.go`/`agent_run_session_start.go` 等文件已有未提交内容；实现轮必须在**当前工作区**上增量修改，不得覆盖 |
| 13 | `thread_commands` 无重复投递历史 | S3 | ADR 已认可「以第一次登记为准」（Node 去重使重复投递无副作用） |
| 14 | **controller-session ADR 仍 `proposed`** | 全部 | **不是**依赖：4C 的串行化只用全局锁 + PK（§4C.14） |

### 4R.7 待批准的 ADR 修订（本轮不修改任何 ADR 文件）

| # | 目标 | 修订内容 |
|---|---|---|
| A1 | Thread **D1** 列清单 | 增加 `thread_entries.status`（`queued|delivered|discarded`，仅 `source='user'` 非空）——对应 G-022 |
| A2 | Thread **D3** 接受谓词 | 把「`pending|active|idle` 时接受」改为「且 `cancel_requested_at IS NULL`、且运行 Workspace 可用」——对应 G-024 |
| A3 | Thread **D4** 措辞 | 写明 `pending` 的物化时点 = Cloud 声明会话执行（`StartSession`）时——对应 G-017 的澄清项 |
| A4 | Thread **D5** 读取与事件 | 补：无游标 = tail 读、`before`、响应 `idleSince`、`nextCursor`/`prevCursor`；并把事件发布条件从「条目写入事务提交后」改为「任何改变该运行 Thread 可见状态的提交之后（含条目 `status` 与 `thread_state`）」——对应 G-017 的扩展项与 G-023 |

**批准方式**：由架构师在 `specs` 仓库提交 Thread ADR 的后续决策文件（`YYYYMMDD-*.md`，按 specs 的连续 ADR 规则），
或在评审中显式确认；**本轮不创建该文件、不改 `status`**。

### 4R.8 Git（本轮）

- `git add` / `commit` / `push` / PR：**未执行**。
- `reset` / `restore` / `checkout .` / `clean` / `stash`：**未执行**。
- 既有未提交修改（cloud 6 modified + 6 untracked；specs 3 modified）：**原样保留，未被覆盖**。
- 本轮唯一写入：`plan/plan.md`、`plan/plan-zh.md`（设计/计划文档，plan §14 允许）。

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

### D-023 — Thread `seq` allocation is `MAX(seq)+1` inside the takeover transaction（Phase 4B decision, **ACCEPTED**）

Status: **Accepted** (architecture resolution round, 2026-09-30); implementation deferred to Phase 4B.

Thread D1 fixes「Cloud 在写入事务中分配、seq 连续」but does not prescribe the mechanism. **Accepted**: in the
`ThreadEventsTakenOver` takeover transaction, allocate `seq` from 2 upward as `MAX(seq)+1` per inserted entry
(seq=1 is the reserved first prompt; G-009). **Serialization proof — no dependency on the `proposed`
controller-session ADR** (superseding the earlier rationale): (1) a run has at most one session execution
(controller-integration D1「一个运行同时至多一个会话执行」, D-021, `execution_work_unregistered_once`), so a run
has one takeover stream; (2) every takeover runs inside `Store.transact` under the global advisory lock
(`pg_advisory_xact_lock`, Phase 1 baseline), so no two takeovers can compute `MAX(seq)` concurrently; (3)
`PRIMARY KEY (run_id, seq)` is the final storage-level barrier rejecting any duplicate. Chosen over a counter
column / separate sequence state because it adds no table and reuses the established serialization (user-turn
entries written via the same `transact` share the allocator, so interleaving stays gapless); a
deterministic-from-Node-order allocation is rejected because seq=1 is a Cloud system turn and it cannot stay
gapless under replay. Node `sequence` (per-execution, `node_event_receipts`) and Thread `seq` (run-scoped) are
**different sequence spaces** and must not be conflated (see §4B.5). See §4B.5/§4B.11.

### D-024 — Phase 4B session-start authority, `thread_state` mapping, echo dedup, empty batch（Phase 4B decision）

Status: Accepted (architecture resolution round, 2026-09-30); implementation deferred to Phase 4B.

- **running authority** = the first **real Node Thread record** taken over by `TakeOverThreadEvents`, with the
  `ThreadEventsTakenOver` hook committing in the same transaction (IssueRun D3「首条 Thread 事件…被接管」;
  controller-integration D6). **No synthetic `session_started` control event exists**: the Node emits only
  `ThreadEvent{record}` (Node protocol D2), whose `record` is an `ora-history` line (Thread D2). Mandate §4 option B.
- **`thread_state`**: `pending` = "session execution registered, no record taken over yet" (Thread D4); the first
  Node record takeover ⇒ `active`. 4B writes only `pending→active`; `idle/ending/ended`/`idle_since` belong to 4C
  (user-turn queue) and the session-terminal hook.
- **echoed first prompt dedup** (locked fact C): an event with `turn_id == initial_turn.turn_id` writes a receipt
  but allocates **no** thread `seq` and inserts **no** entry (seq=1 already presents it); seq=1 content stays
  immutable. 4B adds no `delivered` column (that is 4C's `SubmitUserTurn` concern).
- **empty batch** (`events == []`) is rejected `ABORTED`: no receipt, no `last_event_sequence` advance, no hook, no
  `phase/thread_state` change.

### D-025 — Phase 4B Thread event ceiling and `node_event_receipts` retention initial values（Phase 4B decision, G-015）

Status: Accepted as initial configurable defaults (Phase 4B); G-015 stays PARTIAL until enforcement + evidence land.
**Not implemented in this round**: the values have no approved ADR/spec basis, so enforcing them would be
unapproved behavior; the migration ships the `created_at` index the future sweep needs and nothing else.

Per-run (per-execution) non-terminal event ceiling default **200,000** (config `thread_event_cap`); the check point
is `node_executions.last_event_sequence` (contiguous ⇒ count = max sequence); a non-terminal takeover at the cap is
rejected `ABORTED` (no silent drop). `node_event_receipts` retention default **30 days after the run reaches `done`**
(config `node_event_receipts_retention`), pruned by the dispatch loop; receipts are never pruned while the run is
live (Node replay needs them). Both are versioned defaults, not hard contracts. See §4B.10.

### D-026 — Phase 4B implementation details: record `kind` source, batch bound, fail-closed surfaces, `pending` representation（Phase 4B implementation round, 2026-09-30）

Status: Accepted (implementation round). These are the concrete choices the implementation had to make where the
approved ADRs fix the obligation but not the mechanism.

- **Thread entry `kind`** is the record's own `type` tag, validated against the closed set
  `{meta, update, turnEnded, agentSwitched, handoffDelivered, gap}`. Authority: Thread D2 ("`kind` 取其类型标签";
  Cloud does not parse business fields) and the record format's owner, desktop `crates/history/src/record.rs`,
  whose `HistoryRecord` is `#[serde(tag = "type", rename_all = "camelCase")]` — those six tags are therefore the
  "known set" D2 requires. Cloud must not derive a finer kind: that would mean reading `update`'s payload, which
  D2 forbids. An unrecognized tag fails closed (rolls the batch back; the Node replays).
- **Batch bound** is the approved wire contract's `1..64` (controller-integration D2). This is explicitly **not**
  the deferred per-run `thread_event_cap` of D-025.
- **Fail-closed surfaces** (§13/§17/§20): an unknown run, an execution that is not this run's `agent_session` work,
  a missing seq=1, an unregistered execution and an unknown record `kind` are **invariant errors** — the hook
  returns an error, A panics `databaseFailure`, the transaction rolls back and the Controller receives
  `UNAVAILABLE` ("retry later", so the Node replays). Client-class faults (empty batch, sequence gap, receipt
  payload conflict, wrong execution, unknown execution) are raised by A as `400/409/404` before any write. The two
  classes differ deliberately: a `CONFLICT` tells the Controller to drop the batch, so it must never be used for a
  condition the Node must retry.
- **Stale/cancel/workspace-invalid runs still take the records over**: the Thread entries are appended and the
  receipts commit (the batch was delivered and will be acked), but the `starting→running` CAS is skipped, so the
  run stays `starting` (`thread_state` unchanged). Rationale: Thread D1 records every taken-over Node record, and
  silently dropping a receipted event would lose user-visible conversation the Controller already acknowledged.
  The consequence — a G-011 invalid-workspace run can hold Node entries while `thread_state` is still `pending` —
  is recorded in G-016 and is G-011's, not a new lifecycle rule.
- **`pending` has no writer** (see G-016): 4B writes only `thread_state='active'`; the pre-state is whatever the
  run held, which today is `NULL`. §29's "write only the `pending→active` transition" is honored literally.
- **Supersession**: §4B.3's table row "接管到任意 Node 记录（含 echo） ⇒ `active`" is superseded by mandate §31 —
  an **echo-only** batch leaves `thread_state` untouched (`pending`), because the echo of the Cloud-authored first
  prompt is not session-start evidence. Implemented and covered by `TestThreadTakeoverEchoOnlyBatchDoesNotRun`.

### D-4C-01 — `issue_runs.thread_state` is B-owned and `pending` is materialized by `StartSession`（Phase 4C design, **closes G-016**）

Status: **Accepted (design round, 2026-10-08)**; not implemented. Detail: §4C.2.

**`thread_state` stays a business column written only by business transitions** (Thread D1 + controller-integration
D6 invariant 7). The literal `pending` ("Cloud has declared the first prompt and the session work item; no Node
record taken over yet") is materialized **by B**, inside the Phase 3A `StartSession` transaction that writes
`thread_entries seq=1` and declares the `agent_session` work item, under the predicate `thread_state IS NULL`, with
the affected-rows count asserted to be exactly 1 (the assertion is sound because the write runs only on the branch
where seq=1's `ON CONFLICT DO NOTHING` inserted a row, so no earlier `StartSession` can have committed). The 4C
migration carries a matching idempotent backfill for agent runs that already have seq=1. Rejected: writing it in
A-side `RecordDispatch` (control plane writing a business table) and deriving it at read time from
`phase='starting' AND thread_state IS NULL` (hands a DB-expressible invariant to every reader, and makes one of the
five CHECK values unrepresentable in the table). **Supersedes** D-026's "`pending` has no writer" bullet and the
G-016 note in D-024; it does **not** add a transition — 4B's `pending → active` and its `starting → running`
authority are unchanged. The semantic refinement of Thread D4's wording must ship with the ADR amendment (G-017).

### D-4C-02 — Thread GET: one-snapshot page, `after`/`before`/tail cursors, `limit` 1..500（Phase 4C design）

Status: Accepted (design round); the `before`/tail/`idleSince` parts are an **extension of approved Thread D5**
(G-017). Detail: §4C.3.

`GET .../runs/{rid}/thread` returns `{items (ascending by seq), threadState, idleSince, nextCursor, prevCursor}`
from one transaction ⇒ one snapshot; `threadState`/`status` may be newer than the page frontier, so clients must not
read `threadState` as "the state at my cursor". `after=N` = `seq > N` ascending (D5's original meaning; `after=0` =
from the start); `before=N` = the newest `limit` entries with `seq < N`, still presented ascending; no cursor = tail
read (newest `limit`); `after` + `before` together ⇒ `400 invalid_pagination`; `limit` default 200, max 500 (D5),
out of range ⇒ `400 invalid_pagination`; a non-decimal cursor ⇒ `400 invalid_cursor`. `nextCursor`/`prevCursor` are
opaque decimal-`seq` strings (clients pass them back verbatim); `""` means "end of the data now", not "thread
ended". The reader must **not** reuse `page`/`window` (UUID cursor via `validID`, max 100) and must **not** widen
`window`. Authorization and scoping mirror the comment read (tenant member + readable Issue, `tenant_id`/
`issue_id`/`run_id` triple, soft-deleted ⇒ 404). `stripAgentRunSkeleton` keeps the run resource's shape unchanged;
the Thread reader projects `threadState`/`idleSince` itself.

### D-4C-03 — Thread POST: B writes the entry, A's seam writes the command, one transaction（Phase 4C design）

Status: Accepted (design round); not implemented. Detail: §4C.4 (acceptance matrix) and §4C.5.

`POST .../runs/{rid}/thread/messages` with a mandatory `Idempotency-Key` and body
`{content:[{type:"text",text}]}` (text total ≤ 64 KiB; v1 accepts only `type="text"`). In one
`Store.transact`: authoritative run re-read → acceptance decision → `seq = MAX(seq)+1` → insert the
`source='user'`, `kind='user_turn'`, `status='queued'` entry with a Cloud-generated `turn_id` → CAS
`thread_state='active'` + `idle_since=NULL` (affected rows must be 1) → call `EnqueueThreadCommand`. Accepted iff
`thread_state ∈ {pending,active,idle}` **and** `cancel_requested_at IS NULL` **and** the run Workspace is live;
otherwise `409 thread_closed` (a workspace-invalid `pending` run is rejected fail-closed); missing/soft-deleted/
foreign/non-agent run ⇒ `404 not_found`; an impossible `phase`/`thread_state` combination ⇒ `500`, never a 4xx
conflict. Response `201 {resource: {... status:"queued"}}`. After commit: B publishes the SSE invalidation and A
publishes `ThreadCommandAvailable`. A seam failure rolls the entry, the command and the idempotency record back
together, so a same-key retry is a clean first attempt (§4C.6).

### D-4C-04 — POST idempotency reuses `idempotency_records`; 7-case matrix（Phase 4C design）

Status: Accepted (design round). Detail: §4C.5.

No per-entry idempotency column: the existing generic `idempotency_records(tenant_id,user_id,key,request_hash,
response,status)` is the authority, and `request_hash` includes the path. Cases: (1) first request ⇒ 201; (2)
same key + same body after commit ⇒ the stored response replayed verbatim (same `seq`/`turn_id`, no second entry,
command or event); (3) same key + same body concurrently ⇒ the global advisory lock serializes, the loser replays;
(4) same key + different body ⇒ `409 idempotency_conflict`; (5) **different key** + same body ⇒ two entries and two
turns (idempotency is per key, not per content); (6) same key against a **different run** ⇒ `409
idempotency_conflict` (the path is in the hash, so a key can never replay across runs); (7) replay after the entry
became `delivered`/`discarded` or the thread closed ⇒ **still the original response** (a replay's outcome must not
depend on later state), while a **new** key after closing ⇒ `409 thread_closed`. DB invariant failures are never
disguised as conflicts.

### D-4C-05 — `EnqueueThreadCommand`: A owns `thread_commands`, B supplies business facts, one caller-owned transaction（Phase 4C design）

Status: Accepted (design round); the seam is declared but unimplemented (`UnavailableAgentRunControlPlane`).
Detail: §4C.6.

`EnqueueThreadCommand(t, run, command) (command_id string, err error)` runs inside the caller's transaction, opens
none, and never spans external IO; any error rolls the caller's transaction back. **B** builds the business content
(`SubmitUserTurn{turn_id, content}` / `EndSession{reason}`, `reason ∈ user_ended|idle_timeout|cancelled`) and
generates `turn_id`; **A** generates `command_id`, owns the row and `created_at` (DB time), and emits
`ThreadCommandAvailable{run_id}` only after commit. Unwired seam: on the POST path `503
thread_command_unavailable` (retryable, and because the rollback leaves no idempotency record the retry is a clean
first attempt); on the idle-scan path the state transition is skipped so no run can commit `ending` without its
`EndSession` command. Commands written while no session execution is registered stay in Cloud and are invisible to
`ClaimThreadCommands` (ADR D3).

### D-4C-06 — Four distinct identities: `turn_id`, `command_id`, `execution_id`, `seq`（Phase 4C design）

Status: Accepted (design round). Detail: §4C.6.

`turn_id` (B-generated) is the business identity of a user turn, persisted on the entry and echoed by Node records;
`command_id` (A-generated) is the delivery identity Node dedupes on; `execution_id` (Controller-generated, A-fenced)
is the session identity recorded by delivery registration; `seq` (B-generated, run-scoped, from 1) is the ordering
identity and the pagination cursor and is **never** sent to Node. One user turn ⇒ one entry + at most one command +
at most one delivery registration. `seq` and Node `sequence` (per-execution) remain different spaces; no
`seq = sequence + constant` (or equivalent) may be introduced.

### D-4C-07 — SSE is invalidation only; the `after` vocabulary belongs to the durable log（Phase 4C design）

Status: Accepted (design round). Detail: §4C.7.

Three things stay distinct: the durable log (`thread_entries`, the only replay source), the notification mechanism
(the in-process `SpaceEvent` hub — volatile, buffered, single-instance, no replay), and connection-local delivery
(no guarantee). There is one cursor vocabulary (`seq`) and it is only ever evaluated against the durable log; **only
a GET advances the client's cursor** (`nextCursor`/`prevCursor`), an SSE event merely triggers a GET, and `lastSeq`
never advances a cursor. `after=N` therefore means the same thing on first load and on reconnect: "I hold every
entry with `seq <= N`, give me the rest" — so a reconnect is just the client re-issuing its own GET and the stream
needs no resumability or `Last-Event-ID` semantics. Public event:
`issue_run.thread_appended{issueId, runId, lastSeq}` on the Space stream (requires three new optional `SpaceEvent`
fields; existing event JSON is unchanged). Rejected: a per-Thread `?after=` SSE endpoint with server-side replay —
it duplicates the GET contract and invites clients to treat the stream as the log.

### D-4C-08 — SSE ordering, duplicates and loss are tolerated by construction（Phase 4C design）

Status: Accepted (design round). Detail: §4C.8.

Ordering is **not** promised (events are published after commit, so `lastSeq` can appear to regress); duplicates are
allowed and harmless; **data is never lost** (durable log + authoritative GET) but **notifications may be lost**,
so the frontend must reconnect and poll (≥ 30 s, or on focus). When only `status`/`thread_state` changed, Cloud
still publishes `issue_run.thread_appended` with an unchanged `lastSeq` (one ADR-named event type whose payload is
always a hint; a rename to `thread_updated` is rejected for ADR fidelity). Because forward paging only reveals new
entries, an older entry's `status` flip is observed by re-reading the loaded window; the state change is never a
new entry and never carried as an event fact. After `ended` the events may stop; GET keeps serving the immutable
history.

### D-4C-09 — `active`/`idle` authority: the taken-over records, evaluated in the takeover transaction（Phase 4C design）

Status: Accepted (design round). Detail: §4C.9.

`pending → active` stays 4B's (D-019/D-024, implemented). `active ⇄ idle` is decided **inside the
`ThreadEventsTakenOver` transaction**: the batch's **last taken content record** determines the outcome — a
`turnEnded` with no `source='user' AND status='queued'` entry at commit ⇒ `idle` + `idle_since = now()` (DB time);
any other record ⇒ `active` (and `idle_since` cleared). A queued turn keeps the thread `active`; `idle` is not
triggered by "the batch contained a `turnEnded`" but by the batch **ending** on one. Gated to a live session
(`phase='running'`, non-terminal `thread_state`, `cancel_requested_at IS NULL`); outside the gate the records are
appended and `thread_state` is left alone. A 0-row CAS is an error when the in-transaction re-read said the run was
eligible (the 4B `moved != 1` hardening).

### D-4C-10 — `ending` authority is B with three triggers; `ending → ended` is `SessionEnded` and belongs to Phase 5（Phase 4C design）

Status: Accepted (design round). Detail: §4C.10.

`ending` (with `idle_since` cleared and **exactly one** `EndSession{reason}` committed in the same transaction) is
triggered by: the user ending the thread (`user_ended`), the idle window elapsing (`idle_timeout`, judged by a
B-owned dispatch-loop scan comparing `idle_since + <window>` against **database** time; window = process config
`issue_runs.thread_idle_timeout`, default 15 min, not a column), or a cancel while `phase='running'`
(`cancelled`). `ending` is near-terminal and rejects POST. **`ending → ended` is exclusively `SessionEnded`** (the
session terminal `TakeOverNodeEvent` → the `sessionEnded` hook, controller-integration D6) — Thread → `ended`,
queued turns → `discarded`, run → `delivering` — and that hook is **not implemented: it is Phase 5**. 4C must not
accept a `SessionCommandAccepted` reply, a registered execution result, or anything else as an `ended` authority.
The explicit "user ends the thread" endpoint shape is missing from the ADR ⇒ G-018.

### D-4C-11 — Cancel/terminal interaction keeps 4B's cancel-first semantics and never rewrites Thread history（Phase 4C design）

Status: Accepted (design round). Detail: §4C.11.

POST with `cancel_requested_at` set ⇒ `409 thread_closed`. Cancel at `provisioning`/`starting` ⇒ straight to
`releasing`/`cancelled` with no `ending` and no `EndSession` (inherited from IssueRun D4/D6; it does not contradict
4B's "cancel-first takeover does not enter running", which is unchanged). Cancel at `phase='running'` ⇒ one
transaction writing `cancel_requested_at` + `thread_state='ending'` + `EndSession{cancelled}`, so the combination
"cancelled but still `active`" is unobservable. A cancel arriving after `ending` only writes
`cancel_requested_at`, adds no second `EndSession`, and does not rewrite the reason; the final `status` follows
`cancel_requested_at` at settlement (Phase 5's rule). Undispatched (`queued`) turns are marked `discarded` by the
Phase 5 `SessionEnded` transaction; 4C only guarantees the column and that no new `queued` entry can appear after
`ending`. Replays of an already-accepted POST return the original response even after the run is terminal. No 4C
transition writes `record` or renumbers `seq`; the only mutable column is `status`, one-way
`queued → delivered|discarded`.

### D-4C-12 — Phase 4C requires a migration（Phase 4C design, **not created this round**）

Status: Accepted (design round). Detail: §4C.12.

Migration 0022 (after 0021) adds: (a) the control-plane table `thread_commands` (`id uuid PK` = `command_id`,
`run_id uuid NOT NULL REFERENCES issue_runs(id)`, `kind CHECK IN ('SubmitUserTurn','EndSession')`, `body jsonb`
object CHECK, `created_at` DB-time default, `delivered_at`, `delivered_execution_id`,
`CHECK ((delivered_at IS NULL) = (delivered_execution_id IS NULL))`, partial index
`(run_id, created_at) WHERE delivered_at IS NULL`); (b) `thread_entries.status text CHECK IN
('queued','delivered','discarded')` with `CHECK ((source='user') = (status IS NOT NULL))` and a partial index
`(run_id) WHERE source='user' AND status='queued'`; (c) an idempotent backfill setting `thread_state='pending'` for
agent runs that already have seq=1; (d) a partial index `issue_runs (idle_since) WHERE thread_state='idle'`.
Explicitly **not** done: no change to the `thread_state` value set or `idle_since` (0018 suffices), no counter
column/sequence table, no per-entry idempotency column (the generic `idempotency_records` is reused), no change to
`node_event_receipts`, no `DEFAULT` on `thread_state`, and no second `starting→running` authority table/column.
`status` is required because `discarded` is a Cloud-side decision at session end that cannot be derived from any
existing row.

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
  **architecture RESOLVED → closes at Phase 4B implementation**: seq=1 is preserved (Phase 3A writes only seq=1);
  the allocation mechanism is now fixed and accepted (D-023: `MAX(seq)+1` from 2 inside the takeover transaction,
  §4B.5), with the echoed-first-prompt dedup rule (D-024, §4B.4) guaranteeing seq=1 is never renumbered. The gap
  closes once the Phase 4B takeover path lands and T4B-6/T4B-7 evidence exists.
- **G-010** (derive the D6 `EnqueueExecutionWork` `target` from `sandbox_instances`/`node_instances`) —
  **CLOSED with the minimal deterministic `sessionStartTarget`** (`{workspace_id, sandbox_instance_id, node_id}`
  when a live sandbox/connected Node exists).
- **G-011** (terminal for a `starting` run with invalid/missing workspace) — **PARTIAL**: the fail-closed path
  (leave `starting`, no terminal state, retry loop resurfaces) is implemented + tested (`runWorkspaceLive` /
  `TestAgentSessionStartSoftDeletedWorkspaceFailsClosed`); the authoritative choose-path decision remains for a
  later approval (this round chose fail-closed per §6/§11, never `status=failed` for convenience). **Phase 4 does
  not solve this** — the takeover hook re-checks the workspace and fails closed (no `running`), preserving the gap.

### G-012..G-015 — Phase 4 thread/running gaps（status updated by the Phase 4B architecture resolution round）

- **G-012 — PARTIAL（Phase 4A 闭合 A 侧登记；4B 本轮闭合接管路径，仍余生产 Controller 环）** — `node_executions`
  migrated (0020) + production `RecordDispatch` (`agent_work_dispatch`) + `execution_work.execution_id` fence
  implemented and evidence-backed (D-020/D-021, T4A-1..T4A-14,
  `TestMigration0020NodeExecutionsAppliesFreshAndUpgrades`). Phase 4B (this round) delivered the other half of the
  B-visible path: `node_event_receipts` migrated (0021), `agent_thread_takeover` routed, the real
  `ThreadEventsTakenOver` hook wired, and the gRPC `AgentRunService.TakeOverThreadEvents` registered — all covered
  by T4B-1..T4B-19 plus the gRPC acceptance test. Still Missing: the production Controller actually calling
  `TakeOverThreadEvents` (the Node/Controller relay loop), and `EnqueueThreadCommand`/`GrantRevisionUpload`
  (`EnqueueThreadCommand` is Phase 4C, §4B.12). Settlement of the workspace hook (`RunWorkspaceDeleted`) also stays
  fail-closed, but no production code inserts `workspaces.issue_run_id` yet, so that path is unreachable rather than
  wrong. The gap therefore shrinks but stays PARTIAL; it was **not** split into G-012a/G-012b because the remaining
  half is one coherent item (the Controller-side production loop) rather than two independently closable ones.
- **G-013 — CLOSED（Phase 4B architecture resolution 2026-09-30; implementation 2026-09-30）** — resolved without a
  synthetic event: the `starting→running` authority is the first **real Node Thread record** takeover
  (`ThreadEvent{record}` per Node protocol D2; IssueRun D3 + controller-integration D6 + Thread D4).
  `thread_state` →`active` on the first real record; an echo-only batch writes receipts but activates nothing.
  Echoed first prompt (`turn_id == initial_turn.turn_id`) is deduped (receipt only, no new entry), preserving seq=1
  immutability. Empty batch rejected. Implemented and covered by T4B-1/T4B-2/T4B-7/T4B-8 and the gRPC acceptance
  test. See D-024, D-026, §4B.2–§4B.4. **No dependency on the `proposed` controller-session ADR** (§4B.14).
- **G-014 — CLOSED（Phase 4B architecture resolution 2026-09-30; implementation 2026-09-30）** — Thread `seq`
  allocation mechanism fixed and **D-023 ACCEPTED**: `MAX(seq)+1` inside the takeover transaction, from 2 upward.
  Serialization proven from approved facts (one session execution per run + global advisory lock +
  `PRIMARY KEY (run_id, seq)`), **not** the proposed controller-session D2. Node `sequence` ≠ Thread `seq`. No
  counter table. Implemented and covered by T4B-6/T4B-11/T4B-16 (`TestThreadTakeoverContiguousBatch`,
  `TestThreadTakeoverConcurrentBatches`). See D-023, §4B.5.
- **G-009 — CLOSED（Phase 4B implementation 2026-09-30）** — the seq-continuation obligation the plan already
  assigned to 4B is now proven end to end: seq=1 stays the immutable Cloud-authored first prompt
  (`TestThreadTakeoverFirstRecordRunsTheRun`, `TestThreadTakeoverInitialTurnEchoIsDeduped`), seq≥2 is continuous
  across batches and replays allocate nothing (`TestThreadTakeoverContiguousBatch`,
  `TestThreadTakeoverReplayIsIdempotent`), and concurrent callers never duplicate a seq
  (`TestThreadTakeoverConcurrentBatches`). The gap is closed by evidence, not by the design.
- **G-015 — PARTIAL（Phase 4B 初值已给；enforcement 未实现）** — per-run non-terminal event ceiling default
  200,000 (`thread_event_cap`) and `node_event_receipts` retention default 30 days post-`done`
  (`node_event_receipts_retention`) pinned as versioned config defaults (D-025, §4B.10). **Enforcement is
  deliberately not implemented**: the values have no approved ADR/spec basis, so shipping them would be unapproved
  behavior (mandate §3/§39); T4B-18 is recorded DEFERRED for the same reason. The migration ships only the
  `created_at` index the future prune loop needs. The gap stays PARTIAL and **NON-BLOCKING**; it becomes CLOSED when
  an approved ADR fixes the values and the cap rejection + prune loop land with evidence.
- **G-016 — CLOSED（Phase 4C design round 2026-10-08, D-4C-01）** — the literal `issue_runs.thread_state='pending'`
  now has a designated writer: **B**, in the Phase 3A `StartSession` transaction (same transaction as seq=1 and the
  `agent_session` work item), under `thread_state IS NULL`, with the affected-rows count asserted to be 1, plus an
  idempotent backfill in the 4C migration for agent runs that already have seq=1. Rejected alternatives (recorded
  with reasons in D-4C-01/§4C.2): writing it from A-side `RecordDispatch` (control plane writing a business column
  — controller-integration D6 invariant 7 / §11) and deriving it at read time from `phase='starting' AND
  thread_state IS NULL` (hands a DB-expressible invariant to every reader and leaves one of the five CHECK values
  unrepresentable). Closing the **decision** does not close the **implementation**: T4C-1..T4C-5 record the evidence
  obligations and stay `MISSING`. The gap is not "resolved by evidence" — it is resolved by an accepted design with
  a named writer, which is what a design round can and must close. See §4C.2.

- **G-017 — OPEN / NON-BLOCKING / DEPENDENCY（Phase 4C design round, 2026-10-08）** — the 4C Thread read contract
  extends approved Thread D5 beyond its literal text: a no-cursor **tail** read, the `before` cursor, the
  `idleSince` response field, and the `pending` timing/meaning clarification of D4 (D-4C-01/§4C.2, D-4C-02/§4C.3).
  **Why it is a gap and not a silent decision**: D5 fixes only `after={seq}&limit={n}` ascending, so a client cannot
  reach the tail of a long thread without walking from seq=1, and the panel's "why is this session about to end"
  display needs `idle_since`; but shipping unrecorded extensions of an approved ADR is exactly the "silent
  divergence" the plan forbids. **What is needed to close**: amend Thread D4/D5 (pending materialization point and
  meaning, tail default, `before`, `idleSince`, `nextCursor`/`prevCursor` semantics) or obtain explicit architect
  confirmation of the extension. **Not a blocker** for the rest of 4C, but the corresponding code should not be
  merged before the amendment/confirmation.
  **Update (Phase 4C Final Completion Batch, 2026-10-08)**: the human approved **A3**. The tail default (`limit` 缺省
  200 且取尾部窗口)、`before={seq}`、`idleSince` 与 `nextCursor`/`prevCursor` 已写入 Thread D5 正文并实现，
  直接证据见 `integration/agent_run_thread_read_test.go` 的 `TestThreadReadLimitAndCursorBounds`（缺省 = 取尾、
  上限 500）、`TestThreadReadBeforeCursorSelectsTheWindowBelowIt`（`before` 与 `after` 互为逆运算、互斥为
  `400 invalid_pagination`）、`TestThreadReadReportsIdleSinceAndWindowCursors`。**G-017 = `CLOSED BY A3`.**

- **G-018 — OPEN / NON-BLOCKING（Phase 4C design round, 2026-10-08）** — the ADR's lifecycle table names
  "用户点击『结束』 ⇒ `ending`" but **no endpoint exists** in any decision. The authority is settled (B's lifecycle
  transition, D-4C-10); only the API shape is missing, so the `user_ended` trigger is unreachable in 4C while
  `idle_timeout` and `cancelled` are not. **Why non-blocking**: the idle window and cancellation already exercise the
  same transition and the same `EndSession` seam. **What is needed to close**: decide route/verb/status/idempotency
  (recommended `POST .../runs/{rid}/thread/end`, 202 + `thread_closed` on conflict, mandatory `Idempotency-Key`) and
  land it in the OpenAPI contract. Must stay distinct from "cancel the run" (D-4C-11).
  **Update (Phase 4C Final Completion Batch, 2026-10-08)**: the human approved **G-018** and it landed exactly on that
  recommendation — route `POST /api/v1/tenants/{tid}/issues/{iid}/runs/{rid}/thread/end`, `Idempotency-Key` 必需、
  body `{}`、成功 `202 {"threadState":"ending"}`；幂等预检先于生命周期校验（同键重放原样回放，即使 Thread 已
  `ended`），新键对 `ending | ended` 为 `409 thread_closed`，同键异地为 `409 idempotency_conflict`。它**只**推进到
  `ending`，不写条目、不分配 `seq`、不改 `phase`/`status`。OpenAPI（`api/openapi.json` 与生成的 frontend client）
  与集成证据已同一批次落地。**G-018 = `CLOSED`.**

- **G-019 — OPEN / NON-BLOCKING / DEPENDENCY（Phase 5）** — `ending → ended` and `queued → discarded` are written
  **only** by `SessionEnded` (the session terminal `TakeOverNodeEvent` → `sessionEnded` hook,
  controller-integration D6), which is not implemented. 4C designs the column, the predicates and the transition but
  must not implement a second path to `ended`. **What is needed to close**: Phase 5 implements the hook + delivery,
  reusing `thread_entries.status` and the `ending` predicate; the Thread core-test obligations 「终态顺序」 and
  「轮次结算」 stay `Partial`/`Missing` until then.
  **Update (Phase 4C Final Completion Batch, 2026-10-08)**: **still OPEN, unchanged, and deliberately not touched.**
  本批次没有实现第二条通往 `ended`/`discarded` 的路径：新的 `/thread/end` 端点停在 `ending`，`SessionEnded`、
  `queued → discarded` 与交付/释放仍全部留给 Phase 5。它**不阻塞** Phase 4C 完成。
  **Update (Phase 5 Batch 1, 2026-10-08)**: **`PARTIALLY CLOSED — Thread terminal takeover complete; delivery pipeline
  remains Phase 5 Batch 2`.** 已实现并有直接证据：`agent_session_takeover` 权威接管（唯一入口 `TakeOverNodeEvent`）、
  同事务 `ending → ended`（并覆盖 `pending | active | idle`）、`queued → discarded`、`running → delivering`、
  放出恰好一个未登记的 `deliver_revision` 工作项、收据与 `last_event_sequence` 推进、重放 no-op、任一步失败整体回滚、
  提交后恰好一条 `thread_changed`。**仍未关闭的部分**：交付流水线——`DeliverRevision` 投递、`DeliverySettled`、
  `releasing`、`RunWorkspaceDeleted`、`done`——属 **Phase 5 Batch 2**，本项因此**不得**整项标 `CLOSED`。
  Thread 核心用例的「终态顺序」「轮次结算」「`ending → ended`」已在此轮转为 `Covered`。
  **Update (Phase 5 Batch 2, 2026-10-08)**: **`CLOSED`.** 交付半边已同一轮落地并有直接证据：`deliver_revision` 工作项的
  认领与派发（kind 白名单在共享派发谓词上扩大）、`agent_delivery_takeover` 的权威接管（收据 + 结果 + 钩子 + 序号推进
  同事务）、D5 的失败重试与退避（30s × 2^(n-1) 封顶 10min，且**不产生新的逻辑交付**）、D5 的放弃（>2h 连续失败或运行
  Workspace 的 Node 未知 >30m）→ `delivering → releasing` + D4 派生 `status` + 同事务声明**恰好一个**删除意图、
  `RunWorkspaceDeleted` 的 `releasing → done`（重放 no-op、非 `releasing` 阶段拒绝）、以及两条周期补偿路径
  （放弃扫描、删除重新声明）。证据：`integration/agent_run_delivery_test.go` 的 P5-7…P5-16 与
  `internal/core/agent_run_release_db_test.go` 的 5 个白盒测试。**唯一未实现的部分不是 G-019 的语义缺口，而是
  Revision 登记本身**：Cloud Revision ADR 仍为 `status: proposed`，故 `saved`/`unchanged` 一律 `UNAVAILABLE` + 零写入，
  该受限面单独登记为 **G-030**，不再挂在 G-019 上。
  issue-run 核心用例的「完整阶段链」「未结算不释放」「放弃上限」「Node 报告已上传 ≠ 交付完成」已在此轮转为 `Covered`，
  并新增整节核心用例「A Released Run Declares Its Workspace Delete And Reaches Done Only Through The Delete's Takeover」。

- **G-029 — `DEFERRED / NON-BLOCKING`（Phase 5 Batch 2 登记；Revision ADR Approval Round 裁定, 2026-10-08）** — IssueRun D4 的结算规则里含有
  「Agent 的回复注释」一类内容，但**没有**任何已批准的字段路径规定它写在哪一列/哪一对象。
  **为什么登记而不是自行选定**：字段路径属持久化契约，发明它等于静默扩展 ADR（authority order 禁止）。
  本轮因此**不写**该字段，`releasing` 结算只写 D4 明确规定的 `phase`/`status`/`deliveryState` 与既有活动记录。
  **需要什么**：ADR 明确该注释的来源与落点，或删除该义务。**不阻塞**本轮完成。
  **裁定（Revision ADR Approval Round, 2026-10-08）= `DEFERRED / NON-BLOCKING`**：① Revision ADR **不**要求持久化
  Agent 回复评论（它未提及该义务）；② `issue_runs.result` 的批准字段只有 `{revisionId, deliveryState}`，没有承载该
  注释的位置；③ Agent 的消息已经有两处权威承载——Thread 条目（在线、公开可读）与会话 JSONL（随 Revision 归档，
  Thread D2 明确二者来自同一份记录）；④ 判定「哪条记录的 `kind` 是 Agent 消息」所需的**取值集合由 desktop
  `ora-history` 拥有**，而 Thread D2 禁止 Cloud 解析业务字段 ⇒ 该义务的输入不在 Cloud 的批准域内。
  ⇒ **不为其扩 schema**，并**明确排除**在 Revision ADR 的批准范围之外；若人类仍要保留 D4 的该句，最小修订是把它
  改为「由 `ora-history` 的类型标签选定，其取值由 desktop 决策给出」（**本轮未改任何 ADR**）。

- **G-030 — `CLOSED`（Revision Completion Slice, 2026-10-08）**。关闭依据：人类于 2026-10-08 逐项批准 Cloud 与 Node
  两个 Revision 根决策（`status: approved`，含 B-1..B-4 与 P-1..P-3 的收敛文本），本轮据批准文本实现了 Cloud 侧
  全部行为并逐条取得直接证据：`0023_revisions.sql`（`PRIMARY KEY (id)` + `UNIQUE (run_id)` + 六条 CHECK）、
  `internal/objectstore`（预签名 PUT 签发 + `HEAD` 校验）、可选 `object_store` 配置、`GrantRevisionUpload`、
  D4 的两步校验（本地输入一致性 → `HEAD` 存在/大小/小写 hex SHA-256）、登记与 `releasing` 同事务、重放幂等与
  冲突整体回滚、`verification_failed` 只由 Cloud 记录。**关闭的是「未批准 ADR 阻塞实现」这条**，不是它名下
  衍生的新缺口：真实 MinIO 上无法验收 ⇒ **G-037**；公开读与健康检查两处偏离批准契约 ⇒ **G-035 / G-036**。
  以下保留该缺口的历史登记（三轮：Phase 5 Batch 2 登记 → Approval Round 细化解除条件 → Decision & Amendment Round 收敛文本）。
  历史登记：`specs/decisions/cloud/revision/0-cloud-owned-object-store-and-verified-revisions.md`
  仍为 **`status: proposed`**，而它定义的正是 Revision 的对象键、`revision_ref`、Cloud 侧对象校验与
  `GrantRevisionUpload` 的授权形状。依 authority order（approved ADR > AGENTS.md > plan > implementation）与
  `specs/AGENTS.md`（`proposed` 阶段不得据其编写契约与核心测试用例），**不得**据此实现。现状：无 `revisions` 表、
  0022 仍为最新 migration。本轮的处理是**受阻且不静默降级**：`DeliverySettled` 收到 `revision_delivered`/`revision_unchanged`
  一律回 `UNAVAILABLE` 且**零写入**，运行只能经 D5 放弃或 D6 无会话取消到达 `releasing`。
  **未**发明第二套 upload auth、**未**信任 Node 上报的 tenant/run、**未**先产生外部副作用再补身份。
  **需要什么**：人类把该 ADR 评审为 `approved`，随后另开一轮实现注册、对象校验与上传授权。
  证据：`TestRevisionDeliveredIsRefusedAndTheRunStaysDelivering`（拒绝 + 零写入 + 放弃后重放仍不改写）。
  **Update (Revision ADR Approval Round, 2026-10-08)**: **解除条件细化——不能按现文本批准。**
  approval-readiness 审计判定 **`REVISION_ADR_NOT_READY_FOR_APPROVAL`**：4 项 blocker
  （B-1 Revision 行身份/唯一性/冲突载荷未定义；B-2 对象键「每次尝试」段与**已交付实现**不一致；
  B-3 `revision_ref` 的形状与归属未在本 ADR 定义；B-4 D1 的 `skipped` 前置路径与 **approved** IssueRun 不变量 3
  冲突，且与 D6 的无会话取消**语义重载**同一 `deliveryState` 取值）+ 3 项建议补写的 precision clause
  （只校验存在/大小/SHA-256；同授权 TTL 内重复 `PUT`；`unchanged` 不复用既有 Revision）。
  最小修订提案见本轮 round 记录与报告 §12；**本轮未改 ADR 文件、未改 `status`**（被批准的文本必须由人类定稿）。
  **Update (Revision ADR Decision & Amendment Round, 2026-10-08)**: **B-1..B-4 与 P-1..P-3 已各自收敛为唯一方案，
  并已写入该 ADR 正文（文首与文末「修订记录」明确标识为待审批）**：
  B-1 ⇒ D4「Revision 行的身份、唯一性与登记幂等」：`revisions` 是 controller-integration D6 的**控制面**表，
  由控制面在交付终态接管事务内生成 `id`（uuid）、写行，随后同事务调用 `deliverySettled` 并传入该 `revisionId`；
  `PRIMARY KEY (id)` + `UNIQUE (run_id)`（一次运行至多一行，`unchanged` 也登记）；`INSERT ... ON CONFLICT (run_id)
  DO NOTHING` + 读回比较负载（逐字段相同 = 幂等成功并返回既有 `id`；不同 = 不变式冲突、接管事务整体回滚）；
  重放由 `node_event_receipts` 判定（已结算的交付不会插入第二行），`UNIQUE (run_id)` 是数据库兜底；
  行与收据/结果/阶段推进同事务，因此不存在孤儿行 —— 唯一允许的偏斜方向是「对象已上传而 Revision 未登记」。
  B-2 ⇒ D2「三种身份」表 + 键模板第三段改为 **Cloud 生成的尝试 ID**（`revisions/{tenantId}/{runId}/{deliveryAttemptId}/…`）：
  工作项 ID 由 A seam 在插入时生成，调用方冻结工作项输入时尚不可得（`EnqueueExecutionWork` 返回 id、但 input
  是逐字写入的），因此键用此刻已存在的尝试 ID；尝试 ID 只命名一次尝试的对象，**不是**逻辑交付身份、
  **不是** Revision 身份；两条性质 = 同尝试内键不变、跨尝试键永不相等。改 A seam 签名的方案已列入替代方案表并被否决。
  B-3 ⇒ D4：`revision_ref` 由 Cloud 生成为 `refs/ora/revisions/<runId>` 并写入输入，Node 不构造、不改写、原样回显，
  `base_commit` 同；它按**运行**命名，故 Revision 的内容身份是行内 `final_commit` + 对象摘要，恢复不读 ref。
  B-4 ⇒ **删除 D1 的「缺 `object_store` ⇒ `skipped` 直接释放」路径**：缺配置时 Cloud 照常进入 `delivering`、
  照常放出交付工作项（D3/D6 的硬约束），但拒发授权（`UNAVAILABLE`），交付确定失败，**完全由 approved D5 收口**
  （`deliveryState = failed`、`revisionId = null`）。因此**不新增第四条 `delete_workspace` 触发条件**、
  不新增 `deliveryState` 取值、不新增失败码，`skipped` 归还 D6 的无会话取消专用。**不需要修改 IssueRun ADR**；
  唯一依赖是对 D5「连续失败」的一处澄清 ⇒ 见下方 **G-033**。
  P-1 ⇒ D4「校验的完整范围」（明确做的两件事 + 明确**不**校验的五项 + ownership 由键的形状保证）；
  P-2 ⇒ D4「同一授权内重复 `PUT`」（覆盖写；按声明摘要校验，不符即 `failed{verification_failed}` 并重试）；
  P-3 ⇒ D4（`unchanged` 仍登记；第一版不复用任何既有 Revision）。
  `status` 仍为 **`proposed`**：本轮只收敛文本，**未**自行批准。
  **获批后才可开实现轮**（`revisions` 迁移 + 对象校验 + `GrantRevisionUpload`）。

- **G-031 — `CLOSED`（Revision Completion Slice, 2026-10-08）**。关闭依据：人类批准了 Option B，且**明确「本项不批准任何
  额外 schema 修改」**；approved 修订文本（`(workspace_id, kind)` + 只对未完成的操作成立）已落入 operation D4、
  controller-integration D6 与 `test-cases/cloud/operation/plugin-step-and-run-workspace.md`，实现与批准文本**逐字一致**
  ⇒ 「ADR 与实现择一对齐」这件事已经完成，不再是偏差。**未**新增 migration、**未**改写已应用约束。
  以下保留该缺口的历史登记：
  **（历史）OPEN / NON-BLOCKING / IDENTITY SPELLING DEVIATION（Phase 5 Batch 2, 2026-10-08）** — IssueRun D6 把运行
  Workspace 的幂等身份写作 `(issue_run_id, kind)`，而既有实现的唯一约束是 `(workspace_id, kind)`。
  在本轮的全部路径上两者等价（运行 Workspace 与 run 一一对应、`kind` 固定），因此本轮**沿用既有 schema**，
  **未**新增 migration、**未**改写已应用约束（AGENTS.md 的兼容边界）。
  **需要什么**：ADR 与实现择一对齐（若确需 `issue_run_id`，须有独立 migration 与证据）。**不阻塞**本轮完成。
  **推荐方案（Revision ADR Approval Round, 2026-10-08）= Option B：改 ADR 为 `(workspace_id, kind)`，并限定「未完成的操作」**。
  理由：① `operations` 表**不含** `issue_run_id`，且与用户 Workspace 共用——为运行专属关切给一张共享持久表加列
  （还要加部分唯一索引以免影响用户操作）违反 AGENTS.md 的最小面与兼容边界；② 两种拼写的等价性由**已批准**的
  IssueRun 不变量 1（run ↔ 运行 Workspace 一一对应）加既有约束 `workspaces.issue_run_id UNIQUE`（migration 0018）
  保证，`create_workspace` 路径本身也是**经 Workspace 绑定**识别运行（`isAgentRunWorkspace` 读 `workspaces.issue_run_id`）；
  ③ **决定性理由**：身份必须限定在**未完成**的操作上——`delete_workspace` 被 Node 拒绝 quiesce 后必须能**重新声明**
  （operation D4 的既有重试规则，证据 `TestRefusedQuiesceIsRedeclaredAndStillReachesDone`），而字面的
  `UNIQUE(issue_run_id, kind)` 会禁止第二个操作，把运行永久滞留在 `releasing`。Option A（改实现）则需要改共享表
  才能换得与既有绑定完全相同的性质。**本轮未改 schema、未改 ADR**——修订 approved ADR 需人类批准。
  **精确 amendment proposal（Revision ADR Decision & Amendment Round, 2026-10-08；待人类批准，本轮未应用）**：
  ① `specs/decisions/cloud/operation/20260928-plugin-step-and-run-workspace-release.md:73` —— 把
  「`(issue_run_id, kind)`，由 Cloud 生成，不经过公开幂等键」改为
  「`(workspace_id, kind)`，由 Cloud 生成，不经过公开幂等键；对同一个运行 Workspace，同一时刻至多存在一个
  **未完成**（`queued`/`running`/`retry_wait`/`blocked`）的该 `kind` 操作，已终结的操作不占用该身份，
  因此被拒绝 quiesce 后可以**重新声明**同一 `kind` 的操作」。
  ② `specs/decisions/cloud/controller-integration/20260928-agent-run-executions-thread-and-upload-grants.md:119` ——
  把函数表中的「以 `(issue_run_id, kind)` 幂等地创建运行 Workspace 及其 operation」改为
  「以 `(workspace_id, kind)` 幂等地创建运行 Workspace 及其 operation（同一时刻至多一个未完成的同 `kind` 操作；
  运行 Workspace 与运行一一对应，故与按运行限定等价）」。
  ③ `specs/test-cases/cloud/operation/plugin-step-and-run-workspace.md:62` —— 同一句的用例侧复述同改
  （`specs/AGENTS.md`：ADR 变更须同步核心用例）。
  精确改动 = 两处 approved ADR 的**一句**加一处核心用例的**一句**；**不改 schema、不加列、不加索引**、
  不改任何 Go 代码（既有实现已经是该语义：`(workspace_id, kind)` 的未完成操作由
  `agent_run_workspace_release.go` 的应用级检查表达，数据库侧由既有 `one_project_operation` 传递性串行）。
  **是否再补一个数据库部分唯一索引是独立问题，不在本 amendment 内**（加索引 = migration = 另一轮）。

- **G-032 — `CLOSED`（G-032 Implementation Slice, 2026-10-08）**。关闭依据：人类批准的 **IssueRun D8** 已在 Cloud 侧
  全部落地，四条核心用例义务均由**会失败的**集成测试直接取证：
  ① **唯一放弃条件 = 运行 Workspace 不可达** —— `releaseGivenUp` 只读「该 Workspace 仍有活 sandbox **且**其 Node 状态
  未知超过窗口」，完全不看 `delete_workspace` operation 的状态与重试次数，因此反复失败但 Node 可达（含 Node 明确拒绝
  quiesce）的运行仍留在 `releasing` 并继续重试；② **上限复用 D5 既有配置** —— 判据是 D5 与 D8 共用的一个
  `runWorkspaceNodeUnknown`（**不存在第二套 reachability 定义**），窗口取 `delivery_unreachable_after`（默认 30m），
  **未新增任何 timeout 配置**，也**未**复用 D5 的 2h「连续失败」窗口；③ **超限终态** —— 同一事务内
  `releasing → done` + `failure_reason = workspace_unavailable`，业务 `status` 保持 D4 交付结论不变；④ **残留如实保留**
  —— Workspace 行未软删除、`issue_run_id` 未清除、未写成功形态的 operation result，未完成的 `delete_workspace` 以
  **既有公开失败码** `node_unavailable` 终结（**未新增 schema**、**未发明新错误枚举**、**未新增后台清理机制**、
  **未新增运维重试 API**）。**为什么必须有一个"活的 sandbox"前置条件**：删除走到 `cleanup` 步时 sandbox 已终止、
  已无 Node 可判「未知」，那种情况不是 D8 的条件，否则会把一个仍在推进的释放判死。
  证据：`cloud/integration/agent_run_release_giveup_test.go`（G032-1..G032-9，真实 PostgreSQL；
  `TestG032_3_...` 为核心用例，G032-3/5/6/7 另以 `-count=10` 通过，G032 全组 `-race` 无 DATA RACE）；
  核心用例四条义务已 `Missing` → `Covered`（`test-cases/cloud/issue-run/agent-run-orchestration.md`）。
  **未借本次批准扩大** G-020 / G-021 / G-025 / G-026 / G-027 / G-028 / G-029 / G-034 / G-035 / G-036 / G-037 的范围。
  以下保留该缺口的历史登记（含批准文本与精确 amendment proposal 的原文）：
  **（历史）OPEN / NON-BLOCKING / MISSING LIMIT（Phase 5 Batch 2, 2026-10-08）** — D5 只给**交付**侧放弃窗口
  （连续失败 >2h、Node 未知 >30m）；**Workspace 删除**侧的持续失败没有上限，删除意图由
  `RedeclareRunWorkspaceDeletesOnce` 无限重新声明，运行会一直停在 `releasing`。
  这**不是**本轮引入的回归（本轮之前该路径根本不存在），但属同一生命周期的空缺。
  **需要什么**：后续 ADR 明确删除侧的重试上限与终态（例如耗尽后如何处置）。**不阻塞**本轮完成。
  相关既有证据：`TestRefusedQuiesceIsRedeclaredAndStillReachesDone` 证明「重新声明」这一半边是有效的。
  **最小 proposal（Revision ADR Approval Round, 2026-10-08）**：新增 IssueRun **D8「删除侧也有上限，且只以不可达为条件」**，
  只规定四件事 —— ① **重试来源**：沿用 operation D4 既有的 quiesce 失败重试与「同一 operation 的新执行」规则，
  Cloud 侧由删除重新声明保证任一时刻至多一个未完成的 `delete_workspace`（已有实现即为如此）；
  ② **上限**：只以 D5 已有的**不可达**窗口为准（运行 Workspace 的 Node 状态未知超过 `delivery_unreachable_after`，默认 30m）；
  D5 的 2h「连续失败」窗口**不**适用于删除侧，**Node 明确拒绝 quiesce 不触发放弃**（否则 P5-16 的恢复路径会被判死）；
  ③ **终态**：超过上限后运行进入 `done`，业务 `status` **保持不变**（`releasing` 时按 D4 写入的值），另记
  `failure_reason = workspace_unavailable`（沿用 D3 对 create_workspace 失败的既有取值），`delete_workspace` operation
  以失败码终结；④ **是否留在 `releasing`**：不留在 `releasing`——`done` 是唯一终态，且 `done` **不**表示成功
  （D4「终态不变」）。**本轮未实现**（规范轮）。
  **精确 amendment proposal（Revision ADR Decision & Amendment Round, 2026-10-08；待人类批准，本轮未应用）**：
  在 `specs/decisions/cloud/issue-run/0-agent-run-in-disposable-isolated-workspace.md` 新增 **D8**，并只改 D3 阶段表的
  `done` 行 —— ① `done` 的进入条件由「`delete_workspace` `succeeded`」改为「`delete_workspace` `succeeded`，
  或删除侧超过 D8 的上限（此时 `done` **不代表**删除成功）」；② 新增 D8 正文：
  「**D8：运行 Workspace 的删除也有上限，且只以「不可达」为条件。** 删除侧的重试来源不变——沿用 operation D4
  既有的 quiesce 拒绝重试与「同一 operation 的新执行」规则，Cloud 侧由删除重新声明保证任一时刻至多一个未完成的
  `delete_workspace`。上限只取 D5 已有的**不可达**窗口（运行 Workspace 的 Node 状态未知超过
  `delivery_unreachable_after`，默认 30m），证据是 **Cloud 自己观察到的**该 Workspace 的 Node 状态（与 D5 放弃
  窗口 2 同一证据），不依赖 Node 上报；D5 的 2h「连续失败」窗口**不**适用于删除侧，**Node 明确拒绝 quiesce 不触发
  放弃**（否则 P5-16 的恢复路径会被判死）。超过上限后：`delete_workspace` operation 以其既有失败码终结，运行进入
  `done`；`status` **保持不变**（仍是进入 `releasing` 时按 D4 写入的值），另记 `failure_reason =
  workspace_unavailable`（沿用 D3 对 `create_workspace` 失败的既有取值）。**残留资源的登记方式**：运行 Workspace 行
  仍是活的、`issue_run_id` 仍指向该运行，且存在一个处于**终结失败态**的 `delete_workspace` operation —— 不新增列、
  不新增表、不发明后台清理机制。**谁后续处理**：只有运维（或后续专用决策）；公开 API 对运行 Workspace 的
  `delete` 恒为 404（operation D4），因此**没有**自助重清路径。**本条只保证不再重试、且绝不谎报删除成功**。」
  ③ 同一次变更必须同步核心用例 `specs/test-cases/cloud/issue-run/agent-run-orchestration.md` 中把 `done` 定义为
  「delete 到达 `succeeded`」的两处（用例行与失效模式行），并在证据表登记「超上限 ⇒ `done` + 残留 Workspace」
  的新验证义务（当前为 `Missing`，获批实现后补证据）。
  **未登记的风险（本轮明确披露）**：超上限后运行 Workspace 及其容器/进程/卷**不会被自动回收**，且该 Workspace 在
  公开 API 上不可达（404），因此必须由人类通过运维手段处置；`failure_reason = workspace_unavailable` 是唯一可见信号。

- **G-033 — `CLOSED`（Revision Completion Slice, 2026-10-08）**。关闭依据：人类批准了该 D5 澄清，澄清句已写入 approved
  IssueRun 根决策的 D5，且实现与之一致（`deliveryGivenUp` 的窗口 1 以首个 `deliver_revision` 工作项的 `created_at`
  起算，数据库时钟比较）。**仍存的证据缺口**：起算点本身没有会失败的测试（各放弃测试把配置窗口压到纳秒级），
  已如实登记在 `test-cases/cloud/issue-run/agent-run-orchestration.md` 的对应行（`Partial`）。
  以下保留该缺口的历史登记：
  **（历史）NEW / OPEN / CROSS-ADR CLARIFICATION（Revision ADR Decision & Amendment Round, 2026-10-08）** —
  approved IssueRun D5 写的是「交付连续失败超过 2 小时」，而**缺 `object_store`** 的交付永远不会产生任何失败结果
  （Cloud 拒绝签发授权，Node 连授权都没收到）。本轮 B-4 的收口依赖对 D5 的一处读法：**放弃窗口自该运行的首个
  `deliver_revision` 工作项被放出起算**，即「连续失败」包含「从未产生任何结果」。该读法已在 Phase 5 Batch 2 的
  实现中固化（`deliveryGivenUp` 窗口 1 以首个交付工作项的 `created_at` 起算），但**不是** D5 的字面文本
  ⇒ **本轮不据此改写 approved ADR**，只登记为独立的待批准项。
  **精确 amendment proposal**：在 D5 的放弃窗口处补一句 —— 「窗口自该运行的首个交付工作项被放出起算，因此一个
  从未取得上传授权、从未产生任何交付结果的运行同样收敛（不需要为"能力未配置"发明失败码或新的 `deliveryState` 取值）」。
  **为什么必须单独批准**：它是对 approved ADR 的文本修改，且 B-4「不新增第四条删除触发条件」这一结论建立在该澄清
  之上。**不阻塞**本轮的收敛结论，但**阻塞** B-4 的最终批准（B-4 的推荐文本已把这一点标注为「见修订记录中的
  IssueRun D5 澄清项」，人类可以与其他项一并批准）。

- **G-034 — NEW / OPEN / NON-BLOCKING / LIFECYCLE RESIDUE（Revision ADR Decision & Amendment Round, 2026-10-08）** —
  交付工作项在放弃后**不会被清理**：`execution_work` 行在**全仓没有任何删除者**（无 `DELETE FROM execution_work`），
  而认领路径（`agent_work_claim`/`agent_work_get`/`agent_work_pending`）**没有运行阶段过滤**，因此一个已经
  `releasing`/`done` 的运行的**未登记** `deliver_revision` 工作项仍可被认领并派发。终态结果本身无害
  （`deliverySettled` 在 `phase != 'delivering'` 时是确定性 no-op、零写入），但① 白白消耗一次执行，② 该执行可能对着
  正在被删除的 Workspace 跑，③ 会留下一条永不完结的执行记录。**本轮不修**：属实现行为，且修复形状取决于 B-4 与
  G-032 的最终批准结果（在认领侧加运行阶段谓词，或在放弃/终态时终结未登记的工作项）。**不阻塞**任何批准项。
  **Update (Revision Completion Slice, 2026-10-08)**：人类明确 **G-034 暂不批准任何修复方式、也不纳入本切片**，
  因此本轮**未**为它做任何改动——既没有加认领侧谓词、也没有新增 `execution_work` 的终结者、**未**发明后台清理机制；
  状态仍是 `OPEN / NON-BLOCKING`。（本轮新增的证据只加固了它的「无害」半边：`TestLateDeliveredRevisionRegistersNothingOnAReleasedRun`
  证明已释放运行收到交付结果时零写入。）

- **G-027 — OPEN / NON-BLOCKING / ADR PRECISION（Phase 5 Batch 1, 2026-10-08）** — Thread D4 的末行只说「会话执行终态被接管 ⇒ `ended`」，
  **未**写明该转换的**活状态集合**，也**未**写明终态事件落在已 `ended` 或非会话阶段（`provisioning`/`releasing`）时的行为。
  本轮的判定依据是 D4 不变量 4 的**无条件**措辞 + IssueRun D3 的 `delivering` 进入条件「任何结束原因」，因此把
  `pending | active | idle | ending` 全部接受，把其余（已 `ended`/`delivering`、`provisioning`、`releasing`）按 invariant
  violation 处理（`UNAVAILABLE`、零写入、Node 重放）。**为什么登记而不是自行改 ADR**：ADR 修订需人类批准。
  **需要什么**：在 Thread D4（或 controller-integration D6）写明「活状态集合」与前置于终态/非会话阶段时的行为，
  使该矩阵不必依赖对不变量 4 的解释。**不阻塞**本轮完成。证据见 `TestSessionEndedPreconditionMatrix`。

- **G-028 — OPEN / NON-BLOCKING / PRE-EXISTING LINT BASELINE（observed by the Phase 5 Batch 1 round, 2026-10-08）** —
  `internal/core/space_agents.go` 的 `activeSpaceAgentRoster` **无任何调用者**（全仓含 HEAD 零引用），`task lint` 因此报
  `unused` 1 条。该函数在 `73c2aa4` 引入时即无调用者，**不是**本轮产物：在 `git archive HEAD` 的未改动树上运行同一
  `golangci-lint` 配置得到**逐字相同**的 1 条报告。它与 `internal/core/agent_target.go` 的租户级 roster 读
  （`SELECT id::text, display_name FROM space_agents WHERE tenant_id=$1 AND status='active' …`）职责重叠。
  **为什么不在本轮清理**：删除他人已文档化的读取助手属于 Batch 1 范围之外的行为变更；加 `//nolint` 等于弱化门禁（AGENTS.md 禁止）；
  接线成公开读取面则是新 API。**需要什么**：由拥有 Space Agent roster 读取面的轮次决定接线或删除。
  **不阻塞**本轮完成，但**是本仓 `task lint` 全绿的唯一剩余项**。
  **Update (Revision Completion Slice, 2026-10-08)**：`task lint` 仍只剩这一条（本轮另有一处新引入的 `sqlclosecheck`
  已按**修复根因**处理——`integration/migration_upgrade_path_test.go` 的 `rows.Close()` 改为 `defer rows.Close()`，非
  `nolint`、非删除断言）；在 `git archive HEAD` 的未改动树上运行同一 `golangci-lint` 配置仍得到**逐字相同**的 1 条。

- **G-035 — OPEN / NON-BLOCKING / IMPLEMENTATION DEVIATION FROM AN APPROVED ADR（found by the Revision Completion
  Slice, 2026-10-08）** — Cloud Revision **D5 要求 `GET .../runs/{rid}` 的结果中包含 Revision 元数据**（最终 commit、
  是否有改动、对象大小、`deliveryState`），而实现只把 `{revisionId, deliveryState}` 写进 `issue_runs.result`，该列在
  契约里是 `{type: object, additionalProperties: true}` 的自由袋（`internal/contract/openapi.go` 的 `IssueRun`），
  **没有任何 Revision 投影**：`final_commit`、是否改动（`bundle` 三列是否为 NULL）与两个对象的大小都不出现在公开读上。
  **为什么登记而不是就地修**：这是契约变更——`internal/contract` + `api/openapi.json` 的 schema 要新增字段，按
  `cloud/AGENTS.md` 必须同批 `task frontend:generate` 并提交两个生成物（含前端客户端），而本轮授权范围是
  `object_store` / `0023` / 授权 / 校验 / 登记 / 重放 / 冲突，**不含**公开读 schema 变更；也不得为让它变绿而改写
  approved D5 的措辞。**需要什么**：一轮把 D5 的四项元数据加进 `GET .../runs/{rid}` 的契约（新增类型化字段而非
  继续依赖 `result` 的自由袋），同时 `task frontend:generate` 并补契约/集成测试。**不阻塞**本轮完成；目前公开读
  **事实上**不泄露对象键或 URL，所以 D5 的「只暴露元数据」这一半是成立的，缺的是元数据本身。
  核心用例证据状态见 `specs/test-cases/cloud/revision/object-store-and-revision-registration.md`（该行 `Missing`，
  并已写明这是实现缺口而不只是测试缺口）。

- **G-036 — OPEN / NON-BLOCKING / CONTRACT DECISION NEEDED（found by the Revision Completion Slice, 2026-10-08）** —
  Cloud Revision **D1 要求「健康检查的依赖列表报告对象存储未配置」**（运维可见性，明确**不是**运行语义分支），
  而已发布的 `/healthz` 是严格契约 `{status: ok}` / 503 `Error`（`internal/contract/openapi.go`），既没有「依赖列表」
  这个概念，也**不得**泄露配置。**为什么登记而不是自行扩展**：扩大健康检查的响应形状是路径级契约变更（会触动
  OpenAPI 校验器、前端生成物与既有的健康检查测试），且 D1 的这句话本身就是一个尚未定稿的形状选择——把它做成
  `{status, dependencies:{...}}` 还是新增 `/readyz` 需要一次明确的决策。**需要什么**：与 D1 一并重新评审健康检查契约，
  并在同一变更里更新 OpenAPI / 前端生成物 / 测试。**不阻塞**本轮完成：未配置对象存储的运行语义**已**如实体现在
  `UNAVAILABLE` 与 D5 的收敛上，缺的只是运维可见性这一半。

- **G-037 — OPEN / BLOCKING FOR THE THREE MINIO-BACKED OBLIGATIONS ONLY / TEST EVIDENCE（Revision Completion
  Slice, 2026-10-08）** — approved Cloud Revision ADR 的三项义务**没有直接证据**，因为本轮**没有可达的对象存储端点**：
  ① 授权只作用于声明的键与方法（用签发的 URL `PUT` 声明键成功、其他键/其他方法/过期 URL 被拒）；② 摘要头被强制
  （内容与 `x-amz-checksum-sha256` 不符的 `PUT` 被对象存储拒绝）；③ 存在性与摘要校验（对象缺失、大小不符、摘要不符
  三面各被拒）。`specs/AGENTS.md` 明确「只有测试失败会直接说明该验证义务不成立时，才把测试列为证据」，因此本轮的
  本地 HTTP 替身**不充数**——那三项在
  `specs/test-cases/cloud/revision/object-store-and-revision-registration.md` 里保持 `Missing`，其余义务按实际断言逐条
  标为 `Covered` / `Partial` 并写明理由。**ADR 落地顺序第 3 步（cluster Compose 增加 MinIO + Cloud 的 `object_store`
  配置）本轮未完成**：MinIO 镜像拉取失败（`docker.io` 连接被重置），因此连「配置能被真实端点消费」都未验收。
  另有一项**随该验收一并裁定的实现风险**：预签名 PUT 无法把 `x-amz-checksum-sha256` 纳入签名，而真实 AWS S3 要求
  所有 `x-amz-*` 头参与签名——即义务 ② 在 MinIO 上成立、在真实 S3 上可能需要改形状（例如改用 `x-amz-checksum-*`
  以外的机制或在 `HEAD` 侧兜底）。**需要什么**：可拉取镜像的环境 + 一轮以真实 MinIO 补三项直接证据，并在同一轮
  裁定 S3 侧的摘要强制方式。**不阻塞**本轮其余全部完成面；它阻塞的是「三项义务标 `Covered`」这件事。

- **G-020 — OPEN / NON-BLOCKING / DEPENDENCY（Phase 4C design round, 2026-10-08）** — SSE invalidation delivery is
  **not guaranteed**: the hub is in-process, buffered (8), single-instance, and may drop, duplicate or reorder; it
  has no persistence and no replay (api-boundary ADR, implemented). **Why recorded instead of ignored**: a design
  that assumes reliable notification would invite clients to treat the stream as the log. **What is needed**:
  slice 1 relies on the client reconnecting and polling (a frontend obligation); multi-instance delivery needs a
  broker behind the same Publish/Subscribe boundary and a separate decision.
  **Update (Phase 4C Final Completion Batch, 2026-10-08)**: **still OPEN**. 本批次新增的 `issue_run.thread_changed`
  与既有的 `issue_run.thread_appended` 走**同一条**进程内 hub，因此多实例投递、重放与 broker 的问题一字未变；
  新增的只是「哪些提交必须发提示」这一语义。前端重连/轮询兜底仍未实现（无 Thread 面板），对应核心用例保持
  `Missing`/`Partial`。它**不阻塞** Phase 4C 完成。

- **G-021 — OPEN / NON-BLOCKING（Phase 4C design round, 2026-10-08）** — partitioning of `thread_commands`
  delivery across multiple Controller workers is listed as unsolved by the controller-integration ADR itself. 4C's
  seam and table do not change the problem and only guarantee "at least once + idempotent registration" for a single
  worker. **What is needed to close**: a separate decision on per-run partitioning / lease ownership.
  **Update (Phase 4C Final Completion Batch, 2026-10-08)**: **still OPEN and unchanged** — 本批次没有改
  `ClaimThreadCommands` 的分区语义，`thread_commands` 仍是单 worker 的「至少一次 + 幂等登记」。
  它**不阻塞** Phase 4C 完成。

### G-022..G-024 — conflicts found by the Phase 4C readiness round（2026-10-08）

> Registered under mandate §4（"如发现决策冲突，先登记 Decision/Gap，不得静默修改设计"）。These were **not** applied to any
> ADR when registered; §4R.7 listed the proposed amendments as **待批准**. **All of them are now decided**: A1 was approved
> and applied in the ADR Approval Decision round, and A2/A4 were approved and applied in the Final Completion Batch
> (both are members of this group — see their bullets below and §15's Final Completion Batch round).

- **G-022 — CLOSED BY A1**（2026-10-08 由人类批准 A1 后关闭；原分类 `OPEN / NON-BLOCKING / NEEDS ADR AMENDMENT`） — `thread_entries.status`
  (`queued | delivered | discarded`) is required by D-4C-01/04/05/09, but Thread **D1 enumerates the columns of
  `thread_entries` and does not include `status`** (`source`, `kind`, `record jsonb`, `turn_id`, `node_execution_id`,
  `node_sequence`, `created_at`). Thread D3 *names* the states (`queued`/`delivered`/`discarded`) but never places the
  column in D1's schema. Migration 0018 already shipped the table without it. **Why it matters**: adding a column to an
  enumerated schema is a persistence-schema change, which §14 classifies as architectural. **Why non-blocking**: the
  semantics are already approved (D3 + invariant 4); only the column's existence in D1 is unstated, and no writer of
  `source='user'` rows exists today, so the migration's CHECK validates trivially. **What is needed to close**: the
  Thread ADR amendment A1 in §4R.7; migration 0022 must additionally not *rely* on "there happen to be no rows"
  (backfill `status='queued'` for any `source='user'` row before adding the CHECK, or assert the premise explicitly).
  **Update (Phase 4C Slice 1 implementation round, 2026-10-08)**: the *migration* half of that condition is now satisfied —
  `0022_thread_api_and_commands.sql` adds the column, backfills `status='queued'` for any pre-existing `source='user'` row
  **before** adding the CHECKs, and T4C-5 proves the upgrade path on a database that already has such a row. The gap itself
  **stays OPEN**: it is about D1's enumeration, and amendment A1 is still 待批准. No ADR file was modified and no `status` was changed.
  **Update (ADR Approval Decision round, 2026-10-08)**: the human approved **A1 only**. The amendment was applied to Thread D1's
  `thread_entries` column enumeration and its explanation; **G-022 is now CLOSED BY A1**. Only the ADR text changed —
  **production behavior changed: NO**, **migration changed: NO**, **Phase 5 boundary unchanged**（`discarded` 仍只属 Phase 5）.
  A2 (G-024)、A3 (G-017)、A4 (G-023)、G-018 仍为 **OPEN / READY FOR APPROVAL**，未被自行批准。

- **G-023 — OPEN / NON-BLOCKING / NEEDS ADR AMENDMENT** — D-4C-07/08 publish `issue_run.thread_appended` after **any**
  commit that changes the run's visible Thread state, including a commit that only flips a `thread_entries.status` or
  `thread_state` and appends no entry. Thread **D5 says the event is published after the "条目写入事务" commits**
  ("条目写入事务提交后…发布"), which reads as entry-append-only. **Why it matters**: the event name says "appended";
  publishing it for a status-only change is a contract statement clients will rely on. **Why non-blocking**: the payload
  (`issueId`, `runId`, `lastSeq`) is unchanged and the event is only a hint, so the change is additive for clients.
  **What is needed to close**: amendment A4 in §4R.7. Until it lands, S6 publishes only for commits that append an entry.
  **Update (Phase 4C Final Completion Batch, 2026-10-08)**: the human approved **A4**, and it landed as a **new** event
  rather than by widening the old one: `issue_run.thread_changed{issueId, runId, lastSeq}` 覆盖任何改变 Thread REST
  表示的提交（条目追加、轮次 `status` 变化、`threadState`/`idleSince` 变化），`issue_run.thread_appended` 的名称、
  负载与语义**逐字不变**；两者都只在提交后发布，同一次提交各至多一条（`thread_changed` 对同一提交按 `lastSeq`
  取大去重）。直接证据：`TestThreadChangedCoversStateOnlyCommits`、`TestThreadChangedIsDedupedPerCommit`。
  **G-023 = `CLOSED BY A4`.**

- **G-024 — OPEN / NON-BLOCKING / NEEDS ADR AMENDMENT** — D-4C-03 rejects `POST .../thread/messages` when
  `cancel_requested_at IS NOT NULL` even while `thread_state = 'pending'`. Thread **D3 says the POST is accepted when
  the state is `pending | active | idle`** and names only `ending | ended` as the rejection case. **Why it matters**:
  the conflict is reachable — a cancel issued while the run is `provisioning`/`starting` goes straight to `releasing`
  without ever writing `ending` (IssueRun D4/D6, D-4C-11), so a run can be simultaneously `thread_state='pending'` and
  cancelled. Accepting the message there would persist a user turn that no session will ever execute. **Why
  non-blocking**: the stricter predicate is the correct behavior; only D3's enumeration is incomplete. **What is needed
  to close**: amendment A2 in §4R.7 (extend D3's accept predicate with `cancel_requested_at IS NULL` and the live
  workspace condition), then sync the accept-matrix rows in S4 and the Thread core-test obligation.
  **Batch 1 status (2026-10-08)**: A2 is **still unapproved**, so the two rows are deliberately **not implemented** —
  `requireThreadAccepting` and the `thread_state` CAS in `agent_run_thread_message.go` are narrower than D-4C-03, and
  `TestThreadMessageCancelRowIsDeferred` pins the deferral (a cancelled run with a non-live workspace is still accepted
  today, so landing A2 turns that test red). The rest of the S4 accept matrix is implemented. **This gap stays OPEN.**
  Closing it must not be done by editing the ADR `status` from this round.
  **Update (Phase 4C Final Completion Batch, 2026-10-08)**: the human approved **A2**. Thread D3's accept predicate is now
  `cancel_requested_at IS NULL` **且** `thread_state ∈ pending | active | idle`，两种情况共用同一个 `409 thread_closed`。
  实现同时收窄了**权威生命周期前置条件**（`requireThreadAccepting`）与 **CAS 谓词**（`AND cancel_requested_at IS NULL`），
  因此请求被记录后连并发中的写入者也赢不了；deferral 测试 `TestThreadMessageCancelRowIsDeferred` 已被正式的验收测试
  `TestThreadMessageRejectsAfterCancellationRequested` 取代（`pending`/`active`/`idle` 三态各建夹具，断言零条目、
  零命令、零幂等记录、`thread_state`/`version`/`idle_since` 与 `phase` 全部不变）。**G-024 = `CLOSED BY A2`**；
  ADR 仍**未**声称取消立即终态、立即删 Workspace，也**未**实现 `discarded`。

> **ADR Approval Round update (2026-10-08)**: amendments **A1** (G-022), **A2** (G-024) and **A4** (G-023) are drafted and
> now **`OPEN / READY FOR APPROVAL`** — full texts in §15's `### Round: Phase 4C ADR Approval Round …`. **No ADR file was
> modified and no `status` was changed**; none of the three may be implemented before approval. G-022's migration half
> remains satisfied as described above; G-024's two stricter rows remain deliberately unimplemented.
>
> **ADR Approval Decision update (2026-10-08)**: **A1 (G-022) was approved by a human and applied** to Thread D1 — G-022 is
> now **`CLOSED BY A1`**. **A2 (G-024) and A4 (G-023) remain `OPEN / READY FOR APPROVAL`** and their behavior is unchanged;
> the ADR frontmatter `status` was **not** modified by this round.
>
> **Final Completion Batch update (2026-10-08)**: the human approved **A2 / A3 / A4 / G-018**. All four are written into the
> approved Thread ADR — **A2 into D3**, **G-018 into D4**, **A3 and A4 into D5** — and implemented in the same batch, so
> **G-024 = `CLOSED BY A2`、G-017 = `CLOSED BY A3`、G-023 = `CLOSED BY A4`、G-018 = `CLOSED`**. The ADR frontmatter `status`
> was **not** modified (it was already `approved`), and no migration was needed or written. Still `OPEN` after this batch:
> G-019 (Phase 5 `SessionEnded`/`ending → ended`/`discarded`), G-020 (多实例 SSE), G-021 (多 worker 分区),
> G-025 (migration README 债务), G-026 (`cancel_requested_at` 无生产写入者). See §15's `### Round: Phase 4C Final
> Completion Batch …` for the full evidence and gate record.

### G-025 — migration-list documentation debt（found by the Phase 4C Slice 1 implementation round, 2026-10-08）

- **G-025 — OPEN / NON-BLOCKING / DOCUMENTATION ONLY** — `internal/core/migrations/README.md` and `README.en.md` enumerate
  migrations only through `0017_tenant_membership_and_join.sql`. `0018`–`0021` (Phase 3A/3B and Phase 4B) and `0022`
  (Phase 4C Slice 1) are all missing from that list, so the "what landed in which migration" narrative is stale by six
  files. **Why it matters**: the README is the entry point a reviewer uses to reason about schema history, and §14 treats
  persistence-schema changes as architectural. **Why non-blocking**: the authoritative record is the ordered SQL files
  plus `schema_migrations` checksums, both of which are complete and verified by `CheckSchema`; no behavior depends on the
  README. **Why not fixed in Slice 1**: the debt predates this round by six migrations, and S1's scope is 0022 only;
  documenting `0022` alone would make the list *look* current while still omitting `0018`–`0021`. **What is needed to
  close**: one round that writes `0018`–`0022` into both README files together.
  **Update (Revision Completion Slice, 2026-10-08)**: `0023_revisions.sql` landed this round, so the drift is now
  `0018`–`0023` (seven files). It was **not** fixed here for the same reason: documenting `0023` alone would leave the
  list looking current while still omitting `0018`–`0022`. (Both READMEs also carry a stray duplicate
  `0015_plugins.sql` entry after their checksum/integrity section — same round, same fix.)

### G-026 — `issue_runs.cancel_requested_at` has no production writer（found by the Phase 4C Accelerated Batch 2 round, 2026-10-08）

- **G-026 — OPEN / NON-BLOCKING / MISSING UPSTREAM API** — nothing in Cloud writes `issue_runs.cancel_requested_at` outside
  tests. There is no cancel route in `internal/api/router`, no cancel action in `Store.Control`'s `Public` set, and no
  caller of `ObserveCancelled` other than a test, so a cancellation request can only reach the database out of band. The
  Batch 2 round implemented and tested the *reaction* to a request (`Store.reactToAgentRunCancel` /
  `ReactToCancelledAgentRunsOnce`: D6's no-session release, and `ending` + `EndSession{cancelled}` once a session
  exists), but it invented no public API to create the request — inventing one would be a new contract no approved slice
  defines. **Why it matters**: IssueRun D6's cancel path is reachable in production only once some API records the
  request, so today the reaction is recovery-side only. **Why non-blocking**: the reaction is fail-closed and idempotent —
  it acts only on a request that is durably present, and a run with no request is a deterministic no-op — so wiring the
  writer later changes no behavior of the code already landed. **What is needed to close**: an approved cancel API slice
  (route, authorization, idempotency key, and the `cancel_requested_at` write in its own transaction) plus the
  integration evidence that the request and the reaction meet. This round explicitly did **not** add the column's writer,
  change G-024's deferred POST behavior, or move the cancel path's authority.

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

### Round: Phase 4C Readiness — Architecture Review & Implementation Slice Plan / 2026-10-08

**Plan section executed:**

- Readiness round: `## Phase 4C Readiness — Architecture Review & Implementation Slice Plan`（§4R.0–§4R.8）、
  §13（新增 G-022/G-023/G-024）、§16 marker。**不是**实施轮。

**Status:**

- **Complete**（评审结论：`READY_FOR_PHASE_4C_IMPLEMENTATION_SLICE_1`）

**Files changed:**

- `plan/plan.md`（§4R.0–§4R.8、§13 G-022..G-024、§15 本条、§16 marker）
- `plan/plan-zh.md`（镜像）
- **未改**：production 代码、migration、proto、generated、测试实现、任何 ADR 文件或其 `status`。

**Decisions added/changed:**

- None added；D-4C-01..D-4C-12 全部复核通过（**未静默修改**任何决策）。§4R.7 的 A1–A4 是**待批准**的 ADR 修订提案，
  **未**写入 ADR 文件。

**New gaps:**

- **G-022**（`thread_entries.status` 不在 Thread D1 的列清单中；需 ADR 修订 A1）
- **G-023**（无新条目、仅 `status`/`thread_state` 变化也发 `thread_appended`，与 D5 字面「条目写入事务提交后」冲突；需 A4）
- **G-024**（POST 在 `cancel_requested_at` 已置时拒绝，与 D3 字面「`pending|active|idle` 时接受」冲突；需 A2）
- G-017 由「单一扩展」**重新切分**为「已批准子集 + 4 项响应形状扩展 + 1 项措辞澄清」；G-018 给出端点契约提案。
  三项新缺口均按 mandate §4 **先登记为 Gap**，未静默改设计。

**Tests added/changed:**

- None（本轮禁止改测试实现；T4C-1..T4C-34 仍全部 `DESIGNED / MISSING`，另提议新增 **T4C-35** 供 S7 使用）。

**Gate results:**

- 未运行构建/测试门（本轮为只读评审，无代码改动）。只执行只读 git 检查（`git status --short` × 4 仓库）与文件读取。

**Scope deviations:**

- None。遵守 §6：未 `git add`/`commit`/`push`/PR，未 `reset`/`restore`/`checkout .`/`clean`/`stash`，未覆盖既有未提交修改，
  未改任何 approved ADR 的 `status`。

**Next planned step:**

- **Slice 1** — Migration 0022 + `pending` 物化（指令见 §4R.5 的 S1）。**不依赖** G-017/G-018。

**Git:**

- nothing staged? **YES（无 stage）**
- commit performed? **NO**
- push / PR? **NO**

---

### Round: Phase 4C Implementation Slice 1 — migration 0022 + `pending` materialization / 2026-10-08

**Plan section executed:**

- 实施轮：§4R.5 的 **S1**、§4C.16 的 **T4C-1..T4C-5**、§4C.0 的 D-4C-01（`pending` 物化）、D-4C-12（4C migration 决策）。
  **仅 S1**：S2a–S7 未动。

**Status:**

- **Complete**（S1 交付项全部落地；`task build` / `task test` / `task test:race` 全绿）

**Files changed:**

- 新增 `internal/core/migrations/0022_thread_api_and_commands.sql`（`thread_commands` 表 + `thread_entries.status` +
  两条 CHECK + 两个部分索引 + `pending` 回填 + `issue_runs_idle_threads`）
- `internal/core/agent_run_session_start.go`（`StartSession` 在 seq=1 同一事务内物化 `thread_state='pending'`，CAS 断言恰 1 行；文档注释同步）
- `internal/core/agent_run_session_start_db_test.go`（新增 T4C-1..T4C-4 与 `runVersion` 助手）
- `integration/migration_upgrade_path_test.go`（新增 T4C-5 与 `seedDeclaredAgentRun` 助手）
- `integration/agent_issue_run_skeleton_test.go`（与 0022 的 `thread_entries_user_status` 兼容：既有 user 条目补 `status`，并新增两条约束反例）
- `internal/core/agent_run_thread_takeover_db_test.go`（两处过期注释订正；T4B-19 由「表不存在」改为「接管事务不写入 4C 表」——0022 合法建表，断言必须落在**写入**而非**存在**上）
- `plan/plan.md`、`plan/plan-zh.md`（§15 本条、§16 marker）
- **未改**：Phase 4B 的 running authority、Node/Thread `seq` 分配、receipt、takeover 事务；proto；OpenAPI/generated；任何 ADR 文件或其 `status`。

**Decisions added/changed:**

- None added。D-4C-01 与 D-4C-12 按**已批准**语义落地；**未**静默修改任何决策。

**New gaps:**

- **G-022 仍 OPEN（未关闭）**：Thread D1 的列清单仍无 `status`。本轮实现的 `thread_entries.status` 只依据**已批准**的
  D3 状态集（`queued|delivered|discarded`）与不变量 4；**A1 修订提案未获批准，未写入任何 ADR 文件，未改任何 `status`**。
  升级正确性不依赖 A1：列的语义来自 D3，A1 只是把该列补进 D1 的枚举。
- **G-025（新）**：`internal/core/migrations/README.md` / `README.en.md` 的迁移清单停在 `0017`，`0018`–`0022` 均未登记
  （`0018`–`0021` 为 Phase 4B 遗留，`0022` 属本轮）。纯文档债，不阻塞 S1，未在本轮扩大范围修复。

**Tests added/changed:**

- **T4C-1** `TestAgentSessionStartMaterializesPendingThread` → 声明前 `thread_state IS NULL`、声明后恰为 `pending`，
  且运行仍 `starting`/`dispatched`、恰一条 seq=1、恰一件 `agent_session` 工作。
- **T4C-2** `TestAgentSessionStartPendingIsNotRewrittenOnReplay` → 重放不重写状态、不增条目、不重复放出工作
  （以 `version` 不变为判别证据——状态值本身不变，无法自证「未写」）。
- **T4C-3** `TestAgentSessionStartPendingRollsBackWithSeq1` → A 缝失败时 seq=1 与 `pending` 一起回滚（真实 `Unavailable` 缝）。
- **T4C-4** `TestAgentSessionStartNeverMaterializesPendingForIneligibleRuns` → 已取消 / Workspace 不活的运行零写入。
- **T4C-5** `TestMigration0022ThreadCommandsAndPendingAppliesFreshAndUpgrades` → `0021`→`0022` 升级路径 + 二次 `Migrate`
  幂等 + `CheckSchema`；回填只命中「有 seq=1 真实声明」的运行并排除取消/终态/无声明；`thread_commands` 的主键、FK、
  两条 CHECK 与三个部分索引形状；既有 `source='user'` 条目被安全回填为 `queued`。
- 回归：`TestAgentIssueRunSkeletonSchemaConstraints`（既有 user 条目补 `status`，新增 2 条约束反例）、
  `TestThreadTakeoverHasNoLaterPhaseSideEffects`（改为断言接管事务零写入）。
- **变异验证（非提交内容，仅用于证明 T4C-5 非空转）**：把回填谓词换成 `phase IS NOT NULL` 式阶段代理 → 用例失败
  （`run without a declaration stays NULL`）；删去 `cancel_requested_at IS NULL` → 用例失败；删去 CHECK 前的
  `status='queued'` 回填 → 升级以 `23514` 失败。三条变异均被捕获，随后逐字还原。

**Gate results:**

- `go vet ./internal/core/ ./integration/`：PASS
- `task build`：PASS
- `task test`（真实 PostgreSQL，`REQUIRE_POSTGRES=1`）：PASS（全部包 `ok`，`integration` 47.4s）
- `task test:race`：PASS（`integration` 118.5s，无 `DATA RACE`）
- `task format:check`：**FAIL（既有，与本轮无关）**——5 个 **HEAD 未修改**文件在当前 gofumpt 下不合规
  （`agent_run_control.go`、`agent_run_control_test.go`、`agent_run_settle_db_test.go`、`agent_run_terminal_db_test.go`、
  `agent_target.go`）。本轮触碰的 6 个 Go 文件全部 gofumpt-clean。
- `task lint`：**FAIL（既有，与本轮无关）**——余下 7 项全部落在 HEAD 未修改文件（gocritic×3、gofumpt×3、unused×1）；
  本轮引入的 `sqlclosecheck` 1 项已修复。
- `git diff --check`：本轮文件无新增空白错误（`plan-zh.md:873` 的尾随空格为上一轮遗留，本轮顺手订正）。

**Scope deviations:**

- **1) 前置检查 1 无法满足**：G-022 对应的 Thread ADR D1 修订**未获批准**。按 mandate「先输出最小 ADR 修订提案，
  不得自行将 ADR 标记为 approved」，本轮在最终报告给出 **A1 提案**并**未改任何 ADR**；实施依据是**已批准**的 D3、
  不变量 4 与 D-4C-12。
- **2) 修改了 3 个既有测试文件**（`integration/agent_issue_run_skeleton_test.go`、`internal/core/agent_run_thread_takeover_db_test.go`、
  本轮的会话启动测试）。原因：0022 新增的约束与 4B 时期「表不存在 / 条目无 status」的断言直接冲突。改动为**约束兼容与
  注释订正**，**未**放宽任何 CHECK、**未**跳过任何断言、**未**改动 4B 语义。

**Next planned step:**

- **S2a** — Thread GET 的**已批准子集**（`after` / `limit ≤ 500` / 升序 / `threadState`）。S2b（无游标 tail、`before`、
  `idleSince`、`nextCursor`/`prevCursor`）需先完成 ADR 修订 **A3**。

**Git:**

- nothing staged? **YES（无 stage）**
- commit performed? **NO**
- push / PR? **NO**

---

### Round: Phase 4C Accelerated Implementation Batch 1 — S3 (Thread Command Control Plane) + S4 (Thread POST) / 2026-10-08

**Plan section executed:**

- 加速实施轮（单 Coding Agent，连续推进，**不**在 S3 与 S4 之间结束本轮）：§4R.5 的 **S3** 与 **S4**、§4C.3/§4C.4/§4C.5/§4C.6
  的契约、§4C.14/§4C.15 的错误与鉴权、§4C.16 的 **T4C-13..T4C-18**（S4）与 **T4C-21..T4C-24**（S3）。
- **不在本轮**（mandate §30 明列）：S2a/S2b（Thread GET）、S5（echo→`delivered`、`active⇄idle`）、S6（SSE
  `issue_run.thread_appended`）、S7（`/thread/end`、idle 扫描、`ending→ended`、`SessionEnded`、`queued→discarded`）、
  `DeliverRevision`、Phase 5。S3 的 `ThreadCommandAvailable` 是**内部 Controller 信号**，与 S6 的公开 Space SSE 无关。

**Status:**

- **Complete（S3 完整；S4 core 完整）**。授权标记：`PHASE_4C_ACCEL_BATCH_1_CORE_DONE_WITH_ADR_DEFERRED`——
  S4 接受矩阵中由 **D-4C-03 新增、A2 修订仍未批准**的两行（`cancel_requested_at IS NOT NULL`、运行 Workspace 不活）
  **未实现**，以 G-024 保持 OPEN + 钉住该行的测试与注释交付（下文 Scope deviations）。

**Files changed:**

- **S3（控制面 + A 缝）**：
  - `internal/core/agent_run_thread_command.go`（新）：`EnqueueThreadCommand`（调用方事务内 `INSERT thread_commands`，
    A 生成 `command_id`，`created_at` 用数据库时钟，`delivered_at`/`delivered_execution_id` 初值 NULL）、
    `agentThreadClaim`（纯读 `AgentRunControlPlane` 的 `agent_thread_claim` action）、`agentThreadDelivered`
    （`agent_thread_delivered`）、`SubmitUserTurnCommand`、`contentObjects`。
  - `internal/controlgrpc/agentruns.go`、`internal/controlgrpc/agentruns_test.go`（新）：`ClaimThreadCommands` /
    `RecordThreadCommandDelivered` 的 proto 转换与错误码映射（复用既有 Fault/gRPC 码，**未新增 proto 枚举**）。
  - `internal/controlgrpc/server.go`、`internal/controlgrpc/signals.go`、`internal/core/signals.go`、`internal/core/store.go`、
    `cmd/server/main.go`：`ThreadCommandAvailable` 的**提交后**发布接线（`transact` 成功分支）。
  - `internal/core/agent_run_thread_command_db_test.go`（新）：T4C-21..T4C-24 白盒 DB 测试。
  - `integration/agent_run_thread_command_grpc_test.go`（新）：端到端 gRPC 投递回路。
- **S4（公开 Thread POST）**：
  - `internal/core/agent_run_thread_message.go`（新）：`appendThreadMessage`、`threadMessageContent`、
    `requireThreadAccepting`。
  - `internal/api/router/router.go`：路由 `POST /api/v1/tenants/{tid}/issues/{iid}/runs/{rid}/thread/messages`
    （`threadMessageBodyLimit = 256 KiB` 传输上限；未知字段/类型/JSON 由既有严格解码管线拒绝）。
  - `internal/core/public.go`：公开分派 + `Idempotency-Key` 前置校验（`400 idempotency_key_required`）与同事务
    幂等记录读写。
  - `internal/contract/openapi.go`、`api/openapi.json`、`frontend/src/api/generated.schemas.ts`、
    `frontend/src/api/tenants/tenants.ts`：契约源 + `task frontend:generate` 产物（**未手工编辑**）。
  - `integration/agent_run_thread_message_test.go`（新，约 600 行）：T4C-13..T4C-18 真实 HTTP + PostgreSQL 验收。
- `plan/plan.md`、`plan/plan-zh.md`（§15 本条、§16 marker）。
- `specs/test-cases/cloud/controller-integration/agent-run-executions-and-thread.md`（仅把**已被真实测试直接证明**的
  三条义务由 `Missing` 改为 `Covered`；`授权不持久化` 保持 `Missing`）。
- **未改**：Phase 4B 的 running authority / `seq` 分配 / receipt / takeover 事务；proto；任何 ADR 文件或其 `status`；
  `specs/test-cases/cloud/thread/durable-thread.md`（其 S2/S6 义务仍全 `Missing`）。

**Decisions added/changed:**

- None added。S3/S4 均按**已批准**的 Thread D3、controller-integration D3/D5/D6、D-4C-04/D-4C-05/D-4C-06 落地；
  **未**静默修改任何决策。D-4C-03 的两行因 A2 **未获批准**而**未实现**（见 Scope deviations）。

**New gaps:**

- **G-024 仍 OPEN（未关闭）**：D-4C-03 的接受谓词还拒绝 `cancel_requested_at IS NOT NULL` 与运行 Workspace 不活，
  且其 CAS 带 `cancel_requested_at IS NULL`；**已批准**的 Thread D3 只把 `ending | ended` 列为拒绝情形。ADR 修订
  **A2 未获批准**，故这两行**未实现**，`requireThreadAccepting` 有意窄于 D-4C-03，并以
  `TestThreadMessageCancelRowIsDeferred` 钉住（A2 一旦落地，该测试转红，后续实现不可能静默通过）。
- **G-021 仍未解决（有意）**：`ClaimThreadCommands` 是**单 worker** 保证，命令空间未跨 worker 分区；本轮不解决。
- **G-022 仍 OPEN**：与 S1 相同，**未**改任何 ADR `status`。

**Tests added/changed:**

- **S3 — T4C-21** `TestThreadCommandEnqueueIsCallerTransactionScoped` → `EnqueueThreadCommand` 只写调用方事务；
  调用方回滚则命令一并消失（同事务原子）。
- **S3 — T4C-21** `TestThreadCommandEnqueueRejectsInvalidCommands` → 非法 `kind` / 缺 `body` / 缺 `turn_id` /
  空 content / 非批准 end 原因一律返回错误，不落行。
- **S3 — T4C-22** `TestThreadCommandClaimWaitsForRegisteredExecution` → 会话执行未登记时命令留在 Cloud、不被领取
  （`JOIN LATERAL ... kind='agent_session'` 门控）；`TestThreadCommandClaimGroupsByRunInCreationOrder` → 按 run 分组、
  组内创建序。
- **S3 — T4C-23** `TestThreadCommandDeliveryIsIdempotentAndFenced` → 首次登记生效；同执行重放收敛幂等（返回原成功）；
  异执行 `CONFLICT`、**绝不**复写。
- **S3 — T4C-24** `TestThreadCommandSignalOnlyAfterCommit` → 提交后恰一次 `ThreadCommandAvailable`；回滚零信号
  （确定性注入 observer，**无 sleep**）。
- **S3 端到端** `TestControlGRPCThreadCommandDeliveryLoop` → 经真实 gRPC 领取 → 不登记 → 再领取得到**同一**
  `command_id`；登记后不再返回；`turn_id` 只在命令体内、与 `command_id` 各自稳定互不代偿。
  `TestControlGRPCThreadCommandRejectsUnknownDelivery` → 未知命令 / 非本 run 执行一律拒绝。
- **S3 单元** `TestThreadCommandRendersDurableBody` → 持久化的 `body` 是规范形（`{kind, body}`，命令体只带
  `turn_id`/`content`、不带 `command_id`），proto 转换由该层完成；`TestWatchResponseCarriesThreadCommandAvailable`
  → `WatchResponse` 的 `thread_command_available` 变体携带 `run_id`。
- **S4 — T4C-13** `TestThreadMessagePostAcceptsPendingActiveAndIdle`（3 子测试）→ `pending|active|idle` 均 201；
  写 `source='user'`/`kind='user_turn'`/`status='queued'` 条目、`thread_state='active'`、`idle_since NULL`、
  一条未投递 `submit_user_turn` 命令、响应 `turnId` == 条目 `turn_id` == 命令体 `turn_id`。
- **S4 — T4C-14** `TestThreadMessagePostReplaysUnderTheSameKey` → 逐字节相同响应；条目/命令/幂等记录 **+0**。
- **S4 — T4C-15** `TestThreadMessageConcurrentSameKeyCreatesOneTurn` → 显式 barrier + WaitGroup（**无 sleep**）：
  两请求均 201 且响应逻辑相同，恰 1 条目 + 1 命令。
- **S4 — T4C-16** `TestThreadMessagePostRejectsIdempotencyConflicts` → 同 key 异 body → `409 idempotency_conflict`；
  同 key 用于另一 run → 409，且第二个 run 零条目。
- **S4 — T4C-17** `TestThreadMessageDistinctKeysCreateIndependentTurns` → 两个独立 `turn_id`、seq 1 与 2、2 条目 2 命令。
- **S4 — T4C-18** `TestThreadMessageSeamFailureRollsBackEverything` → 缝未接线 ⇒ `503 thread_command_unavailable`，
  条目/命令/`thread_state`/`version`/幂等记录**全部回滚**；换真实控制面后用**同一 key** 重试 → 201（干净首发）。
  `TestThreadMessageRejectsMalformedAndOversizedRequests` → 缺 key / 坏 JSON / 多余 JSON 值 / 未知字段 / 类型错 /
  非 text 块 / 64 KiB+1 → 对应稳定 Fault；恰 64 KiB → 201 且完整落库。
  `TestThreadMessagePostRejectsClosedAndUnknownRuns` → `ending|ended` ⇒ 409 `thread_closed`；跨 tenant / 跨 Issue /
  未知 / 软删 / team run ⇒ 404 `not_found`；非成员 ⇒ 403 `membership_required`（D3「与评论授权一致」）。
  `TestThreadMessagePostInvariantBreakIsInternal` → 运行中却 `thread_state=pending`、或已启动却无 Thread 状态 ⇒
  500 `internal_error`，Thread 不被推动（**不**伪装成 409）。
  `TestThreadMessageCancelRowIsDeferred` → **G-024 钉**：`cancel_requested_at` 已置 + Workspace 非活仍 201 且
  `thread_state='active'`（A2 落地即转红）。
- **回归**：Phase 3B/4A/4B/S1 既有测试全绿（`task test` / `task test:race`）。
- **测试编号对齐**：mandate 对 S4 的 T4C-13..18 有自己的逐条编号（首发成功 / 同 key 重放 / 同 key 并发 / 同 key 异
  body-run 冲突 / 异 key 同 body / 缝失败回滚），而 §4C.16 设计矩阵把 T4C-13..18 编为另外六条义务（接受矩阵 / 条目+命令
  同事务 / 缝失败整体回滚 / 幂等 7 例 / 同 key 并发 / `turn_id` 三处一致）。两套编号**指向同一组测试**，逐条对应为：
  §4C.16 **T4C-13** ← `TestThreadMessagePostAcceptsPendingActiveAndIdle` + `TestThreadMessagePostRejectsClosedAndUnknownRuns`
  + `TestThreadMessagePostInvariantBreakIsInternal`（mandate 的「首发成功」= 其中 201 臂）；§4C.16 **T4C-14** ←
  `TestThreadMessagePostAcceptsPendingActiveAndIdle`（mandate 的「同 key 重放」另由 `TestThreadMessagePostReplaysUnderTheSameKey`）；
  §4C.16 **T4C-15** ← `TestThreadMessageSeamFailureRollsBackEverything`（= mandate T4C-18）；§4C.16 **T4C-16** ←
  `TestThreadMessagePostReplaysUnderTheSameKey` + `TestThreadMessagePostRejectsIdempotencyConflicts` +
  `TestThreadMessageDistinctKeysCreateIndependentTurns`（= mandate T4C-14/16/17）；§4C.16 **T4C-17** ←
  `TestThreadMessageConcurrentSameKeyCreatesOneTurn`（= mandate T4C-15）；§4C.16 **T4C-18** ←
  `TestThreadMessagePostAcceptsPendingActiveAndIdle`（mandate T4C-13 的 `turnId` 断言）。**两套编号的六条义务均已覆盖。**

**Gate results:**

- `go build ./...`：PASS
- `task build`：PASS
- `task test`（真实 PostgreSQL，`REQUIRE_POSTGRES=1`）：PASS（exit 0，全部包 `ok`）
- `task test:race`：PASS（无 `DATA RACE`）
- `task format:check`：**FAIL（既有基线，与本轮无关）**——5 个 **HEAD 未修改**文件（`agent_run_control.go`、
  `agent_run_control_test.go`、`agent_run_settle_db_test.go`、`agent_run_terminal_db_test.go`、`agent_target.go`）。
  本轮**新增**的 1 项（`agent_run_thread_command.go` 的参数合并）已修复，门禁回到与基线**完全相同**的 5 文件。
- `task lint`：**FAIL（既有基线，与本轮无关）**——余下 7 项全部落在 HEAD 未修改文件（gocritic×3、gofumpt×3、
  unused×1 `activeSpaceAgentRoster`）。本轮引入的 2 项（`behaviour`、`unrecognised` misspell）已修复。
- `task frontend:generate`：PASS（`api/openapi.json` + `frontend/src/api` 仅**增量**变化；md5sum 复跑证明生成**幂等**，
  第二次运行零变化）。**无任何手工编辑**。
- `task frontend:check`：漂移步骤（`git status --porcelain -- frontend/src/api`）**预期失败**——重新生成的客户端处于
  未提交状态，而本轮禁止 commit。已用 md5sum 前后对比证明生成稳定后，直接运行 `npm --prefix frontend run check`：
  **EXIT=0**（53 测试文件 / 299 测试通过，行覆盖 90.93%）。**未**跳过生成、**未**手工编辑产物。
- `git diff --check`：本轮文件无新增空白错误。

**Scope deviations:**

- **1) G-024 的两行未实现（按 mandate 显式延后，非静默）**：指令要求「若 A2 仍未批准，则**不要**静默实现它——
  在 plan 中保持 G-024 OPEN，实现 S4 其余契约，并为该冲突矩阵行保留显式测试/注释或延后证据」。本轮即如此执行：
  `requireThreadAccepting` 与 `AppendThreadMessage` 的 CAS 均**有意**窄于 D-4C-03（不带 `cancel_requested_at IS NULL`），
  注释中显式标注 A2 未批准，`TestThreadMessageCancelRowIsDeferred` 钉住该行。
- **2) `phase='running' + thread_state='pending'` 归为 500 而非 409**：§4C.4 的接受矩阵中含该行，但它不是客户端可
  作用的状态——`phase='running'` 的唯一写者（首条记录接管）在**同一条语句**里写 `active`，故该组合是不可达的
  自身不变量破坏。按 §4C.15「内部错误不得伪装成客户端可解释的冲突」返回 500。`thread_state` 缺失同理。
- **3) 非成员返回 403 而非 404**：`membership()` 既有语义（`store.go`）与 plan §4C.15/T4C-11 的切分一致
  （跨 tenant / 跨 Issue / 软删 run ⇒ 404；**非成员 ⇒ 拒绝**，与评论授权一致）。**未**改动既有授权语义。
- **4) 传输上限取 256 KiB**：Thread D3 的 64 KiB 是**解码后文本**上限（在 core 校验，`400 content_too_large`），
  JSON 信封不计入；故路由层 `MaxBytesReader` 取 `threadMessageBodyLimit = 256 KiB`，超传输上限降级为
  `400 invalid_json`。**未**放宽 D3 的文本上限。

**Next planned step:**

- **S5** — echo→`delivered` + `active⇄idle` 生命周期。**S2a**（Thread GET 已批准子集）可在 S5 前并行；
  **S2b/S6/S7** 仍需先完成 ADR 修订 **A2**（G-024）/ **A3**（G-017）/ **A4**（G-023）。

**Git:**

- nothing staged? **YES（无 stage）**
- commit performed? **NO**
- push / PR? **NO**
- destructive git（`reset`/`restore`/`checkout .`/`clean`/`stash`）? **NO**

---

### Round: Phase 4C Accelerated Implementation Batch 2 — S5 (user-turn echo + `active⇄idle`) + S7 (ending triggers, stopped at `ending`) / 2026-10-08

**Plan section executed:**

- 加速实施轮（单 Agent，连续推进 S5 → S7，中途不结束本轮）：§4C.9（D-4C-09 `active`/`idle` 权威）、§4C.10（D-4C-10
  `ending` 权威）、§4C.11（D-4C-11 取消与终态）、§4C.13 的 **1/2/7/8**（echo 去重推广、`thread_state` 四条 4C 分支与
  「0 行只有在事前判定为可写时才是错误」、idle 扫描 + `cmd/server` 接线、不动 list）、§4C.14 的 **2/5/6/7/8**、
  §4C.16 的 **T4C-19/20/28/29/30**（S5）与 **T4C-31/32/33/34**（S7）。
- **不在本轮**（mandate §12/§30 明列）：S2a（Thread GET）、S2b、S6（公开 Space SSE）、G-023、Phase 5、`SessionEnded`、
  `queued → discarded`、`deliver_revision`、`ending → ended`、`running → delivering`。**T4C-35（`/thread/end` 端点）
  未实现**：G-018 仍是**未批准**的 ADR 修订，endpoint 形状无已批准契约，故 `user_ended` 不可达且不实现。

**Status:**

- **Complete（S5 完整；S7 除 G-018 门禁项外完整）**。授权标记：
  **`PHASE_4C_ACCEL_BATCH_2_CORE_DONE_WITH_G018_DEFERRED`**。S5 达到 checkpoint 时记录
  **`S5_IMPLEMENTATION_CHECKPOINT_REACHED`**（S5 定向测试 + Phase 4B 接管回归 + Batch 1 S3/S4 套件全绿；**未 commit**），
  随后继续 S7。

**S5 — 用户轮次 echo 与 `active⇄idle` 生命周期（`internal/core/agent_run_thread.go`）：**

- **用户轮次 echo 从「仅首提示」推广到所有 Cloud 生成的用户轮次**（D-4C-08）：批次中某事件的 `turn_id` 命中已存在的
  `source='user'`/`kind='user_turn'` 行时，接管事务只做 `queued → delivered` 的 CAS；**不**新增条目、**不**分配 seq、
  **不**改写原 `record`/`content`/`turn_id`、**不**新建命令、**不**重新入队。已 `delivered` 的行是相同内容的重放 ⇒
  逐字节不动，`delivered` **永不回退**为 `queued`（行级 CAS 只写 `status='delivered'` 一条路径）。首提示 echo（seq=1，
  `source='system'`）行为**不变**：仅收据、不产生条目、不消耗 seq。
- **批次后生命周期**（D-4C-04/D-4C-09）：仅在**本事务提交了真实 Node 记录**（`taken > 0`）且事后权威重读为
  `phase='running' AND status='running'` 时才判定；`active` + 批**末条有效记录**为 `turnEnded` + 无 `source='user' AND
  status='queued'` ⇒ `thread_state='idle'`、`idle_since = now()`（数据库时钟）；`idle` + 末条非 `turnEnded` ⇒
  `thread_state='active'`、`idle_since = NULL`。判定依据是**批次末条有效记录**，不是「批次中出现过 `turnEnded`」；被
  echo 掉的用户轮次也算**有效**（Node 声称持有该轮 ⇒ 不空闲），首提示 echo **不**算有效。`pending → active` 保持
  Phase 4B 规则未改写。两条 CAS 的 0 行都是**不变量损坏** ⇒ 整批回滚，绝不静默忽略。

**S7 — 结束触发（`internal/core/agent_run_thread_end.go`，新；`agent_run_thread_command.go` 增 `EndSessionCommand`）：**

- **本轮严格停止在 `thread_state='ending'`**。三触发器中的 `user_ended` 因 G-018 不可达；`idle_timeout` 与
  `cancelled` 已实现并测试。`endAgentThread` 用精确 CAS（`thread_state IN ('pending','active','idle')` ⇒ `ending`、
  清 `idle_since`）并在**同一事务**内经 **A 缝** `EnqueueThreadCommand` 放出恰好一条 `EndSession{reason}`；0 行 ⇒
  `databaseFailure` 回滚。业务层**不**写 `thread_commands`，`command_id` 由 A 生成。
- **idle 窗口**：`issue_runs.thread_idle_timeout`（点号命名的**配置 key，不是列**，默认 **15m**，`config.Load` 对
  ≤0 落默认值，`configs/config.yaml` 有 `issue_runs:` 段，`CLOUD_ISSUE_RUNS_THREAD_IDLE_TIMEOUT` 可覆盖）；
  判定与比较**全部用数据库时间**（`idle_since < now() - make_interval(secs => $2)`），任何 Go/Controller/Node 进程时钟
  都不参与。`scanEndableIdleThreads` 只扫 `phase='running' AND status='running' AND thread_state='idle' AND
  idle_since IS NOT NULL AND cancel_requested_at IS NULL`，**每个 run 一个短事务**再权威重读同一谓词并 CAS；扫描间隔
  10s ≪ 窗口（实现选择，§3 未固定秒数，D-008 为先例）。`ThreadIdleTimeout <= 0` ⇒ **整个 pass 是 no-op**（未配置，
  而不是零长窗口把每个刚 idle 的 Thread 立刻结束）。重复 tick **不产生第二条** `EndSession`。
- **取消**（IssueRun D6）：`reactToAgentRunCancel` / `ReactToCancelledAgentRunsOnce` 是 B-owned 反应，两种结果分别对应
  D6 的两条路径 —— **无会话**（`thread_state IS NULL`）：`phase='starting'` ⇒ 直接 `releasing`/`cancelled`、
  `result.deliveryState = 'skipped'`、同事务 `declareDelete`、issue 时间线 `run.cancelled`，**不写 `ending`、不发
  `EndSession`**；**有会话**：`thread_state → 'ending'` + `EndSession{cancelled}`，run 自身 `phase/status` **不动**，不
  release、不删 Workspace、不跳过会话关停/revision 生命周期。已 `ending` ⇒ 只保留 `cancel_requested_at`，**无**第二条
  命令。`phase='provisioning'` 有意**不在**扫描内（其取消由 `create_workspace` 终态事务按 D3/D6 结算），与
  `settleRunWorkspace` 的 provisioning 取消分支**互不替代**。
- **`cmd/server/main.go`**：`store.ThreadIdleTimeout = cfg.IssueRuns.ThreadIdleTimeout` 与两个 10s
  `pluginmarket.RunSyncLoop`（`EndIdleAgentThreadsOnce`、`ReactToCancelledAgentRunsOnce`）接线在既有 syncGroup 内，
  ctx 取消即停、退出前等待在途 pass。

**Scope deviations（如实登记，不静默）：**

- **1) 无会话取消的判别键：ADR 优先于 plan。** plan §4C.11 把「直进 `releasing`」写成按 `phase='provisioning'|'starting'`
  判别，而 IssueRun D6 的原文是「取消发生在 `provisioning`/`starting`（**尚无会话**）时」——括号里的限定语才是判别键，且
  IssueRun 不变量 3 只允许 D6 的**无会话**取消声明 `delete_workspace`。按「approved ADR > plan」，实现以
  `thread_state IS NULL`（D-4C-01 由 `StartSession` 物化 `pending`，故该列为「是否曾声明会话」的持久记录）判别。因此
  「`starting` 且**会话已声明**（`thread_state='pending'`）」的取消走 `ending` + `EndSession{cancelled}`，**不**release。
  该偏离写在 `reactToAgentRunCancel` 的文档注释与测试 `TestCancelWithASessionEndsTheThread` 中（`pending` 用例即判别键的
  证据）。**未**修改任何 ADR 正文或 `status`；plan §4C.11 的措辞冲突保留在案，等待 A 修订正式对齐。
- **2) 取消反应的**上游写者**不存在**：Cloud 目前没有任何 API 写 `issue_runs.cancel_requested_at`（G-026）。本轮实现的是
  **反应**（恢复侧），并明确**不**发明公开取消 API。

**Files changed:**

- **production**：`internal/core/agent_run_thread.go`（S5 echo 推广 + 批次后生命周期）、`internal/core/agent_run_thread_end.go`
  （新：`endAgentThread` / `endIdleAgentThread` / `scanEndableIdleThreads` / `EndIdleAgentThreadsOnce` /
  `reactToAgentRunCancel` / `releaseCancelledStartingRun` / `scanCancelledAgentRuns` / `ReactToCancelledAgentRunsOnce`）、
  `internal/core/agent_run_thread_command.go`（`EndSessionCommand(reason)`）、`internal/core/store.go`（`ThreadIdleTimeout`
  字段，零值 = 未配置 = no-op）、`internal/config/config.go`（`IssueRunsConfig.ThreadIdleTimeout` +
  `DefaultThreadIdleTimeout = 15m`）、`configs/config.yaml`（`issue_runs.thread_idle_timeout: 15m`）、
  `cmd/server/main.go`（两个 ending 循环 + 配置接线）。
- **tests（新）**：`internal/core/agent_run_thread_lifecycle_db_test.go`（S5：T4C-19/20/28/29/30 + echo-only 回归）、
  `internal/core/agent_run_thread_end_db_test.go`（S7：T4C-31/32/33/34 + 边界）。两者均为 `internal/core` 白盒 +
  **真实 PostgreSQL**，经 `Store.Control` 的生产 action 与生产 seam 驱动，idle 窗口一律用 SQL 回填 `idle_since`，不使用
  进程时钟、不使用任意 sleep。T4C-33 用 channel barrier 并发，逐条断言**允许的串行结果集**。
- **specs（证据表同步，`specs` 仓库）**：`test-cases/cloud/thread/durable-thread.md`（Phase 4C implementation status 段；
  `连续与幂等`/`冲突整批拒绝`/`同事务写入与幂等`/`关闭后拒绝` → `Covered`，`轮次结算` → `Partial`（`delivered` 半有证据、
  `discarded` 半属 Phase 5），`空闲期满结束`/`追加消息重置`/`进入 idle 的判定` → `Covered`；**GET 与 SSE 两节保持
  `Missing`**）、`test-cases/cloud/issue-run/agent-run-orchestration.md`（`取消是请求` → `Partial`，登记 ADR D6
  「（尚无会话）」判别式与 plan §4C.11 措辞的差异及 G-026；header 拆出 Batch 1+Batch 2 已落地项）、
  `test-cases/cloud/controller-integration/agent-run-executions-and-thread.md`（Batch 2 段：S5 echo 结算、A 侧
  `EndSession{reason}` 投放，并显式区分 `EndSession`（Cloud→Node 请求）与 `SessionEnded`（Node→Cloud 终态，Phase 5））。
  只翻转有**直接测试证据**的义务（Phase 4B、Batch 1 与本轮的证据，均在本次门禁中实跑为绿）；Phase 5 契约
  （`discarded`、`ending → ended`、GET/SSE、`/thread/end`）一律保持 `Missing`。
- **未改**：proto、migration、generated/OpenAPI、前端、任何 ADR 文件或其 `status`、Phase 4B 的 running authority /
  seq 分配 / 收据 / 接管事务、`specs/test-cases/**` 中 S2/S6 相关义务的状态。

**Tests:**

- S5：`TestThreadTakeoverUserTurnEchoMarksDelivered`、`TestThreadTakeoverUserTurnLifecycleIsOneWay`、
  `TestThreadTakeoverIdleWhenTurnEndedAndNoQueuedTurn`、`TestThreadTakeoverIdleNeedsTheLastRecordToBeTurnEnded`、
  `TestThreadTakeoverReturnsIdleThreadToActive`、`TestThreadTakeoverInitialEchoLeavesLifecycleAlone`。
- S7：`TestIdleScanEndsAnExpiredThreadOnce`、`TestIdleScanLeavesUnconfiguredAndUnfinishedWindowsAlone`、
  `TestCancelWithoutASessionReleasesTheRun`、`TestCancelWithASessionEndsTheThread`（`pending`/`active`/`idle`）、
  `TestCancelAfterEndingEmitsNoSecondRequest`、`TestCancelDuringProvisioningIsLeftToSettlement`、
  `TestPendingCancelWinsOverIdleTimeout`、`TestThreadEndingConcurrencyMatrix`（4 个子场景）、
  `TestThreadHistoryIsImmutableAcrossEnding`。
- 回归：Phase 4B 接管全量（`TestThreadTakeover*`）、Batch 1 S3/S4（`TestThreadCommand*`、`integration` 的
  `TestThreadMessage*` / `TestAgentRunThreadTakeover*` / `TestControlGRPCThreadCommand*`）全绿。
- **T4C-34 的 GET 半边仍不可测**：S2a GET 在**不实现**清单内，本轮只覆盖「`record` 永不改写、`seq` 永不重编号、唯一可变列
  是单向的 `status`」，并附 Phase 5 边界断言（无 `ended`、无 `discarded`）。

**Gates:**

- `task format:check` FAIL（**5 文件**：`agent_run_control.go`、`agent_run_control_test.go`、`agent_run_settle_db_test.go`、
  `agent_run_terminal_db_test.go`、`agent_target.go`）——**全部为 HEAD 未修改的既有基线**；本轮两个新测试文件已修正
  gofumpt 后**不再出现**。
- `task lint` FAIL（**7 项**：3 gocritic + 3 gofumpt + 1 unused `activeSpaceAgentRoster`）——同样**全部落在 HEAD 未修改
  文件**，本轮**零新增**发现。
- `task build` PASS；`task test` PASS（全包 0 FAIL，`internal/core` 23.2s、`integration` 52.8s）；
  `task test:race` PASS（无 DATA RACE）；`git diff --check` 干净（cloud 与 specs 均干净）。
- **未运行 `task format`**（它会重写既有未提交文件）。

**Gaps / ADR boundaries（本轮结束时的真实状态）:**

- **G-018 stays OPEN** —— `/thread/end` 端点**未实现**（无 endpoint ⇒ 无 route ⇒ 无 OpenAPI 变更），`user_ended` 不可达，
  T4C-35 **不适用**。作为**明确的 deferred item** 登记，且**不因此**把本轮判为失败。
- **G-019 stays OPEN** —— Phase 5 边界：本轮无 `ending → ended`、无 `SessionEnded`、无 `queued → discarded`、无
  `running → delivering`、无 `deliver_revision`、无 Revision 结算、无 workspace 删除完成、无 `done`。
- **G-022 stays OPEN** —— migration 0022 中 `thread_entries.status` 的存在**不**自动关闭 ADR gap（未批准 A1）。
- **G-024 stays OPEN 且行为未变** —— `cancel_requested_at` 已置 + `pending/active/idle` 的 POST **deferred 行为未被
  任何 side effect 偷偷改变**；`TestThreadMessageCancelRowIsDeferred` 仍绿。
- **G-026 NEW / OPEN** —— `cancel_requested_at` 无生产写者（见 §13）。
- **G-017（S2b 读取面）、G-020、G-021、G-023、G-025** 状态未变。

**Next planned step:**

- **S2a**（Thread GET 的已批准子集）——可立即开工；**S2b** 需先完成 ADR 修订 **A3**（G-017）；**S6** 的「仅状态变化也
  发布」需 **A4**（G-023）；**S7 的 `/thread/end`（T4C-35）**需先取得 G-018 的 ADR 修订或架构师确认；Phase 5
  （`SessionEnded` → `ended` + `discarded`）需 G-019 的专门轮次；**G-026** 需要一个已批准的取消 API 切片。

**Git:**

- nothing staged? **YES（无 stage）**
- commit performed? **NO**
- push / PR? **NO**
- destructive git（`reset`/`restore`/`checkout .`/`clean`/`stash`）? **NO**

---

### Round: Phase 4C Accelerated Implementation Batch 3 — S2a (Thread GET, approved subset) + S6 (SSE invalidation notice) / 2026-10-08

**Plan section executed:**

- §4R.5 的 **S2a** 与 **S6**（依 §4C.3 GET 契约、§4C.7/§4C.8 SSE 契约、§4C.16 的 T4C 编号），随后做一次 §4R.6 的
  **Batch 3 集成与回归**。**S2b 未实现**（G-017 的 ADR 修订 **A3/A4 未批准**，见下）。

**Status:**

- **S2a：Complete。S6：Complete。S2b：Deferred（`S2B_DEFERRED_PENDING_ADR`）。**
- Checkpoint：`S2A_IMPLEMENTATION_CHECKPOINT_REACHED`、`S6_IMPLEMENTATION_CHECKPOINT_REACHED`，**未 commit**。

**S2a — Thread GET（仅已批准子集）**

- **契约落地**：`GET /api/v1/tenants/{tid}/issues/{iid}/runs/{rid}/thread`，参数只有 `after={seq}`（严格 `seq > after`）
  与 `limit={n}`（缺省 200 = `ThreadPageDefault`，上限 500 = `ThreadPageLimit`），条目**按 `seq` 升序**，响应
  `{items, threadState}`。**未实现**：无游标取尾、`before`、`idleSince`、`nextCursor`/`prevCursor`、`PublicRequest.Before`；
  也没有接受矩阵之外的任何新错误码——越界 `limit` → `400 invalid_pagination`，非负十进制之外的 `after` → `400 invalid_cursor`。
- **`after` 缺省的读法**：D5 只定义 `after` 而无缺省，唯一不新增语义的读法是「从头」，与显式 `after=0` 等价；tail 读是
  D-4C-02 对 D5 的扩展、属于 G-017，**不**实现（**不是** tail）。
- **快照**：`items` 与 `threadState` 出自**同一个** `Store.transact`（`thread_read.go`）。
- **`threadState` 的来源**：由 Thread 响应投影 `issue_runs.thread_state`，**不**改 run 资源（`stripAgentRunSkeleton` 语义保持）；
  `thread_state IS NULL`（会话尚未声明）与「非 agent run」一样是 `404 not_found`——不发明空 `threadState`，因为 D4 的状态集是
  封闭的（`pending|active|idle|ending|ended`）。未 materialize 的 Thread 该答什么属于未批准的读取面，因此随 G-017 登记。
- **授权**：复用 Thread D3 的 Issue read 权限（与 comments 同），三重作用域 tenant + Issue + 活 run；跨 tenant / 跨 Issue /
  软删 run / 非 agent run 一律 `404`，非成员 `403`（既有 `membership()` 语义）。
- **只读**：单次事务内**零写**（不分配 `seq`、不写状态、不发事件）；前端列表的 `page`/`window` 语义与校验**未**复用或放宽
  （`after` 是十进制 `seq`，`validID` 会拒绝每个合法 Thread 游标，故独立解析）。
- **条目投影**（D-022 身份不出网）：只投影 `seq/source/kind/record/createdAt`，`node_execution_id`/`node_sequence`/`run_id`
  不上线；`turnId`/`status` 只随 `source='user'` 出现（0022 的 CHECK 让这等价于 `status IS NOT NULL`）。

**S6 — SSE 失效提示**

- **事件形状**：`SpaceEvent` 增量新增可选的 `issueId`/`runId`/`lastSeq`（`omitempty`）。**兼容性是逐字节的**：
  `TestThreadAppendedEventSerialization` 把 4 个 4C 之前的形状（`space.updated`、`project.created`、
  `space.member_updated`、`plugins.catalog_updated`）按字面固定，新事件 `{"type":"issue_run.thread_appended",
  "spaceId":"s","issueId":"i","runId":"r","lastSeq":7}` 只多三个字段。类型名 `issue_run.thread_appended` 与 D5 一致。
- **发布机制（本轮的核心决定）**：hint 在**调用方事务**上排队（`transaction.appends`），由 `Store.transact` 在
  `tx.Commit()` 返回 nil 之后、与 `signalOperations`/`signalThreadCommands` 并列释放（`publishThreadAppends`）。
  「回滚不发」因此是**结构性**的：panic 回滚、提交失败、以及任何未来新增的 entry 写入路径都不需要各自记得不要发。
- **三个发布点（全部是 entry 写入）**：session 声明写 `seq=1`（`agent_run_session_start.go`）、Thread POST 写用户轮次
  （`agent_run_thread_message.go`）、接管写 Node 记录（`agent_run_thread.go` 的 hook 之后）。`lastSeq` = 该提交的
  `MAX(seq)`，是**高水位**（「有数据在 seq ≤ lastSeq」），不是逐条承诺，也**不**推进客户端游标。
- **space 解析**：取该 tenant 唯一活跃 space；解析不到就**丢弃** hint（不让通知失败污染业务事务）；`Store.Events == nil` 是 no-op。
- **SSE 传输层未改**：`/api/v1/tenants/:tid/spaces/:spaceId/events`、授权 = space 读权限、`data: {json}\n\n`、内存单实例 hub
  都在 Phase 3/4B 已存在且够用；**未**新建 broker，G-020 保持 OPEN。
- **保持 exactly-once 的克制**：SSE event id、Thread `seq`、Node `sequence`、`command_id` **四个身份不混用**；恢复永远走
  GET + `after` 游标；通知丢失不丢数据是「数据在 PostgreSQL，通知只是提示」的直接推论。

**Scope deviations:**

1. **S6 发布点是 3 处，plan §4R.5 只点名 2 处**（`public.go`、`agent_run_thread.go`）。理由：D5 的语义是「**条目写入**事务
   提交后」，三处都是条目写入；语义**未扩大**（同一事件类型、无状态变化通知、无新字段）。实际落点也与 S6 的文件清单不同：
   机制放在新文件 `thread_events.go`，POST 的写入点在 `agent_run_thread_message.go`（`public.go` 只负责 dispatch）。
2. **`after` 缺省 = 从头**（见上），不是 tail；`PublicRequest.Before` 未添加。
3. **`thread_state IS NULL` ⇒ `404`** 是本轮为「未 materialize」选的读法；连同 tail/`before`/`idleSince`/游标一并登记在 G-017 下，
   未擅自批准 ADR、未改任何 ADR 的 `status`。

**Files changed:**

- S2a：`internal/core/thread_read.go`（新）、`internal/core/public.go`、`internal/api/router/router.go`、
  `internal/contract/openapi.go`、`api/openapi.json`、`frontend/src/api/generated.schemas.ts`、
  `frontend/src/api/tenants/tenants.ts`（后三者是生成物，未手改）、`integration/agent_run_thread_read_test.go`（新）。
- S6：`internal/core/thread_events.go`（新）、`internal/core/hub.go`、`internal/core/hub_test.go`、`internal/core/store.go`、
  `internal/core/agent_run_session_start.go`、`internal/core/agent_run_thread_message.go`、`internal/core/agent_run_thread.go`、
  `integration/agent_run_thread_events_test.go`（新）、`integration/agent_run_thread_takeover_test.go`（lease seed 改为
  `ON CONFLICT ... DO UPDATE`，使其可被 Thread scene 复用；断言与语义未变）。

**Tests added/changed:**

- S2a：`TestThreadReadReturnsAnAscendingWindowAfterTheCursor`（T4C-7/T4C-10：`after=N` 严格大于、`after=0` = 从头、缺省 =
  从头、末尾/越界为**空**而非报错；`threadState` 与窗口同事务；`turnId`/`status` 只随用户轮次；内部列不泄漏）、
  `TestThreadReadOfAThreadWithNoEntriesIsEmpty`（已声明但无条目的 Thread 是**合法空窗**，`threadState='pending'`）、
  `TestThreadReadLimitAndCursorBounds`（T4C-9：缺省 200 用 250 条验证、500 上限用整读验证、`limit=1/2` 与 `after=cursor`
  的前进遍历无洞无重、`limit` 越界/非法 → `invalid_pagination`、游标非整数/负数/小数/非十进制 → `invalid_cursor`、
  `after=` 等同缺省、未知查询参数被忽略）、`TestThreadReadKeepsTheRunResourceShapeUnchanged`（T4C-10：run 字段集按字面断言，
  且用 `thread_state='idle'` + `idle_since` 双非空的 run，泄漏无处可藏）、`TestThreadReadAuthorizationMatchesComments`
  （T4C-11：非成员 403、跨 tenant/跨 Issue/软删/非 agent run 404）、`TestThreadReadPagingIsGapFreeUnderConcurrentAppend`
  （T4C-12：3 个写者 × 4 条真实 POST 与读者 `after` 前进交错，读者只在读完整条 Thread 后停，最终全窗 1..13 无洞）、
  `TestThreadReadHasNoBusinessWrites`（只读：entries/commands/receipts/idempotency/activities/run 行（含 `version`、
  `updated_at`）指纹在多次读（含被拒的读）前后完全相同）。
- S6：`TestThreadAppendedIsPublishedOnlyAfterCommit`（T4C-25：三个发布点各一条通知且 `lastSeq` 依次 1/2/3，其中接管那条的
  Node `sequence=1` 证明 `lastSeq` 数的是 Thread `seq` 而不是 Node 序；同一提交同时是 running 权威；批次中途
  `notAHistoryTag` 让整批回滚 ⇒ 零通知、条目仍 3、`last_event_sequence` 仍 1；GET `after=0` 仍是 1..3 + `active`）、
  `TestThreadAppendedIsNotPublishedForStateOnlyChanges`（**T4C-26 不实现**（G-023）：echo 只做 `queued → delivered`，
  零通知、不新增条目、run 仍在 `starting`；并**用 GET 重读**证明该翻转在**同一窗口**可见且历史逐字节不变）、
  `TestThreadAppendedRollbackReleasesNothing`（T4C-25 回滚半边：POST 的 A 缝失败 → `503 thread_command_unavailable`，
  零通知、零条目、零命令、状态仍 `pending`）、`TestThreadAppendedCommitFailureReleasesNothing`（T4C-25 提交半边：用
  `NOT VALID DEFERRABLE INITIALLY DEFERRED` 的 FK 让 **COMMIT 本身**失败 → `500 internal_error`，零通知、只剩首提示、状态仍
  `pending`）、`TestThreadAppendedDuplicatesAndLossAreHarmless`（T4C-27：重放批次零通知且数据不变；订阅者断开后仍提交一次写入，
  GET `after=0` 恢复 1..3 且重读稳定；非成员拿 space 流 `403`）、`TestThreadAppendedEventSerialization`
  （`internal/core` 白盒：4 个旧形状 + 新形状逐字节）。
- **两个变异测试**证明了这些断言的载荷：把 `publishThreadAppends` 移到 `tx.Commit()` **之前**，只有提交失败那条测试会红
  （`a POST whose commit failed published {...}, want no notice at all`）——正因为 panic 回滚根本走不到释放那一行，所以
  「提交失败」这半边必须单独有测试。变异后已从备份恢复并重新编译。
- 未实现（保持 OPEN，**不**标 Covered）：T4C-6、T4C-8（随 S2b）、T4C-26（G-023）。

**Specs evidence（`specs` 仓库）:**

- `test-cases/cloud/thread/durable-thread.md`：新增 Batch 3 实现状态段；读取节按**已批准子集**与**未批准扩展半边**分行——
  「快照一致与升序窗口（已批准子集）」与「并发追加下无空洞」→ `Covered`，「快照一致与升序窗口（未批准扩展半边）」→
  `Missing`（G-017），「游标与 limit 校验」→ `Partial`（`before` 半边未实现），「状态翻转在重读时可见」→ `Partial`
  （`delivered` 半边有 GET 直接证据，`discarded` 属 Phase 5）；SSE 节「通知形状与提交序（条目写入）」→ `Covered`，
  「（仅状态变化）」→ `Missing`（G-023），「丢失通知不丢数据」→ `Covered`，「重复与乱序无害」→ `Partial`（服务端半边，
  客户端面板与其组件测试未实现），「客户端重连与轮询兜底」→ `Missing`。
- `test-cases/cloud/controller-integration/agent-run-executions-and-thread.md`：新增 Batch 3 段（S2a/S6 已实现范围 + 未实现清单 +
  「授权过滤不因新事件类型而改变」），并把 Phase 4B 段的「Thread API/SSE 均未实现」明确标注为**当时**状态并指向后续段落。
- **只为有直接测试证据的义务翻转状态**；未实现项（tail/`before`/`idleSince`/游标、仅状态变化通知、前端重连轮询、Phase 5 契约、
  `/thread/end`）一律保持 `Missing`/`Partial`，**未**把未实现功能标 `Covered`。

**Gate results（Batch 3）:**

- `task format:check`：**FAIL（5 文件）**——`agent_run_control.go`、`agent_run_control_test.go`、`agent_run_settle_db_test.go`、
  `agent_run_terminal_db_test.go`、`agent_target.go`，**全部是 HEAD 未修改文件的既有基线**。本轮新增文件
  `integration/agent_run_thread_read_test.go` 一度被 gofumpt 指出（`turnID any, status any`），**已修**，现在**零新增**。
- `task lint`：**FAIL（7 项 = 3 gocritic + 3 gofumpt + 1 unused）**，同样全部落在 HEAD 未修改文件（`agent_run_control.go`、
  `agent_run_control_test.go`、`agent_run_settle_db_test.go`、`agent_target.go`、`space_agents.go`），与 Batch 2 基线**逐项相同**。
  本轮新增的 3 项**已全部修掉**：新事件测试的 `bodyclose`（补上所有权转移说明），以及 `SpaceEvent` 因新增三个字段涨到 96 字节
  而在 `hub.go` 的 `Publish`/`PublishAll` 上触发的 2 个 `hugeParam`（**按值传参是刻意的**：`ch <- e` 给每个订阅者一份拷贝，
  指针会把同一个结构别名给所有订阅者；用紧邻的最小 `//nolint:gocritic` + 理由记录，未改 `sizeThreshold`、未改签名）。
- `task build`：**PASS**（`go build ./cmd/server ./cmd/gateway ./cmd/cloudctl ./cmd/simulator ./cmd/devsetup`）。
- `task test`：**PASS**（exit 0；全包 `ok`，含 `integration` 56.5s、`internal/core` 23.4s；0 FAIL）。改完 T4C-26 的 GET 重读断言后
  又跑了一次全量（`/tmp/batch3_test_final.log`）。
- `task test:race`：**PASS**（`go test -race -count=1 ./...`，exit 0，全包 `ok`，**0 DATA RACE**）。因本轮最后又动了测试文件，
  另在最终工作树上补跑 `go test -race -count=1 ./integration/` → **ok 135.6s**。
- `task frontend:generate`：**生成幂等**——`task openapi` 与 `npm run api:generate` 各再跑一次后，`api/openapi.json`、
  `frontend/src/api/generated.schemas.ts`、`frontend/src/api/tenants/tenants.ts` 的 md5 **一字未变**（生成物与工作树中已存在的
  版本同源，即「未提交」而非「陈旧」）。
- `task frontend:check`：**`npm --prefix frontend run check` PASS**（exit 0：format、lint、types、tests + 覆盖率、模块文档/测试、
  死代码、重复、build 全绿）。组合任务的漂移步骤 `test -z "$(git status --porcelain -- frontend/src/api)"` **FAIL**，原因是
  **仅** 那两个生成物处于未提交状态（`M`），而本轮**禁止**任何 `git add`/`commit`，因此无法、也不应让它为空——按 mandate 用
  **生成幂等证据 + 其余前端门禁结果**替代写 PASS。
- `git diff --check`：cloud 与 specs **均干净**（exit 0）。
- **未运行 `task format`**（它会重写既有未提交文件）；只对**本轮新增**的 `integration/agent_run_thread_read_test.go` 跑了
  `gofumpt -w -extra` + `goimports -w`。

**Gaps / ADR boundaries（本轮结束时的真实状态）:**

- **G-017 stays OPEN —— S2b `S2B_DEFERRED_PENDING_ADR`**：`decisions/cloud/thread/` 下**只有** `0-durable-agent-thread-with-user-turns.md`
  一个已批准 ADR，其中 D5 只写了 `after={seq}` + `limit`（≤500）+ 升序 + `threadState`，**没有** tail/`before`/`idleSince`/游标。
  因此 **A3/A4 未批准**，S2b 的扩展**一行未写**（无 `before` 解析、无 `PublicRequest.Before`、无 `nextCursor`/`prevCursor`、无 `idleSince`），
  T4C-6/T4C-8 保持未实现。**未擅自批准 ADR，未改任何 ADR 文件或其 `status`。**
- **G-023 stays OPEN（`A4` 未批准）**：只实现「有新条目」的提交发布；**仅状态变化**（echo 的 `queued → delivered`、S7 结束路径写
  `thread_state='ending'` 而**不**写条目）**零通知**，T4C-26 **不实现**。`TestThreadAppendedIsNotPublishedForStateOnlyChanges`
  用**双向**断言把这个边界钉住：状态变化**是**持久的、通知**不**被发明——将来实现 A4 时它必然红。
- **G-018 / G-019 / G-020 / G-021 / G-022 / G-024 / G-025 / G-026 状态未变**：`/thread/end` 仍未实现；无 `ending → ended`、
  无 `discarded`、无 Phase 5；SSE 仍是内存单实例 hub（无 broker、无回放）；migration 0022 的 `thread_entries.status` 不关闭
  任何 ADR gap。
- **Phase 4B / Batch 1 / Batch 2 的行为零变化**：running 权威（首个真实记录同事务）、`node_event_receipts` 收据身份、
  `seq = MAX(seq)+1` 分配器与 `seq=1` 不可变首提示、replay/`CONFLICT`/缺口整批拒绝、S3 命令所有权、S4 POST 幂等、S5 的
  echo `delivered`/`active⇄idle`、S7 停在 `ending`、D6「缝在调用方事务内」全部由既有测试与新增测试共同覆盖。
  **控制面只写控制面表、业务面只写业务表**：`thread_events.go` 只读 `collab_workspaces`/`thread_entries` 并发事件，不写任何表。

**Next planned step:**

- **Final Gate 轮**：在 S2a/S6 已落地的基础上做一次收口（全套门禁 + 与 Phase 4B/4C 全部基线的对照 + gap 复核）。
- 需要 ADR 修订才能开工的项：S2b（**A3**/G-017）、仅状态变化通知（**A4**/G-023）、`/thread/end`（G-018）；Phase 5
  （`SessionEnded → ended` + `discarded`）需 G-019 的专门轮次。

**Git:**

- nothing staged? **YES（无 stage）**
- commit performed? **NO**
- push / PR? **NO**
- destructive git（`reset`/`restore`/`checkout .`/`clean`/`stash`）? **NO**（cloud 与 specs 的既有未提交修改全部原样保留）

---

### Round: Phase 4C Final Gate — Full Acceptance / Regression / Gap-Closure Review / 2026-10-08

**Plan section executed:**

- Final acceptance round for Phase 4C after Batch 1–3: §4R.1–§4R.8（已批准切分）, §4C.0–§4C.20（设计）, `## 13. Open Gaps` G-016–G-026,
  §14 change-control, `## 15`（Batch 1–3 记录）, `## 16` marker。**不是新的实现轮**：范围是验收、回归、缺口分类与收口判定。

**Status:**

- Complete（**CORE COMPLETE，含 APPROVAL-DEFERRED 项**）。本轮的最终判定见 `## 16. Current Execution Marker`。

**Files changed:**

- `integration/agent_run_thread_events_test.go`（**新增测试** `TestOnlyTheFirstRecordAdvancesRunning`；该文件此前为本轮未提交的新文件）
- `specs/test-cases/cloud/issue-run/agent-run-orchestration.md`（证据表：在**已为 `Covered`** 的「首条 Thread 事件接管即
  `starting → running`（唯一权威）」行**追加**新测试作为证据；**未改动任何状态标记**——`Missing → Covered` 零次翻动）
- `plan/plan.md`、`plan/plan-zh.md`（本记录与 marker）

**Decisions added/changed:**

- None。**未新增决策、未修改任何 ADR 文件、未改动任何 ADR 的 `status`**；A1/A2/A3/A4 全部**仍未批准**。

**New gaps:**

- None。G-017..G-026 **逐条复核后状态不变**（分类见 `## 16` 与 §4C.18）：**0 项 BLOCKER**、1 项 Phase 5 deferred、1 项文档债、
  其余 8 项 OPEN / NON-BLOCKING。

**Tests added/changed:**

- 新增 `TestOnlyTheFirstRecordAdvancesRunning`（`integration`，真实 HTTP + 真实 PostgreSQL）→ 证明 running 的**唯一权威**：
  `StartSession` + `ClaimWork` + `RecordDispatch`、公开 Thread POST、`ClaimThreadCommands`（纯读）、
  `RecordThreadCommandDelivered`、以及 echo 批次的接管，逐个执行后 run 仍是 `starting`/`dispatched` 且 `thread_state ≠ running`；
  只有随后**首条真实 Node 记录**的接管才在同一提交内把它推到 `running`/`running`/`active`。同一测试内固定了
  `thread_commands.delivered_at`（投递登记）与 `thread_entries.status='delivered'`（echo 结算）是**两个不同阶段**
  （登记时条目仍 `queued`）。**未修改、未弱化、未跳过任何既有测试**；**未新增 sleep**。

**Gate results:**

- `task format:check`：**FAIL → exit 201（PRE-EXISTING BASELINE）** —— 5 个文件、与 Phase 4C 之前完全同一集合
  （`internal/core/{agent_run_control.go, agent_run_control_test.go, agent_run_settle_db_test.go, agent_run_terminal_db_test.go,
  agent_target.go}`，最后改动于 `73c2aa4`，`git diff --quiet HEAD` 全部为真 ⇒ **未改动**）。**本轮的 `agent_run_thread_events_test.go`
  不在其中**。按 §24「不得清理无关 lint baseline」**故意不修**。
- `task lint`：**FAIL → exit 201（PRE-EXISTING BASELINE）** —— **5 个文件 / 7 issues**（gocritic 3 + gofumpt 3 + unused 1）：
  `agent_run_control.go`（2）、`agent_target.go`（2）、`agent_run_control_test.go`（1）、`agent_run_settle_db_test.go`（1）、
  `space_agents.go`（1）；**全部 `git diff --quiet HEAD` 为真（未改动，最后改动于 `73c2aa4`）**。**同样不修**。
- `task build`：**PASS（exit 0）**。
- `task test`：**PASS（exit 0）**，15 包 `ok`、**0 FAIL**；`integration` 57.336s、`internal/core` 23.910s、`controlgrpc` 16.659s；
  PostgreSQL 必需（`REQUIRE_POSTGRES=1`）下真实运行，**零 skip**。
- `task test:race`：**PASS（exit 0）**，15 包 `ok`、**0 FAIL**、**0 DATA RACE**。
- （上述 exit code 由**直接捕获**获得；本轮早期一次试跑把输出接进管道，`$?` 取到的是 `tail` 的状态，该结果已作废并重跑。）
- `task frontend:generate`：**PASS（exit 0）+ 生成幂等**（`api/openapi.json`、`frontend/src/api/generated.schemas.ts`、
  `frontend/src/api/tenants/tenants.ts` 连跑两次 md5 一字未变；openapi diff **纯增** `180 insertions / 0 deletions`）；
  OpenAPI 的 Thread GET 参数恰为 `tid/iid/rid/limit/after`（**无 `before`**），200 schema 恰为 `{items, threadState}`（无游标字段），
  错误响应集与既有 `GET .../comments`、`GET .../issues/{iid}` **逐项相同**（非新增错误面）。
- `task frontend:check`：组合任务里**唯一**失败步骤仍是 `test -z "$(git status --porcelain -- frontend/src/api)"`，原因是那两个
  **生成物处于未提交状态**（`M`），而本轮禁止 `git add`/`commit` ⇒ 无法、也不应让它为空——**不是生成器漂移**；其余步骤由
  `npm --prefix frontend run check` 直接复跑证明为 **PASS（exit 0）**。
- `git diff --check`：cloud 与 specs **均干净**（exit 0）。
- 迁移检查：本仓库**没有** `task migrate:check`；`task migrate` 会**改写共享开发库**，本轮不改外部状态。迁移证据由
  `TestMigration0022ThreadCommandsAndPendingAppliesFreshAndUpgrades` 承载——走真实 `Migrate` + `CheckSchema` 与真实 PostgreSQL
  （隔离 schema）：**全新库全链**、**0021 → 0022 升级**、**重复 `Migrate` 幂等**、列/CHECK/PK/FK/部分索引断言与 23514/23503 拒绝路径。
- `internal/core` 白盒 + `integration` 高风险并发测试另以 `-count=10` 重复（见下）**全部 PASS**。
- **Phase 4C introduced gate regressions = 0**。

**Concurrency / race stress（mandate §16）:**

- `go test ./internal/core -count=10 -run '...'`（`TestThreadEndingConcurrencyMatrix`、`TestIdleScanEndsAnExpiredThreadOnce`、
  `TestIdleScanLeavesUnconfiguredAndUnfinishedWindowsAlone`、`TestCancelWithASessionEndsTheThread`、`TestCancelWithoutASessionReleasesTheRun`、
  `TestCancelAfterEndingEmitsNoSecondRequest`、`TestPendingCancelWinsOverIdleTimeout`、`TestThreadTakeoverReplayIsIdempotent`、
  `TestThreadTakeoverConcurrentBatches`、`TestThreadTakeoverUserTurnLifecycleIsOneWay`）：**exit 0**（30.349s）。
- `go test ./integration -count=10 -run '...'`（`TestThreadMessageConcurrentSameKeyCreatesOneTurn`、
  `TestThreadReadPagingIsGapFreeUnderConcurrentAppend`、`TestAgentRunThreadTakeoverOverGRPC`、
  `TestThreadMessagePostReplaysUnderTheSameKey`、`TestOnlyTheFirstRecordAdvancesRunning`）：**exit 0**（12.568s）。
- **禁止的规避手段零使用**：`integration/agent_run_thread*.go`、`internal/core/agent_run_thread*.go` 中 `time.Sleep` 出现次数为 **0**；
  并发起点由 channel / `sync.WaitGroup` 同步，并断言**完整**允许结果集（而非只接受最常见结果）。

**Gap classification（mandate §19，逐条恰好一类）:**

| Gap | 分类 | 说明 |
|---|---|---|
| G-016 | **CLOSED**（Phase 4C 设计轮 D-4C-01，本轮复核未变） | `pending` 由 B 在 `StartSession` 事务物化 + 幂等回填；`TestAgentRunThreadTakeover…` 与 0022 升级测试提供直接证据 |
| G-017 | **OPEN · NON-BLOCKING FOR PHASE 4C CORE** | S2b（tail/`before`/`idleSince`/游标）是 D5 的**扩展**，需 **A3**；S2a 已批准子集完整且已取证，故不阻塞 |
| G-018 | **OPEN · NON-BLOCKING FOR PHASE 4C CORE** | `/thread/end` 端点形状缺失 ⇒ `user_ended` 触发不可达；idle 超窗与 cancel 两条触发已实现 |
| G-019 | **DEFERRED TO PHASE 5** | `ending → ended` 与 `queued → discarded` 的写者是 `SessionEnded`（Phase 5）；4C 不自造第二终态权威 |
| G-020 | **OPEN · NON-BLOCKING FOR PHASE 4C CORE** | SSE 是提示不是日志；单实例 hub 未改、未建 broker；「重连 + 周期轮询」是**客户端义务**，随 Thread 面板轮次落地 |
| G-021 | **OPEN · NON-BLOCKING FOR PHASE 4C CORE** | 多 Controller worker 分区由 controller-integration ADR 自列为未决；4C 只保证单 worker 下「至少一次 + 登记幂等」 |
| G-022 | **OPEN · NON-BLOCKING FOR PHASE 4C CORE** | `thread_entries.status` 不在 D1 列清单 ⇒ 需 **A1**；迁移半条件已满足（0022 先回填再加 CHECK），语义已批准 |
| G-023 | **OPEN · NON-BLOCKING FOR PHASE 4C CORE** | 「仅状态变化也发布」与 D5 字面冲突 ⇒ 需 **A4**；未批准 ⇒ **不实现**，用双向断言钉住边界 |
| G-024 | **OPEN · NON-BLOCKING FOR PHASE 4C CORE** | POST 的 `cancel_requested_at` 谓词与 D3 字面冲突 ⇒ 需 **A2**；未批准 ⇒ 两行**故意不实现**，`TestThreadMessageCancelRowIsDeferred` 固定该 deferred |
| G-025 | **DOCUMENTATION DEBT** | `migrations/README*.md` 只列到 `0017`；权威记录（有序 SQL + `schema_migrations` 校验和 + `CheckSchema`）完整，无行为依赖 |
| G-026 | **OPEN · NON-BLOCKING FOR PHASE 4C CORE** | `cancel_requested_at` 无生产写入者；反应侧 fail-closed 且幂等，补上写入者不改变已落地行为 |

- **OPEN · BLOCKING PHASE 4C COMPLETION = 0**。

**Scope deviations:**

- None（本轮为验收轮）。实现偏离**未新增**；已登记的偏离（无会话取消用 `thread_state IS NULL` 判别而**非** plan §4C.11 的 `phase`）
  仍以**已批准 IssueRun D6 + 不变量 3 优先于 plan** 为据，属 Batch 2 记录，本轮未改。

**Next planned step:**

- 取得 **A1/A2/A3/A4（G-022/G-024/G-017/G-023）** 的 ADR 修订或架构师确认后，落地 S2b、`POST .../thread/end`（G-018）、
  仅状态变化通知与 POST 的取消谓词；Phase 5（`SessionEnded → ended` + `discarded` + `delivery`，G-019）另开轮次。

**Git:**

- nothing staged? **YES（无 stage）**
- commit performed? **NO**
- push / PR? **NO**
- destructive git（`reset`/`restore`/`checkout .`/`clean`/`stash`）? **NO**（cloud 与 specs 的既有未提交修改全部原样保留）

---

### Round: Phase 4C ADR Approval Round — A1 完整修订 + A2/A3/A4/G-018 最小提案 / 2026-10-08

**Plan section executed:**

- 规范收口轮（**不是**实现轮）：§4R.7 的 A1–A4 提案、`## 13. Open Gaps` 的 G-017/G-018/G-022/G-023/G-024、Thread ADR
  D1/D3/D4/D5 与不变量、IssueRun D6、controller-integration D6、migration 0022、S2a/S4/S5/S7 实现。

**Status:**

- Complete（**五项提案全部就绪，等待人类决定**）。退出标记 `ADR_APPROVAL_ROUND_READY_FOR_HUMAN_DECISION`。

**Files changed:**

- `plan/plan.md`、`plan/plan-zh.md`（本记录）
- **未修改任何 approved ADR**（本轮未获任何批准，按 mandate §26）；**未改**生产 Go 代码、migration 0022、任何新 migration、
  OpenAPI、frontend generated output、proto。

**Decisions added/changed:**

- None。**未自行批准任何 amendment**，未把 proposal 当 approval，未改动任何 ADR 的 `status`。

**Proposal status:**

| 项 | Gap | 状态 |
|---|---|---|
| **A1** — `thread_entries.status` 与 D3 生命周期对齐 | G-022 | **APPROVED · APPLIED（2026-10-08，人类批准）** ⇒ G-022 **`CLOSED BY A1`**（完整修订文本见下；应用记录见 §15 `### Round: Phase 4C ADR Approval Decision …`） |
| **A2** — 取消请求存在时拒绝新的 Thread POST | G-024 | **READY FOR APPROVAL** |
| **A3** — Thread GET 的 v1 读取扩展 | G-017 | **READY FOR APPROVAL**（S2b 仍 `S2B_DEFERRED_PENDING_ADR`） |
| **A4** — 仅状态变化也发布失效提示 | G-023 | **READY FOR APPROVAL** |
| **G-018** — 用户主动结束 Thread 的公开端点 | G-018 | **READY FOR APPROVAL** |

#### A1（完整修订，待批准后落入 Thread ADR D1）

**A1 — Align `thread_entries.status` with the D3 lifecycle**

*Context*：Thread D1 逐项列举 `thread_entries` 的列（`source`、`kind`、`record jsonb`、`turn_id`、`node_execution_id`、
`node_sequence`、`created_at`），**不含 `status`**；而同一 ADR 的 D3 已经命名并描述了每轮次的状态
（`queued`/`delivered`/`discarded`）与其转换。migration `0022` 已按 D3 落地该列，S1 的幂等回填（`source='user'` 行写
`queued`）在加 CHECK **之前**执行，S4 写 `queued`，S5 写 `queued → delivered`，`discarded` 尚无生产写入者
（属 Phase 5 的 `sessionEnded`）。因此当前是 **schema/实现领先于 D1 的枚举**，不是行为分歧。

*Decision*：D1 的 `thread_entries` 列清单增加

- `status text`（可空）

语义（与已批准的 D3 完全一致，无新增行为）：

- `source = 'user'` 时 `status ∈ { queued, delivered, discarded }` 且**恒非空**；Cloud 首次持久化该用户轮次时写 `queued`。
- `source ∈ { node, system }` 时 `status IS NULL`。
- Node 接管到携带同一 `turn_id` 的用户消息记录时，在**同一接管事务**内 CAS `queued → delivered`；`delivered` **不回退**，
  重放逐字节不变。
- 会话终态接管后仍未执行的 `queued` 轮次 → `discarded`（**Phase 5** 的 `sessionEnded` 钩子，见
  `specs/decisions/cloud/controller-integration/20260928-agent-run-executions-thread-and-upload-grants.md` D6）。
- 数据库约束即该语义的编码：`CHECK (status IS NULL OR status IN ('queued','delivered','discarded'))` 与
  `CHECK ((source = 'user') = (status IS NOT NULL))`。
- `status` 是**轮次生命周期**列，与 `issue_runs.thread_state`（Thread 生命周期）不是同一件事；也与
  `thread_commands.delivered_at`（投递登记）不是同一件事——投递登记时条目仍可为 `queued`。

*Consequences*：D1 与 D3 恢复一致，migration 0022 不再是 D1 之外的静默扩展；前端可据此区分「已发送、排队中」与
「Agent 已接收」（D3 已声明该用途）；D5 读取面只在 `source='user'` 时上线 `status`。

*Non-goals*：**不**授权当前实现 `discarded`（仍属 Phase 5）；**不**改变 D3/D4/D5、`turn_id` 身份、`seq` 分配、
投递语义、Phase 5 边界；**不**新增列、**不**新增迁移。

**A1 一致性审计（逐项与已批准内容/实现对齐，A1 不新增任何行为）：**

| 对照 | 结论 |
|---|---|
| D3 | `queued`/`delivered`/`discarded` 三态与转换**已存在**于 D3；A1 只是把承载它们的列写进 D1 |
| migration 0022 | 列、两个 CHECK、`thread_entries_queued` 部分索引、先回填后加约束的顺序均已落地 |
| S1（首提示） | `INSERT ... (run_id, seq, source, kind, record, turn_id)`（`source='system'`）⇒ `status IS NULL`，与 CHECK 一致 |
| S4（POST） | `INSERT ... (run_id, seq, source, kind, record, turn_id, status)` 写 `queued` |
| S5（echo） | `UPDATE thread_entries SET status='delivered' ... WHERE ... AND status='queued'`，CAS 0 行即整批回滚 |
| Phase 5 | `discarded` 仍无生产写入者；A1 不授权提前实现 |

#### A2（最小提案）

**A2 — Reject a new Thread POST while a cancellation has been requested**

- 规则：`POST .../runs/{rid}/thread/messages` 在 `issue_runs.cancel_requested_at IS NOT NULL` 时**必须拒绝**，
  返回 `409 thread_closed`。
- 理由：取消请求已经声明该会话正在收尾（`specs/decisions/cloud/issue-run/0-agent-run-in-disposable-isolated-workspace.md` D6
  「取消与结束是请求，由会话收尾后生效」），继续接收新用户轮次会持久化一个**永远不会被执行**的轮次；该拒绝与
  `ending | ended` 的拒绝语义一致。
- 接受谓词因此成为：`cancel_requested_at IS NULL AND thread_state ∈ { pending, active, idle }`（并要求 live workspace，
  与 D-4C-03 一致）。**这是当前实现唯一一处「比字面更窄」的偏离**，落地 A2 恰好消除它。
- Non-goals：**不**规定取消请求如何写入（**G-026** 仍 OPEN）、**不**改变 `ending` 转换、**不**触及 Phase 5 终态结算、
  **不**改动 `discarded` 语义、**不**新增 cancel API。
- 落地时的测试影响：`TestThreadMessageCancelRowIsDeferred` 会**转红**——这正是应当重写它的信号（它是为固定 deferral 而写的）。

#### A3（最小提案）

**A3 — Thread GET v1 read extensions**

- Query：`after=<seq>` 与 `before=<seq>` **互斥**；`limit ≤ 500`（缺省 200，沿用 D5 上限）。
- Default：两者都不给 ⇒ 返回 Thread **tail** 窗口。
- Ordering：HTTP 响应**始终**按 `seq` 升序，即使使用 `before` 查询。
- Cursor：`after=N` ⇒ `seq > N`；`before=N` ⇒ 取 `seq < N` 中离 N 最近的 `limit` 条，最终响应仍升序。
- Response：保留 `items`、`threadState`；新增 `idleSince`、`nextCursor`、`prevCursor`。
- 身份：cursor 是 **Cloud Thread `seq`**，**不是** Node sequence、**不是** SSE 事件 id、**不是** `execution_id`。
- Non-goals：**不**规定 snapshot token、服务端 cursor 对象、durable SSE resume token、多实例重放、跨请求事务。
  v1 保持简单。
- 落地范围：S2b（`GET` 的 `PublicRequest.Before`、`thread_read.go` 的窗口选择、OpenAPI、生成客户端）。批准前**不实现**。

#### A4（最小提案）

**A4 — Publish an invalidation hint for state-only changes**

- **推荐（首选）**：新增泛化事件 `issue_run.thread_changed{issueId, runId, lastSeq}`，语义为「任何改变 Thread REST 表示的
  事务提交之后**可以**发布」，覆盖：条目追加、用户轮次 `status` 变化（`queued → delivered`）、`threadState` 变化
  （`active ⇄ idle`、`→ ending`）、`idleSince` 变化。`issue_run.thread_appended` 保留其 append-only 语义。
- **备选**：把既有 `issue_run.thread_appended` 扩大为 invalidation hint，并在 ADR 中写明「事件名是**历史名称**，不保证
  每次都真的 append」。代价是事件名与语义不再一致（D5 的措辞正是围绕「条目写入事务」写的）。
- 两种方案都必须保留：仅提交后发布；回滚零事件；SSE **可有损**；重复无害；**GET 是唯一 source of truth**；
  payload **无内容**；**无** exactly-once 保证。
- Non-goals：**不**解决 G-020（多实例、重放、broker）——A4 只定义「什么时候提示」。

#### G-018（最小提案）

**G-018 — User-initiated end endpoint**

- Route：`POST /api/v1/tenants/{tid}/issues/{iid}/runs/{rid}/thread/end`。
- 必须带 `Idempotency-Key`；body 空 JSON `{}`（沿用既有 strict decoder 与幂等 body hash 机制，而非无 body）。
- 成功：`202 Accepted`，响应 `{"threadState": "ending"}`。
- 事务（单个 caller-owned 事务）：幂等预检 → 授权 → run/Thread 权威重读 → 状态校验 →
  CAS `pending | active | idle → ending` → `enqueueThreadCommand(EndSession{reason: user_ended})` → 写幂等响应 →
  commit → **提交后** `ThreadCommandAvailable`。**条目历史不修改**（不新增条目、不分配 `seq`）。
- 生命周期：首次 ⇒ 202；同 key 重放 ⇒ 回放原 202；新 key 对已 `ending | ended` ⇒ `409 thread_closed`。
- 禁止：`ending → ended`、立即进入运行终态、立即删除 Workspace、`queued → discarded`。
- 授权：与 Thread read/write 一致（Space 成员读权限），跨 tenant / 跨 Issue / 隐藏 run 沿用既有 concealment 规则；
  **不**发明新的授权模型。

#### 跨提案一致性（mandate §25）

| 检查 | 结论 |
|---|---|
| A1 vs Phase 5 | A1 提到 `discarded` 但**不授权**当前实现；`discarded` 仍只属 Phase 5 的 `sessionEnded` |
| A2 vs G-018 | 两者不同且互补：**取消请求** ⇒ 拒绝**新 message**（被动收窄）；**用户结束** ⇒ 主动把 Thread 推到 `ending` |
| A3 vs A4 | GET cursor 是 durable 读取契约；SSE 事件只是失效提示。二者**不得**绑定成同一个 cursor |
| A4 vs G-020 | A4 只定义「什么时候提示」，**不**解决 replay / broker / 多实例持久化 |
| G-018 vs Phase 5 | 用户结束**只**推进到 `ending`，**不得**推进到 `ended`（`ended` 由 `SessionEnded` 决定） |

**Suggested approval order（理由见括号）：**

1. **A1**（已有 schema 偏差，最应先修正文档）
2. **A2**（直接影响 lifecycle 与已实现代码的接受谓词）
3. **G-018**（补上 `user_ended` 触发的公开契约）
4. **A3**（读取增强，解锁 S2b）
5. **A4**（通知语义增强）

**Gate results:**

- 本轮只改 Markdown（`plan/plan.md`、`plan/plan-zh.md`）：**未运行** test / lint / build（无生产变更）。
- `git diff --check`：cloud 与 specs **均干净**。

**Scope deviations:**

- None。本轮**未**自行批准任何 amendment、**未**改 ADR、**未**实现任何 deferred 行为。

**Next planned step:**

- 等待人类对 A1（优先）以及 A2 / G-018 / A3 / A4 的明确批准；批准后按 order 逐项落入对应 approved ADR 正文，
  再按 §4R.5 依赖序开工对应实现（A1 落地后仅需文档一致性；A2/G-018 落地后才改 S4/S7 的接受与端点；A3 落地后才开工 S2b；
  A4 落地后才改 S6 的发布面）。

**Git:**

- nothing staged? **YES（无 stage）**
- commit performed? **NO**。push / PR? **NO**
- destructive git（`reset`/`restore`/`checkout .`/`clean`/`stash`）? **NO**（cloud 与 specs 的既有未提交修改全部原样保留）

---

### Round: Phase 4C ADR Approval Decision — 落地 A1 / G-022 / 2026-10-08

**Plan section executed:**

- 规范收口轮（ADR Approval Decision）：**只**应用人类**已批准**的 **A1 / G-022** —— 修改 Thread ADR D1 的
  `thread_entries` schema enumeration 与对应说明，使其与已批准的 D3 lifecycle 一致。§4R.7 的 **A2 / A3 / A4** 与
  **G-018** 本轮**未获批准**，**一行未落地**。

**Status:**

- Complete。退出标记 `ADR_APPROVAL_ROUND_A1_APPLIED`。

**Files changed:**

- `specs/decisions/cloud/thread/0-durable-agent-thread-with-user-turns.md` —— **唯一**改动的 specs 文件，且**只改 D1**。
- `plan/plan.md`、`plan/plan-zh.md`（本记录 + G-022 状态更新）。
- **未改**：生产 Go 代码、migration 0022、任何新 migration、OpenAPI、frontend generated output、proto、任何其他 ADR 文件、
  Thread ADR 的 `status` 字段。

**Decisions added/changed:**

- 应用 **A1**：D1 的 `thread_entries` 列清单增加 `status`，并写明 ①`source = 'user'` ⇒ 非 NULL 且取值限于
  `queued | delivered | discarded`，`source ∈ {node, system}` ⇒ 恒 NULL；②Cloud 首次持久化写 `queued`；echo 携带同一
  `turn_id` 时在同一事务内 `queued → delivered`（不回退）；会话终态接管把仍 `queued` 的轮次改为 `discarded`
  （同 D3 末条，**属 Phase 5**——本轮**未实现、未授权实现**）；③`thread_entries.status`（**轮次**）≠
  `issue_runs.thread_state`（**Thread**，D4）≠ `thread_commands.delivered_at`（**命令投递登记**），三者互不代偿 ——
  命令可以已登记投递而对应轮次仍是 `queued`；④数据库以 `CHECK (status IS NULL OR status IN ('queued','delivered','discarded'))`
  与 `CHECK ((source = 'user') = (status IS NOT NULL))` 编码同一规则（与 migration 0022 逐字一致）。
- **未增加任何行为**、**未扩大 amendment 范围**。D3 / D4 / D5、turn identity、Thread `seq` 语义、Node sequence 语义、
  命令投递语义、running authority、Phase 5 边界**全部未动**。

**Proposal status:**

| 项 | Gap | 状态 |
|---|---|---|
| **A1** — `thread_entries.status` 与 D3 生命周期对齐 | G-022 | **APPROVED · APPLIED（2026-10-08，人类批准）** ⇒ G-022 **`CLOSED BY A1`** |
| **A2** — 取消请求存在时拒绝新的 Thread POST | G-024 | **OPEN / READY FOR APPROVAL**（未动） |
| **A3** — Thread GET 的 v1 读取扩展 | G-017 | **OPEN / READY FOR APPROVAL**（未动；S2b 仍 `S2B_DEFERRED_PENDING_ADR`） |
| **A4** — 仅状态变化也发布失效提示 | G-023 | **OPEN / READY FOR APPROVAL**（未动） |
| **G-018** — 用户主动结束 Thread 的公开端点 | G-018 | **OPEN / READY FOR APPROVAL**（未动） |

**Tests added/changed:**

- None（纯文档轮，无生产或测试改动）。specs 侧**只**改 ADR D1：`test-cases/cloud/thread/durable-thread.md` 与
  `test-cases/cloud/issue-run/agent-run-orchestration.md` 中**没有**逐项枚举 D1 列的表述，故无证据表需要同步，
  也**没有**任何 `Covered` / `Partial` / `Missing` 被翻转 —— `discarded`、`SessionEnded`、Phase 5 仍**非** `Covered`。

**Gate results:**

- 本轮只改 Markdown：**未运行** test / lint / build（无生产变更）；本仓库**没有** docs/markdown lint task。
- `git diff --check`：cloud 与 specs **均干净**（exit 0）。
- `git status --short`（cloud）与 `git -C ../specs status --short`：既有未提交修改**逐项保留**，无新增删除。

**Scope deviations:**

- None。**未**自行批准 A2 / A3 / A4 / G-018，**未**把「看起来不错 / 继续 / 可以」当批准，**未**改任何 ADR 的 `status`，
  **未**把 A1 扩展到 D3 / D4 / D5、migration 0022 或任何实现。

**Next planned step:**

- 等待人类对 **A2 / G-018 / A3 / A4** 的明确批准；批准后按 `A2 → G-018 → A3 → A4` 逐项落入对应 approved ADR 正文，
  再按依赖序开工（A2/G-018 落地后才改 S4/S7 的接受谓词与端点；A3 落地后才开工 S2b；A4 落地后才改 S6 的发布面）。
  **A1 已落地，不产生任何实现任务**（生产代码与 migration 早已按已批准的 D3 与该列一致）。

**Git:**

- nothing staged? **YES（无 stage）**
- commit performed? **NO**。push / PR? **NO**
- destructive git（`reset`/`restore`/`checkout .`/`clean`/`stash`）? **NO**（cloud 与 specs 的既有未提交修改全部原样保留）

---

### Round: Phase 5 Batch 1 — `SessionEnded` Terminal Takeover → `ending → ended` → `queued → discarded` / 2026-10-08

**Plan section executed:**

- Phase 5 Batch 1（mandate §1–§23）。会话执行终态 Node 事件的权威接管，以及与之同事务的 Thread 终态、
  `queued → discarded`、`running → delivering` + 交付工作项放出、收据与序号推进。权威重读完成
  （`cloud/AGENTS.md`、`plan/plan.md`、`plan/plan-zh.md`、`specs/AGENTS.md`、IssueRun ADR、Thread ADR、
  controller-integration ADR、Phase 4B 接管实现、Phase 4C S5/S7 实现、Final Completion Batch 测试）。

**Status:**

- Complete。退出标记 `PHASE_5_BATCH_1_DONE`。**G-019 拆分为 `PARTIALLY CLOSED — Thread terminal takeover complete;
  delivery pipeline remains Phase 5 Batch 2`**；新增 G-027、G-028。

**Files changed:**

- 新增 `internal/core/agent_run_session_end.go` —— A 侧 `agentSessionTakeover`：严格 single-event 路径
  （`kind='agent_session'` 守卫、终态 outcome 与 reason 闭集校验、node 身份校验、`sequence <= last+1` 缺口规则、
  重放按**规范事件**与**结果**双比对、收据 `INSERT` → `result` `UPDATE` → `SessionEnded` 钩子 → fenced
  `last_event_sequence` 推进，全部在同一调用方事务内）。
- 新增 `internal/core/agent_run_session_settle.go` —— `SessionEnded` 钩子（B 侧）：Thread `ending → ended`
  （并覆盖 `pending | active | idle`）、清 `idle_since`、剩余 `queued` 用户轮次 → `discarded`、`running → delivering`
  并放出恰好一个未登记的 `deliver_revision` 工作项，全部复用既有事务、不新开事务。
- 改 `internal/core/control.go` —— 注册控制动作 `agent_session_takeover`（经既有 `submitted` 包装，submission 重放语义与
  其他 takeover 一致）；**未**新增 endpoint / route / background job。
- 改 `internal/controlgrpc/executions.go` —— `TakeOverNodeEvent` 按 outcome 分派两个接管动作；会话终态路径的收据负载是
  **规范对象** `{sequence, result, event(base64)}`（`node_event_receipts.event` 是 jsonb object 且按规范值比对），
  clone 路径保持既有的 base64 text 负载不变。
- 改 `integration/agent_run_thread_takeover_test.go`（复用既有夹具的小幅扩展）。
- 新增 `integration/agent_run_session_end_test.go` —— 本轮全部验收测试（P5-1..P5-6、§11/§13/§14/§15/§19）。
- 改 `internal/core/agent_run_settle.go`（`SessionEnded` 钩子的共享结算路径）。
- `plan/plan.md`、`plan/plan-zh.md`（本轮记录 + G-019 拆分 + G-027/G-028）。
- specs：`test-cases/cloud/thread/durable-thread.md`（「轮次结算」「`ending → ended`」「状态翻转在重读时可见」
  「通知形状与提交序（仅状态变化）」转 `Covered`；实现状态段更新）、
  `test-cases/cloud/issue-run/agent-run-orchestration.md`（新增「会话终态接管即 `running → delivering`」义务行）、
  `test-cases/cloud/controller-integration/agent-run-executions-and-thread.md`（「终态顺序」转 `Covered` + 新增两行义务）、
  `decisions/cloud/thread/0-durable-agent-thread-with-user-turns.md`（D1 中「写它的钩子属 Phase 5」的**过时前瞻**改为已实现；
  **决策内容与 `status` 未改**）。
- **未改**：proto（`ExecutionResult_AgentSessionEnded` 即已批准形状，本轮**未**新增任何 event）、任何 migration
  （0022 未动、无新 migration、`discarded`/`ended` 的既有 schema 已足够）、`api/openapi.json` 与 frontend 生成物
  （无 REST 形状变化）、`.golangci.yml` 或任何抑制、任何 ADR 的语义或 `status`。

**Decisions added/changed:**

- 无新 D-xxx。本轮判定**依据**既有 approved 决策：controller-integration D2（终态事件走 `TakeOverNodeEvent`，且必须在其
  前面全部序号已接管之后，否则 `CONFLICT`）与 D6（`sessionEnded` 钩子在**同一事务**内做 Thread `ended`、`discarded`、
  进入 `delivering` 并放出交付工作项；钩子报错则整事务回滚、Controller 不确认、Node 重放；重放不再调用钩子）、
  Thread D3（未被执行的 `queued` 轮次在接管会话终态的事务里标 `discarded`）、Thread D4 末行 + 不变量 4、
  IssueRun D3（`delivering` 进入条件「会话执行有终态结果（任何结束原因）」，放出 `DeliverRevision` 工作项，`status` = `running`）、
  Thread D5 / A4（`thread_changed` 覆盖任何改变 Thread REST 表示的提交）。
- **范围由 ADR 扩大**：IssueRun D3/D6 把「会话终态」与「`delivering`」规定为同一事务语义，故本轮实现包含
  `running → delivering` 与交付工作项放出；mandate 的「Thread terminal only」默认边界**不适用**。交付的**执行与结算**
  明确留给 Batch 2。

**New gaps:**

- **G-027**（ADR 精度）：Thread D4 未写明 `ending → ended` 的活状态集合，也未写明终态事件落在已 `ended` / 非会话阶段时的行为。
  本轮按不变量 4 的无条件措辞接受 `pending | active | idle | ending`，其余按 invariant violation 处理（`UNAVAILABLE`、零写入）。
  建议 ADR 写明，避免依赖解释。
- **G-028**（PRE-EXISTING lint 基线）：`internal/core/space_agents.go` 的 `activeSpaceAgentRoster` 无调用者
  （`73c2aa4` 引入时即无、全仓含 HEAD 零引用、`git archive HEAD` 未改动树上同样报出），是本仓 `task lint` 唯一剩余项。
  与 `agent_target.go` 的租户级 roster 读重叠；删除/接线均超出 Batch 1 范围，故只登记不处理（**未**加 `nolint`）。
- **G-019 拆分**：`PARTIALLY CLOSED — Thread terminal takeover complete; delivery pipeline remains Phase 5 Batch 2`。
- G-015/G-020/G-021/G-025/G-026 状态未变。

**Tests added/changed:**

- 新增 `integration/agent_run_session_end_test.go`（真实 gRPC `ExecutionService.TakeOverNodeEvent` + 真实 PostgreSQL +
  真实 SSE/公开 HTTP）：
  - `TestSessionEndedEndsThreadDiscardsQueuedAndReleasesDelivery` → P5-1/2/3 + §11 + §14 + §19（关闭性）：终态接管后
    同一事务内 `ended`、已 `delivered` 不变、`queued → discarded`、条目数与 `max(seq)` 不变、其余条目逐字节不变、
    **公开 GET 重读同一窗口**读回 `discarded`、收据与序号推进、恰好一个未登记的交付工作项、提交后恰好一条
    `thread_changed`（无 `thread_appended`、`lastSeq` = Thread 高水位）、之后 POST 永久 `409 thread_closed`。
  - `TestSessionEndedReplayIsANoOp` → P5-4/§8：同 submission 重放与纯字节重放都不再发通知、不写第二条收据、不移动生命周期、
    不重复放出工作项、不回退 `discarded`。
  - `TestSessionEndedRefusalsLeaveNoTrace` → P5-5/§8：缺口、未知执行、同序号异字节、同序号异结果四种拒绝，零收据、
    零序号推进、Thread 与生命周期不变。
  - `TestSessionEndedHookFailureRollsBackTheWholeTakeover` → P5-6/§7：软删运行 Workspace 制造**真实**售后失败，
    receipt/result/`ended`/`discarded`/`delivering`/工作项/序号**全部**回滚、零通知。
  - `TestSessionEndedPreconditionMatrix` → §9/§13：活状态四行（`pending`/`active`/`idle`/`ending`）由真实公开路径驱动并
    回读断言状态与 `idle_since`；拒绝三行（已 `ended`/`delivering`、`provisioning`、`releasing`）断言 `UNAVAILABLE` + 零残留。
  - `TestSessionEndedRefusesADeliveryExecution` → §16：`kind='deliver_revision'` 的执行收到会话终态结果被 `CONFLICT` 拒绝。
  - `TestDiscardedAppearsOnlyOnTheSessionTerminalTakeover` → §15：`/thread/end` 只到 `ending` 且**不**丢弃轮次、
    **不**再放第二条 `EndSession`；只有会话终态接管才写 `discarded`。
  - `TestSessionEndedSerializesWithConcurrentThreadMutations` → §19：竞态 POST、同一终态并发双发、空闲扫描器抢占三个
    子用例，结局都是合法串行结果，且终态事务获胜后 Thread 永久 `ended`、无 `queued` 残留。

**Gate results:**

- `task format:check`：**exit 0**。
- `task lint`：**exit 201**，**1 条**、且为 **PRE-EXISTING BASELINE**（G-028：`internal/core/space_agents.go` 的
  `activeSpaceAgentRoster` unused）。**本轮引入 0 条**：该报告在 `git archive HEAD` 的**未改动树**上运行同一
  `.golangci.yml` 时逐字相同；本轮新增的文件在整轮 lint 输出中零条目。**未**加任何 `nolint`、**未**改配置。
- `task build`：**exit 0**。
- `task test`：**exit 0**（全包 `ok`，含真实 PostgreSQL 的 `integration` 与 `internal/core`）。
- `task test:race`：**exit 0**（`CGO_ENABLED=1`，全包 `ok`，**0 DATA RACE**）。
- `git diff --check`：cloud 与 specs **均干净**。
- 定向：`go test ./integration/ -run 'TestSessionEnded|TestDiscarded' -count=1 -v` **全 PASS**（10 个顶层测试 /
  7 个子用例）。
- `api/openapi.json` 与 frontend 生成物**未变**（本轮无 REST 形状变化），故未运行 `task frontend:generate`。

**Scope deviations:**

- **由 approved ADR 扩大的范围**：`running → delivering` + 交付工作项放出（IssueRun D3/D6 规定为与会话终态**不可拆**的同事务语义）。
  这是**遵守** ADR 的结果，不是自选扩展；据此本轮**未**使用 `PHASE_5_BATCH_1_SCOPE_EXPANDED_BY_APPROVED_ADR` 标记，
  因为该事务范围内的实现已完整交付。
- **未实现且未触碰**（§16）：`DeliverRevision` 投递与执行、`DeliverySettled`、`releasing` 完成、Workspace 删除完成、
  `done`、多实例 SSE、多 worker 命令分区、公开取消 API、migration README 清理。
- **收据负载的适配（已登记）**：会话终态路径把收据负载定为规范对象 `{sequence, result, event(base64)}`，
  因为 `node_event_receipts.event` 是 jsonb object 且按规范值比对，而会话终态事件**不是** ThreadEvent、没有 `record` 可规范化；
  clone 路径的 text/base64 负载逐字不变。
- **`DeliverRevisionSpec` 的对象键与 `revision_ref`**：仅在 **proposed** ADR 中定义，Cloud 侧按每次交付尝试生成 id——
  该适配属 Batch 2 的登记与派发路径，本轮只放出**未登记**的工作项，未定义其最终形状。

**Git status:**

- 无 `git add` / commit / push / PR / `reset` / `restore` / `checkout .` / `clean` / `stash`。
  cloud 与 specs 的既有未提交修改**原样保留**（specs 的 4 个已修改文件仍为已修改）。

**Next round:**

- **Phase 5 Batch 2 — Delivery → Release → Done**：`DeliverRevision` 派发与执行、`DeliverySettled` 与 Revision 登记、
  交付重试/退避与放弃、`releasing` 完成、`RunWorkspaceDeleted`、`done`/`failed` 终态结算、`GrantRevisionUpload`，
  以及 G-019 的交付半边、G-020/G-021。

### Round: Phase 5 Batch 2 — Delivery → Releasing → Done / 2026-10-08

**Plan section executed:**

- Phase 5 Batch 2（mandate §1–§40）。交付执行的工作项认领与派发、`DeliverRevision` 的 wire 投影、
  `DeliverySettled` 接管与 D5 重试/退避/放弃、`delivering → releasing` 与删除意图同事务声明、
  `RunWorkspaceDeleted` 的 `releasing → done` 终态结算、两条周期补偿路径（放弃扫描与删除重新声明）、
  `GrantRevisionUpload` 的受阻判定，以及 Phase 5 Final Gate。权威重读完成
  （`cloud/AGENTS.md`、`plan/plan.md`、`plan/plan-zh.md`、`specs/AGENTS.md`、IssueRun ADR D3/D4/D5/D6、
  controller-integration ADR D2/D6、Thread ADR、Batch 1 实现与测试、既有 revision/upload-grant/
  workspace-deletion 实现、proto）。

**Status:**

- **Blocked（success 半边）**。退出标记 `PHASE_5_BATCH_2_BLOCKED`。**G-019 = `CLOSED`**；新增 G-029、G-030、G-031、G-032。
  受阻原因单一且可判定：**Cloud Revision ADR 仍为 `status: proposed`**
  （`specs/decisions/cloud/revision/0-cloud-owned-object-store-and-verified-revisions.md`），而 authority order 为
  approved ADR > AGENTS.md > plan > implementation；`specs/AGENTS.md` 规定 `proposed` 阶段不得据其编写契约与核心测试用例。
  因此「交付成功 → Revision 登记 → 对象校验 → `GrantRevisionUpload`」这半边**不得实现**。受阻部分**不静默降级**：
  `DeliverySettled` 收到 `saved`/`unchanged` 时返回 `UNAVAILABLE` 且**零写入**（见 P5-13），运行只能经 D5 放弃或 D6 无会话取消
  到达 `releasing`。mandate 的其余可判定部分（§3–§4、§6–§24 的交付失败/放弃/释放/删除终态半边）**已完整交付**。

**Files changed:**

- 新增 `internal/core/agent_run_delivery.go` —— A 侧 `agentDeliveryTakeover`（`agent_delivery_takeover` 控制动作）：
  `executionId`/`operationId`/`sequence > 0`/event object 校验、未登记 404、`kind != 'deliver_revision'` 409、
  结果非法 400、结果 node 与执行 node 不一致 409、序号缺口 409、重叠时按**收据与结果双比对**（`receipt_conflict`/`result_conflict`）；
  正常路径 `INSERT node_event_receipts` → `UPDATE node_executions SET result` → `DeliverySettled` 钩子（报错 ⇒
  `panic(databaseFailure{err})`，整事务回滚）→ fenced `last_event_sequence` 推进。同文件承载 `deliverySettled` 的决策顺序
  （运行/执行/工作身份 → 未知 kind 拒绝 → `phase != 'delivering'` 确定性 no-op → `saved`/`unchanged` 的受阻拒绝 →
  `failed` reason 闭集校验 → `deliveryGivenUp` → `releaseAfterDelivery`，否则按 D5 退避重新放出**同一个逻辑交付**的
  `deliver_revision`），以及 D5 的 `deliveryRetryBase = 30s` / `deliveryRetryCap = 10min` / `deliveryRetryBackoff`。
- 新增 `internal/core/agent_run_release.go` —— `runSessionExecution`、`runReleaseStatus`（D4：`user_ended`/`idle_timeout` →
  `completed`；`cancelled` → `cancelled`；`agent_failed`/`interrupted` → `failed`）、`releaseAfterDelivery`
  （CAS `delivering → releasing`，`revisionId` 置 `NULL`，`appendActivity "run."+status`，随后 `s.declareDelete`）、
  `deliveryGivenUp`（首个 `deliver_revision` 工作项早于 `make_interval(secs => $2)`，或运行 Workspace 的 Node 在
  `node_instances`/`sandbox_instances` 上处于未知态超过窗口）、`runWorkspaceDeleted`（`done` → no-op；`releasing` → CAS 到 `done`；
  其余阶段报错）。
- 新增 `internal/core/agent_run_workspace_release.go` —— `DeleteRunWorkspace` 缝（重读 run/workspace，要求 `phase='releasing'`，
  已有未完成 `delete_workspace` 时幂等 no-op，`projectBusy` 时报错，actor 取 Workspace owner，
  `idempotency_key = "agent-run:"+runID+":delete_workspace"`，`previous` 为 `stripAgentRunSkeleton` 后的 Workspace），
  `runWorkspaceForOperation`、`agentRunWorkspaceDeleteOp`、`settleRunWorkspaceDeleted`（钩子报错 ⇒ `panic(databaseFailure{err})`）。
- 新增 `internal/core/agent_run_delivery_loop.go` —— 两条周期补偿：`GiveUpStaleDeliveriesOnce`（D5 放弃扫描）与
  `RedeclareRunWorkspaceDeletesOnce`（删除意图重新声明）。**不是**内存队列权威：两者都只读 PostgreSQL 并再走既有的
  声明/释放事务路径。
- 改 `internal/core/agent_run_execution_work.go` —— 认领与派发的 kind 白名单由 `agent_session` 扩为
  `('agent_session','deliver_revision')`，排序仍为 `available_at, created_at, id`；`agent_work_get`/`agent_work_pending`
  **未**加 kind 过滤（恢复读路径不变）。
- 改 `internal/core/control.go` —— 注册 `agent_delivery_takeover`（经既有 `submitted` 包装，submission 重放语义与其他 takeover 一致）；
  `advance` 中对 workspace 操作分派 `settleRunWorkspaceOnDone` / `settleRunWorkspaceDeleted`。**未**新增 endpoint / route。
- 改 `internal/core/agent_run_settle.go` —— `businessAgentRunHooks` 不再内嵌 `UnavailableAgentRunHooks`，五个钩子
  （`RunWorkspaceSettled`、`ThreadEventsTakenOver`、`SessionEnded`、`DeliverySettled`、`RunWorkspaceDeleted`）全部委派真实内核；
  `UnavailableAgentRunHooks` 仍保留给「未接线任何钩子」的部署，继续 fail closed。
- 改 `internal/controlgrpc/executions.go` —— `TakeOverNodeEvent` 按 outcome 把交付终态路由到 `agent_delivery_takeover`；
  `input()` 增加 `deliver_revision` → `DeliverRevisionSpec` 投影；`resultObject()`/`result()` 增加
  `RevisionDelivered`/`RevisionUnchanged`/`RevisionFailed` 双向投影与 `storedObject`/`storedObjectOf` 往返；
  `revisionFailureReasonName` 拒绝 `UNSPECIFIED` 与 `VERIFICATION_FAILED`（proto 明确「Node 从不上报后者」）。
- 改 `internal/config/config.go`、`internal/core/store.go`、`cmd/server/main.go` —— 新增 D5 的两个部署级时长
  `delivery_give_up_after`（默认 2h）/ `delivery_unreachable_after`（默认 30m）及其
  `CLOUD_ISSUE_RUNS_*` 覆盖；`main.go` 增加两条 `pluginmarket.RunSyncLoop` 周期任务，
  间隔 `agentDeliveryInterval = 10 * time.Second`。
- 改 `configs/config.yaml` —— 按 `thread_idle_timeout` 的既有写法补上两个新 key 的注释与默认值
  （此前已实现但示例配置未记录）。
- 新增 `integration/agent_run_delivery_test.go` —— 本轮全部验收测试（P5-7…P5-16）。
- 新增 `internal/core/agent_run_release_db_test.go` —— 释放/终态与 D5 退避数字的白盒测试（5 个）。
- 改 `internal/core/agent_run_settle_db_test.go`（钩子接线变更后的断言口径）、
  `integration/agent_run_thread_takeover_test.go`（复用夹具的小幅扩展）、`plan/plan.md`、`plan/plan-zh.md`。
- specs：`test-cases/cloud/issue-run/agent-run-orchestration.md`（**G-019 = `CLOSED`**；4 行证据升级 + 新增
  「Node 报告『已上传』不等于交付完成」；新增整节核心用例「A Released Run Declares Its Workspace Delete And Reaches Done
  Only Through The Delete's Takeover」+ 5 行义务）、
  `test-cases/cloud/controller-integration/agent-run-executions-and-thread.md`（kind 白名单扩大、`agent_delivery_takeover`
  事务形状、`DeliverySettled` 决策顺序、`RunWorkspaceDeleted`；「目标与输入校验」转 `Covered`，授权不持久化保持 `Missing` 并写明受阻原因）、
  `test-cases/cloud/thread/durable-thread.md`（Batch 2 未改动 Thread 侧；G-019 = `CLOSED`）。
- **未改**：任何 proto（`DeliverRevision`/`RevisionDelivered`/`RevisionUnchanged`/`RevisionFailed` 均沿用既有已批准形状）、
  任何 migration（0022 仍为最新，**未**新增、**未**修改已应用文件）、`api/openapi.json` 与 frontend 生成物（无 REST 形状变化）、
  `.golangci.yml` 或任何抑制、任何 ADR 的语义或 `status`。

**Decisions added/changed:**

- 无新 D-xxx。本轮判定**依据**既有 approved 决策：IssueRun D3（`delivering → releasing` 的完成条件）、
  D4（`status` 在 `releasing` 派生，且 `done` 不改写业务结果——「终态不变」）、D5（>2h 连续失败或 Node 未知 >30m 即放弃；
  退避 30s × 2^(n-1) 封顶 10min）、D6（运行 Workspace 由 Cloud 以系统身份创建/删除，幂等身份 `(issue_run_id, kind)`，
  公开 start/stop/delete 一律 404）、controller-integration D6（交付终态经 `TakeOverNodeEvent` → 控制动作接管，
  钩子与收据/序号推进同事务）、operation D4（`agent_work_dispatch` 是共享派发谓词）。
- **受阻判定**：Cloud Revision ADR 为 `proposed` ⇒ 依 authority order 与 `specs/AGENTS.md`，Revision 登记/对象校验/
  `GrantRevisionUpload` **不得实现**。本轮**未**自选降级、**未**发明第二套 upload auth、**未**伪造 `saved` 结果。
  登记为 **G-030**。

**New gaps:**

- **G-019 = `CLOSED`**：交付执行、结算、重试/放弃、释放、删除终态、`done` 全部落地并有直接证据；受阻的只是
  Revision 登记本身的**成功**分支（其归属由 G-030 承载）。
- **G-030**（**APPROVED MANDATORY BLOCKER**）：`specs/decisions/cloud/revision/0-cloud-owned-object-store-and-verified-revisions.md`
  仍为 `status: proposed`，无对应 `revisions` 表（0022 为最新 migration），故 Revision 登记、对象校验与
  `GrantRevisionUpload` 无法在合规前提下实现。解除条件：该 ADR 被人类评审为 `approved`。
- **G-029**（ADR 精度 / D4）：D4 的 Agent 回复注释没有已批准的字段路径，故 `releasing` 结算不写该字段，
  以免发明契约。建议 ADR 明确其来源或删除该义务。
- **G-031**（实现与 ADR 的标识拼写偏差）：D6 写幂等身份 `(issue_run_id, kind)`，而既有实现的唯一约束为
  `(workspace_id, kind)`；两者在本轮全部路径上等价（运行 Workspace 与 run 一一对应），故**未**改 schema。
  建议 ADR 与实现择一对齐。
- **G-032**（`releasing` 侧无放弃上限）：D5 只给交付侧放弃窗口，Workspace 删除侧的持续失败没有上限；
  删除意图由 `RedeclareRunWorkspaceDeletesOnce` 无限重新声明。这**不是**本轮引入的回归，但属同一生命周期的
  空缺，建议后续 ADR 明确。
- G-020/G-021/G-025/G-026/G-027/G-028 状态未变（G-027 仍为 `OPEN / DOCUMENTATION CLARIFICATION`，
  本轮**未**自行改 ADR；G-028 仍为 `PRE-EXISTING BASELINE`）。

**Tests added/changed:**

- 新增 `integration/agent_run_delivery_test.go`（真实 gRPC `ExecutionService` + 真实 PostgreSQL + 真实公开 HTTP）：
  - `TestDeliveryExecutionClaimCarriesTheFixedSpecAndMovesNothing` → P5-7：认领交付工作项携带固定 spec、不移动任何生命周期状态。
  - `TestFailedDeliveryKeepsDeliveringAndReleasesOneBackoffRetry` → P5-8：失败保持 `delivering`，只放**一条** 30s 退避重试，
    且是**同一个逻辑交付**、不产生删除意图。
  - `TestDeliveryGiveUpReleasesTheRunWithD5StateAndD4Status`（3 子用例，含 Node 心跳老化路径）与
    `TestGiveUpStaleDeliveriesPassReleasesOnItsOwnClock` → P5-9：D5 放弃后 `deliveryState = failed`、D4 派生 `status`、
    释放与删除意图同事务，且扫描器按数据库时钟自行推进。
  - `TestReleasingDeclaresExactlyOneDeleteOperation` → P5-10：恰好一个删除操作；释放后重放与迟到结果均为 no-op。
  - `TestDeleteTerminalStateSettlesTheRunToDone` → P5-11：删除终态把运行结算到 `done`。
  - `TestFullLifecycleSessionEndToDoneSkipsNoPhase` → P5-12：全链 `["starting","running","delivering","releasing","done"]`，
    终态 `done`/`completed`/`ended` + `deliveryState = failed` + 恰好一个 `succeeded` 删除。
  - `TestRevisionDeliveredIsRefusedAndTheRunStaysDelivering` → P5-13：`revision_delivered` 被拒为 `UNAVAILABLE` 且**零写入**；
    放弃后重放同样不再改写。
  - `TestDeliveryReplayMatrix` → P5-14：交付接管的重放矩阵（同 submission、纯字节重放、重叠冲突）。
  - `TestDeliverySerializesWithConcurrentWriters` → P5-15：三个 barrier 子用例，交付与并发写者只产生合法串行结局。
  - `TestRefusedQuiesceIsRedeclaredAndStillReachesDone` → P5-16：Node 拒绝 quiesce 后重新声明，仍能到达 `done`。
- 新增 `internal/core/agent_run_release_db_test.go`（真实 PostgreSQL 白盒）：
  `TestRunWorkspaceDeletedMovesReleasingToDoneAndKeepsStatus`（`completed`/`cancelled`/`failed` 三行，版本 +1）、
  `TestRunWorkspaceDeletedReplayIsANoOp`、`TestRunWorkspaceDeletedRefusesPhasesThatCannotHaveASucceededDelete`
  （`provisioning`/`starting`/`running`/`delivering`）、`TestRunWorkspaceDeletedRefusesAnUnknownRun`、
  `TestDeliveryRetryBackoffFollowsD5sNumbers`（{0,1}→30s、2→60s、3→120s、4→240s、5→480s、{6,7,16,1000}→600s）。
- 改 `internal/core/agent_run_settle_db_test.go`：钩子全部真实化后，断言口径从「占位钩子 fail closed」改为
  「真实钩子拒绝无运行身份的调用方对象」+ `UnavailableAgentRunHooks` 对未接线部署仍 fail closed。

**Gate results:**

- `task format:check`：**exit 0**。
- `task lint`：**exit 201**，**1 条**、且为 **PRE-EXISTING BASELINE**（G-028：`internal/core/space_agents.go` 的
  `activeSpaceAgentRoster` unused）。证明方式：`git archive HEAD | tar -x -C /tmp/basehead` 后在**未改动树**上运行同一
  `.golangci.yml`，报告逐字相同（仅 `task:` 包装行不同）⇒ **Phase 5 引入 0 条 lint 回归**。本轮曾出现 1 条**自身**新发现
  （`internal/controlgrpc/executions.go` 的 `storedObjectOf` G115 int64→uint64），已按**修复根因**处理（显式非负钳制 + 注释），
  **未**加 `nolint`、**未**改配置、**未**删除代码。
- `task build`：**exit 0**。
- `task test`：**exit 0**（23 个包行全部 `ok`/无测试，**0 FAIL、0 SKIP**，含真实 PostgreSQL）。
- `task test:race`：**exit 0**（`CGO_ENABLED=1`，全包 `ok`，**0 DATA RACE**；`integration` 186.8s、`internal/core` 26.0s）。
- `git diff --check`：cloud 与 specs **均干净**。
- `api/openapi.json` 与 frontend 生成物**未变**（本轮无 REST 形状变化），故未运行 `task frontend:generate` / `frontend:check`。
- 手动核验：临时测试文件加载真实 `configs/config.yaml` 断言两个新 key 解析为 2h/30m（**PASS**），随后删除该临时文件。

**Scope deviations:**

- **受阻（已登记，非自选）**：Revision 登记 / 对象校验 / `GrantRevisionUpload` 因 G-030 未实现，
  且**不静默降级**——`saved`/`unchanged` 一律 `UNAVAILABLE` + 零写入。
- **新增配置面（超出 mandate 明列文件）**：D5 的两个时长需要部署级可配置，故新增 struct 字段、store 字段、
  两条周期任务与 `configs/config.yaml` 注释。理由：ADR 的「2h/30m 默认」若硬编码则不可部署，且既有
  `thread_idle_timeout` 已确立同一模式。
- **实现与 ADR 的标识拼写偏差**：见 G-031（沿用既有 `(workspace_id, kind)` 唯一约束，**未**改 schema）。
- **未触碰**（§0 记录的既有债务）：多实例 SSE（G-020）、多 worker 命令分区（G-021）、migration README（G-025）、
  公开取消 writer（G-026）、G-027 的 ADR 澄清、G-028 的 lint 债务。
- **工作树中一处非本轮改动**：`.gitignore` 有 4 行新增（`plan/`、`plan/plan.md`、`plan/plan-zh.md`），
  与 `plan/*.md` 已被跟踪的事实相冲突，且不属本轮 mandate 的文件集。**未**改动、**未**回滚（禁止破坏性 git），
  在此披露交由人类判定归属。

**Git status:**

- 无 `git add` / commit / push / PR / `reset` / `restore` / `checkout .` / `clean` / `stash`。
  cloud 与 specs 的既有未提交修改（含 Batch 1 成果与 specs 的 ADR 过时前瞻修订）**原样保留**。

**Next round:**

- **Project Final Acceptance + milestone commit**（若人类将 G-030 的 ADR 评审为 `approved`，则 Revision 登记/上传授权
  可作为独立一轮实施）；G-020/G-021/G-025/G-026/G-027/G-028 与 G-029/G-031/G-032 按各自分类保留。

### Round: Revision ADR Approval Round — G-030 approval-readiness audit + G-029/G-031/G-032 clarification / 2026-10-08

**Plan section executed:**

- mandate「Revision ADR Approval Round — Unblock G-030 and freeze the final Phase 5 contract」。**规范轮，非实现轮**：
  对 `specs/decisions/cloud/revision/0-cloud-owned-object-store-and-verified-revisions.md`（`status: proposed`）做
  approval-readiness 审计，逐项判定它是否已足以固定生产契约，并对 G-029/G-031/G-032 给出最小裁定建议。
  权威重读完成（`cloud/AGENTS.md`、`plan/plan.md`、`plan/plan-zh.md`、`specs/AGENTS.md`、Revision 根决策、
  IssueRun 根决策 D3/D4/D5/D6、controller-integration D1/D4/D6、operation 根决策 + release 决策、Thread D1/D2、
  node revision 决策（同样 `proposed`）、Phase 5 Batch 1/2 实现、`proto/…/agent_executions.proto`、既有
  `revisions`/object-store/upload-grant 实现现状）。

**Status:**

- Complete（审计轮）。退出标记 **`REVISION_ADR_APPROVAL_BLOCKED`** —— 判定
  **`REVISION_ADR_NOT_READY_FOR_APPROVAL`**。**未**改任何 ADR 文件、**未**改任何 `status`、**未**改 production code。
- 交付面清单：`Production code changed: NO`；`proto changed: NO`；`migration changed: NO`；`OpenAPI/frontend changed: NO`；
  `ADR text changed: NO`；`ADR status changed: NO`；`self-approved: NO`；`Revision production behavior implemented: NO`；
  `staged: NO`；`committed: NO`；`pushed: NO`；`PR: NO`；`destructive git: NO`。
- **4 项 blocker**（每项都有两种合理实现，故不得据以批准）：B-1 Revision 行的身份/唯一性/冲突载荷未定义；
  B-2 对象键的「每次尝试」段与**已交付实现**的身份不一致；B-3 `revision_ref` 的形状与归属未在本 ADR 中定义；
  B-4 D1 的 `delivered` 前置 `skipped` 路径与 **approved** IssueRun 不变量 3 冲突，且 `skipped` 与 D6 的无会话取消**语义重载**。
- **3 项 precision clause**（不阻塞，但建议批准前补一句）：P-1 校验范围要写明「只校验存在/大小/SHA-256，不校验
  `final_commit`/`base_commit` 关系、`revision_ref` 前缀或 content-type」；P-2 同一授权 TTL 内重复 `PUT` 同一键的语义；
  P-3 `unchanged` 不复用任何既有 Revision（第一版无「从 Revision 续接」，故不存在 base Revision）。
- 本轮**只提出**最小文本修订提案，**未**写入 ADR 文件：被批准的文本必须由人类决定，代写等于替人类定稿。

**G-030 approval-readiness 逐项判定（YES/NO）:**

| 契约面 | 判定 | 依据 / 缺口 |
|---|---|---|
| revision_id 谁生成 / 生成时机 | **NO** | ADR 只把 `id` 列为列；未写生成者、生成事务与「外部副作用前必须存在」的先后 |
| replay 是否复用同一 revision identity | **NO** | 只有「同一提交身份重试即可，对象不变」；无 Revision 行的幂等键 |
| bundle / history 对象键 | **YES**（形状）/ **NO**（段落身份） | D2 给出模板；第三段 `{deliveryWorkId}` 与实现不一致（B-2） |
| manifest / 对象元数据 | **N/A** | ADR 无 manifest 概念，只校验 bundle 与 history 两个对象——不要求，因此不得自行发明 |
| `revision_ref` | **NO** | 仅 proto 与 node 侧 `proposed` 决策定义；Cloud ADR 未定义（B-3） |
| 谁请求 grant / 谁签发 | **YES** | D3：Controller 在派发交付执行后调 `GrantRevisionUpload(execution_id)`；Cloud 签发 |
| grant scope / 目标对象身份 | **YES** | D2/D3 + controller-integration D4：单键、`PUT`、`upload_grant_ttl`、键必须是该执行输入中的键 |
| run/execution binding | **YES** | 只对「已登记、尚无结果的交付执行」且运行处于 `delivering` 时签发 |
| expiry / replay | **YES** | `upload_grant_ttl`（proposed 默认 15 分钟）；重复调用签发新授权、不落账 |
| 是否可覆盖已有 object | **NO**（P-2） | 只写了「跨尝试不覆盖」；同尝试内重复 `PUT` 同一键未裁定 |
| Node 能否指定任意键 | **YES（明确禁止）** | 不变量 2：Node 只能写 Cloud 为本次尝试指定的键 |
| 校验 object exists / size / digest | **YES** | D4 步骤 1：`HEAD` + checksum 模式，比对存在、大小、SHA-256 |
| 校验 object ownership | **NO**（由构造成立） | 键含 `tenantId`/`runId` 已结构性保证；ADR 未写明「因此不需要额外 ownership 校验」（P-1） |
| content type / encoding | **NO**（P-1） | 未规定；不得自行新增，但应写明「不校验」 |
| commit/base 关系、history/bundle 关系 | **NO**（P-1） | 未规定；实现只能原样记录 Node 报告 |
| 登记事务 | **YES** | D4 步骤 2：校验在事务外，`revisions` 行与 `delivering → releasing` 在同一接管事务 |
| Revision ownership / 公开可见性 | **YES** | 列含 `tenant_id/run_id/workspace_id/project_id`；D5：公开读取只暴露元数据、不下发对象 URL |
| uniqueness / 幂等身份 / 冲突载荷 | **NO** | 见 B-1；clone 路径有明确冲突规则，Revision 路径无 |
| `unchanged` 的条件 / 是否登记 / 是否复用 | **YES / YES / P-3** | D4：`final_commit = base_commit` 且无 bundle，仍登记；第一版无 base Revision 可复用 |
| `unchanged` 时 `result.revisionId` | **YES** | IssueRun D4：`result = {revisionId, deliveryState}`；controller-integration D6 钩子 `unchanged(revisionId)` |
| Node measurement 是 evidence 还是 authority | **YES** | 明确是 evidence：「Controller 转述 Node 的『已上传』正是 Revision 方案否定的交付证据」+ 不变量 3 |
| 何时认定 success / 是否同一事务 | **YES** | 校验通过后登记；登记与 `delivering → releasing` 同一事务（D4 步骤 2 + D6 钩子表） |

**Files changed:**

- **无 production code、无 proto、无 migration、无 OpenAPI/frontend、无 ADR 文件、无 `status` 变更。**
- 仅 `plan/plan.md`、`plan/plan-zh.md`（本轮审计与裁定记录）。
- **受影响但未修改**：`specs/decisions/cloud/revision/0-cloud-owned-object-store-and-verified-revisions.md`（`proposed`
  保持原样，4 项 blocker + 3 项 precision clause 的最小修订提案见本轮记录与本轮报告）、
  `specs/decisions/cloud/operation/20260928-plugin-step-and-run-workspace-release.md`（D4 的 `(issue_run_id, kind)`
  建议修订为 `(workspace_id, kind)` 并限定「未完成的操作」，见 G-031）、
  `specs/decisions/cloud/issue-run/…`（不变量 3 需增加第四个 `delete_workspace` 合法来源或改写 D1；新增 D8 删除侧上限，
  见 G-032）、`internal/core/agent_run_session_settle.go`（`sessionDeliverySpec` 的 ADAPTATION 注释中「Reconciling the
  literal spelling is Phase 5 Batch 2's job」在 Batch 2 结束后**已过时**，属下一实现切片的清理项，本轮**未**动）。

**Decisions added/changed:**

- **无新 D-xxx，未改任何 ADR。** 本轮产出的是**修订提案**：B-1/B-2/B-3/B-4 与 P-1/P-2/P-3 的最小文本（见本轮报告 §12）。
- 判定依据：authority order（approved ADR > AGENTS.md > plan > implementation）；`specs/AGENTS.md`（`proposed`
  阶段不得据其编写契约与核心测试用例）；`cloud/AGENTS.md`（外部副作用前必须有持久稳定身份；事务内不做 HTTP）。

**New gaps:**

- G-030 **状态未变**（`OPEN / APPROVED MANDATORY BLOCKER`），但本轮把它的解除条件从「ADR 批准」细化为
  **「ADR 按 B-1..B-4 修订后再批准」**：按当前文本批准会同时固化一个与 approved IssueRun 不变量 3 冲突的
  `skipped` 路径（B-4），因此**不能**按现文本批准。
- G-029 → **`DEFERRED / NON-BLOCKING`**（裁定）：Revision ADR 不要求持久化 Agent 回复评论；`issue_runs.result`
  的批准字段只有 `{revisionId, deliveryState}`；Agent 的消息已由 Thread API（在线）与会话 JSONL（归档）两处承载；
  而「哪条 `kind` 是 Agent 消息」由 desktop `ora-history` 拥有，Thread D2 又禁止 Cloud 解析业务字段 ⇒ 该义务的
  输入不在 Cloud 的批准域内。**不为它扩 schema**，并**明确排除**在本轮 Revision 批准之外。
- G-031 → 仍 `OPEN / NON-BLOCKING`，本轮给出**推荐 Option B**（改 ADR 为 `(workspace_id, kind)` 且限定未完成操作），
  理由见本轮报告 §10。**本轮未改 schema、未改 ADR。**
- G-032 → 仍 `OPEN / NON-BLOCKING`，本轮给出最小 proposal（新增 IssueRun **D8**：只以「Workspace 的 Node 不可达
  超过 `delivery_unreachable_after`」为放弃条件，终态为 `done` + `status` 不变 + `failure_reason = workspace_unavailable`，
  Node 拒绝 quiesce **不**触发放弃）。**本轮未实现。**
- G-020/G-021/G-025/G-026/G-027/G-028 状态未变（G-027 仍为 `OPEN / DOCUMENTATION CLARIFICATION`，
  **未**自行改 ADR；G-028 仍为 `PRE-EXISTING BASELINE`）。

**Approval text proposed to the human（本轮未应用，供人类定稿）:**

- **NOT-ready 分支（本轮的判定）**：`REVISION_ADR_NOT_READY_FOR_APPROVAL` —— 建议人类**先**按 B-1..B-4 的
  最小修订提案修订 ADR 正文（并建议一并写入 P-1..P-3），**再**给出下面的 READY 分支文本。
- **READY 分支（修订后可用的一句话批准，供人类逐字决定）**：

  > 我批准 `specs/decisions/cloud/revision/0-cloud-owned-object-store-and-verified-revisions.md` 在按 B-1..B-4
  > 修订后成为 `approved`：**并**同时批准 **G-031 的 Option B**（把运行 Workspace 的幂等身份由
  > `(issue_run_id, kind)` 改为 `(workspace_id, kind)`，并限定为「未完成的操作」）**和 G-032 的 D8**
  > （删除侧只以不可达为放弃条件，终态 `done` + `status` 不变 + `failure_reason = workspace_unavailable`）。
  > **不包括** G-029（`DEFERRED / NON-BLOCKING`，不为它扩 schema）。

- **明确排除项（不得含糊）**：G-029 **不**列入；G-031/G-032 若人类不愿一并批准，可逐项拆出，但**必须**在批准文本里
  显式写明「不包括」——B-4 与 G-031 的措辞是**同一批**契约面，遗漏任一项会让实现再次进入「两种合理实现」。
- 本轮**未**写入任何 ADR 文件、**未**改 `status`：被批准的文本必须由人类定稿（mandate §10）。

**Tests added/changed:**

- 无（规范轮）。**未**新增/修改任何测试，**未**运行 Go 门禁（本轮无 Go 变更）。

**Gate results:**

- `git diff --check`：cloud 与 specs **均干净**。
- cloud / specs `git status --short`：见本轮报告 §15（既有的 Phase 5 未提交成果与 specs 文档修改**原样保留**；
  `.gitignore` 的 4 行未知修改**未**触碰、**未**回滚、**未**纳入本轮归属判断）。
- 本轮无 Go 变更，故**未**运行 `format:check`/`lint`/`build`/`test`/`test:race`（mandate §13 明确无需全量 Go test）。

**Scope deviations:**

- 无。本轮严格遵守「只审计 + 只提案」：未改 production code、proto、migration、OpenAPI、frontend、任何 ADR 文件或 `status`。
- **主动披露**：B-2 的具体证据来自**已交付**的 Batch 2 实现（对象键第三段）——即「proposed ADR 的字面拼写」与
  「已落地实现」不一致。本轮**未**修改任何一侧，只在提案中要求 ADR 追认实现实际采用的身份（或由人类要求反向修改实现）。

**Git status:**

- 无 `git add` / commit / push / PR / `reset` / `restore` / `checkout .` / `clean` / `stash`。

**Next round:**

- **Revision Completion Slice + Project Final Acceptance**——但**前置**是：人类按 B-1..B-4（建议连同 P-1..P-3、G-031 的
  Option B、G-032 的 D8）给出批准或反向裁定；随后一个实现切片落地
  `object_store` 配置 + 对象存储客户端（签发/HEAD 校验）+ `revisions` 迁移 + `GrantRevisionUpload` +
  交付成功分支 + D1 的 skip 分支（按 B-4 裁定后的语义），最后做 Project Final Acceptance 与里程碑提交。

### Round: Phase 5 — Revision ADR Decision & Amendment Round / 2026-10-08

**Plan section executed:** §7 Phase 5（Revision 半边）—— 把上一轮审计出的 4 个审批阻塞项（B-1..B-4）与 3 条精度补充
（P-1..P-3）收敛为**唯一**方案并写入两个 `proposed` ADR 的正文，另为 G-031 / G-032 输出**精确的 amendment
proposal**（不改 approved ADR）。**本轮不实现任何 Revision 生产路径。**

**Status:**

- 指标：**`REVISION_ADR_DECISIONS_READY_FOR_HUMAN_APPROVAL`**（B-1..B-4 与 P-1..P-3 均只剩一个明确推荐方案；
  所有跨 ADR 变更逐项独立列出；两个 Revision ADR 的 `status` **未**改动）。
- 交付面清单：`Production code changed: NO`；`proto changed: NO`；`migration changed: NO`；
  `OpenAPI/frontend changed: NO`；`tests changed: NO`；`ADR status changed: NO`；`self-approved: NO`；
  `Revision production behavior implemented: NO`；`staged: NO`；`committed: NO`；`pushed: NO`；`PR: NO`；
  `destructive git: NO`；`approved ADR text changed: NO`（G-031/G-032 只输出提案）。

**决策（本轮定稿）:**

| 项 | 结论 |
|---|---|
| B-1 Revision 行身份 | 控制面在交付终态接管事务内生成 `id` 并写行；`PRIMARY KEY (id)` + `UNIQUE (run_id)`；`ON CONFLICT (run_id) DO NOTHING` + 读回比较负载；行与阶段推进同事务 ⇒ 只允许「对象多于行」，不允许「行指向未校验对象」 |
| B-2 对象键 | 第三段 = **Cloud 生成的尝试 ID**（在冻结工作项输入前即存在）；尝试 ID 只命名一次尝试的对象，不是交付身份、不是 Revision 身份；跨尝试键永不相等。改 A seam 签名的方案已否决 |
| B-3 `revision_ref` | Cloud 是唯一生成者（`refs/ora/revisions/<runId>`，按运行），Node 只回显；内容身份 = 行内 `final_commit` + 对象摘要，恢复不读 ref |
| B-4 缺 `object_store` | **删除**「`skipped` 直接释放」路径；照常 `delivering` + 照常放出交付工作项 + 拒发授权 + 由 **approved D5** 收口（`failed`）。不新增删除触发条件、不新增 `deliveryState` 取值、改 IssueRun ADR：**不需要**（只需 G-033 的 D5 澄清） |
| P-1 | 校验 = HEAD 三项（存在/大小/小写 hex SHA-256）+ 本地输入一致性与形状比较；明确**不**校验 Git 谱系、bundle 可应用性、JSONL 可解析性、content-type/编码、ref 当前指向 |
| P-2 | 同授权内重复 `PUT` 是覆盖写；按声明摘要校验，不符 ⇒ `failed{verification_failed}` + 重试，绝不登记不符声明的对象 |
| P-3 | `unchanged` 仍登记 Revision；第一版不复用任何既有 Revision |
| G-031 | Option B：`(workspace_id, kind)` + 限定「未完成的操作」，落在 2 个 approved ADR 各一句 + 1 个核心用例一句 |
| G-032 | IssueRun D8：上限只取 D5 的**不可达**窗口，终态 `done` + `status` 不变 + `failure_reason = workspace_unavailable`，残留按「活的 Workspace 行 + 终结失败态 operation」登记，**不发明后台清理机制**、绝不谎报删除成功 |

**Files changed:**

- `specs/decisions/cloud/revision/0-cloud-owned-object-store-and-verified-revisions.md`（`proposed`，正文收敛：
  D1 收口改写、D2 三种身份 + 键模板、D3 拒发授权、D4 身份/幂等/`revision_ref`/校验范围/重复 `PUT`、
  不变量 6 重写 + 新增 7/8/9、替代方案 +3 行、文末「修订记录」表格；`status` 未变）
- `specs/decisions/node/revision/0-internal-snapshot-and-incremental-bundle-upload.md`（`proposed`，最小补入：
  D2.4 与 D4 要求结果原样回显输入的 `revision_ref`/`base_commit`、不改写；不变量 7；文末「修订记录」表格；
  `status` 未变）
- `plan/plan.md`（本文件：头部、G-030/G-031/G-032、新增 G-033/G-034、本轮 round 记录）
- `plan/plan-zh.md`（对应中文记录）

**New gaps:** G-033（IssueRun D5 的「连续失败」需澄清为「自首个交付工作项起算」，B-4 的收口依赖它）；
G-034（放弃后未登记的 `deliver_revision` 工作项仍可被认领 —— 认领路径无阶段过滤，`execution_work` 从无删除者）。
两者均**不阻塞**本轮，且**未**在本轮修复。

**Tests:** 未跑测试、也未改任何测试（本轮无生产代码变更）。本会话最后一次完整门禁运行
（`task test:race`：15 个包 + `integration` 全 `ok`，race exit 0）之后，Go / proto / migration / OpenAPI /
frontend 均无改动，因此该结果仍然有效，但**不**代表本轮产生新证据。两个 `proposed` ADR 的核心测试用例
（`specs/test-cases/{cloud,node}/revision/`）仍只有 `.gitkeep` —— 按 `specs/AGENTS.md`，核心用例只在 ADR 变成
`approved` 之后才编写。

**Gates:**

- `git -C specs diff --check`、`git diff --check`（cloud）**均无空白错误**。
- 两个仓库的 `git status --short` 已复查：既有未提交工作**全部保留**（cloud 见下；specs 的 3 个 thread /
  test-case 修改均为本轮之前就存在的工作，未被触碰）。
- 未使用 `git add/commit/push/reset/restore/checkout/clean/stash`，未开 PR。

**Scope deviations:** 无新增偏离。相对上一轮的**范围变化**只有一条并已获授权：本轮允许在 `proposed` ADR 内写入
明确标识为待审批的最小修订文本（上一轮禁止改 ADR 文件）。`.gitignore` 的 4 行改动（空白行、`plan/`、
`plan/plan.md`、`plan/plan-zh.md`）**不是本轮产物**，本轮**未修改、未回滚、未纳入归属判断**，仅如实披露。

**Git status:**

- 未提交修改（cloud，既有）：`.gitignore`、`cmd/server/main.go`、`configs/config.yaml`、
  `integration/agent_run_thread_takeover_test.go`、`internal/config/config.go`、`internal/controlgrpc/executions.go`、
  `internal/core/agent_run_execution_work.go`、`internal/core/agent_run_settle.go`、
  `internal/core/agent_run_settle_db_test.go`、`internal/core/control.go`、`internal/core/store.go`、
  `plan/plan.md`、`plan/plan-zh.md`；未跟踪：`integration/agent_run_delivery_test.go`、
  `integration/agent_run_session_end_test.go`、`internal/core/agent_run_delivery.go`、
  `internal/core/agent_run_delivery_loop.go`、`internal/core/agent_run_release.go`、
  `internal/core/agent_run_release_db_test.go`、`internal/core/agent_run_session_end.go`、
  `internal/core/agent_run_session_settle.go`、`internal/core/agent_run_workspace_release.go`。
- `specs`：`decisions/cloud/revision/…`、`decisions/node/revision/…`（本轮）、
  `decisions/cloud/thread/0-durable-agent-thread-with-user-turns.md`、
  `test-cases/cloud/{controller-integration,issue-run,thread}/*.md`（既有）。

**Next round（Revision Completion Slice）——前置：人类先批准：**

1. **前置（人类）**：① 批准 Cloud Revision ADR（`proposed → approved`，含 B-1..B-4 与 P-1..P-3 的最终文本）；
   ② 单独批准 Node Revision ADR；③ 批准 G-033 的 D5 澄清；④ 批准 G-031 的 Option B；⑤ 批准 G-032 的 D8。
2. **实现切片（获批后）**：`object_store` 配置 + 对象存储客户端（签发预签名 PUT + HEAD 校验）+
   migration `0023_revisions.sql`（`PRIMARY KEY (id)` + `UNIQUE (run_id)`）+ `GrantRevisionUpload` +
   交付成功分支（`delivered`/`unchanged` ⇒ 校验 ⇒ 登记 ⇒ `deliverySettled(saved|unchanged)` ⇒ `releasing`）+
   校验失败分支（`failed{verification_failed}` ⇒ D5 退避重试）+ 无会话取消/缺配置路径的回归。
   实现注意（本轮读代码发现，留给实现轮）：业务钩子当前只接受 Node 可报告的 6 个失败码，
   Cloud 自己记录的 `verification_failed` 必须能被 `deliverySettled` 接受（`revisionFailureReasons` 现为闭集）。
3. **测试**：先按 `specs/AGENTS.md` 为两个已批准的 Revision ADR 补齐核心用例（当前只有 `.gitkeep`），
   再以本地 MinIO 集成测试覆盖「授权只写指定键」「checksum 不符被拒」「HEAD 不符不登记」「重放同结果返回同一
   `revisionId`」「同运行不同负载被拒」。
4. **修订既有 ADR 的落地**（与批准同步）：G-031 的两句 + 用例一句、G-032 的 D8 + D3 表 + 用例两处、G-033 的一句。
5. 全部落地后才是 **Project Final Acceptance** 与里程碑提交。

### Round: G-032 Implementation Slice — Workspace Delete Give-Up → `done` without lying about deletion success / 2026-10-08

**Plan section executed:**

- §5 Batch 2 的删除侧收口（已批准的 IssueRun **D8**）；§13 的 **G-032** 条目；§16 marker。
  **范围严格限定**：不触碰 G-037 / G-034 / G-020 / G-021 / G-025 / G-026 / G-027 / G-028 / G-029 / G-035 / G-036。

**Status:** Complete — 退出标记 `G032_IMPLEMENTATION_DONE`。

**Files changed:**

- `internal/core/agent_run_release.go`：新增共用的 `runWorkspaceNodeUnknown`（D5 与 D8 **唯一**的不可达判据，
  窗口参数化、"unknown" 极性保持 fail-closed）、`releaseGivenUp`（D8 决策 = 活 sandbox 前置 + 共用判据）、
  `giveUpRunWorkspaceRelease`（D8 终态事务：operation 终结失败 + `releasing → done` + `failure_reason`）；
  `deliveryGivenUp` 的窗口 2 改为调用共用判据（语义不变）；文件头与 `runWorkspaceDeleted` 的注释补上 D8 的第二种 `done`。
- `internal/core/agent_run_delivery_loop.go`：新增 `giveUpStaleWorkspaceRelease` / `scanReleasingRuns` /
  `GiveUpStaleWorkspaceReleasesOnce`（与另两条补偿轮同形状：只读扫描 + 每 run 一个短事务）；`RedeclareRunWorkspaceDeletesOnce`
  的注释记录 §11（`done` 天然不在扫描内）。
- `cmd/server/main.go`：Phase 5 Batch 2 的循环块加入第三个 goroutine（同一 10s 节奏，同一 `syncGroup` 生命周期）。
- `integration/agent_run_release_giveup_test.go`（新）：G032-1..G032-9。
- `integration/agent_run_delivery_test.go`：`givingUpScene` 与 `ageRunWorkspaceNode` 改为委托新助手（行为不变）。
- `specs/test-cases/cloud/issue-run/agent-run-orchestration.md`：四条义务 `Missing` → `Covered` + 直接证据清单。
- `plan/plan.md`、`plan/plan-zh.md`：头部、G-032 条目、§16 marker。

**Decisions added/changed:** None（**未**新增配置、**未**新增 schema、**未**新增公开错误码、**未**改任何 approved ADR 的决策语义）。

**New gaps:** None。G-032 = `CLOSED`；G-037 / G-034 仍 `OPEN`，其余缺口不变。

**Tests added/changed:**

- `TestG032_1_SuccessfulDeleteIsUnchangedByTheGiveUpPass` → 成功删除仍是 `releasing → done`、无 `failure_reason`，之后的放弃轮对 run 与 Workspace 均零写入。
- `TestG032_2_UnreachableBelowTheWindowStaysReleasing` → 心跳仅早于窗口的运行保持 `releasing`、版本不变、delete 仍 `queued`。
- `TestG032_3_UnreachablePastTheWindowSettlesDoneWithoutClaimingDeletion`（**核心**）→ 超限 ⇒ `done` + `status` 不变 + `failure_reason = workspace_unavailable`；Workspace 未软删除、绑定未清除、版本未被写；operation 终结失败为 `node_unavailable`；无任何 `succeeded`；Timeline 零新增。
- `TestG032_4_RefusedQuiesceNeverTriggersGiveUpEvenPastTheWindow` → 删除意图时间戳推早一小时而 Node 可达时两轮放弃轮零写入；随后重新声明并正常 `done`。
- `TestG032_5_SuccessfulDeleteWinsTheRaceAndThePassIsANoOp` → 成功先落地后放弃轮零写入；以及 barrier 同起并发下只允许「已释放」或「已放弃」两种终局。
- `TestG032_6_GiveUpWinsTheRaceAndAStaleDeleteSettlementIsRefused` → 放弃先落地后过期 `advance` 被 `stale_operation` 拒绝，终态/status/版本/Workspace 全部不变。
- `TestG032_7_ADoneRunIsNeverRedeclaredOrRewritten` → `done` 之后三轮三条补偿轮 + 一次 Controller 领取均零写入、不新增 operation。
- `TestG032_8_BusinessStatusMatrixSurvivesTheGiveUp` → `completed` / `cancelled` / `failed` 三行全部保持。
- `TestG032_9_NoBackwardLifecycleAfterGiveUp` → 观测到的阶段轨迹逐步前进（`assertForwardOnly`），三条补偿轮与一条可领取 operation 检查共同证明 `done` 是终态。

**Gate results:** `task format:check` exit 0；`task lint` 仅剩 1 条 PRE-EXISTING BASELINE（G-028，`git archive HEAD` 上逐字相同 ⇒ 本切片 0 回归）；`task build` exit 0；`task test` exit 0；`task test:race` exit 0（G032 全组 `-race` 无 DATA RACE）；G032-3/5/6/7 另以 `-count=10` 通过；两个仓库 `git diff --check` 干净。**禁止 arbitrary sleep**：窗口推进改由数据库时间控制（改 Node 心跳时刻），并发用 barrier + channel。

**Scope deviations:** None。未新增 timeout 配置、未复用 D5 的 2h 窗口、未实现第二套 reachability 定义、未新增 schema、未发明公开错误枚举、未新增后台清理机制、未把无直接测试的义务标 `Covered`、未改 ADR 决策语义、未触碰 G-028 的 lint 基线（未删函数 / 未接线 / 未 `nolint` / 未改 lint 配置）。

**Next planned step:** **Git normalization → milestone commit → push → PR**。不要开启新的功能开发批次。

**Git:** staged **NO** / commit **NO** / push **NO** / PR **NO** / destructive git **NO**。

---

### Round: Revision Completion Slice — 对象存储 + 亲自校验后的 Revision 登记 + 全项目 Final Acceptance / 2026-10-08

**Plan section executed:**

- 上一轮「Next round」清单第 1–5 项，人类已在审批轮逐项批准（① Cloud Revision ADR → `approved`（含 B-1..B-4 与 P-1..P-3）；
  ② Node Revision ADR → `approved`（含 D2.4 回显、D4 约束、不变量 7、测试义务），且**两者独立批准、不视为一方自动批准另一方**；
  ③ G-033 的 IssueRun D5 澄清；④ G-031 的 Option B（`(workspace_id, kind)` + 限定未完成操作，**不批准任何额外 schema 修改**）；
  ⑤ G-032 的 IssueRun D8（含 `done` 条件、残留/不谎报/不新增清理机制三句））。本轮据此实现 Cloud 侧全部行为，
  并把获批文本同步进 `specs` 的核心用例与证据状态。**批准范围之外明确不动**：G-029 保持 `DEFERRED / NON-BLOCKING`；
  G-034 不批准任何修复方式、不纳入本切片；不借本次批准扩大 G-020 / G-021 / G-025 / G-026 / G-027 / G-028 的范围。

**Status:**

- Complete。退出标记 **`REVISION_COMPLETION_SLICE_DONE`** + **`PROJECT_FINAL_ACCEPTANCE_PASSED`**。
- **G-030 = `CLOSED`**（关闭的是「未批准 ADR 阻塞实现」这条；其名下衍生出的三项新缺口见 G-035 / G-036 / G-037）。
- 交付面清单：`Production code changed: YES`；`proto changed: NO`；`migration changed: YES`（**新增** `0023_revisions.sql`，
  **未**触碰任何已应用文件）；`OpenAPI/frontend changed: NO`；`ADR status changed: YES`（两个 Revision 根决策
  `proposed → approved`，依人类逐项批准；approved ADR 的既有决策**未**被改写）；`tests changed: YES`；
  `self-approved: NO`（未自行批准任何 ADR、未自行扩大批准范围）；`staged: NO`；`committed: NO`；`pushed: NO`；`PR: NO`；
  `destructive git: NO`。

**实现（Cloud 侧，全部按批准的 D1–D5 与不变量 1–9）:**

| 面 | 落地 |
|---|---|
| D1 配置与凭据 | 新增可选 `object_store` 段（`internal/config`：endpoint/region/bucket/凭据**文件路径**/`upload_grant_ttl`）；缺省即「未配置」，缺省 TTL 为批准的 15 分钟；凭据只在 Cloud 进程内，配置结构里只有路径字段 |
| 对象存储客户端 | `internal/objectstore`：签发限定到**单个键 + `PUT`** 的预签名 URL，以及 `HEAD` 读回存在性、大小、摘要；两件事都经注入的 HTTP 客户端，可在测试中替换 |
| 迁移 | `internal/core/migrations/0023_revisions.sql`：`id uuid PRIMARY KEY`、`run_id uuid NOT NULL UNIQUE REFERENCES issue_runs(id)`、tenant/workspace/project 外键、`repository_url` 长度 CHECK、commit 形状 CHECK（40/64 位小写 hex）、`revision_ref LIKE 'refs/ora/revisions/%'`、bundle 三列「全有或全无」+ 大小/摘要形状 CHECK、`history_*` NOT NULL + 形状 CHECK、`expires_at` 恒 NULL（保留不是目标）；**未**修改任何已应用迁移 |
| D3 上传授权 | `GrantRevisionUpload`：只对「已登记、尚无结果」的交付执行签发，一次一个键；未知执行 `NOT_FOUND`、会话执行 `CONFLICT`、已有结果或运行已释放 `CONFLICT`、未配置对象存储 `UNAVAILABLE`；签发**零写入**（授权是能力不是记录） |
| D4 校验与登记 | 第 1 步本地输入一致性（无 I/O，先于第 2 步）→ 第 2 步 `HEAD` 三项 → 第 3 步**同一事务**内收据 → 结果 → `revisions` 行 → 释放钩子；不成立即 `failed{verification_failed}` 且**不写**行、运行留 `delivering` 走 D5 退避；`verification_failed` 只由 Cloud 记录，线上送来一律 `InvalidArgument` |
| 幂等与冲突 | `UNIQUE (run_id)` 兜底 + 读回比较：负载逐字段相同 ⇒ 幂等返回既有 `id`；不同 ⇒ 整条接管事务回滚（收据、结果、阶段推进都不写） |

**Files changed（cloud）:**

- 新增：`internal/core/migrations/0023_revisions.sql`、`internal/core/agent_run_revision.go`、
  `internal/core/agent_run_revision_registration.go`、`internal/core/agent_run_revision_test.go`、`internal/objectstore/`、
  `internal/controlgrpc/executions_test.go`。
- 修改：`internal/config/config.go` 与 `config_test.go`、`internal/controlgrpc/executions.go` 与 `agentruns.go`、
  `internal/core/{control,store,agent_run_hooks,agent_run_settle,agent_run_execution_work}.go`、`cmd/server/main.go`、
  `configs/config.yaml`、`internal/README.md` / `README.en.md`、`integration/migration_upgrade_path_test.go`、
  `integration/agent_run_thread_takeover_test.go`、`integration/agent_run_delivery_test.go`（本轮新增多个测试）、
  `.gitignore`（**非本轮产物**，见「Scope deviations」）。
- **未改**：proto（无新增 RPC/字段）、`api/openapi.json` 与 frontend 生成物、`.golangci.yml` 或任何抑制、
  任何已应用的迁移文件、任何 approved ADR 的既有决策文本。

**Files changed（specs）:**

- `test-cases/cloud/revision/object-store-and-revision-registration.md`（新文件，替换 `.gitkeep`）：按批准的 D1–D5 与
  不变量逐条列义务，并按本轮**实际断言**标注 `Covered` / `Partial` / `Missing`；
- `test-cases/cloud/{controller-integration/agent-run-executions-and-thread,issue-run/agent-run-orchestration,thread/durable-thread}.md`：
  **同步被批准契约推翻的旧行为描述**（此前写「Node 报告已上传 ⇒ Cloud 拒绝、不登记、不释放」，现改为「Cloud 亲自核验
  对象后才登记并释放」；被删除的测试引用一并更新），并逐行更新证据状态与缺口；
- `decisions/cloud/revision/…`、`decisions/node/revision/…`：`status: proposed → approved`；
- `test-cases/node/revision/internal-snapshot-and-bundle-upload.md`（新文件，Node 侧义务全部 `Missing`——desktop 侧实现未开始）。

**Tests added/changed:**

- `integration/agent_run_delivery_test.go`：`TestRevisionDeliveredIsVerifiedRegisteredAndReleasesTheRun`（两种线上形状）、
  `TestRevisionVerificationFailureIsCloudsOwnVerdict`（`HEAD` 不成立 / 无对象存储）、
  `TestRevisionResultContradictingItsInputNeverReachesTheObjectStore`（四个矛盾载荷变体 ⇒ **零**探测）、
  `TestRevisionRegistrationReusesAnIdenticalRowAndRollsBackADifferentOne`（幂等复用 / 冲突整体回滚）、
  `TestLateDeliveredRevisionRegistersNothingOnAReleasedRun`（不变量 9）、
  `TestGrantRevisionUploadSignsOneGrantPerObjectKey`（授权面与拒绝矩阵），并强化
  `TestDeliveryExecutionClaimCarriesTheFixedSpecAndMovesNothing`（尝试段身份）与
  `TestGiveUpStaleDeliveriesPassReleasesOnItsOwnClock`；`TestFullLifecycleSessionEndToDoneSkipsNoPhase` 改走**交付成功**
  路径（终态 `deliveryState = saved` + `revisionId` = 已登记行）。
- `internal/controlgrpc/executions_test.go`：`TestStoredObjectKeepsTheDeclaredSizeReadable`（proto 的 `uint64` 必须
  以 `Object.N` 认识的形式落库，否则声明的大小会读回 0）、`TestRevisionFailureRefusesCloudsOwnVerificationVerdict`。
- `internal/config/config_test.go`：`TestObjectStoreSectionIsOptionalAndCarriesTheApprovedDefaultLifetime`。
- `integration/migration_upgrade_path_test.go`：`TestMigration0023RevisionsAppliesFreshAndUpgrades`（fresh + 升级两条路径、
  两次 `Migrate` 幂等、`CheckSchema`、PK/UNIQUE 用**违反**而非读 `pg_indexes` 证明、12 个 CHECK 违规各自失败、
  0023 不改写它落在其上的既有行）。

**Gates（Project Final Acceptance，本轮实测）:**

- `task format:check` exit 0；`task build` exit 0。
- `task lint` exit 201，**只剩 1 条 PRE-EXISTING BASELINE**（`internal/core/space_agents.go:37` 的 `unused` = G-028）：
  在 `git archive HEAD` 的**未改动树**上以同一 `.golangci.yml` 运行 `golangci-lint` 得到**逐字相同**的 1 条报告
  ⇒ 本切片引入 0 条 lint 回归。本轮自身出现过 1 条 `sqlclosecheck`（`migration_upgrade_path_test.go` 的 `rows.Close()`），
  已按**修复根因**处理（改 `defer`），非 `nolint`、非删除断言。
- `task test`（`REQUIRE_POSTGRES=1` + 真实 PostgreSQL）：exit 0，**420 PASS / 0 FAIL / 0 SKIP**。
- `task test:race`：exit 0，**0 DATA RACE**。
- `git diff --check`（cloud）与 `git -C specs diff --check` 均干净。

**Scope deviations:** 无新增偏离。两项**如实披露、未修**的实现缺口（G-035 公开读未投影 D5 的 Revision 元数据、
G-036 健康检查不报告对象存储）与一项测试证据缺口（G-037 真实 MinIO 上的三项义务）**不在本轮授权范围内**，
因此登记而不就地修改；它们不影响已批准行为在 Cloud 上的落地（G-035/G-036 分别是「元数据的展示面」与
「运维可见性」，G-037 只影响证据状态）。`ADRs 落地顺序`第 3 步（cluster Compose 增加 MinIO + `object_store` 配置）
本轮**未完成**：MinIO 镜像拉取失败（`docker.io` 连接被重置）⇒ 归入 G-037。
`.gitignore` 的 4 行改动（`plan/`、`plan/plan.md`、`plan/plan-zh.md`）**不是本轮产物**，本轮**未修改、未回滚、未纳入归属判断**。

**Git status:**

- cloud：`plan/plan.md`、`plan/plan-zh.md`（既有 staged 状态保留，本轮在其上追加）；本轮新增/修改见上「Files changed（cloud）」；
  两份 plan 文件此前显示为 index deletion 的异常状态**未复现**（复核为 `M ` + 工作树干净）。
- specs：本轮修改的 4 个 test-case 文件与两个 ADR 的 `status`。
- **未** `git add` / commit / push / PR / 任何破坏性 git 操作。

### Round: Phase 4C Final Completion Batch — A2 / A3 / A4 / G-018 applied + implemented, then the Final Gate / 2026-10-08

**Plan section executed:**

- 收口轮（mandate：Phase 4C Final Completion Batch）。人类审批决定：**批准 A2、A3、A4 与 G-018**（A1/G-022 已在上一轮批准并落地）。
  本轮按 **A2 → A3 → A4 → G-018** ①把四项写入对应 approved ADR 正文，②实现四项行为，③对 Phase 4B + 4C 做全量回归与 Final Gate，
  ④在不存在已批准 mandatory blocker 的前提下**结束 Phase 4C**。**不得进入 Phase 5。**

**Status:**

- Complete。退出标记 `PHASE_4C_FINAL_COMPLETION_BATCH_DONE`。Phase 4C 判定：**`PHASE_4C_COMPLETE`**。

**Files changed:**

- `specs/decisions/cloud/thread/0-durable-agent-thread-with-user-turns.md` —— **A2 → D3**、**G-018 → D4**、**A3 + A4 → D5**。
  frontmatter `status` **未改**（早已是 `approved`），也没有新增任何后续 ADR 文件。
- `specs/test-cases/cloud/thread/durable-thread.md`（接受谓词 / 读取 / SSE 三节证据同步，新增核心用例
  **A User End Request Ends The Thread Exactly Once**）、
  `specs/test-cases/cloud/issue-run/agent-run-orchestration.md`、
  `specs/test-cases/cloud/controller-integration/agent-run-executions-and-thread.md`（状态段落与缺口状态）。
- 生产：`internal/core/agent_run_thread_message.go`（A2）、`internal/core/thread_read.go`（A3）、
  `internal/core/thread_events.go`（A4）、`internal/core/agent_run_thread_end.go` + `internal/core/public.go` +
  `internal/api/router/router.go` + `internal/contract/openapi.go`（G-018）。
- 契约/生成：`api/openapi.json`、`frontend/src/api/generated.schemas.ts`、`frontend/src/api/tenants/tenants.ts`
  （**全部由 `task frontend:generate` 生成，无手改**）。
- 测试：`integration/agent_run_thread_message_test.go`、`integration/agent_run_thread_read_test.go`、
  `integration/agent_run_thread_events_test.go`、新增 `integration/agent_run_thread_end_test.go`。
- `plan/plan.md`、`plan/plan-zh.md`（本轮记录 + G-017/G-018/G-019/G-020/G-021/G-023/G-024 状态）。
- **未改**：`internal/core/migrations/0022_thread_api_and_commands.sql`（applied migration，逐字节未动）、任何新 migration、
  proto、`.golangci.yml`、任何 lint/format 配置或抑制。

**Decisions added/changed:**

- **A2（G-024）**：D3 的接受谓词改为 `cancel_requested_at IS NULL` **且** `thread_state ∈ pending | active | idle`；
  `ending | ended` 与「已记录取消请求」共用同一个 `409 thread_closed`。取消**仍**是请求：不立即终态、不立即删除 Workspace，
  `discarded` 语义不变也**未**实现。
- **A3（G-017）**：D5 的读取面固定为「游标是 Cloud Thread `seq`」——无游标 = tail、`after=N` 取 `seq > N`、
  `before=N` 取 `seq < N` 的至多 `limit` 条且响应仍升序、`after` 与 `before` 互斥、`limit` 缺省 200 / 上限 500，
  响应带 `idleSince`/`nextCursor`/`prevCursor`；窗口一律 `seq` 范围查询，不用 `OFFSET`；v1 不提供跨请求一致性快照。
- **A4（G-023）**：新增 `issue_run.thread_changed{issueId, runId, lastSeq}`，覆盖**任何**改变 Thread REST 表示的提交；
  `issue_run.thread_appended` **未被加宽**（名称、负载、语义逐字不变）。两者都只在提交后发布、回滚不发、重复无害。
- **G-018**：D4 增加用户主动结束的公开端点 `POST .../runs/{rid}/thread/end`（`Idempotency-Key` 必需、body `{}`），
  首次请求在一个调用方事务内做幂等预检 → 授权 → 权威重读与状态校验 → CAS 到 `ending` + `EndSession{user_ended}` + 幂等响应，
  返回 `202 {"threadState":"ending"}`；幂等预检**先于**生命周期校验（同键重放即使 Thread 已 `ended` 仍回放那个 `202`），
  新键对 `ending | ended` 是 `409 thread_closed`；**只**推进到 `ending`，`ended` 仍由会话终态决定。

**Proposal status:**

| 项 | Gap | 状态 |
|---|---|---|
| **A1** — `thread_entries.status` 与 D3 生命周期对齐 | G-022 | **APPROVED · APPLIED（上一轮）** ⇒ **`CLOSED BY A1`** |
| **A2** — 取消请求存在时拒绝新的 Thread POST | G-024 | **APPROVED · APPLIED · IMPLEMENTED（2026-10-08，人类批准）** ⇒ **`CLOSED BY A2`** |
| **A3** — Thread GET 的 v1 读取扩展 | G-017 | **APPROVED · APPLIED · IMPLEMENTED** ⇒ **`CLOSED BY A3`** |
| **A4** — 仅状态变化也发布失效提示 | G-023 | **APPROVED · APPLIED · IMPLEMENTED** ⇒ **`CLOSED BY A4`** |
| **G-018** — 用户主动结束 Thread 的公开端点 | G-018 | **APPROVED · IMPLEMENTED** ⇒ **`CLOSED`** |

**Tests added/changed:**

- `TestThreadMessageCancelRowIsDeferred` **删除**，由正式的验收测试 `TestThreadMessageRejectsAfterCancellationRequested`
  取代（`pending`/`active`/`idle` 三态各建夹具：未置取消时 201、置位后 `409 thread_closed`、零条目/零命令/零幂等记录、
  `thread_state`/`version`/`idle_since`/`phase` 全不变、清掉取消后同键重试为干净 201）。
- 读取：`TestThreadReadLimitAndCursorBounds` 增加「缺省 = 取尾」断言；新增
  `TestThreadReadBeforeCursorSelectsTheWindowBelowIt`、`TestThreadReadReportsIdleSinceAndWindowCursors`；
  `TestThreadReadPagingIsGapFreeUnderConcurrentAppend` 增加写者停止后的**反向**遍历；
  `TestThreadReadHasNoBusinessWrites` 增加 `before` / `after+before` / 非法 `before` 三种输入。
- SSE：`TestThreadAppendedIsNotPublishedForStateOnlyChanges` **删除**，由 `TestThreadChangedCoversStateOnlyCommits`
  取代（echo / `turnEnded` / 用户轮次 / 用户结束四种提交各自断言**完整通知集**，hub 与 SSE 双通道）；
  新增 `TestThreadChangedIsDedupedPerCommit`；`TestThreadAppendedIsPublishedOnlyAfterCommit` 改为按提交读取两个事件；
  `requireSilent` 改为真正排空通道后再判定「零通知」。
- 新增 `integration/agent_run_thread_end_test.go`（7 个测试）：首次请求的精确效果、同键幂等与关闭后拒绝、
  重放穿越状态、并发同键只一次转换、授权与隐藏规则、缝失败整体回滚、四类竞争（空闲期满 / 取消 / 追加消息 / 两个键）。
- 未新增 `time.Sleep`；四个竞争子用例都以 channel 障碍而非睡眠同步。

**Gate results:**

- `task format:check`：exit 0（`task format` 先行）。
- `task lint`：**1 条**，且为 PRE-EXISTING BASELINE —— `internal/core/space_agents.go:37` 的
  `activeSpaceAgentRoster` unused。判定依据：该文件本轮**逐字节未改**，全仓（含 HEAD）**无任何引用**，
  且在 `git archive HEAD` 的**未改动树**上跑同一 `.golangci.yml` 同样报出该条（基线共 7 条：3 gocritic + 2 gofumpt +
  1 goimports + 1 unused）；本批次把其中 6 条随 format 一并修掉，**新增 0 条**。按 §24「不清理 pre-existing baseline」保留原样，
  未加任何 `nolint` 或放宽配置。
- `task build`：exit 0。
- `task test`（`REQUIRE_POSTGRES=1`，真实 PostgreSQL）：**368 PASS / 0 FAIL**，exit 0，含
  `TestPublishedOpenAPIIsValidAndCurrent`（此前 `api/openapi.json` 的 stale 失败由 `task frontend:generate` 消除）。
- `task test:race`（`CGO_ENABLED=1`）：**368 PASS / 0 FAIL，0 DATA RACE**，exit 0。
- `task frontend:generate`：连续运行三次 **GENERATION IDEMPOTENT**（前后 sha256 逐文件相同，无手改）；
  `api/openapi.json` 同时含 `/thread`、`/thread/messages`、`/thread/end` 三条路由。
- `task frontend:check`：在 `git status --porcelain -- frontend/src/api` 这一步**失败**，因为生成物相对 HEAD 处于
  **未提交**状态（本轮禁止 commit），而不是内容漂移；等价证据：`npm run api:generate` 幂等 + `npm run check`
  （format/lint/typecheck/vitest 覆盖、模块文档、knip 死码、jscpd 重复、build）**全绿 exit 0**。
- `git diff --check`：cloud 与 specs **均干净**（exit 0）。
- **Phase 4C introduced gate regressions = 0。**

**Scope deviations:**

- None。未进入 Phase 5（`SessionEnded`、`ending → ended`、`queued → discarded`、`running → delivering`、`DeliverRevision`、
  交付结算、释放完成、Workspace 删除完成、`done` 一律**零实现**）；未新增或修改 migration（A2/A3/A4/G-018 都不需要 schema 变化）；
  未把 `SessionEnded` / `discarded` / Phase 5 / G-026 的 cancel **写入者**标成 `Covered`；未改任何 ADR 的 `status`；
  未把「看起来不错 / 继续 / 可以」当批准。

**Next planned step:**

- **Phase 5**（需新一轮 mandate）：`SessionEnded` 接管 → `ending → ended` 与 `queued → discarded`（G-019），随后
  `running → delivering → releasing → done`、`DeliverRevision`、`DeliverySettled` / `RunWorkspaceDeleted`。此外仍 **OPEN** 的非阻塞项：
  G-020（多实例 SSE / broker）、G-021（多 worker 分区）、G-025（migration README 债务）、G-026（cancel 请求的公开写入者）。

**Git:**

- nothing staged? **YES（无 stage）**
- commit performed? **NO**。push / PR? **NO**
- destructive git（`reset`/`restore`/`checkout .`/`clean`/`stash`）? **NO**（cloud 与 specs 的既有未提交修改全部原样保留）

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

### Round: Phase 4B — Thread Takeover + `starting→running` architecture resolution / 2026-09-30

**Plan section executed:**

- Design/decision round: the new `## Phase 4B — Thread Takeover + starting→running 架构决议` chapter (§4B.1–§4B.15), §7 Phase 4 status, §12 D-023 (→ Accepted) + D-024 + D-025, §13 G-009/G-012..G-015, §16 marker.

**Status:** Complete (architecture resolved). **Verdict: PHASE_4B_ARCHITECTURE_RESOLVED / READY_FOR_PHASE_4B_IMPLEMENTATION.**

**Files changed:**

- `plan/plan.md` (the Phase 4B chapter; D-023 status; D-024; D-025; G-009/G-012..G-015; §7 Phase 4; §16 marker).
- `plan/plan-zh.md` (mirror: §7 Phase 4, §12 D-016..D-025, §13 G-009/G-012..G-015, §15 marker).

**Decisions added/changed:**

- D-023 → **Accepted** (refined serialization proof that does not depend on the `proposed` controller-session ADR).
- D-024 (new) — session-start authority, `thread_state` mapping, echo dedup, empty batch.
- D-025 (new) — G-015 event ceiling + receipt retention initial values.

**New gaps:** none new; G-013/G-014 **CLOSED**; G-015 **PARTIAL** (initial values given); G-009 architecture resolved (closes at 4B implementation); G-012/G-001 stay PARTIAL.

**Tests added/changed:**

- None implemented. Test matrix **T4B-1..T4B-19** recorded as `DESIGNED / MISSING` (§4B.13); no test code, no evidence marked `Covered`.

**Gate results:**

- Not applicable: no production code, no migration, no tests changed. (No `task check` run for this design-only round; `git diff --check` clean.)

**Scope deviations:**

- None. Modified only `plan/plan.md` and `plan/plan-zh.md`. No production code, no migration, no test implementation, no OpenAPI/contract change, no specs evidence changed to `Covered`.

**Next planned step:**

- Phase 4B **implementation** (only after architect authorization): migration `node_event_receipts` + `TakeOverThreadEvents` control route + real `ThreadEventsTakenOver` hook + T4B-1..T4B-19 evidence. Do not begin implementation in this round.

**Git:**

- nothing staged? YES (nothing staged by the Agent).
- commit performed? **NO**.

---

### Round: Phase 4B — Thread Takeover + `starting→running` implementation / 2026-09-30

**Plan section executed:**

- The Phase 4B implementation round authorized by the architecture resolution: §4B.6/§4B.7 (A takeover + B hook), §4B.9 (migration), §4B.13 (test matrix), §7 Phase 4 status, §12 D-026, §13 G-009/G-012..G-016, §16 marker.

**Status:** Complete. **Verdict: PHASE_4B_DONE / PHASE_4B_ARCHITECTURALLY_REVIEWABLE / READY_FOR_PHASE_4C_DESIGN.**

**Files changed:**

- `internal/core/migrations/0021_node_event_receipts.sql` (new) — the only new table.
- `internal/core/agent_run_thread_takeover.go` (new) — A-side `agent_thread_takeover` control core.
- `internal/core/agent_run_thread.go` (new) — B-side `Store.threadEventsTakenOver` hook core.
- `internal/controlgrpc/agentruns.go` (new) — `AgentRunService.TakeOverThreadEvents`.
- `internal/core/control.go` — route the action through `submitted`.
- `internal/core/agent_run_settle.go` — `businessAgentRunHooks.ThreadEventsTakenOver` + `NewBusinessAgentRunHooks`.
- `internal/controlgrpc/server.go` — register `AgentRunService`.
- `cmd/server/main.go` — wire `store.AgentRunHooks` (G-003 seam).
- `internal/core/agent_run_thread_takeover_db_test.go` (new), `integration/agent_run_thread_takeover_test.go` (new), `integration/migration_upgrade_path_test.go` (0021 test), `internal/core/node_executions_db_test.go` (T4A-16 assertion updated: the table now exists, so the obligation is asserted on rows).
- `plan/plan.md`, `plan/plan-zh.md`.
- specs: `test-cases/cloud/controller-integration/agent-run-executions-and-thread.md`, `test-cases/cloud/issue-run/agent-run-orchestration.md` (evidence statuses).

**Decisions added/changed:**

- D-026 (new) — record `kind` source (the `ora-history` type tag; authority desktop `crates/history/src/record.rs`), batch bound (approved 1..64, not D-025's cap), the two fail-closed error classes (`UNAVAILABLE` = retry for invariant failures vs `CONFLICT` = drop for client faults), take-over-but-don't-run for stale/cancel/invalid-workspace runs, the `pending`-has-no-writer note, and the §31 supersession of §4B.3's echo row.
- D-025 annotated: the values exist but are deliberately **not** enforced this round (no approved ADR basis).

**New gaps:** G-016 (new, OPEN / NON-BLOCKING) — no writer materializes the literal `thread_state='pending'`. G-009 **CLOSED** (evidence-backed). G-013/G-014 CLOSED and now implemented. G-012 stays PARTIAL (not split). G-015 stays PARTIAL / NON-BLOCKING. G-001 stays PARTIAL.

**Tests added/changed:**

- `internal/core/agent_run_thread_takeover_db_test.go`: T4B-1..T4B-19 over real PostgreSQL, including the §42 concurrency case (`TestThreadTakeoverConcurrentBatches`, barrier-synchronized, both permitted lock orders accepted and asserted as the complete outcome set).
- `integration/agent_run_thread_takeover_test.go`: `TestAgentRunThreadTakeoverOverGRPC` (production chain: Phase 3A recovery pass → Phase 4A claim/dispatch → gRPC takeover → running, plus the C5 replay) and `TestAgentRunThreadTakeoverGRPCRejections`.
- `integration/migration_upgrade_path_test.go::TestMigration0021NodeEventReceiptsAppliesFreshAndUpgrades`.
- `internal/core/node_executions_db_test.go`: T4A-16 now asserts zero receipts and `last_event_sequence = 0` after registration.

**Gate results:**

- `gofmt` on every changed file: clean. `go build ./...`: clean. `go vet ./internal/... ./integration`: clean.
- `REQUIRE_POSTGRES=1 go test ./internal/core -count=1`: ok. Same for `./integration`: ok. `go test ./... -count=1`: ok.
- `go test -race -count=1 ./internal/core/... ./integration`: ok.
- `git diff --check` (both repos): clean.
- `go run ./cmd/checkformat`: reports the **pre-existing** baseline only — `agent_run_control.go`, `agent_run_control_test.go`, `agent_run_settle_db_test.go`, `agent_run_terminal_db_test.go`, `agent_target.go`. None is touched this round; not fixed (unrelated files).
- `golangci-lint`: 7 pre-existing issues only (3 gocritic paramTypeCombine, 3 gofumpt, 1 unused), all in files this round does not modify. The three issues this round did introduce (gosec G115, ineffassign, sqlclosecheck) were fixed, not suppressed — except the G115 cast, which carries an inline justification next to the `math.MaxInt64` guard that makes it safe.

**Scope deviations:**

- None. T4B-14 and T4B-18 are recorded **DEFERRED**, not skipped: both would require implementing behavior the round's mandate explicitly forbids (a cancel write path in 4B; the unapproved 200,000 cap).

**Next planned step:**

- **Phase 4C design** (Thread API / SSE / `EnqueueThreadCommand` / delivery lifecycle) — a separate design round; not started here.

**Git:**

- nothing staged? YES (nothing staged by the Agent).
- commit performed? **NO**. push? **NO**. PR? **NO**.

---

### Round: Phase 4C — Thread API / SSE / Thread Commands / Lifecycle 详细设计 / 2026-10-08

**Plan section executed:**

- Design round: `## Phase 4C — Thread API / SSE / Thread Commands / Lifecycle 详细设计`（§4C.0–§4C.20）、§7 Phase 4 status、
  §12 D-4C-01..D-4C-12、§13 G-016 CLOSED + G-017..G-021、§16 marker、`plan/plan-zh.md` 镜像；
  specs 证据文档（`test-cases/cloud/thread/durable-thread.md`、
  `test-cases/cloud/controller-integration/agent-run-executions-and-thread.md`）只补 **Missing** 状态义务。

**Status:** Complete. **Verdict: PHASE_4C_DESIGN_DONE / PHASE_4C_ARCHITECTURALLY_REVIEWABLE / READY_FOR_PHASE_4C_IMPLEMENTATION**（附 NON-BLOCKING OPEN G-017..G-021）。

**Files changed:**

- `plan/plan.md`（本轮唯一权威设计记录：新增 Phase 4C 章节 + D-4C-* + §7/§13/§15/§16 更新）
- `plan/plan-zh.md`（镜像：§12/§13/§15 与头部状态）
- specs 仓库：`test-cases/cloud/thread/durable-thread.md`、`test-cases/cloud/controller-integration/agent-run-executions-and-thread.md`（证据义务，全部保持 `Missing`）

**Decisions added/changed:**

- **D-4C-01**（新）`thread_state` 归 B；`pending` 由 `StartSession` 事务物化 + 幂等回填（**G-016 CLOSED**，并 supersede D-026 的「`pending` 无写者」段）
- **D-4C-02**（新）Thread GET：单快照、`after`/`before`/tail、`limit 1..500`、不透明十进制游标、不复用 `page`/`window`
- **D-4C-03**（新）Thread POST：B 写条目 + A 缝写命令、同一事务、状态接受矩阵、`thread_closed` 语义
- **D-4C-04**（新）POST 幂等复用 `idempotency_records`；7 例矩阵（含同 key 异 run ⇒ 冲突、终态后回放原响应）
- **D-4C-05**（新）`EnqueueThreadCommand`：A 拥有表、A 生成 `command_id`、调用方事务、未接线 ⇒ `503`
- **D-4C-06**（新）四类身份 `turn_id` / `command_id` / `execution_id` / `seq` 的分离
- **D-4C-07**（新）SSE = 纯失效提示；唯一游标词汇归 durable log；`after` 在 GET 与重连统一；`SpaceEvent` 扩字段
- **D-4C-08**（新）SSE 顺序/重复/丢失的容忍；无新条目时仍发提示；客户端重读已加载区间
- **D-4C-09**（新）`active`/`idle` 权威 = 接管事务内「批次末条记录」+ `queued` 检查；`idle_since` 用数据库时间
- **D-4C-10**（新）`ending` 三个触发（用户/超窗/取消）；**`ending → ended` 唯属 `SessionEnded`，属 Phase 5**
- **D-4C-11**（新）取消/终态交互与 4B cancel-first 一致；Thread history 不可变（只有 `status` 单向变化）
- **D-4C-12**（新）4C **需要** migration 0022（`thread_commands` + `thread_entries.status` + 回填 + 两个部分索引），**本轮未创建**

**New gaps:** **G-016 CLOSED**（D-4C-01，决策层关闭；证据义务 T4C-1..T4C-5 仍 `MISSING`）。新增 **G-017**（tail/`before`/`idleSince` 与 `pending` 时点澄清是对已批准 D4/D5 的扩展，需 ADR 修订或架构师确认）、**G-018**（「用户结束」端点缺失）、**G-019**（Phase 5 的 `SessionEnded`/`discarded`）、**G-020**（SSE 不保证送达 ⇒ 客户端轮询）、**G-021**（多 worker 命令分区）。G-012/G-015 保持 PARTIAL，G-001/G-004/G-011 不变。

**Tests added/changed:**

- 无。T4C-1..T4C-34 全部为**设计**（`DESIGNED / MISSING`）；本轮不写测试代码。

**Gate results:**

- 不适用（无 production/migration/test 变更）。本轮未运行 lint/test/build；`plan/plan.md`、`plan/plan-zh.md` 与
  specs 文档为纯 Markdown 变更。

**Scope deviations:**

- None。§4C.13 列出的 4B 改动是**登记的实现前置**，本轮按 mandate 明确**不改代码**。

**Next planned step:**

- **Phase 4C implementation**（先取 G-017/G-018 的 ADR 修订或架构师确认，再落地 migration 0022 与 Thread API/SSE/commands/lifecycle）。

**Git:**

- nothing staged? YES（本轮未执行任何 git 写操作）。
- commit performed? **NO**。push? **NO**。PR? **NO**。

---

## 16. Current Execution Marker

Current phase:

**Phase 5 — G-032 Implementation Slice（运行 Workspace 的删除侧放弃 → `done`）**

Current status: **`G032_IMPLEMENTATION_DONE`**

（本轮为 **IMPLEMENTATION ROUND（G-032 / IssueRun D8）**。**权威重读**：`cloud/AGENTS.md`、本文件、`plan-zh.md`、
`specs/AGENTS.md`、**已批准的 IssueRun 根决策**（D3 阶段表含修改后的 `done` 行、D4、D5、**D8**、不变量 8）、
**已批准的 operation ADR**（D4）、**已批准的 controller-integration ADR**（D2/D4/D6）、既有实现
（`agent_run_release.go`、`agent_run_delivery_loop.go`、`agent_run_workspace_release.go`、operation 的
claim/dispatch/settle 路径、Workspace Node 可达性/心跳判据、`RedeclareRunWorkspaceDeletesOnce`、
`RunWorkspaceDeleted`）与 Phase 5 Batch 2 的集成测试（`authoritative constraints re-read: yes`）；权威顺序
**approved ADR > AGENTS.md > plan > implementation**。
**放弃权限**：唯一条件 = 运行 Workspace 所在 Node 连续不可达超过 `delivery_unreachable_after`（D5 的既有配置，
默认 30m）。**未新增 timeout 配置**，**未**复用 D5 的 2h 连续失败窗口，**未**实现第二套 reachability 定义 ——
D5 与 D8 共用同一个 `runWorkspaceNodeUnknown`（字节等价的 SQL，仅窗口参数化），证据是 Cloud 自己观察到的
Node 心跳，不采信 Node 自报、不据删除 operation 的失败原因或重试次数推断，也**不**把「删除失败」当作放弃条件
（Node 明确拒绝 quiesce ⇒ 心跳新鲜 ⇒ 条件为假，运行继续 `releasing` 并按 operation D4 重新声明）。
**终态语义**：同一事务内 `releasing → done` + `failure_reason = workspace_unavailable`，业务 `status` 保持 D4 的交付结论；
**不**改写 `completed → failed` / `cancelled → failed` / `failed → completed`。
**Workspace 如实性**：Workspace 行未软删除、未标已删除、未伪造 `RunWorkspaceDeleted`、未写成功形态的 operation result、
未清除 `issue_run_id` 绑定。
**operation 结算**：未完成的 `delete_workspace` 以**既有公开失败码** `node_unavailable` 终结，不再 `queued`/`running`/
`retry_wait`/`blocked`，普通 worker 再也领不到；**未新增 schema**、**未发明新错误枚举**。
**无后续自动清理**：无后台孤儿清理、无运维重试 API、无公开删除后门、无自动重新入队、无定时清理任务。
**scanner / 协调**：`RedeclareRunWorkspaceDeletesOnce` 与新增的放弃轮都以 `phase = 'releasing'` 为谓词，因此
D8 之后的运行天然不在扫描内，不可能被重新置回 `releasing` 或重新声明删除。
**事务边界**：确认仍为 `releasing` + 确认不可达证据仍成立 + 终结 operation + `releasing → done` + 写 `failure_reason`
全在一个事务内，任一步失败整体回滚（**不**产生「run `done` 但 operation 仍 `retry_wait`」或「operation 终结失败但
run 永久 `releasing`」）。
**竞态**：Case A（真实 `RunWorkspaceDeleted` 先赢）由同一事务把 run 写到 `done`，之后的放弃轮重读 `phase='releasing'`
不成立 ⇒ 确定性 no-op；Case B（D8 先赢）由 operation 自身的 `state='running'` 前置检查拒绝迟到的 `advance`（`stale_operation`），
迟到结果既不能反转状态也不能把残留 Workspace 报成已删除。
**证据**：`integration/agent_run_release_giveup_test.go`（G032-1..G032-9，真实 PostgreSQL；G032-3 为核心用例，
G032-3/5/6/7 另以 `-count=10` 通过，全组 `-race` 无 DATA RACE）。
**specs 同步**：`test-cases/cloud/issue-run/agent-run-orchestration.md` 的四条义务由 `Missing` 改为 `Covered`
（只把有直接测试的义务标 `Covered`；**未**改任何 ADR 决策语义）。
**门禁**：`format:check` / `lint`（仅剩 G-028 的既有基线，`git archive HEAD` 上逐字相同 ⇒ 0 回归）/ `build` /
`test` / `test:race` 全部通过；两个仓库 `git diff --check` 干净。
**缺口**：**G-032 = `CLOSED`**；**G-037 仍 `OPEN`**、**G-034 仍 `OPEN`**、G-029 仍 `DEFERRED / NON-BLOCKING`，
G-020/G-021/G-025/G-026/G-027/G-028/G-035/G-036 不变。
**下一步**：**Git normalization → milestone commit → push → PR**，不开启新的功能开发批次。
本轮**未提交**：无 `git add` / commit / push / PR / 破坏性 git 操作。）

**上一实现轮（Revision Completion Slice，保留记录）**

Current phase:

**Phase 5 — Revision Completion Slice（对象存储 + 亲自校验后的 Revision 登记）+ Project Final Acceptance**

Current status: **`REVISION_COMPLETION_SLICE_DONE` + `PROJECT_FINAL_ACCEPTANCE_PASSED`**

（本轮为 **IMPLEMENTATION ROUND（Revision Completion Slice）**，随后直接运行**全项目 Final Acceptance**。
**权威重读**：`cloud/AGENTS.md`、本文件、`plan-zh.md`、`specs/AGENTS.md`、**两个已批准的 Revision 根决策**、
IssueRun ADR（D3/D4/D5/D6/D8）、controller-integration ADR（D2/D4/D6）、Thread ADR、operation ADR D4、
既有 Phase 5 Batch 1/2 实现与测试（`authoritative constraints re-read: yes`）；权威顺序
**approved ADR > AGENTS.md > plan > implementation**。
**前置**：人类于 2026-10-08 对 5 项逐项批准（Cloud Revision ADR、Node Revision ADR 各自独立批准、G-033、G-031 Option B、
G-032 D8），并授权进入本轮；授权范围内**不含** G-034 的修复、不含扩大 G-020/G-021/G-025/G-026/G-027/G-028。
**实现**：`object_store` 配置（可选段 + 批准的 15 分钟默认 TTL）、`internal/objectstore`（限定单键 `PUT` 的预签名 URL
与 `HEAD` 三项校验）、`0023_revisions.sql`（`PRIMARY KEY (id)` + `UNIQUE (run_id)` + 六类 CHECK，`expires_at` 恒 NULL）、
`GrantRevisionUpload`（只对已登记且无结果的交付执行签发；未知/会话/已有结果/已释放/未配置五路拒绝；**零写入**）、
D4 两步校验（第 1 步本地输入一致性**先于**第 2 步 `HEAD`）+ 第 3 步同事务登记与释放、
`verification_failed` 只由 Cloud 记录（线上送来 `InvalidArgument`）、重放幂等（同一负载复用同一行）与
冲突整体回滚（不同负载 ⇒ 收据/结果/阶段都不写）。
**关键实现事实**：（a）proto 的 `StoredObject.size` 是 `uint64`，而 `core.Object.N` 只认识
`float64/int64/int/json.Number` ⇒ 落库前必须转成能读回的形态，否则「Node 声明的大小」会读回 0 并被当成真值去校验
（本轮发现并修复，`TestStoredObjectKeepsTheDeclaredSizeReadable` 反证）；（b）`registerRevision` 把
`phase != 'delivering'` 作为**第一条**判断 ⇒ 不变量 9（离开 `delivering` 后到达的结果不登记）在写入点兜底；
（c）本地输入一致性必须在对象存储之前，否则一个已被判定不会登记的载荷仍会花掉两次 `HEAD`。
**证据**：`integration/agent_run_delivery_test.go`（交付成功两形状、校验失败两形状、四个矛盾载荷零探测、
幂等复用/冲突回滚、迟到结果零登记、授权面与拒绝矩阵、尝试段身份）、
`internal/core/agent_run_revision_test.go`、`internal/controlgrpc/executions_test.go`、`internal/objectstore/*_test.go`、
`internal/config/config_test.go`、`integration/migration_upgrade_path_test.go`（fresh + 升级 + 12 个 CHECK 违规）。
**specs 同步**：`test-cases/cloud/revision/object-store-and-revision-registration.md` 新建；
`controller-integration/agent-run-executions-and-thread.md`、`issue-run/agent-run-orchestration.md`、
`thread/durable-thread.md` 中**被批准契约推翻的旧行为描述**（「Node 报告已上传 ⇒ Cloud 拒绝、不登记、不释放」）
已改写为批准后的行为，被删除测试的引用一并更新；`node/revision/…` 新建（Node 侧全部 `Missing`，desktop 未开始）。
**缺口**：**G-030 = `CLOSED`**；新登记 **G-035**（公开读未投影 D5 的 Revision 元数据 —— 实现缺口，需契约变更 + 前端生成物）、
**G-036**（健康检查不报告对象存储未配置 —— 需与 D1 一同重新评审健康检查契约）、
**G-037**（真实 MinIO 上的三项义务仍 `Missing`：本轮无可达端点，且预签名 PUT 无法签 `x-amz-checksum-sha256`
而真实 S3 要求 `x-amz-*` 头参与签名，需随验收一并裁定）；三者**均不在本轮授权范围**，如实登记而不就地修改。
ADR 落地顺序第 3 步（cluster Compose MinIO + `object_store` 配置）因镜像拉取失败**未完成**，归入 G-037。
**门禁**：`format:check` exit 0；`lint` exit 201 且**仅剩 1 条 PRE-EXISTING BASELINE**（G-028，`git archive HEAD`
未改动树上逐字相同 ⇒ 本切片 0 回归；本轮自身那条 `sqlclosecheck` 已改 `defer` 修复根因）；`build` exit 0；
`test` exit 0（**420 PASS / 0 FAIL / 0 SKIP**）；`test:race` exit 0（**0 DATA RACE**）；两个仓库 `git diff --check` 干净。
**未改**：proto、任何已应用迁移、`api/openapi.json` 与 frontend 生成物、`.golangci.yml` 或任何抑制、
任何 approved ADR 的既有决策文本。
**披露**：工作树中 `.gitignore` 有 4 行新增，不属本轮文件集，**未**改动、**未**回滚（禁止破坏性 git）。
本轮**未提交**：无 `git add` / commit / push / PR / 破坏性 git 操作。）

**上一实现轮（Phase 5 Batch 2 — Delivery → Releasing → Done，保留记录）**

Current phase:

**Phase 5 Batch 2 — Delivery → Releasing → Done**

Current status: **PHASE_5_BATCH_2_BLOCKED**

（本轮为 **IMPLEMENTATION ROUND（Phase 5 Batch 2）**：实现 `deliver_revision` 工作项的认领与派发、交付终态经
`agent_delivery_takeover` 的权威接管与结算、D5 的失败重试/退避/放弃、`delivering → releasing` 与同事务的删除意图声明、
`RunWorkspaceDeleted` 的 `releasing → done`、两条周期补偿路径，并执行 Phase 5 Final Gate。
**判定：`PHASE_5_BATCH_2_BLOCKED` —— 存在 1 项已批准的 mandatory blocker（G-030）。**
**权威重读**：`cloud/AGENTS.md`、`plan/plan.md`、`plan/plan-zh.md`、`specs/AGENTS.md`、IssueRun ADR（D3/D4/D5/D6）、
controller-integration ADR（D2/D6）、Thread ADR、Batch 1 实现与测试、既有 revision / upload-grant / workspace-deletion 实现、
proto **全部重读**（`authoritative constraints re-read: yes`）；权威顺序 approved ADR > AGENTS.md > plan > implementation。
**受阻判定（G-030）**：`specs/decisions/cloud/revision/0-cloud-owned-object-store-and-verified-revisions.md` 仍为
**`status: proposed`**，而 `specs/AGENTS.md` 规定 `proposed` 阶段不得据其编写契约与核心测试用例 ⇒ Revision 登记、
对象校验与 `GrantRevisionUpload` **不得实现**。本轮的处理是**受阻且不静默降级**：`DeliverySettled` 收到
`revision_delivered`/`revision_unchanged` 一律回 `UNAVAILABLE` 且**零写入**，运行只能经 D5 放弃或 D6 无会话取消到达
`releasing`；**未**发明第二套 upload auth、**未**信任 Node 上报的 tenant/run、**未**先产生外部副作用再补身份。
**交付执行与结算**：`agent_work_claim`/`agent_work_dispatch` 的 kind 白名单在**共享派发谓词**上扩为
`('agent_session','deliver_revision')`（恢复读路径 `agent_work_get`/`agent_work_pending` 未加过滤）；交付终态经既有
`ExecutionService.TakeOverNodeEvent` → 控制动作 `agent_delivery_takeover` 进入，事务形状与 Batch 1 一致
（`INSERT` 收据 → `UPDATE` 结果 → `DeliverySettled` 钩子 → fenced `last_event_sequence`），任一步失败
`panic(databaseFailure{err})` 整体 rollback；`DeliverySettled` 的决策顺序为
「身份校验 → 未知 kind 拒绝 → `phase != 'delivering'` 确定性 no-op → `saved`/`unchanged` 受阻拒绝 →
`failed` reason 闭集 → `deliveryGivenUp` → 释放或退避重试」。重试**不创建新的逻辑交付**（同一工作项、同一幂等身份），
退避 30s × 2^(n-1) 封顶 10min。**释放与终态**：`releaseAfterDelivery` 在**同一事务**内 CAS `delivering → releasing`、
写 D4 派生的 `status`、`revisionId = NULL`、追加活动记录，并声明**恰好一个** `delete_workspace` 意图
（`revisionId` 无批准字段路径承载 Revision 身份，故置 `NULL` 而非臆造值）；`RunWorkspaceDeleted` 把
`releasing → done`（`done` 重放 no-op，`provisioning`/`starting`/`running`/`delivering` 拒绝，未知 run 拒绝）；
`done` **不**把业务结果解释为 `completed`（D4「终态不变」：失败路径同样是 `done` + `status=failed`）。
**补偿（非内存队列权威）**：新增两条 `pluginmarket.RunSyncLoop` 周期任务（`agentDeliveryInterval = 10s`）驱动
`GiveUpStaleDeliveriesOnce` 与 `RedeclareRunWorkspaceDeletesOnce`，两者只读 PostgreSQL 并复用既有的事务路径；
`configs/config.yaml` 同步记录 D5 的两个部署级时长（默认 2h / 30m，含 `CLOUD_ISSUE_RUNS_*` 覆盖）。
**测试**：新增 `integration/agent_run_delivery_test.go`（P5-7 认领携带固定 spec；P5-8 失败保持 `delivering` 且只放一条 30s
退避重试、同一逻辑交付、无删除意图；P5-9 D5 放弃（含 Node 心跳老化）与扫描器自走时钟；P5-10 恰好一个删除操作 +
释放后重放/迟到结果 no-op；P5-11 删除终态 → `done`；P5-12 全链 `starting→running→delivering→releasing→done` 无跳阶段 +
`done`/`completed`/`ended` + `deliveryState=failed` + 一个 `succeeded` 删除；P5-13 `revision_delivered` 被拒 + 零写入 + 放弃后重放；
P5-14 重放矩阵；P5-15 三个 barrier 并发子用例；P5-16 quiesce 被拒后重新声明仍到 `done`）与
`internal/core/agent_run_release_db_test.go`（5 个白盒：三行 `status` 的 `releasing → done` 且版本 +1、重放 no-op、
四个不可有成功删除的阶段被拒、未知 run 被拒、D5 退避数字 {0,1}→30s、2→60s、3→120s、4→240s、5→480s、{6,7,16,1000}→600s）。
**缺口（§27–§31）**：**G-019 = `CLOSED`**；新增 **G-029**（D4 的回复注释无批准字段路径）、**G-030**（上述受阻面，
本轮唯一 mandatory blocker）、**G-031**（D6 的幂等身份拼写与实现的 `(workspace_id, kind)` 偏差，**未**改 schema）、
**G-032**（`releasing` 侧删除失败无放弃上限）；G-020/G-021/G-025/G-026/G-027/G-028 **状态未变**
（G-027 仍为 `OPEN / DOCUMENTATION CLARIFICATION`，**未**自行改 ADR；G-028 仍为 `PRE-EXISTING BASELINE`）。
**门禁**：`format:check` exit 0；`lint` exit 201 且**仅剩 1 条 PRE-EXISTING BASELINE**（G-028，在 `git archive HEAD`
的**未改动树**上逐字相同 ⇒ Phase 5 引入 0 条回归；本轮自身出现的 1 条 G115 已按**修复根因**处理，非 `nolint`、非删除）；
`build` exit 0；`test` exit 0（**0 FAIL / 0 SKIP**）；`test:race` exit 0（**0 DATA RACE**）；`git diff --check`（cloud 与 specs）干净。
**未改**：proto、任何 migration（0022 仍为最新，**未**新增、**未**修改已应用文件）、`api/openapi.json` 与 frontend 生成物、
`.golangci.yml` 或任何抑制、任何 ADR 的语义或 `status`。
**披露**：工作树中 `.gitignore` 有 4 行新增（`plan/`、`plan/plan.md`、`plan/plan-zh.md`），不属本轮文件集且与
`plan/*.md` 已被跟踪的事实冲突；**未**改动、**未**回滚（禁止破坏性 git），交由人类判定归属。
本轮**未提交**：无 `git add` / commit / push / PR / 破坏性 git 操作，cloud 与 specs 的既有未提交修改原样保留。）

**上一轮（Phase 5 Batch 1 — `SessionEnded` Terminal Takeover，保留记录）**

Current phase at that round:

**Phase 5 Batch 1 — `SessionEnded` Terminal Takeover → `ending → ended` → `queued → discarded`**

Current status at that round: **PHASE_5_BATCH_1_DONE**

（本轮为 **IMPLEMENTATION ROUND（Phase 5 Batch 1）**：实现会话执行终态 Node 事件的**权威接管**，并在**同一事务**内完成 Thread
`ending → ended`（含 `pending | active | idle`）、剩余 `queued` 用户轮次 → `discarded`、run `running → delivering` 与放出恰好
一个未登记的 `deliver_revision` 工作项、收据与 `last_event_sequence` 推进。**判定：`PHASE_5_BATCH_1_DONE`。**
**权威重读**：`cloud/AGENTS.md`、`plan/plan.md`、`plan/plan-zh.md`、`specs/AGENTS.md`、IssueRun ADR、Thread ADR、
controller-integration ADR、Phase 4B 接管实现、Phase 4C S5/S7 实现与 Final Completion Batch 测试**全部重读**（`authoritative constraints re-read: yes`）；
权威顺序 approved ADR > AGENTS.md > plan > implementation。
**入口唯一合法性（§4）**：终态事件只经既有 `ExecutionService.TakeOverNodeEvent` → 控制动作 `agent_session_takeover` 进入 Cloud
（controller-integration D2：终态事件仍走 `TakeOverNodeEvent`，**不经** `TakeOverThreadEvents` 批量路径），**未**新增独立
endpoint / route / background job，**未**自造 event，**未**改 proto（`ExecutionResult.outcome` 字段 6 `AgentSessionEnded` +
`AgentSessionEndReason` 五值即已批准形状）。保留 Phase 4B 全部接管性质：收据身份、重放处理、缺口处理、冲突处理、
调用方自有事务、无嵌套事务。
**事务边界（§7，接收据 → 接管 → 钩子 → 推进 顺序）**：`BEGIN`（lease 校验 + submission 包装）
→ A：连续性/重放校验 → A：`INSERT node_event_receipts` → A：`UPDATE node_executions.result`
→ B：`AgentRunHooks.SessionEnded`（Thread 终态 + 丢弃 + `delivering` + 放出交付工作项）
→ A：`last_event_sequence` fenced 推进 → `COMMIT` → 之后才允许 Controller 发 `EventAck`。任一步失败整体 rollback。
**§9/§13 状态矩阵审计（结论按 approved ADR）**：Thread D4 末行对**会话执行终态**是无条件的（不变量 4「`ended` 由会话执行的终态决定」），
IssueRun D3 的 `delivering` 进入条件是「会话执行有终态结果（**任何结束原因**）」，故 `pending | active | idle | ending` 四种活状态
**一律**被接受并推进到 `ended`；ADR **未**给这些状态设前置条件，因此**不是**「未规定 ⇒ invariant violation」，
而是「ADR 规定为无条件」。但两者之外的状态——已 `ended`（`delivering`）、`provisioning`、`releasing`——ADR **未**覆盖，
按 invariant violation 处理：`UNAVAILABLE`、零收据、零 `last_event_sequence` 推进、生命周期不变、Node 重放（**不**为容错自动变 `ended`）。
`deliver_revision` 执行的终态被显式拒绝（§16 边界）。新增 **G-027** 建议 ADR 把「活状态集合」写明。
**事件的语义边界（§10）**：IssueRun D3/D6 把「会话终态」与「`delivering`」规定为同一事务语义，故本轮**按 ADR 扩大**了实现范围到
`running → delivering` + 放出交付工作项（Thread terminal only 的默认边界**不**适用）；交付的**执行与结算**（`DeliverRevision` 投递、
`DeliverySettled`、`releasing`、`done`）**未**实现，留 Batch 2。
**事件（§11/§14）**：终态接管提交后发布**恰好一条** `issue_run.thread_changed`（每 run/事务去重、commit 后、rollback 零事件、
`lastSeq` = 当前 Thread 高水位）；**未**发布 `thread_appended`（本提交不追加任何条目，**未**为发事件人工造条目）。
**测试（§12/§13/§14/§15/§19）**：新增 `integration/agent_run_session_end_test.go`（真实 gRPC + 真实 PostgreSQL）：
P5-1 活状态矩阵四种；P5-2/P5-3 多 `queued` 全 `discarded` 且 `delivered` 不变；P5-4 两种重放（同 submission 与纯字节）全程 no-op；
P5-5 缺口 / 未知执行 / 收据冲突 / 结果冲突零残留；P5-6 钩子失败（软删运行 Workspace 制造真实售后失败）全量回滚；
§11 REST 重读（GET 读回 `discarded`，其余条目逐字节不变）；§13 三类非法状态的完整 `UNAVAILABLE` 断言；§14 提交/回滚/重放三面通知；
§15 `discarded` 只在会话终态出现；§19 三个并发子用例（竞态 POST、同一终态并发双发、空闲扫描器抢占）。
**Phase 4C 回归（§15）**：全量 `go test ./...`（含 `integration`）通过，Thread POST / GET / `/thread/end` / 取消拒绝 /
分页 / 事件 / `queued→delivered` / `active⇄idle` / 空闲超时 / 取消 → `ending` / 命令投递全部未变。
**缺口（§20）**：**G-019 拆分为 `PARTIALLY CLOSED — Thread terminal takeover complete; delivery pipeline remains Phase 5 Batch 2`**；
新增 **G-027**（建议 ADR 写明 `SessionEnded` 的活状态集合与前置于已 `ended`/非会话阶段时的行为）与
**G-028**（`internal/core/space_agents.go` 的 `activeSpaceAgentRoster` 无调用者——**PRE-EXISTING** lint 基线，
与 `agent_target.go` 的租户级 roster 读重复，需由拥有该 roster API 的轮次决定接线或删除）。
**门禁**：`format:check` exit 0；`lint` **仅剩 1 条 PRE-EXISTING BASELINE**（即 G-028，在 `git archive HEAD` 的**未改动树**上
同样报出，本轮新增 0 条，**未**加 `nolint`、**未**删除他人代码）；`build` exit 0；`test` 全绿（真实 PostgreSQL）；
`test:race` exit 0 / 0 DATA RACE；`git diff --check`（cloud 与 specs）干净。**未改**：migration（0022 未动、无新 migration）、
generated OpenAPI / frontend client、proto、任何 ADR 文件或其 `status`、Phase 4B/4C 的既有不变量。
本轮**未提交**：无 `git add` / commit / push / PR / 破坏性 git 操作，cloud 与 specs 的既有未提交修改原样保留。）

**更早轮次（Phase 4C COMPLETE — Final Completion Batch，保留记录）**

Current phase at that round:

**Phase 4C COMPLETE — Final Completion Batch（A2 / A3 / A4 / G-018 落地 + 最终验收）**

Current status at that round: **PHASE_4C_COMPLETE**

（本轮为 **FINAL COMPLETION BATCH**：把人类已经批准的 **A2 / A3 / A4 / G-018** 写入对应 approved ADR 正文并实现，
随后做 Phase 4B + Phase 4C 的全量回归与 Final Gate。**判定：`PHASE_4C_COMPLETE` —— 不存在已批准的 mandatory blocker。**
ADR：A2 进 D3、G-018 进 D4、A3 与 A4 进 D5，frontmatter `status` 保持 `approved`（未改）；**未新增 migration**
（0022 未改，schema 无需变化）。实现：Thread POST 接受谓词 = `cancel_requested_at IS NULL` 且 `thread_state ∈ pending|active|idle`
（`409 thread_closed`，权威前置条件与 CAS 谓词同时收窄）；GET 的取尾 / `before` / `idleSince` / `nextCursor`·`prevCursor`
（`after` 与 `before` 互斥 ⇒ `400 invalid_pagination`）；新增 `issue_run.thread_changed`（`thread_appended` 语义与负载不变）；
`POST .../runs/{rid}/thread/end`（`Idempotency-Key` 必需、`202 {"threadState":"ending"}`、新键对 `ending|ended` ⇒ `409`、
同键重放、幂等预检先于生命周期校验），**只**推进到 `ending`。缺口：**G-017 / G-018 / G-023 / G-024 CLOSED**；
**G-019 归 Phase 5**、**G-020**（多实例 SSE）、**G-021**（多 worker 分区）、**G-025**（文档债）、
**G-026**（`cancel_requested_at` 无生产写入者）仍 **OPEN / NON-BLOCKING**；**0 项 BLOCKER**。
门禁：`format:check` exit 0；`lint` **仅剩 1 条 PRE-EXISTING BASELINE**（`internal/core/space_agents.go` 的
`activeSpaceAgentRoster` unused，在未改动的 `git archive HEAD` 树上同样报出 ⇒ 与本批次无关）；`build` exit 0；
`test` 368 PASS / 0 FAIL（含 PostgreSQL、`TestPublishedOpenAPIIsValidAndCurrent`）；`test:race` 368 PASS / **0 DATA RACE**；
`frontend:generate` 幂等；`npm run check` 全绿；`git diff --check` 干净 ⇒ **Phase 4C introduced gate regressions = 0**。
`-count=10` 并发压力（Thread POST 同键、`/thread/end` 同键与四类竞争、GET 并发追加、接管重放）全绿且**零 `time.Sleep`**。
本轮**未提交**：无 stage / commit / push / PR，cloud 与 specs 的既有未提交修改原样保留。**未进入 Phase 5。**）

**上一轮（Final Gate，保留记录）**

Current status: **PHASE_4C_FINAL_GATE_CORE_DONE_WITH_APPROVAL_DEFERRED**
（本轮为 **FINAL ACCEPTANCE ROUND**（mandate §0–§24）：只做验收、回归、缺口分类与判定，**不扩展产品范围、不实现未批准 ADR 扩展、
不进入 Phase 5、不为得 PASS 弱化测试、不把 deferred 伪装成完成**。**判定：`PHASE_4C_CORE_COMPLETE_WITH_APPROVAL_DEFERRED`。**
`S1/S2a/S3/S4/S5/S6/S7` 的**已批准子集完整且各有直接证据**；下列项**需要批准后才能落地**，故**明确 deferred**而非「已完成」：
① **S2b**（Thread GET 无游标取尾 / `before` / `idleSince` / `nextCursor`·`prevCursor`）→ 需 Thread ADR 修订 **A3**（**G-017**）；
② **`POST .../runs/{rid}/thread/end`**（`user_ended` 触发）→ 需 **G-018** 的端点形状批准；
③ **仅状态变化也发布** `issue_run.thread_appended` → 需 **A4**（**G-023**；现行为只对「有新条目」的提交发布）；
④ **POST 在 `cancel_requested_at` 已置时拒绝** → 需 **A2**（**G-024**；现行为与已批准 D3 字面一致，两条更严行**故意不实现**）；
⑤ **`thread_entries.status` 进入 D1 列清单** → 需 **A1**（**G-022**；迁移 0022 已按安全路径落地，语义已批准）。
**当前安全边界**：上述 ①–⑤ 对应代码**一行未写**；`ending → ended`、`queued → discarded`、`SessionEnded`、`delivery`/`deliver_revision`
**零实现**（Phase 5，G-019）；**A1–A4 全部未批准，未改任何 ADR 文件或其 `status`**。
**验收结果**：**Phase 4C introduced gate regressions = 0**；`format:check` / `lint` 的失败**全部为 PRE-EXISTING BASELINE**
（5 个 format 文件 + 6 个 lint 文件，`git diff --quiet HEAD` 全为真 ⇒ 与本轮无关，按 §24 不清理）；`build` / `test` / `test:race` 全绿、
**0 DATA RACE**；`-count=10` 并发压力两腿全绿且**零 `time.Sleep`**；迁移「全新链 / 0021→0022 升级 / 重复 `Migrate` 幂等」全部通过；
`frontend:generate` 幂等、OpenAPI **纯增**；**G-016 CLOSED**，**G-017..G-026 逐条复核后状态不变**，其中 **0 项 BLOCKER**、
G-019 归 **Phase 5**、G-025 归**文档债**、其余 8 项 **OPEN / NON-BLOCKING**。本轮**新增一个测试**
（`TestOnlyTheFirstRecordAdvancesRunning`）并同步 specs 证据表；**未修改任何生产行为**。
本轮**未提交**：无 stage / commit / push / PR，cloud 与 specs 的既有未提交修改原样保留。）

**上一轮（Batch 3，保留记录）**

**Phase 4C ACCELERATED IMPLEMENTATION BATCH 3 — S2a（Thread GET，仅已批准子集）+ S6（SSE 失效提示）**

Status: **PHASE_4C_ACCEL_BATCH_3_CORE_DONE_WITH_S2B_DEFERRED**
（本轮为 **ACCELERATED IMPLEMENTATION ROUND**（单 Agent，按 mandate 顺序 S2a → S6 → 条件性 S2b → Batch 3 集成回归，中途不结束本轮）：
**S2a 完整** —— `GET .../runs/{rid}/thread` 只落地 Thread D5 的**已批准子集**：`after={seq}`（严格 `seq > after`）、
`limit={n}`（缺省 200、上限 500）、条目按 `seq` **升序**、响应 `{items, threadState}`，`threadState` 与 `items` 出自**同一次**
`Store.transact` 快照；授权复用 Thread D3 的 Issue read（与 comments 同），跨 tenant / 跨 Issue / 软删 / 非 agent run 一律
`404`、非成员 `403`；**只读**：不分配 `seq`、不写状态、不发事件；`node_execution_id`/`node_sequence`/`run_id` 不上线，
`turnId`/`status` 只随 `source='user'`。**未实现**（G-017）：无游标取尾、`before`、`idleSince`、`nextCursor`/`prevCursor`、
`PublicRequest.Before`；`after` 缺省按「不新增语义」读作**从头**（= `after=0`），**不是** tail；`thread_state IS NULL`
答 `404`（不发明空 `threadState`），同样登记在 G-017。**S6 完整** —— `SpaceEvent` 增量新增可选 `issueId`/`runId`/`lastSeq`
（`omitempty`，4 个 4C 之前的事件形状**逐字节不变**），提交后发布 `issue_run.thread_appended`；hint 在**调用方事务**上排队
（`transaction.appends`），由 `Store.transact` 在 `tx.Commit()` 返回 nil **之后**释放，因此「回滚不发」是**结构性**的；
**三个 entry 写入点**（session 声明 `seq=1`、Thread POST、接管）都发布，`lastSeq` = 该提交的 `MAX(seq)`（高水位，不推进
客户端游标）；**仅状态变化零通知**（G-023 未批准 ⇒ T4C-26 **不实现**，用双向断言钉住边界）；SSE 传输层/Memory hub **未改**、
未建 broker，G-020 保持 OPEN。**S2b 未实现** —— `decisions/cloud/thread/` 下只有唯一已批准 ADR，其 D5 不含 tail/`before`/
`idleSince`/游标，**A3/A4 未批准**，故标记 **`S2B_DEFERRED_PENDING_ADR`**（该 deferred **不**否定已完成的 S2a/S6）。
测试 T4C-7/9/10/11/12 与 T4C-25/27 全部落地并通过（真实 HTTP + 真实 PostgreSQL；`internal/core` 白盒固定事件字节形状）；
**两个变异测试**（把发布移到 `tx.Commit()` 之前）证明断言有载荷。`task build` / `task test` / `task test:race` 全绿
（race 0 DATA RACE）；`git diff --check` cloud 与 specs 均干净；`task frontend:generate` **生成幂等**（md5 不变）；
`npm --prefix frontend run check` PASS（组合 `frontend:check` 的漂移步骤只因生成物**未提交**而失败，按 mandate 以幂等证据替代）；
**未运行 `task format`**。**未改**：proto、migration、任何 ADR 文件或其 `status`、Phase 4B running authority / seq 分配 /
收据 / 接管事务、S3 命令所有权、S4 幂等、S5 echo `delivered`、S7 `ending`、D6 调用方事务。**G-017/G-018/G-019/G-020/
G-021/G-022/G-023/G-024/G-025/G-026 全部保持 OPEN 且未被静默关闭**。
**既有门禁失败与本轮无关**：`task format:check`（5 文件）与 `task lint`（7 项）全部落在 **HEAD 未修改**文件，与本轮新增零重合
（本轮新增的 1 项 bodyclose + 2 项 hugeParam 已修）。**未提交**：无 stage / commit / push / PR，既有未提交修改原样保留。）

**Phase 4C Accelerated Batch 3 record (this round — IMPLEMENTATION):** 见 §15
"Round: Phase 4C Accelerated Implementation Batch 3 — S2a (Thread GET, approved subset) + S6 (SSE invalidation notice) / 2026-10-08"。

**Phase 4C Accelerated Batch 2 marker (previous round — IMPLEMENTATION), preserved:**

Current phase at that round:

**Phase 4C ACCELERATED IMPLEMENTATION BATCH 2 — S5（用户轮次 echo + `active⇄idle`）+ S7（结束触发，停止在 `ending`）**

Current status at that round: **PHASE_4C_ACCEL_BATCH_2_CORE_DONE_WITH_G018_DEFERRED**
（本轮为 **ACCELERATED IMPLEMENTATION ROUND**（单 Agent，连续推进 S5 → S7，中途不结束本轮）：**S5 完整** —— 用户轮次
echo 从「仅首提示」推广到所有 Cloud 生成的用户轮次，命中 `source='user'`/`kind='user_turn'` 的行只做 `queued → delivered`
的 CAS，**不**新增条目、**不**分配 seq、**不**改写 `record`/`turn_id`、**不**新建命令、**不**重新入队，`delivered` 永不回退；
首提示 echo（seq=1）行为不变；批次后生命周期以「批次**末条有效记录**」判定（**不是**「批中出现过 `turnEnded`」），`active`
+ 末条 `turnEnded` + 无 `queued` 用户轮次 ⇒ `idle` + `idle_since = now()`，`idle` + 末条非 `turnEnded` ⇒ `active` +
清 `idle_since`，且只在事后重读为 `running`/`running` 时判定，两条 CAS 的 0 行都按不变量损坏整批回滚。**S7 完整（除
G-018 门禁项）** —— 结束严格停止在 `thread_state='ending'`：idle 扫描按**数据库时间**比较 `idle_since + thread_idle_timeout`
（配置 key，默认 15m，≤0 落默认；`<= 0` 时整个 pass 是 no-op），每个 run 一个短事务、精确 CAS、重复 tick 不产生第二条
`EndSession{idle_timeout}`；取消按 IssueRun D6 分流（无会话 ⇒ `starting` 直进 `releasing`/`cancelled` +
`deliveryState='skipped'` + `declareDelete`，**不**写 `ending`、**不**发 `EndSession`；有会话 ⇒ `ending` +
`EndSession{cancelled}`，run 自身 `phase/status` 不动），已 `ending` 不重复发。三触发的 `user_ended` 因 **G-018 未获批准**
**不实现**（无 endpoint、无 route、无 OpenAPI 变更，T4C-35 不适用），作为明确 deferred item 登记。**ADR 优先于 plan 的
一处偏离已登记**：无会话取消以 `thread_state IS NULL` 判别（IssueRun D6 的「（尚无会话）」限定语），而非 plan §4C.11
按 `phase` 判别。测试 T4C-19/20/28/29/30 与 T4C-31/32/33/34 全部落地并通过（`internal/core` 白盒 + 真实 PostgreSQL，
T4C-33 用 channel barrier）；S5 checkpoint 记录 `S5_IMPLEMENTATION_CHECKPOINT_REACHED` 且**未 commit**。
`task build` / `task test` / `task test:race` 全绿；`git diff --check` 干净；**未运行 `task format`**。
**G-018 / G-019 / G-022 / G-024 全部保持 OPEN 且行为未变**，新增 **G-026**（`cancel_requested_at` 无生产写者）。
**未改**：proto、migration、generated/OpenAPI、前端、任何 ADR 文件或其 `status`、Phase 4B 的 running authority /
seq 分配 / 收据 / 接管事务、`ending → ended`/`SessionEnded`/`discarded`/`delivering`/`deliver_revision`。
**既有门禁失败与本轮无关**：`task format:check`（5 文件）与 `task lint`（7 项）全部落在 **HEAD 未修改**文件。
**未提交**：无 stage / commit / push / PR，既有未提交修改原样保留。）

**Phase 4C Accelerated Batch 2 record（上一轮）:** 见 §15
"Round: Phase 4C Accelerated Implementation Batch 2 — S5 (user-turn echo + `active⇄idle`) + S7 (ending triggers, stopped at `ending`) / 2026-10-08"。

**Phase 4C Accelerated Batch 1 marker (previous round — IMPLEMENTATION), preserved:**

Current phase at that round:

**Phase 4C ACCELERATED IMPLEMENTATION BATCH 1 — S3（Thread Command Control Plane）+ S4（Thread POST）**

Current status at that round: **PHASE_4C_ACCEL_BATCH_1_CORE_DONE_WITH_ADR_DEFERRED**
（本轮为 **ACCELERATED IMPLEMENTATION ROUND**（单 Agent，连续推进 S3 → S4，中途不结束本轮）：§4R.5 的 **S3** 与 **S4**
一次交付。**S3 完整**：`EnqueueThreadCommand` 在**调用方事务**内写 `thread_commands`、A 生成 `command_id`、`created_at`
用数据库时钟、`delivered_at`/`delivered_execution_id` 初值 NULL；`ClaimThreadCommands{epoch,limit}` 纯读、按 run 分组、
组内创建序、仅返回**已登记 agent_session 执行**的 run 及其 `execution_id` 与目标 Node；`RecordThreadCommandDelivered`
首次登记生效、同执行重放收敛幂等、异执行 `CONFLICT` **绝不**复写；`ThreadCommandAvailable{run_id}` **只在提交后**发布
（D-4C-05/D-4C-06）；错误经既有内部 Fault/gRPC 码映射，**未新增 proto 枚举**。**S4 core 完整**：
`POST /api/v1/tenants/{tid}/issues/{iid}/runs/{rid}/thread/messages`（强制 `Idempotency-Key`、`{content:[{type:"text",text}]}`、
文本总量 ≤ 64 KiB、v1 仅 text）在**同一事务**内按序完成：幂等预检 → 鉴权 → 权威重读 run → 生命周期前置 → 分配 Thread
`seq` → 生成 Cloud `turn_id` → 写 `source='user'`/`kind='user_turn'`/`status='queued'` 条目 → `thread_state='active'` 并清
`idle_since` → 经 **A 缝** `EnqueueThreadCommand` 放出 `SubmitUserTurn` → 写幂等记录 → COMMIT → 提交后
`ThreadCommandAvailable`。业务层**不**直接写 `thread_commands`。测试 T4C-13..T4C-18 与 T4C-21..T4C-24 全部落地并通过；
`task build` / `task test` / `task test:race` 全绿；OpenAPI 与前端客户端经 `task frontend:generate` 仅**增量**更新且生成**幂等**，
`npm --prefix frontend run check` EXIT=0（53 文件 / 299 测试）。
**A2 未获批准 ⇒ G-024 仍 OPEN**：D-4C-03 的 `cancel_requested_at IS NOT NULL` 与「Workspace 不活」两行**未实现**，
`requireThreadAccepting`/CAS 有意窄于 D-4C-03，并以 `TestThreadMessageCancelRowIsDeferred` 钉住；故标记为
**CORE_DONE_WITH_ADR_DEFERRED** 而非 DONE。**G-021 仍有意未解决**（`ClaimThreadCommands` 仅为单 worker 保证）。
**未改**：Phase 4B 的 running authority / `seq` 分配 / receipt / takeover 事务、proto、任何 ADR 文件或其 `status`、
`specs/test-cases/cloud/thread/durable-thread.md`。**既有门禁失败与本轮无关**：`task format:check`（5 文件）与
`task lint`（7 项）全部落在 **HEAD 未修改**文件（见 §15 本条）。**未提交**：无 stage / commit / push / PR，
既有未提交修改原样保留。）

**Phase 4C Accelerated Batch 1 record (preserved — IMPLEMENTATION):** 见 §15
"Round: Phase 4C Accelerated Implementation Batch 1 — S3 (Thread Command Control Plane) + S4 (Thread POST) / 2026-10-08"。

**Phase 4C Slice 1 marker (previous round — IMPLEMENTATION), preserved:**

Current phase at that round:

**Phase 4C IMPLEMENTATION SLICE 1 DONE — Migration 0022 + `pending` materialization（实施轮；仅 S1）**

Current status at that round: **PHASE_4C_SLICE_1_DONE / PHASE_4C_IMPLEMENTATION_IN_PROGRESS**
（本轮为 **IMPLEMENTATION ROUND**，严格限于 §4R.5 的 **S1**：新增 migration `0022_thread_api_and_commands.sql`
（`thread_commands` 控制面表 + `thread_entries.status` 与其两条 CHECK + `thread_entries_queued` /
`thread_commands_undelivered` / `issue_runs_idle_threads` 三个部分索引 + 对「有 seq=1 真实声明」的 agent run 回填
`thread_state='pending'`），并让 B-owned `StartSession` 在 seq=1 的**同一事务**内物化 `thread_state='pending'`
（D-4C-01 / D-4C-12，关闭 G-016 的实现侧）。测试 T4C-1..T4C-5 全部落地并通过；`task build` / `task test` /
`task test:race` 全绿。**回填按业务事实而非阶段代理**：谓词是 `EXISTS(thread_entries seq=1, source='system',
kind='user_turn')`，并排除取消、终态、无 Workspace、软删除与非 agent 运行；CHECK 前先安全回填 `source='user'`
条目的 `status='queued'`，升级不会失败（三条变异均被 T4C-5 捕获）。
**G-022 仍 OPEN**：A1 修订提案**未获批准**，**未**改任何 ADR 文件或其 `status`，实现依据是已批准的 D3 + 不变量 4 + D-4C-12。
**新登记 G-025**（migration README 停在 0017 的文档债）。**未改**：Phase 4B 的 running authority / `seq` 分配 /
receipt / takeover 事务、proto、OpenAPI/generated。**既有门禁失败与本轮无关**：`task format:check` 与 `task lint`
只在 **HEAD 未修改**文件上报错（见 §15 本条）。**未提交**：无 stage / commit / push / PR，既有未提交修改原样保留。）

**Phase 4C Slice 1 record (this round — IMPLEMENTATION):** 见 §15
"Round: Phase 4C Implementation Slice 1 — migration 0022 + `pending` materialization / 2026-10-08"。

**Phase 4C readiness marker (previous round — REVIEW ONLY), preserved:**

Current phase at that round:

**Phase 4C READINESS REVIEWED — Architecture Review & Implementation Slice Plan（评审轮；未实现）**

Current status at that round: **PHASE_4C_DESIGN_DONE / PHASE_4C_ARCHITECTURALLY_REVIEWABLE / READY_FOR_PHASE_4C_IMPLEMENTATION_SLICE_1**
（该轮为 **Architecture Review + Implementation Readiness**：只核验、只登记、只切分。交付 §4R.0–§4R.8：
G-017 的**逐条切分**（`after`/`limit ≤ 500`/升序/`threadState`/`{issueId,runId,lastSeq}` = 已有批准依据；
无游标 tail 读、`before`、`idleSince`、`nextCursor`/`prevCursor` = 新扩展，需 ADR 修订）、G-018 的用户结束端点契约
（`POST .../thread/end`，202，强制幂等键，`EndSession{user_ended}` 同事务；`ending → ended` **仍归 Phase 5**）、
D-4C-01..D-4C-12 逐条复核、**新登记 G-022/G-023/G-024**、7 个可独立验收的实施切片 S1–S7（每片含 Scope / 预计修改文件 /
Migration 影响 / Transaction contract / Ownership contract / 对应 T4C 测试 / Regression tests / Exit criteria）、
依赖与风险表、**待批准的 ADR 修订 A1–A4**（未落地到任何 ADR 文件）。**关键核验**：proto 已完整（4C **无需改 proto**）；
`thread_entries` 无 `status` 列；`thread_commands` 表不存在；`SpaceEvent` 缺三字段；`PublicRequest` 无 `Before`。
**未实现**：production 代码、migration、测试、proto、API/OpenAPI 变更一律未做。**未提交**：无 stage / commit /
push / PR，既有未提交修改原样保留。）

**Phase 4C readiness record (previous round — REVIEW ONLY):** 见 §15
"Round: Phase 4C Readiness — Architecture Review & Implementation Slice Plan / 2026-10-08"
与 `## Phase 4C Readiness — Architecture Review & Implementation Slice Plan`（§4R.0–§4R.8）。

**Phase 4C design marker (previous round — DESIGN ONLY), preserved:**

Current status at that round:

**Phase 4C DESIGN DONE — Thread API / SSE / Thread Commands / Lifecycle（纯设计轮；未实现）**

Current status: **PHASE_4C_DESIGN_DONE / PHASE_4C_ARCHITECTURALLY_REVIEWABLE / READY_FOR_PHASE_4C_IMPLEMENTATION**
（本轮只更新 `plan/plan.md`、`plan/plan-zh.md` 与 specs 证据文档。交付 D-4C-01..D-4C-12、§4C.0–§4C.20：
`thread_state` 归属与 `pending` 物化（**G-016 CLOSED**）、Thread GET/POST 契约、`EnqueueThreadCommand` 缝与
四类身份、SSE 失效提示与统一 `after` 语义、`active/idle/ending` 生命周期、取消/终态交互、4C migration 决策
（**未创建**）、10 场景并发矩阵、API 错误分类、T4C-1..T4C-34 测试设计矩阵。**NON-BLOCKING OPEN**：
G-017（tail/`before`/`idleSince` 扩展已批准 D5）、G-018（用户结束端点缺失）、G-019（`SessionEnded`/`discarded` 属
Phase 5）、G-020（SSE 不保证送达）、G-021（多 worker 命令分区）。**未实现**：production 代码、migration、测试、
API/OpenAPI 变更一律未做。**未提交**：无 stage / commit / push / PR，既有未提交修改原样保留。）

**Phase 4C design record (this round — DESIGN ONLY):** 见 §15
"Round: Phase 4C — Thread API / SSE / Thread Commands / Lifecycle 详细设计 / 2026-10-08"
与 `## Phase 4C — Thread API / SSE / Thread Commands / Lifecycle 详细设计`（§4C.0–§4C.20）。

**Phase 4B implementation marker (previous round — IMPLEMENTATION), preserved:**

Current status at that round: **PHASE_4B_DONE / PHASE_4B_ARCHITECTURALLY_REVIEWABLE / READY_FOR_PHASE_4C_DESIGN**
（交付 migration `0021_node_event_receipts.sql`、production 接管核心 `agent_thread_takeover` +
`ThreadEventsTakenOver` 钩子 + gRPC `TakeOverThreadEvents` + `cmd/server` 接线，以及真实 PostgreSQL 的
T4B-1..T4B-19 与端到端 gRPC 验收。**G-009 CLOSED**，G-013/G-014 CLOSED 且已实现，G-012/G-015 仍 PARTIAL，
G-016 当时 OPEN —— 已由本轮 4C 设计关闭。**未提交**：无 stage / commit / push / PR。）

**Phase 4B architecture resolution record (prior round — DESIGN / DECISION ONLY), preserved:**

1. **G-013 CLOSED（D-024）** — `starting→running` 唯一权威 = 首条**真实 Node Thread 记录**被 `TakeOverThreadEvents`
   接管且 `ThreadEventsTakenOver` 钩子同事务提交（IssueRun D3 + controller-integration D6 + Thread D4）；
   **无** synthetic `session_started` 事件（Node 协议 D2 只发 `ThreadEvent{record}`）。`thread_state`：
   `pending`（执行已登记、无记录；Thread D4）→ 首条记录接管 → `active`；4B 只写 `pending→active`。echo 首 prompt
   （`turn_id == initial_turn.turn_id`）只写收据、不分配 seq、不新增条目（seq=1 不可变）；空批次 `ABORTED` 拒绝。
   §4B.2–§4B.4。
2. **G-014 CLOSED / D-023 ACCEPTED** — Thread `seq` = 接管事务内 `MAX(seq)+1`（从 2 起）。串行化证明来自
   **approved** 事实：(1) 一运行至多一个会话执行（controller-integration D1 / D-021）；(2) 所有接管经
   `Store.transact` 全局 advisory lock；(3) `PRIMARY KEY (run_id, seq)` 兜底。**不依赖 `proposed` 的
   controller-session ADR**。Node `sequence`（`node_event_receipts`，执行内）≠ Thread `seq`（run-scoped），不混淆。
   不新增计数器。§4B.5。
3. **G-015 PARTIAL** — 初值：单运行非终态 event 上限 `thread_event_cap` 默认 200,000（检查点
   `last_event_sequence`）；`node_event_receipts` 保留 `node_event_receipts_retention` 默认 `done` 后 30 天。
   enforcement 与清理循环随 4B 实现（D-025，§4B.10）。
4. **迁移 proposal**：Phase 4B **唯一**新表 `node_event_receipts(execution_id, sequence)` + `event jsonb`
   （FK→`node_executions`）；**不改业务表**（`thread_entries` 0018、`issue_runs.thread_state` 值集已足够）。
   §4B.9。
5. **测试矩阵 T4B-1..T4B-19** 全部 `DESIGNED / MISSING`（§4B.13）；4B 最小范围 = 迁移 + `TakeOverThreadEvents`
   路由 + 真实化 `ThreadEventsTakenOver` 钩子，**不含** API/`thread_commands`/`deliver_revision`/Phase 5（§4B.12）。
6. **controller-session ADR 仍 `proposed`**：本决议证明 Cloud 侧 4B **不依赖**它（§4B.14），**不报告**
   `BLOCKED_ON_CONTROLLER_SESSION_ADR`；其 D1–D5 是 desktop 侧独立待批准决策。
7. **未改 production / migration / 测试**；仅 `plan/plan.md`（本章 + D-023 状态 + D-024/D-025 + G-009/G-012..G-015 +
   §7 Phase 4 + 本 marker）与 `plan/plan-zh.md`（镜像）。git：未 stage/commit/push/PR。

**Previous round marker (Phase 4A — A-side dispatch registration, D-020/D-021), preserved:**

Current status at that round: **PHASE_4A_DONE / PHASE_4A_ARCHITECTURALLY_REVIEWABLE / NOT_READY_FOR_PHASE_4B_IMPLEMENTATION**
（4A 只落地 A 侧 `RecordDispatch` 执行登记：`execution_work(agent_session) → Controller claim → Controller 分配
execution_id → agent_work_dispatch → node_executions 行 + execution_work.execution_id 栅栏`。`IssueRun.phase` 全程保持
`starting`/`dispatched`，**绝不进入 running**；4B（Thread takeover、starting→running、thread seq≥2、node_event_receipts、
D-023 MAX(seq)+1）当时**禁止实现**，`PHASE_4B = NOT READY`——该结论已由本轮架构决议取代：`PHASE_4B = READY`。）

**Phase 4A implementation record (previous round — A-side dispatch registration, D-020/D-021):**

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


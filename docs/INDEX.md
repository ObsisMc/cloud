# Ora Cloud — Documentation Index

Entry point for anyone (human or agent) picking up this codebase. Two reading paths:

| I am… | Start here | Then |
| --- | --- | --- |
| **An AI agent** continuing development | [`development/agent/architecture.md`](development/agent/architecture.md) | [`development/agent/adding-features.md`](development/agent/adding-features.md) when adding an endpoint |
| **A new teammate** getting oriented | [`development/onboarding/overview.md`](development/onboarding/overview.md) | [`development/onboarding/progress.md`](development/onboarding/progress.md) for what's done vs. pending |

## Doc map

```
docs/
  INDEX.md                      ← you are here (map + pointers)
  development/
    agent/                      ← for AI agents: dense, precise, convention-focused
      architecture.md           ← request path, core conventions, invariants, traps
      api-reference.md          ← every endpoint (public + internal), fields, error codes
      database.md               ← all tables & migrations, add/alter rules
      adding-features.md        ← how-to: add an endpoint / sub-resource safely
    onboarding/                 ← for new teammates: readable, tables & diagrams
      overview.md               ← what Ora Cloud is, the moving parts, glossary
      progress.md               ← what's built / in-progress / deferred (status board)
  migrations/
    multica-issue-board/        ← single-migration archive (analysis → design → test → final)
  acceptance.md                 ← product acceptance criteria (pre-existing)
  authentication.md             ← auth model (pre-existing)
  core-contract.md              ← core behavioural contract (pre-existing)
  execution-contract.md         ← execution/operations contract (pre-existing)
```

## Source of truth (one doc per topic)

| Topic | Where | Notes |
| --- | --- | --- |
| **Project status / roadmap** | [progress.md](development/onboarding/progress.md) | done / in-progress / planned / deferred / blocked |
| **Issue architecture** | [agent/architecture.md](development/agent/architecture.md) + [12-collaboration-architecture.md](migrations/multica-issue-board/12-collaboration-architecture.md) | the latter is the frozen Wave-3 design (rev. 2, plus §36/§37 revisions) |
| **API** | [agent/api-reference.md](development/agent/api-reference.md) | live endpoint/field reference; `api/openapi.json` is the machine truth |
| **Database** | [agent/database.md](development/agent/database.md) | live table + migration inventory |
| **Collaboration interaction model** | [12-collaboration-architecture.md §37](migrations/multica-issue-board/12-collaboration-architecture.md#37-wave-3b-0--collaboration-interaction-model-frozen) | **authoritative product semantics** — `@`, the four target modes, cardinality, context, timeline, fixtures. Frozen by Wave 3B-0 |
| **Collaboration / integration ports** | [12-collaboration-architecture.md §6.4](migrations/multica-issue-board/12-collaboration-architecture.md#64-canonical-port-inventory-unified-by-wave-3b-0) | the **single** canonical port inventory (15 ports; `FormDescriptorProvider` added by 3B-2). Other docs must point here, not repeat a list |
| **Workflow interaction (Issues-facing)** | [12-collaboration-architecture.md §38](migrations/multica-issue-board/12-collaboration-architecture.md#38-wave-3b-2--workflow-interaction-design-frozen) | the **contract** (`FormDescriptor`, single Confirm boundary, AI Assist authority, draft decision, API surface, `0009`) + **§38.37** implementation record |
| **How to add code** | [agent/adding-features.md](development/agent/adding-features.md) | endpoint / sub-resource how-to + verify |

## Repo layout (one glance)

| Path | What lives there |
| --- | --- |
| `cmd/` | Entry points: `server`, `cloudctl`, `openapi`, `simulator`, demos. |
| `internal/api/router/` | HTTP layer — route allowlist + dual-JWT auth + strict JSON. |
| `internal/core/` | Business logic — `Store.Public`/`Control`, raw SQL in advisory-locked tx, migrations. |
| `internal/contract/` | OpenAPI generator (`openapi.go`) → `api/openapi.json`. |
| `internal/simulator/` | Test/dev substrate (in-memory node + credential signing). |
| `integration/` | End-to-end tests against real PostgreSQL (isolated schema per test). |
| `api/openapi.json` | **Generated** — do not hand-edit; run `go run ./cmd/openapi`. |
| `scripts/` | Dev/demo shell wrappers. |

## Current status (see [progress.md](development/onboarding/progress.md) for detail)

- ✅ Core platform: tenants, memberships, identity, projects, workspaces, operations.
- ✅ **Issue Board** — wave 1 (core Kanban), wave 2 (status catalog, comments, labels, subscribers,
  numbers, properties, search, batch, saved views, groups) and **Wave 3A** (collaboration
  foundation: polymorphic assignee, comment author actors, IssueRun, timeline projection, context
  refs). Migrations 0005–0009; the formal React frontend in `frontend/` is migrated and speaks to
  the real API.
- ✅ **Wave 3B-0** — Collaboration Architecture Alignment (docs only): the interaction model is frozen
  in [§37](migrations/multica-issue-board/12-collaboration-architecture.md#37-wave-3b-0--collaboration-interaction-model-frozen)
  and the 14 integration ports are unified in
  [§6.4](migrations/multica-issue-board/12-collaboration-architecture.md#64-canonical-port-inventory-unified-by-wave-3b-0).
- ✅ **Wave 3B-1** — Collaboration Interaction Foundation (migration `0008`): the first real end-to-end
  `@` collaboration chain — `GET /collaboration/targets` → `@` picker → Human Mention / Agent & Team
  Task → deterministic context → mock execution → IssueRun lifecycle / Activity / reply Comment →
  `GET /issues/{iid}/timeline` — with no real Agent/Team/Workflow/Runtime modules.
- ✅ **Wave 3B-2 — Workflow Interaction Shell** (migration `0009`): `@Workflow` → `FormDescriptor` →
  dynamic form → optional AI Assist → **Review → Confirm** → `IssueRun` → mock execution → Timeline.
  New ports `FormDescriptorProvider` / `InputAssistProvider`; workflow output lands as a `system`
  activity. Real Workflow / AI providers remain **blocked on external design**
  ([§38.37](migrations/multica-issue-board/12-collaboration-architecture.md#3837-implementation-record-2026-09-20--implemented--verified)).
- 🧭 Planned — **Wave 3C** (Issue Detail & Collaboration UI). Real Agent/Team/Workflow modules are
  **blocked on external design**.
- ⏸️ Deferred: attachments, issue↔project binding, PR links, realtime, bots/squads, Autopilot.

## ⚠️ Repository governance follow-up (unresolved)

`AGENTS.md` states that `specs/` is an independent Git repository reachable via `git -C specs`, and the
ADR-first rule requires an approved ADR before coding. **In the current working copy that does not
hold**: `cloud/specs` does not exist, `mor/specs` has no `.git`, and `mor/.gitignore` ignores `/specs/`
— so the ADR deliverable has **no version-controlled home**. Recorded by Wave 3B-0; **not** fixed
(no `git init`, no `.gitignore` change, no restructuring). It needs an owner decision before the next
ADR is written.

## Golden rules (both audiences)

1. **Additive first** — new files/routes/migrations; never refactor core to add a feature.
2. **Never edit an applied migration** — add a new `NNNN_*.sql`; alter tables there.
3. **`api/openapi.json` is generated** — after changing `internal/contract/openapi.go`, run
   `go run ./cmd/openapi` or the contract test fails.
4. **Verify before claiming done** — `go build ./...`, `go test ./internal/... ./cmd/...`, and the
   integration suite against Docker PostgreSQL (see [adding-features.md](development/agent/adding-features.md) §Verify).

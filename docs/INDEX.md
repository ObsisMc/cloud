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
- ✅ **Issue Board — wave 1** (core Kanban) and **wave 2** (status catalog, comments, labels,
  subscribers, numbers, properties, search, batch, saved views, groups). Migrations 0005 + 0006.
- ⏸️ Deferred: attachments, issue↔project binding, PR links, realtime, bots/squads, Autopilot.

## Golden rules (both audiences)

1. **Additive first** — new files/routes/migrations; never refactor core to add a feature.
2. **Never edit an applied migration** — add a new `NNNN_*.sql`; alter tables there.
3. **`api/openapi.json` is generated** — after changing `internal/contract/openapi.go`, run
   `go run ./cmd/openapi` or the contract test fails.
4. **Verify before claiming done** — `go build ./...`, `go test ./internal/... ./cmd/...`, and the
   integration suite against Docker PostgreSQL (see [adding-features.md](development/agent/adding-features.md) §Verify).

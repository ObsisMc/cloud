# Architecture — for agents

Dense reference. Goal: let you modify this codebase without breaking its invariants. Read this
before `adding-features.md`. Line refs are pointers, not promises — re-check before relying on them.

## Request path

### Public API (`Action == ""`)

```
HTTP → router.New (one generic handler per Route)
     → verify service JWT (Bearer)              [kind=service, role=gateway]
     → verify user JWT (X-Ora-User-Token)       [claims.Caller == service.Subject]
     → strict JSON decode (UseNumber, no trailing tokens, reject null body)
     → body allowlist: unknown_field / invalid_field_type
     → store.Public(ctx, *core.PublicRequest)
        → transact { pg_advisory_xact_lock(67420911) }     ← global write lock
           → identity()     (auto-create user from source+subject)
           → membership()   (tenant active, member active, user active; some paths admin)
           → GET → readPublic()
           → POST/DELETE → idempotency_records (Idempotency-Key header required)
                         → switch dispatch → raw SQL via *sql.Tx
```

### Internal/control API (`Action != ""`)

```
HTTP → router.New → verify service JWT (kind=service, role=node/controller)
     → store.Control(ctx, *core.ControlRequest) → switch on Action
```

Only `/internal/v1/access` and `/internal/v1/admissions` also verify a user token. Everything else
is service-only.

## The four central types

| Type | File | Role |
| --- | --- | --- |
| `core.Object` | `store.go` | `map[string]any`. Accessors `S/N/B/O`. `N` accepts float64/int64/int/json.Number. |
| `core.PublicRequest` | `public.go` | Method/Path/TenantID/…IDs/Key/Limit/After/Query/GroupBy + Body + Identity. |
| `core.Fault` | `store.go` | `{Code, Status, Params}`. `reject(status,code)` / `require(ok,status,code)` panic it; recovered to JSON. |
| `Route` | `router.go` | `{Method, Path, Action, Fields}`. `Fields` = strict body allowlist. |

## Core conventions (follow exactly)

- **All SQL inside `transact`** — one short tx, global advisory lock serializes writes. No
  cross-request state, no prepared statements.
- **`t.list` / `t.one`** wrap SQL in `SELECT row_to_json(resource) FROM (…) resource` and camelCase
  only the **top-level** keys; nested jsonb keys keep their DB names. Aggregate aliases read as
  their SQL name (`SELECT min(position)` → key `min`).
- **Optimistic concurrency** — `version(o, body.N("version"))`: `v<=0` → 428 `version_required`,
  mismatch → 409 `version_conflict`. Every mutating read-then-write bumps `version=version+1`.
- **Soft delete** — `deleted_at=now()`; every query spells `AND deleted_at IS NULL` explicitly. No
  hard erase of business rows (join tables like `issue_labels` are hard-deleted).
- **Idempotency** — POST/DELETE require `Idempotency-Key`; replay returns stored response, changed
  body → 409 `idempotency_conflict`. PUT is version-guarded instead.
- **`updated_at`** is NOT universal — set it explicitly in each `UPDATE …, updated_at=now()`.
- **Tenant scope** — every query filters `tenant_id=$…`. Board reads are tenant-wide (not
  owner-scoped); saved views are owner-scoped.
- **`validText(s,max)`** trims + rejects empty/over-length (400 `invalid_input`).
- **`validID`** = uuid format check; bad id → usually 404 `not_found` (load) or 400 (body field).

## Errors

Envelope `{code, params, requestId}`. Common codes by status:

- **400** `invalid_json` `unknown_field` `invalid_field_type` `invalid_input` `invalid_status`
  `invalid_priority` `invalid_anchor` `idempotency_key_required` …
- **401** `invalid_service_credential` `invalid_user_credential`
- **403** `membership_required` `user_disabled`
- **404** `not_found` `assignee_not_found` `parent_not_found` `user_not_found`
- **409** `version_conflict` `position_conflict` `status_conflict` `label_conflict`
  `system_status_required` `idempotency_conflict`
- **428** `version_required`
- **500** `internal_error`

## Traps (learned the hard way — see migrations/multica-issue-board/09-observations.md)

1. **Nested issue paths are swallowed** by `case r.IssueID != "" || HasSuffix(path,"/issues")`.
   Dispatch `/comments` `/labels` `/subscribers` **inside** that case; in `readPublic` put nested
   GETs **before** `case r.IssueID != ""`.
2. **OpenAPI `strings.Contains(path, "/issues")`** matches every sub-route — insert new resource
   branches **before** it, and before the hard-coded `status` enum branch in `inputSchema`.
3. **`validField` is closed-typed** — default = string; non-string body fields 400. Add a `case`
   for arrays (`ids`), objects (`filter`/`properties`), integers (`position`).
4. **Contract test enforces `additionalProperties:false`** — every response field must exist in the
   schema. Adding a response field without updating `openapi.go` fails integration with
   "OpenAPI response mismatch".
5. **Gin static segments beat `:param`** — `/issues/batch` is safe (literal `batch` wins over
   `:iid`); `c.Param("iid")` is empty there.
6. **Migrations can't seed runtime entities** — tenants exist only at runtime; seed per-tenant rows
   lazily (`ON CONFLICT DO NOTHING`) on first read/write.

## Auth model (summary — see ../../authentication.md)

- Two independent credentials on public routes: **service** JWT (`Bearer`, role `gateway`) +
  **user** JWT (`X-Ora-User-Token`, `Caller` must equal service `Subject`).
- `identity()` auto-provisions a `users` row from `(source, subject)` on first sight.
- `membership()` enforces active tenant + active membership + active user; admin paths additionally
  require role=admin.

## Where things are

| Concern | File |
| --- | --- |
| Route allowlist + auth + JSON guard | `internal/api/router/router.go` |
| Public dispatch + shared helpers | `internal/core/public.go` |
| Store / transact / Object / Fault / Migrate | `internal/core/store.go` |
| Control (internal) dispatch | `internal/core/control.go` |
| Issue board | `internal/core/issues.go`, `issue_*.go` |
| Projects / workspaces / operations | `internal/core/{project,workspace,operation,node}.go` |
| OpenAPI generator | `internal/contract/openapi.go` |
| Migrations | `internal/core/migrations/NNNN_*.sql` |

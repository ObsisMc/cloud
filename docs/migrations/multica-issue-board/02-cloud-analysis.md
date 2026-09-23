# Ora Cloud — Analysis (the target)

Target repo root: the local Ora Cloud checkout. Go module `github.com/wanglongan587/cloud`.
Go 1.27.1 + Gin + GORM (pool only; business logic uses raw SQL via `*sql.Tx`), PostgreSQL.

## 1. Request path (public API)

```
HTTP → internal/api/router.New (generic handler, one per Route)
     → verify service JWT (Bearer)   [kind=service, role=gateway for public]
     → verify user JWT (X-Ora-User-Token)   [caller must match service sub]
     → strict JSON decode (UseNumber, no trailing tokens, unknown_field/invalid_field_type)
     → store.Public(context, *core.PublicRequest)
        → transact { pg_advisory_xact_lock(67420911) }
           → identity()   (auto-create user from source+subject)
           → membership() (tenant_memberships active, tenant active, user active)
           → GET  → readPublic()
           → POST/DELETE → idempotency_records (requires Idempotency-Key header)
                        → switch dispatch
```

Key facts for the migration:

- The route allowlist is `router.Routes()` ([router.go:26](../../internal/api/router/router.go#L26)). The generic
  handler ([router.go:87](../../internal/api/router/router.go#L87)) is fully data-driven: `Route{Method, Path, Action, Fields}`.
  `Action == ""` means a **public** route; `Fields` is the strict body allowlist.
- `PublicRequest` ([public.go:13](../../internal/core/public.go#L13)) carries `TenantID/ProjectID/WorkspaceID/
  OperationID/UserID` (all from `:param`), `Limit/After` (query), `Key` (Idempotency-Key header), `Body`, `Identity`.
  It currently has **no issue id field** — the router passes no `:iid` param today.
- `Public` dispatch ([public.go:21](../../internal/core/public.go#L21)): `/me` and `/me/tenants` short-circuit before
  membership; every other public path does `membership(t, r.TenantID, uid, isAdmin)` then GET→`readPublic`,
  write→idempotency+switch. All resource SQL already filters `tenant_id` (+ `owner_user_id` where single-owner).
- Scoping helpers already exist and are reused: `identity`, `membership`, `project`, `workspace`, `ownagedOperation`
  ([store.go:266-328](../../internal/core/store.go#L266-L328)); `validText`, `validRef` ([public.go:166-176](../../internal/core/public.go#L166-L176));
  `version(o,v)` → 428 if `v<=0`, 409 if mismatch ([store.go:330](../../internal/core/store.go#L330)); `reject/require`
  panic with `*Fault` ([store.go:62-68](../../internal/core/store.go#L62-L68)).

## 2. SQL / persistence conventions

- Migrations are embedded and ordered: `internal/core/migrations/*.sql`, applied by `Store.Migrate`
  ([store.go:239](../../internal/core/store.go#L239)) under the advisory lock; each file is SHA256-checksummed into
  `schema_migrations` and is immutable once applied (`migration_checksum_mismatch` on change).
- Current migrations: `0001_core.sql` … `0004_*` (baseline verified applied on PostgreSQL 17).
- Table shape conventions (from [0001_core.sql](../../internal/core/migrations/0001_core.sql)): `id uuid PRIMARY KEY`,
  `version bigint NOT NULL DEFAULT 1 CHECK(version>0)`, `created_at timestamptz NOT NULL DEFAULT now()`,
  `deleted_at timestamptz` (soft delete), un-deleteable rows use `deleted_at IS NULL` predicates.
- Owner scoping is structural: `FOREIGN KEY(tenant_id, owner_user_id) REFERENCES tenant_memberships(tenant_id,user_id)`
  + `UNIQUE(id, tenant_id, owner_user_id)`. For a **shared** resource (board), rely on plain `tenant_id` FK to `tenants(id)`.
- Reads use `t.list` → `SELECT row_to_json(resource) FROM (...) resource`, snake_case→camel via `camel()`
  ([store.go:98-143](../../internal/core/store.go#L98-L143)). Column output is `map[string]any` with `float64` for
  JSON numbers (relevant: `double precision` `position`).
- `transact` serializes all writes via `pg_advisory_xact_lock(67420911)` — a single-cluster lock; no partial/long
  transactions must span external work ([store.go:158](../../internal/core/store.go#L158)).

## 3. Contract / OpenAPI

- `contract.Document()` ([openapi.go:61](../../internal/contract/openapi.go#L61)) derives schemas + paths from
  `router.Routes()`. Helpers: `resource(names, nullableNames)`, `fields()`, `optional()`, `enumeration()`,
  `inputSchema(name, r)`, `optionalField(name, r)`, `isList(r)`, `responseSchema(r)`.
- `isList` currently matches only `/tenants`, `/members`, `/projects`, `/workspaces`, `/resource-status` —
  it drives the `{items,nextCursor}` response wrapper and the `limit/after` query params.
- `responseSchema` default fallthrough names an unknown path's resource `Project` and wraps POST in
  `{resource, operation}` @202 — **not correct for a synchronous issue resource**, so the issue routes need an
  explicit branch (see [03-design.md](03-design.md)).
- `inputSchema` has a `case "status": enumeration("active","disabled")` used by tenant-members today; the issue
  routes reuse the field name `status` with different values, so that case must be path-disambiguated.

## 4. Auth model

- Dual Ed25519 JWT: service `Bearer` (kind=service; public requires role **gateway**) + user `X-Ora-User-Token`
  (kind=user, `caller` must equal service `sub`). Identity auto-provisions a `users` row; `identity()` then
  `membership()` gates tenant access. Roles `gateway/controller/node`; user has no role.
- Implication: issue endpoints are **public** routes (`Action == ""`), tenant-scoped, reachable with the
  gateway service credential + a user token carrying that user's membership. Creator is the authenticated user
  (from `identity()`), not a client-supplied id — reuses Cloud's existing creator scoping.

## 5. Test / demo harness already present

- `internal/simulator`: `NewCredentials()` (gateway/controller/node/user Ed25519 keys), `Client.Call(method, path,
  role, claims, user, key, body)` issues real HTTP with signed tokens ([controller.go:77](../../internal/simulator/controller.go#L77)).
- `integration/cloud_test.go` `setup(t)`: creates an isolated `test_<uuid>` schema, migrates it, bootstraps a
  tenant+admin, then exercises public routes through the router with the simulator client. The issue integration
  test will follow the same pattern to avoid touching the persistent `ora` database.

## 6. Boundaries the migration must respect

1. No changes to `transact`, `identity`, `membership`, `camel`, `page`, `version`, `validText`, or the idempotency
   flow — the issue code is a consumer of those.
2. Additive files only: one new migration + one new `issues.go` in `internal/core`.
3. Three existing files get **minimal, additive** edits: `router.go` (routes + pass `:iid`), `public.go`
   (dispatch + readPublic), `openapi.go` (Issue schema + response/input branches). See [03-design.md](03-design.md).
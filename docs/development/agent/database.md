# Database — for agents

PostgreSQL 17. Migrations are embedded, checksummed, applied in order by `Store.Migrate()` /
`cloudctl migrate`, recorded in `schema_migrations`. **Immutable once applied** — never edit an
applied file; add a new `NNNN_*.sql`.

## Migrations

| File | What it adds |
| --- | --- |
| `0001_core.sql` | tenants, users, user_identities, tenant_memberships, credential_refs, projects, project_storage, workspaces, workspace_worktrees, tasks, operations, external_effects, execution_tickets, sandbox_instances, node_instances, controller_leases, idempotency_records |
| `0002_aggregate_guards.sql` | deferred-constraint triggers (one main workspace per project, last-admin guard, …) |
| `0003_resource_versions.sql` | `version` columns + optimistic-concurrency backfill |
| `0004_effect_intent_and_ticket_scope.sql` | effect intent + ticket scoping hardening |
| `0005_issues.sql` | `issues` (board) |
| `0006_issue_extensions.sql` | issue_statuses, issue_comments, labels, issue_labels, issue_subscribers, issue_views + `issues` ALTERs (`number`, `properties`, status format check) |

## Table inventory

### Identity & tenancy
| Table | Purpose | Key columns |
| --- | --- | --- |
| `tenants` | org boundary | id, name, status, deleted_at |
| `users` | person | id, display_name, status, deleted_at |
| `user_identities` | external identity → user | user_id, source, subject |
| `tenant_memberships` | tenant↔user + role | tenant_id, user_id, role(admin/member), status |
| `credential_refs` | named secret references | tenant_id, owner_user_id, purpose, ref |

### Projects / workspaces / operations
| Table | Purpose | Key columns |
| --- | --- | --- |
| `projects` | dev-environment repo | tenant_id, owner_user_id, name, repository_url, default_branch, lifecycle |
| `project_storage` | per-project volume state | project_id, observed_state |
| `workspaces` | main/isolated worktree env | project_id, tenant_id, owner_user_id, kind, desired/observed_state, runtime_generation |
| `workspace_worktrees` | git worktree metadata | workspace_id, branch_name, base_commit_id |
| `tasks` | isolated workspace title | workspace_id, title |
| `operations` | durable async work | tenant_id, project_id, workspace_id, kind, state, step, version |
| `external_effects` | external-action plan/evidence | operation_id, kind, state, external_id, request, result |
| `execution_tickets` | node execution lease | workspace_id, state |
| `sandbox_instances` | running sandbox | workspace_id, … |
| `node_instances` | registered nodes | connection_state, initialized, heartbeat |
| `controller_leases` | controller leadership | epoch fencing |
| `idempotency_records` | POST/DELETE replay | tenant_id, user_id, key, request_hash, response, status |

### Issue board (0005/0006)
| Table | Purpose | Key columns |
| --- | --- | --- |
| `issues` | board card | tenant_id, creator_user_id, assignee_user_id?, parent_issue_id?, title, description, status, priority, position, number, properties(jsonb), version, deleted_at |
| `issue_statuses` | status catalog | tenant_id, key, name, category, color, icon, is_system, position, deleted_at · UNIQUE(tenant_id,key) |
| `issue_comments` | comment thread | tenant_id, issue_id, author_user_id, body, version, deleted_at |
| `labels` | tenant label | tenant_id, name, color, deleted_at · partial UNIQUE(tenant_id,name) WHERE deleted_at IS NULL |
| `issue_labels` | issue↔label join | (issue_id,label_id) PK · hard delete |
| `issue_subscribers` | issue watchers | (issue_id,user_id) PK, tenant_id |
| `issue_views` | saved filter | tenant_id, owner_user_id, name, filter(jsonb), version, deleted_at |

## Conventions

- Every business table: `id uuid PK`, `version bigint DEFAULT 1 CHECK(version>0)`,
  `created_at`/`updated_at timestamptz DEFAULT now()`, soft-delete `deleted_at timestamptz`.
- **Scope = `tenant_id`**; composite FKs `(tenant_id, user_id) → tenant_memberships` bind a
  user-reference to a member-of-record.
- **Cascade behaviour is deliberate**: `issues.parent_issue_id … ON DELETE SET NULL` (orphan
  children), join tables hard-delete, business rows soft-delete.
- Query pattern: `t.list`/`t.one` → `SELECT row_to_json(resource) FROM (…) resource`, top-level
  keys camelCased, nested jsonb keys keep DB names.

## Adding a table / altering one

1. New file `internal/core/migrations/NNNN_descriptive.sql` (next number; embedded by Go embed).
2. Follow the column/constraint conventions above; add indexes for the read patterns you'll use.
3. To alter an existing table, `ALTER` in the **new** file — never edit the original.
4. `Store.Migrate()` applies pending files automatically; the upgrade test
   (`integration/cloud_test.go`) keeps a hardcoded **pre-upgrade** baseline list — add your file
   there only if the test intends to exercise upgrading *through* it, otherwise leave it.
5. If you add a tenant-scoped lookup that must exist per tenant (like the status catalog), seed it
   lazily in code (`ON CONFLICT DO NOTHING`), not in the migration.

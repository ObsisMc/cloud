# API Reference — for agents

Every route in the allowlist (`router.Routes()`). All public routes are tenant-scoped and require
service-JWT (gateway) + user-JWT + active membership. Error codes: see
[architecture.md](architecture.md) §Errors. Conventions (idempotency, version, pagination): see
[architecture.md](architecture.md) §Core conventions.

Legend: 🔑 = `Idempotency-Key` required · 🔢 = `version` precondition (428/409).

## Identity & tenancy

| Method | Path | Body fields | Response |
| --- | --- | --- | --- |
| GET | `/api/v1/me` | — | User |
| GET | `/api/v1/me/tenants` | — | `{items, nextCursor}` |
| GET | `/tenants/{tid}/members` | — | `{items, nextCursor}` |
| PUT | `/tenants/{tid}/members/{uid}` | `role`, `status`, 🔢`version` | membership (admin only) |

## Issues (board)

`Issue`: `id, tenantId, creatorUserId, assigneeUserId?, parentIssueId?, title, description, status,
priority, position, number, properties, labels[], version, createdAt, updatedAt, deletedAt?`.

| Method | Path | Body fields | Response |
| --- | --- | --- | --- |
| GET | `/issues` | query `?q=` | `{items, nextCursor}` |
| POST 🔑 | `/issues` | `title`*, `description`, `status`, `priority`, `assigneeUserId`, `parentIssueId`, `properties` | `{resource: Issue}` |
| GET | `/issues/{iid}` | — | Issue |
| PUT | `/issues/{iid}` | any create field + 🔢`version` | Issue |
| DELETE 🔑 | `/issues/{iid}` | 🔢`version` | Issue (soft-deleted) |
| POST 🔑 | `/issues/{iid}/move` | `status`, `beforeId`, `afterId`, 🔢`version` | Issue |
| POST 🔑 | `/issues/batch` | `ids`*[≤100], `status`, `priority`, `assigneeUserId` | `{items, nextCursor}` |
| GET | `/issue-groups` | query `?by=status\|priority\|assigneeUserId`, `?q=` | `{groups:[{key,items}]}` |

## Issue extensions (wave 2)

| Method | Path | Body fields | Response |
| --- | --- | --- | --- |
| GET | `/issue-statuses` | — | `{items:[IssueStatus]}` (auto-seeds 7) |
| POST 🔑 | `/issue-statuses` | `key`*, `name`*, `description`, `category`, `color`, `icon` | `{resource: IssueStatus}` |
| PUT | `/issue-statuses/{sid}` | `name`, `description`, `category`, `color`, `icon`, `position`, 🔢`version` | IssueStatus |
| DELETE 🔑 | `/issue-statuses/{sid}` | 🔢`version` | IssueStatus (409 if system) |
| GET | `/labels` | — | `{items:[Label]}` |
| POST 🔑 | `/labels` | `name`*, `color` | `{resource: Label}` (409 dup) |
| PUT | `/labels/{lid}` | `name`, `color`, 🔢`version` | Label |
| DELETE 🔑 | `/labels/{lid}` | 🔢`version` | Label |
| GET | `/issue-views` | — | `{items:[IssueView]}` (owner-scoped) |
| POST 🔑 | `/issue-views` | `name`*, `filter`(object) | `{resource: IssueView}` |
| PUT | `/issue-views/{vid}` | `name`, `filter`, 🔢`version` | IssueView |
| DELETE 🔑 | `/issue-views/{vid}` | 🔢`version` | IssueView |
| GET | `/issues/{iid}/comments` | — | `{items:[Comment]}` |
| POST 🔑 | `/issues/{iid}/comments` | `body`* | `{resource: Comment}` |
| PUT | `/issues/{iid}/comments/{cid}` | `body`, 🔢`version` | Comment |
| DELETE 🔑 | `/issues/{iid}/comments/{cid}` | 🔢`version` | Comment |
| GET | `/issues/{iid}/labels` | — | `{items:[Label]}` |
| POST 🔑 | `/issues/{iid}/labels` | `labelId`* | `{resource: Label}` |
| DELETE 🔑 | `/issues/{iid}/labels/{lid}` | — | Label |
| GET | `/issues/{iid}/subscribers` | — | `{items:[User]}` |
| POST 🔑 | `/issues/{iid}/subscribers` | `userId`* | `{resource: User}` |
| DELETE 🔑 | `/issues/{iid}/subscribers` | `userId`* | `{resource: User}` |

## Projects / workspaces / operations

| Method | Path | Body fields | Response |
| --- | --- | --- | --- |
| GET | `/projects` | — | `{items, nextCursor}` (owner-scoped) |
| POST 🔑 | `/projects` | `name`*, `repositoryUrl`*, `defaultBranch`, `credentialRefId` | 202 `{resource, workspace, operation}` |
| GET | `/projects/{pid}` | — | Project |
| PATCH | `/projects/{pid}` | `name`, 🔢`version` | Project |
| DELETE 🔑 | `/projects/{pid}` | 🔢`version` | 202 `{resource, operation}` |
| GET | `/projects/{pid}/workspaces` | — | `{items, nextCursor}` |
| POST 🔑 | `/projects/{pid}/workspaces` | `title`*, `baseRef`* | 202 `{resource, operation}` |
| GET | `/workspaces/{wid}` | — | Workspace |
| POST 🔑 | `/workspaces/{wid}/start` | 🔢`version` | 202 `{resource, operation}` |
| POST 🔑 | `/workspaces/{wid}/stop` | 🔢`version` | 202 `{resource, operation}` |
| DELETE 🔑 | `/workspaces/{wid}` | 🔢`version` | 202 `{resource, operation}` |
| GET | `/operations/{oid}` | — | Operation |
| POST 🔑 | `/operations/{oid}/retry` | 🔢`version` | `{operation}` |
| GET | `/resource-status` | — | `{items}` (admin projection) |
| POST 🔑 | `/workspaces/{wid}/administrative-stop` | 🔢`version` | 202 `{resource, operation}` (admin) |

## Internal / control API (`/internal/v1`)

Service-only (no user token except where noted). `epoch` = controller fencing. See
[../../core-contract.md](../../core-contract.md) + [../../execution-contract.md](../../execution-contract.md).

| Action | Path | Notes |
| --- | --- | --- |
| access | `/internal/v1/access` | +user token; `tenantId, workspaceId, action, epoch` |
| admit | `/internal/v1/admissions` | +user token; `tenantId, workspaceId, action, ticketId, kind, epoch` |
| lease_acquire / renew / release | `/internal/v1/controller-lease/*` | controller leadership |
| claim | `/internal/v1/operations/claim` | poll next operation |
| snapshot | `/internal/v1/operations/{oid}/snapshot` | restricted aggregate |
| plan | `/internal/v1/operations/{oid}/effects` | persist external-effect plan |
| effect_result | `/internal/v1/operations/{oid}/effects/{eid}/result` | report substrate outcome |
| advance / defer | `/internal/v1/operations/{oid}/advance` `/defer` | step machine |
| node_register / status / idle | `/internal/v1/nodes/*` | node lifecycle |
| node_finish | `/internal/v1/nodes/tickets/{ticket}/finish` | execution ticket |

## Field validation (`validField`)

| Field(s) | Accepted JSON type |
| --- | --- |
| default (incl. `title`, `status`, `name`, `body`, `key`, …) | string |
| `version`, `epoch`, `position`, `retrySeconds`, … | integer (json.Number, Int64) |
| `ids` | array of strings |
| `filter`, `properties` | object |
| `idle`, `initialized` | bool |

Unknown body key → 400 `unknown_field`; wrong type → 400 `invalid_field_type`; `null` body on
non-GET → 400 `invalid_json` (send `{}` for an empty body, e.g. label detach).

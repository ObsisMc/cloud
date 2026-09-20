/**
 * Cloud Issue domain types, mirroring the backend contract in `api/openapi.json`.
 *
 * These are the *real* shapes returned by `/api/v1/tenants/:tid/issues*` and are
 * deliberately separate from the MSW mock types in `@/mocks/data/types` (which the
 * non-issue preview features still use). The two must not drift into each other.
 */

/** Board priority levels; the backend validates against exactly these five. */
export type IssuePriority = 'urgent' | 'high' | 'medium' | 'low' | 'none'

/** Polymorphic assignee owner. `user` is resolved; `agent`/`team` are opaque this wave. */
export type AssigneeType = 'user' | 'agent' | 'team'

/** Tenant label attached to issues. */
export interface Label {
  id: string
  tenantId: string
  name: string
  color: string
  version: number
  createdAt: string
  updatedAt: string
}

/** A status column in the tenant's catalog (7 system columns seeded lazily + custom ones). */
export interface IssueStatusColumn {
  id: string
  tenantId: string
  key: string
  name: string
  description: string
  category: 'unstarted' | 'started' | 'done' | 'closed'
  color: string
  icon: string
  isSystem: boolean
  position: number
  version: number
  createdAt: string
  updatedAt: string
}

/** A board issue, as `SELECT i.*` plus the attached `labels` array. */
export interface Issue {
  id: string
  tenantId: string
  creatorUserId: string
  assigneeType: AssigneeType
  assigneeId: string | null
  assigneeUserId: string | null
  parentIssueId: string | null
  projectRef: string | null
  title: string
  description: string
  status: string
  priority: IssuePriority
  position: number
  number: number
  properties: Record<string, unknown>
  version: number
  createdAt: string
  updatedAt: string
  labels: Label[]
}

/** A threaded comment on an issue. `authorType` is polymorphic; only `user` is resolved. */
export interface IssueComment {
  id: string
  tenantId: string
  issueId: string
  authorType: 'user' | 'agent' | 'team' | 'system'
  authorId: string
  authorUserId: string | null
  parentId: string | null
  body: string
  seq: number
  version: number
  createdAt: string
  updatedAt: string
}

/** A queued issue run. It is persisted as `queued`; nothing dispatches it this wave. */
export interface IssueRun {
  id: string
  tenantId: string
  issueId: string
  executorType: 'agent' | 'team' | 'workflow'
  executorId: string
  input: Record<string, unknown>
  status: string
  createdAt: string
  updatedAt: string
}

/** Reference-not-copy pointer to an external resource attached to an issue. */
export interface ContextRef {
  id: string
  tenantId: string
  issueId: string
  refType:
    | 'parent_issue'
    | 'run'
    | 'timeline_message'
    | 'pull_request'
    | 'project'
    | 'workspace'
    | 'acceptance_criteria'
  refId: string
  createdAt: string
}

/** A tenant member, used to resolve user assignee display names. */
export interface TenantMember {
  id: string
  userId: string
  role: 'admin' | 'member'
  status: 'active' | 'disabled'
  version: number
  displayName: string
}

/** A tenant the signed-in user belongs to (from `/api/v1/me/tenants`). */
export interface Tenant {
  id: string
  name: string
  status: string
  role: string
}

/** Paginated list envelope the public API returns for `items` endpoints. */
export interface Page<T> {
  items: T[]
  nextCursor: string
}

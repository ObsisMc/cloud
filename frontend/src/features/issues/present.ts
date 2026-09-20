import { statusLabelText } from '@/components/common/issue-badges'
import type { Issue, IssueStatusColumn, TenantMember } from './types'

/** Board-facing identifier: the tenant-wide `number`, shown as `#42`. */
export function issueNumber(issue: Issue): string {
  return `#${issue.number}`
}

/** Builds a `userId -> displayName` lookup from the member list, for assignee labels. */
export function memberNameById(members: TenantMember[] | undefined): ReadonlyMap<string, string> {
  const map = new Map<string, string>()
  for (const m of members ?? []) map.set(m.userId, m.displayName)
  return map
}

/** Resolved assignee type: only `user` has a resolvable human this wave. */
export function assigneeType(issue: Issue): 'user' | 'agent' | 'team' | null {
  if (issue.assigneeUserId) return 'user'
  if (issue.assigneeType === 'agent') return 'agent'
  if (issue.assigneeType === 'team') return 'team'
  return null
}

/** Human label for the assignee; agent/team are opaque and fall back to their type name. */
export function assigneeName(issue: Issue, members: ReadonlyMap<string, string>): string {
  if (issue.assigneeUserId) return members.get(issue.assigneeUserId) ?? '用户'
  if (issue.assigneeType === 'agent') return 'Agent'
  if (issue.assigneeType === 'team') return 'Team'
  return '未分配'
}

/** Column header label: canonical keys use a localized label, custom keys use the catalog name. */
export function columnLabel(statuses: readonly IssueStatusColumn[], key: string): string {
  const canonical = statusLabelText(key)
  if (canonical !== key) return canonical
  return statuses.find((s) => s.key === key)?.name ?? key
}

/**
 * Column keys in catalog order, with any key absent from the catalog appended at
 * the end. Boards and the grouped list share this so a custom/unseeded status never
 * silently drops its cards.
 */
export function orderedColumns(
  statuses: readonly IssueStatusColumn[],
  keys: Iterable<string>,
): string[] {
  const columns = statuses.map((s) => s.key)
  for (const key of keys) if (!columns.includes(key)) columns.push(key)
  return columns
}

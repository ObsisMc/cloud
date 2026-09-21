import { HttpResponse } from 'msw'
import { db } from '@/mocks/data/store'
import type { IssuePriority, IssueStatus, Project } from '@/mocks/data/types'

/** Base path every mock endpoint lives under, kept distinct from the real `/api` contract. */
export const MOCK_BASE = '/mock-api'

type PathParams = Record<string, string | readonly string[] | undefined>

export function pathParam(params: PathParams, name: string): string {
  const value = params[name]
  if (typeof value === 'string') return value
  if (value?.length === 1 && typeof value[0] === 'string') return value[0]
  throw new Error(`missing path parameter: ${name}`)
}

export function jsonObject(value: unknown): Record<string, unknown> {
  if (typeof value === 'object' && value !== null && !Array.isArray(value)) {
    return Object.fromEntries(Object.entries(value))
  }
  return {}
}

export function stringField(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined
}

export function booleanField(value: unknown): boolean | undefined {
  return typeof value === 'boolean' ? value : undefined
}

function enumField<T extends string>(value: unknown, values: readonly T[]): T | undefined {
  return values.find((candidate) => candidate === value)
}

export function issueStatus(value: unknown): IssueStatus | undefined {
  return enumField(value, ['backlog', 'todo', 'in_progress', 'in_review', 'blocked', 'done'])
}

export function issuePriority(value: unknown): IssuePriority | undefined {
  return enumField(value, ['none', 'low', 'medium', 'high', 'urgent'])
}

export function projectStatus(value: unknown): Project['status'] | undefined {
  return enumField(value, ['planned', 'in_progress', 'completed', 'paused'])
}

export function runtimeAction(value: unknown): 'start' | 'stop' | undefined {
  return enumField(value, ['start', 'stop'])
}

export function notFound(message = 'not found') {
  return HttpResponse.json({ message }, { status: 404 })
}

export function requireWorkspace(slug: string) {
  // The route slug is a mock workspace slug in demo mode, and the mock-api
  // interceptor rewrites cloud-mode requests to the seeded workspace, so an
  // unknown slug genuinely means the workspace does not exist.
  return db.workspaces.find((w) => w.slug === slug) ?? null
}

export function withMember(userId: string) {
  const user = db.users.find((u) => u.id === userId)
  const member = db.members.find((m) => m.userId === userId)
  if (!user || !member) return null
  return { ...user, role: member.role, status: member.status, joinedAt: member.joinedAt }
}

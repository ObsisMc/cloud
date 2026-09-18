import { HttpResponse } from 'msw'
import { db } from '../data/store'

/** Base path every mock endpoint lives under, kept distinct from the real `/api` contract. */
export const MOCK_BASE = '/mock-api'

export function notFound(message = 'not found') {
  return HttpResponse.json({ message }, { status: 404 })
}

export function requireWorkspace(slug: string) {
  return db.workspaces.find((w) => w.slug === slug) ?? null
}

export function withMember(userId: string) {
  const user = db.users.find((u) => u.id === userId)
  const member = db.members.find((m) => m.userId === userId)
  if (!user || !member) return null
  return { ...user, role: member.role, status: member.status, joinedAt: member.joinedAt }
}

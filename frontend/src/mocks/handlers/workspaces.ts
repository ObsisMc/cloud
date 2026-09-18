import { http, HttpResponse } from 'msw'
import { db } from '../data/store'
import { MOCK_BASE, notFound, requireWorkspace } from './shared'

export const workspaceHandlers = [
  http.get(`${MOCK_BASE}/workspaces`, () => HttpResponse.json(db.workspaces)),

  http.get(`${MOCK_BASE}/workspaces/:slug`, ({ params }) => {
    const ws = requireWorkspace(params['slug'] as string)
    return ws ? HttpResponse.json(ws) : notFound('workspace not found')
  }),

  http.get(`${MOCK_BASE}/workspaces/:slug/members`, ({ params }) => {
    const ws = requireWorkspace(params['slug'] as string)
    if (!ws) return notFound('workspace not found')
    const list = db.members
      .filter((m) => m.workspaceId === ws.id)
      .map((m) => {
        const user = db.users.find((u) => u.id === m.userId)!
        return { ...user, role: m.role, status: m.status, joinedAt: m.joinedAt }
      })
    return HttpResponse.json(list)
  }),
]

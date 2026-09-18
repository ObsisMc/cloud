import { http, HttpResponse } from 'msw'
import { db } from '@/mocks/data/store'
import { MOCK_BASE, notFound, pathParam, requireWorkspace } from './shared'

export const workspaceHandlers = [
  http.get(`${MOCK_BASE}/workspaces`, () => HttpResponse.json(db.workspaces)),

  http.get(`${MOCK_BASE}/workspaces/:slug`, ({ params }) => {
    const ws = requireWorkspace(pathParam(params, 'slug'))
    return ws ? HttpResponse.json(ws) : notFound('workspace not found')
  }),

  http.get(`${MOCK_BASE}/workspaces/:slug/members`, ({ params }) => {
    const ws = requireWorkspace(pathParam(params, 'slug'))
    if (!ws) return notFound('workspace not found')
    const list = db.members
      .filter((m) => m.workspaceId === ws.id)
      .map((m) => {
        const user = db.users.find((u) => u.id === m.userId)
        if (!user) return null
        return Object.assign({}, user, { role: m.role, status: m.status, joinedAt: m.joinedAt })
      })
      .filter((member) => member !== null)
    return HttpResponse.json(list)
  }),
]

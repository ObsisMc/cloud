import { http, HttpResponse } from 'msw'
import { db } from '../data/store'
import { MOCK_BASE, notFound, requireWorkspace } from './shared'

export const inboxHandlers = [
  http.get(`${MOCK_BASE}/workspaces/:slug/inbox`, ({ params }) => {
    const ws = requireWorkspace(params['slug'] as string)
    if (!ws) return notFound('workspace not found')
    const list = [...db.inboxItems.filter((i) => i.workspaceId === ws.id)].sort((a, b) =>
      b.createdAt.localeCompare(a.createdAt),
    )
    return HttpResponse.json(list)
  }),

  http.patch(`${MOCK_BASE}/workspaces/:slug/inbox/:id`, async ({ params, request }) => {
    const item = db.inboxItems.find((i) => i.id === params['id'])
    if (!item) return notFound('inbox item not found')
    const patch = (await request.json()) as { read?: boolean }
    if (typeof patch.read === 'boolean') item.read = patch.read
    return HttpResponse.json(item)
  }),

  http.post(`${MOCK_BASE}/workspaces/:slug/inbox/mark-all-read`, ({ params }) => {
    const ws = requireWorkspace(params['slug'] as string)
    if (!ws) return notFound('workspace not found')
    db.inboxItems.filter((i) => i.workspaceId === ws.id).forEach((i) => (i.read = true))
    return new HttpResponse(null, { status: 204 })
  }),
]

import { http, HttpResponse } from 'msw'
import { db } from '@/mocks/data/store'
import {
  MOCK_BASE,
  booleanField,
  jsonObject,
  notFound,
  pathParam,
  requireWorkspace,
} from './shared'

export const inboxHandlers = [
  http.get(`${MOCK_BASE}/workspaces/:slug/inbox`, ({ params }) => {
    const ws = requireWorkspace(pathParam(params, 'slug'))
    if (!ws) return notFound('workspace not found')
    const list = db.inboxItems
      .filter((i) => i.workspaceId === ws.id)
      .toSorted((a, b) => b.createdAt.localeCompare(a.createdAt))
    return HttpResponse.json(list)
  }),

  http.patch(`${MOCK_BASE}/workspaces/:slug/inbox/:id`, async ({ params, request }) => {
    const item = db.inboxItems.find((i) => i.id === pathParam(params, 'id'))
    if (!item) return notFound('inbox item not found')
    const patch = jsonObject(await request.json())
    const read = booleanField(patch['read'])
    if (read !== undefined) item.read = read
    return HttpResponse.json(item)
  }),

  http.post(`${MOCK_BASE}/workspaces/:slug/inbox/mark-all-read`, ({ params }) => {
    const ws = requireWorkspace(pathParam(params, 'slug'))
    if (!ws) return notFound('workspace not found')
    db.inboxItems.filter((i) => i.workspaceId === ws.id).forEach((i) => (i.read = true))
    return new HttpResponse(null, { status: 204 })
  }),
]

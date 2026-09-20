import { http, HttpResponse } from 'msw'
import { db } from '@/mocks/data/store'
import {
  MOCK_BASE,
  jsonObject,
  notFound,
  pathParam,
  requireWorkspace,
  runtimeAction,
} from './shared'

export const runtimeHandlers = [
  http.get(`${MOCK_BASE}/workspaces/:slug/runtimes`, ({ params }) => {
    const ws = requireWorkspace(pathParam(params, 'slug'))
    if (!ws) return notFound('workspace not found')
    return HttpResponse.json(db.runtimes.filter((r) => r.workspaceId === ws.id))
  }),

  http.post(`${MOCK_BASE}/workspaces/:slug/runtimes/:id/action`, async ({ params, request }) => {
    const runtime = db.runtimes.find((r) => r.id === pathParam(params, 'id'))
    if (!runtime) return notFound('runtime not found')
    const action = runtimeAction(jsonObject(await request.json())['action'])
    if (!action) return notFound('runtime action is invalid')
    runtime.status = action === 'start' ? 'running' : 'stopped'
    return HttpResponse.json(runtime)
  }),
]

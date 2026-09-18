import { http, HttpResponse } from 'msw'
import { db } from '../data/store'
import { MOCK_BASE, notFound, requireWorkspace } from './shared'

export const runtimeHandlers = [
  http.get(`${MOCK_BASE}/workspaces/:slug/runtimes`, ({ params }) => {
    const ws = requireWorkspace(params['slug'] as string)
    if (!ws) return notFound('workspace not found')
    return HttpResponse.json(db.runtimes.filter((r) => r.workspaceId === ws.id))
  }),

  http.post(`${MOCK_BASE}/workspaces/:slug/runtimes/:id/action`, async ({ params, request }) => {
    const runtime = db.runtimes.find((r) => r.id === params['id'])
    if (!runtime) return notFound('runtime not found')
    const body = (await request.json()) as { action: 'start' | 'stop' }
    runtime.status = body.action === 'start' ? 'running' : 'stopped'
    return HttpResponse.json(runtime)
  }),
]

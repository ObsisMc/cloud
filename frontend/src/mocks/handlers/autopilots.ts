import { http, HttpResponse } from 'msw'
import { db } from '../data/store'
import type { Autopilot } from '../data/types'
import { MOCK_BASE, notFound, requireWorkspace } from './shared'

export const autopilotHandlers = [
  http.get(`${MOCK_BASE}/workspaces/:slug/autopilots`, ({ params }) => {
    const ws = requireWorkspace(params.slug as string)
    if (!ws) return notFound('workspace not found')
    return HttpResponse.json(db.autopilots.filter((a) => a.workspaceId === ws.id))
  }),

  http.get(`${MOCK_BASE}/workspaces/:slug/autopilots/:id`, ({ params }) => {
    const autopilot = db.autopilots.find((a) => a.id === params.id)
    return autopilot ? HttpResponse.json(autopilot) : notFound('autopilot not found')
  }),

  http.patch(`${MOCK_BASE}/workspaces/:slug/autopilots/:id`, async ({ params, request }) => {
    const autopilot = db.autopilots.find((a) => a.id === params.id)
    if (!autopilot) return notFound('autopilot not found')
    const patch = (await request.json()) as Partial<Autopilot>
    Object.assign(autopilot, patch)
    return HttpResponse.json(autopilot)
  }),
]

import { http, HttpResponse } from 'msw'
import { db } from '../data/store'
import { MOCK_BASE, notFound, requireWorkspace } from './shared'

export const skillHandlers = [
  http.get(`${MOCK_BASE}/workspaces/:slug/skills`, ({ params }) => {
    const ws = requireWorkspace(params.slug as string)
    if (!ws) return notFound('workspace not found')
    return HttpResponse.json(db.skills.filter((s) => s.workspaceId === ws.id))
  }),

  http.patch(`${MOCK_BASE}/workspaces/:slug/skills/:id`, async ({ params, request }) => {
    const skill = db.skills.find((s) => s.id === params.id)
    if (!skill) return notFound('skill not found')
    const patch = (await request.json()) as { enabled?: boolean }
    if (typeof patch.enabled === 'boolean') skill.enabled = patch.enabled
    return HttpResponse.json(skill)
  }),
]

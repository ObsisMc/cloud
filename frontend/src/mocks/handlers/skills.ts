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

export const skillHandlers = [
  http.get(`${MOCK_BASE}/workspaces/:slug/skills`, ({ params }) => {
    const ws = requireWorkspace(pathParam(params, 'slug'))
    if (!ws) return notFound('workspace not found')
    return HttpResponse.json(db.skills.filter((s) => s.workspaceId === ws.id))
  }),

  http.patch(`${MOCK_BASE}/workspaces/:slug/skills/:id`, async ({ params, request }) => {
    const skill = db.skills.find((s) => s.id === pathParam(params, 'id'))
    if (!skill) return notFound('skill not found')
    const patch = jsonObject(await request.json())
    const enabled = booleanField(patch['enabled'])
    if (enabled !== undefined) skill.enabled = enabled
    return HttpResponse.json(skill)
  }),
]

import { http, HttpResponse } from 'msw'
import { db } from '@/mocks/data/store'
import { MOCK_BASE, notFound, pathParam, requireWorkspace } from './shared'

export const squadHandlers = [
  http.get(`${MOCK_BASE}/workspaces/:slug/squads`, ({ params }) => {
    const ws = requireWorkspace(pathParam(params, 'slug'))
    if (!ws) return notFound('workspace not found')
    return HttpResponse.json(db.squads.filter((s) => s.workspaceId === ws.id))
  }),

  http.get(`${MOCK_BASE}/workspaces/:slug/squads/:id`, ({ params }) => {
    const squad = db.squads.find((s) => s.id === pathParam(params, 'id'))
    return squad ? HttpResponse.json(squad) : notFound('squad not found')
  }),
]

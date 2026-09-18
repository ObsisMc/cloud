import { http, HttpResponse } from 'msw'
import { db } from '@/mocks/data/store'
import { MOCK_BASE, notFound, pathParam, requireWorkspace } from './shared'

export const agentHandlers = [
  http.get(`${MOCK_BASE}/workspaces/:slug/agents`, ({ params }) => {
    const ws = requireWorkspace(pathParam(params, 'slug'))
    if (!ws) return notFound('workspace not found')
    return HttpResponse.json(db.agents.filter((a) => a.workspaceId === ws.id))
  }),

  http.get(`${MOCK_BASE}/workspaces/:slug/agents/:id`, ({ params }) => {
    const agent = db.agents.find((a) => a.id === pathParam(params, 'id'))
    return agent ? HttpResponse.json(agent) : notFound('agent not found')
  }),
]

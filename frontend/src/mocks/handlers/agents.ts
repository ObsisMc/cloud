import { http, HttpResponse } from 'msw'
import { db } from '../data/store'
import { MOCK_BASE, notFound, requireWorkspace } from './shared'

export const agentHandlers = [
  http.get(`${MOCK_BASE}/workspaces/:slug/agents`, ({ params }) => {
    const ws = requireWorkspace(params.slug as string)
    if (!ws) return notFound('workspace not found')
    return HttpResponse.json(db.agents.filter((a) => a.workspaceId === ws.id))
  }),

  http.get(`${MOCK_BASE}/workspaces/:slug/agents/:id`, ({ params }) => {
    const agent = db.agents.find((a) => a.id === params.id)
    return agent ? HttpResponse.json(agent) : notFound('agent not found')
  }),
]

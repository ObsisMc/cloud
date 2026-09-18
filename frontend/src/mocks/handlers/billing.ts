import { http, HttpResponse } from 'msw'
import { db } from '@/mocks/data/store'
import { MOCK_BASE, notFound, pathParam, requireWorkspace } from './shared'

export const billingHandlers = [
  http.get(`${MOCK_BASE}/workspaces/:slug/billing`, ({ params }) => {
    const ws = requireWorkspace(pathParam(params, 'slug'))
    if (!ws) return notFound('workspace not found')
    let amount = 0
    if (ws.plan === 'business') amount = 899
    else if (ws.plan === 'pro') amount = 299
    return HttpResponse.json({
      plan: ws.plan,
      seats: db.members.length,
      renewalDate: new Date(Date.now() + 21 * 86_400_000).toISOString(),
      amount,
    })
  }),

  http.get(`${MOCK_BASE}/workspaces/:slug/invoices`, ({ params }) => {
    const ws = requireWorkspace(pathParam(params, 'slug'))
    if (!ws) return notFound('workspace not found')
    return HttpResponse.json(db.invoices.filter((i) => i.workspaceId === ws.id))
  }),
]

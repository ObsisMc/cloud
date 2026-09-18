import { http, HttpResponse } from 'msw'
import { db } from '../data/store'
import { MOCK_BASE, notFound, requireWorkspace } from './shared'

export const billingHandlers = [
  http.get(`${MOCK_BASE}/workspaces/:slug/billing`, ({ params }) => {
    const ws = requireWorkspace(params['slug'] as string)
    if (!ws) return notFound('workspace not found')
    return HttpResponse.json({
      plan: ws.plan,
      seats: db.members.length,
      renewalDate: new Date(Date.now() + 21 * 86_400_000).toISOString(),
      amount: ws.plan === 'business' ? 899 : ws.plan === 'pro' ? 299 : 0,
    })
  }),

  http.get(`${MOCK_BASE}/workspaces/:slug/invoices`, ({ params }) => {
    const ws = requireWorkspace(params['slug'] as string)
    if (!ws) return notFound('workspace not found')
    return HttpResponse.json(db.invoices.filter((i) => i.workspaceId === ws.id))
  }),
]

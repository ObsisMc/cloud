import { http, HttpResponse } from 'msw'
import { db, nextId, nextIssueIdentifier } from '../data/store'
import type { Issue } from '../data/types'
import { MOCK_BASE, notFound, requireWorkspace } from './shared'

export const issueHandlers = [
  http.get(`${MOCK_BASE}/workspaces/:slug/issues`, ({ params, request }) => {
    const ws = requireWorkspace(params['slug'] as string)
    if (!ws) return notFound('workspace not found')
    const url = new URL(request.url)
    const status = url.searchParams.get('status')
    const projectId = url.searchParams.get('projectId')
    const assigneeId = url.searchParams.get('assigneeId')
    let list = db.issues.filter((i) => i.workspaceId === ws.id)
    if (status) list = list.filter((i) => i.status === status)
    if (projectId) list = list.filter((i) => i.projectId === projectId)
    if (assigneeId) list = list.filter((i) => i.assigneeId === assigneeId)
    list = [...list].sort((a, b) => a.order - b.order)
    return HttpResponse.json(list)
  }),

  http.get(`${MOCK_BASE}/workspaces/:slug/issues/:id`, ({ params }) => {
    const issue = db.issues.find((i) => i.id === params['id'])
    return issue ? HttpResponse.json(issue) : notFound('issue not found')
  }),

  http.post(`${MOCK_BASE}/workspaces/:slug/issues`, async ({ params, request }) => {
    const ws = requireWorkspace(params['slug'] as string)
    if (!ws) return notFound('workspace not found')
    const body = (await request.json()) as Partial<Issue>
    const now = new Date().toISOString()
    const status = body.status ?? 'backlog'
    const columnOrders = db.issues
      .filter((i) => i.workspaceId === ws.id && i.status === status)
      .map((i) => i.order)
    const issue: Issue = {
      id: nextId('issue'),
      workspaceId: ws.id,
      identifier: nextIssueIdentifier(),
      title: body.title?.trim() || 'Untitled issue',
      description: body.description ?? '',
      status,
      priority: body.priority ?? 'none',
      assigneeId: body.assigneeId ?? null,
      projectId: body.projectId ?? null,
      labels: body.labels ?? [],
      createdAt: now,
      updatedAt: now,
      commentCount: 0,
      // New issues land at the top of their column, same as before.
      order: columnOrders.length > 0 ? Math.min(...columnOrders) - 1 : 0,
    }
    db.issues.unshift(issue)
    return HttpResponse.json(issue, { status: 201 })
  }),

  http.patch(`${MOCK_BASE}/workspaces/:slug/issues/:id`, async ({ params, request }) => {
    const issue = db.issues.find((i) => i.id === params['id'])
    if (!issue) return notFound('issue not found')
    const patch = (await request.json()) as Partial<Issue>
    Object.assign(issue, patch, { updatedAt: new Date().toISOString() })
    return HttpResponse.json(issue)
  }),

  http.delete(`${MOCK_BASE}/workspaces/:slug/issues/:id`, ({ params }) => {
    const idx = db.issues.findIndex((i) => i.id === params['id'])
    if (idx === -1) return notFound('issue not found')
    db.issues.splice(idx, 1)
    return new HttpResponse(null, { status: 204 })
  }),
]

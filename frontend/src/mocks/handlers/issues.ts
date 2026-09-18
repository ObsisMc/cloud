import { http, HttpResponse } from 'msw'
import { db, nextId, nextIssueIdentifier } from '@/mocks/data/store'
import type { Issue } from '@/mocks/data/types'
import {
  MOCK_BASE,
  issuePriority,
  issueStatus,
  jsonObject,
  notFound,
  pathParam,
  requireWorkspace,
  stringField,
} from './shared'

export const issueHandlers = [
  http.get(`${MOCK_BASE}/workspaces/:slug/issues`, ({ params, request }) => {
    const ws = requireWorkspace(pathParam(params, 'slug'))
    if (!ws) return notFound('workspace not found')
    const url = new URL(request.url)
    const status = url.searchParams.get('status')
    const projectId = url.searchParams.get('projectId')
    const assigneeId = url.searchParams.get('assigneeId')
    let list = db.issues.filter((i) => i.workspaceId === ws.id)
    if (status) list = list.filter((i) => i.status === status)
    if (projectId) list = list.filter((i) => i.projectId === projectId)
    if (assigneeId) list = list.filter((i) => i.assigneeId === assigneeId)
    list = list.toSorted((a, b) => a.order - b.order)
    return HttpResponse.json(list)
  }),

  http.get(`${MOCK_BASE}/workspaces/:slug/issues/:id`, ({ params }) => {
    const issue = db.issues.find((i) => i.id === pathParam(params, 'id'))
    return issue ? HttpResponse.json(issue) : notFound('issue not found')
  }),

  http.post(`${MOCK_BASE}/workspaces/:slug/issues`, async ({ params, request }) => {
    const ws = requireWorkspace(pathParam(params, 'slug'))
    if (!ws) return notFound('workspace not found')
    const body = jsonObject(await request.json())
    const now = new Date().toISOString()
    const status = issueStatus(body['status']) ?? 'backlog'
    const columnOrders = db.issues
      .filter((i) => i.workspaceId === ws.id && i.status === status)
      .map((i) => i.order)
    const issue: Issue = {
      id: nextId('issue'),
      workspaceId: ws.id,
      identifier: nextIssueIdentifier(),
      title: stringField(body['title'])?.trim() || 'Untitled issue',
      description: stringField(body['description']) ?? '',
      status,
      priority: issuePriority(body['priority']) ?? 'none',
      assigneeId: stringField(body['assigneeId']) ?? null,
      projectId: stringField(body['projectId']) ?? null,
      labels: Array.isArray(body['labels'])
        ? body['labels'].filter((label): label is string => typeof label === 'string')
        : [],
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
    const issue = db.issues.find((i) => i.id === pathParam(params, 'id'))
    if (!issue) return notFound('issue not found')
    const body = jsonObject(await request.json())
    const patch: Partial<Issue> = {}
    const title = stringField(body['title'])
    const description = stringField(body['description'])
    const status = issueStatus(body['status'])
    const priority = issuePriority(body['priority'])
    if (title !== undefined) patch.title = title
    if (description !== undefined) patch.description = description
    if (status !== undefined) patch.status = status
    if (priority !== undefined) patch.priority = priority
    Object.assign(issue, patch, { updatedAt: new Date().toISOString() })
    return HttpResponse.json(issue)
  }),

  http.delete(`${MOCK_BASE}/workspaces/:slug/issues/:id`, ({ params }) => {
    const idx = db.issues.findIndex((i) => i.id === pathParam(params, 'id'))
    if (idx === -1) return notFound('issue not found')
    db.issues.splice(idx, 1)
    return new HttpResponse(null, { status: 204 })
  }),
]

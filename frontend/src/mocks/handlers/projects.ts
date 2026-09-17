import { http, HttpResponse } from 'msw'
import { db, nextId } from '../data/store'
import type { Project } from '../data/types'
import { MOCK_BASE, notFound, requireWorkspace } from './shared'

export const projectHandlers = [
  http.get(`${MOCK_BASE}/workspaces/:slug/projects`, ({ params }) => {
    const ws = requireWorkspace(params.slug as string)
    if (!ws) return notFound('workspace not found')
    return HttpResponse.json(db.projects.filter((p) => p.workspaceId === ws.id))
  }),

  http.get(`${MOCK_BASE}/workspaces/:slug/projects/:id`, ({ params }) => {
    const project = db.projects.find((p) => p.id === params.id)
    return project ? HttpResponse.json(project) : notFound('project not found')
  }),

  http.post(`${MOCK_BASE}/workspaces/:slug/projects`, async ({ params, request }) => {
    const ws = requireWorkspace(params.slug as string)
    if (!ws) return notFound('workspace not found')
    const body = (await request.json()) as Partial<Project>
    const project: Project = {
      id: nextId('project'),
      workspaceId: ws.id,
      title: body.title?.trim() || 'Untitled project',
      description: body.description ?? '',
      icon: body.icon ?? 'Boxes',
      color: body.color ?? '#3b82f6',
      status: body.status ?? 'planned',
      leadId: body.leadId ?? db.users[0].id,
      targetDate: body.targetDate ?? null,
      createdAt: new Date().toISOString(),
    }
    db.projects.unshift(project)
    return HttpResponse.json(project, { status: 201 })
  }),

  http.patch(`${MOCK_BASE}/workspaces/:slug/projects/:id`, async ({ params, request }) => {
    const project = db.projects.find((p) => p.id === params.id)
    if (!project) return notFound('project not found')
    const patch = (await request.json()) as Partial<Project>
    Object.assign(project, patch)
    return HttpResponse.json(project)
  }),
]

import { http, HttpResponse } from 'msw'
import { db, nextId } from '@/mocks/data/store'
import type { Project } from '@/mocks/data/types'
import {
  MOCK_BASE,
  jsonObject,
  notFound,
  pathParam,
  projectStatus,
  requireWorkspace,
  stringField,
} from './shared'

export const projectHandlers = [
  http.get(`${MOCK_BASE}/workspaces/:slug/projects`, ({ params }) => {
    const ws = requireWorkspace(pathParam(params, 'slug'))
    if (!ws) return notFound('workspace not found')
    return HttpResponse.json(db.projects.filter((p) => p.workspaceId === ws.id))
  }),

  http.get(`${MOCK_BASE}/workspaces/:slug/projects/:id`, ({ params }) => {
    const project = db.projects.find((p) => p.id === pathParam(params, 'id'))
    return project ? HttpResponse.json(project) : notFound('project not found')
  }),

  http.post(`${MOCK_BASE}/workspaces/:slug/projects`, async ({ params, request }) => {
    const ws = requireWorkspace(pathParam(params, 'slug'))
    if (!ws) return notFound('workspace not found')
    const body = jsonObject(await request.json())
    const project: Project = {
      id: nextId('project'),
      workspaceId: ws.id,
      title: stringField(body['title'])?.trim() || 'Untitled project',
      description: stringField(body['description']) ?? '',
      icon: stringField(body['icon']) ?? 'Boxes',
      color: stringField(body['color']) ?? '#3b82f6',
      status: projectStatus(body['status']) ?? 'planned',
      leadId: stringField(body['leadId']) ?? db.users[0].id,
      targetDate: stringField(body['targetDate']) ?? null,
      createdAt: new Date().toISOString(),
    }
    db.projects.unshift(project)
    return HttpResponse.json(project, { status: 201 })
  }),

  http.patch(`${MOCK_BASE}/workspaces/:slug/projects/:id`, async ({ params, request }) => {
    const project = db.projects.find((p) => p.id === pathParam(params, 'id'))
    if (!project) return notFound('project not found')
    const body = jsonObject(await request.json())
    const title = stringField(body['title'])
    const description = stringField(body['description'])
    const status = projectStatus(body['status'])
    if (title !== undefined) project.title = title
    if (description !== undefined) project.description = description
    if (status !== undefined) project.status = status
    return HttpResponse.json(project)
  }),
]

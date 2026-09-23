import { describe, expect, it } from 'vitest'
import { db } from '@/mocks/data/store'
import {
  booleanField,
  jsonObject,
  pathParam,
  requireWorkspace,
  runtimeAction,
  stringField,
  withMember,
} from './shared'

const BASE = '/mock-api'
const SLUG = db.workspace.slug

async function json(method: string, path: string, body?: unknown) {
  const init: RequestInit =
    body === undefined
      ? { method }
      : { method, headers: { 'content-type': 'application/json' }, body: JSON.stringify(body) }
  const res = await fetch(`${BASE}${path}`, init)
  return { status: res.status, body: res.status === 204 ? null : await res.json() }
}

describe('mock project handlers', () => {
  it('lists, creates with defaults, reads and patches a project', async () => {
    const list = await json('GET', `/workspaces/${SLUG}/projects`)
    expect(list.status).toBe(200)
    expect(list.body.length).toBeGreaterThan(0)

    const created = await json('POST', `/workspaces/${SLUG}/projects`, { title: '  ' })
    expect(created.status).toBe(201)
    expect(created.body).toMatchObject({
      title: 'Untitled project',
      description: '',
      status: 'planned',
      targetDate: null,
      workspaceId: db.workspace.id,
    })

    const explicit = await json('POST', `/workspaces/${SLUG}/projects`, {
      title: 'Named',
      description: 'd',
      icon: 'Rocket',
      color: '#000',
      status: 'paused',
      leadId: 'lead',
      targetDate: '2026-12-31',
    })
    expect(explicit.body).toMatchObject({
      title: 'Named',
      description: 'd',
      icon: 'Rocket',
      color: '#000',
      status: 'paused',
      leadId: 'lead',
      targetDate: '2026-12-31',
    })

    const read = await json('GET', `/workspaces/${SLUG}/projects/${created.body.id}`)
    expect(read.status).toBe(200)
    expect(read.body.id).toBe(created.body.id)

    const patched = await json('PATCH', `/workspaces/${SLUG}/projects/${created.body.id}`, {
      title: 'Renamed',
      description: 'desc',
      status: 'completed',
      ignored: 1,
    })
    expect(patched.body).toMatchObject({
      title: 'Renamed',
      description: 'desc',
      status: 'completed',
    })

    const untouched = await json('PATCH', `/workspaces/${SLUG}/projects/${created.body.id}`, {
      status: 'not-a-status',
    })
    expect(untouched.body.status).toBe('completed')
  })

  it('404s for unknown workspaces and projects', async () => {
    expect((await json('GET', '/workspaces/nope/projects')).status).toBe(404)
    expect((await json('POST', '/workspaces/nope/projects', { title: 'x' })).status).toBe(404)
    expect((await json('GET', `/workspaces/${SLUG}/projects/missing`)).status).toBe(404)
    expect((await json('PATCH', `/workspaces/${SLUG}/projects/missing`, {})).status).toBe(404)
  })
})

describe('mock issue handlers', () => {
  it('filters the list by status, project and assignee', async () => {
    const all = await json('GET', `/workspaces/${SLUG}/issues`)
    const sample = all.body.find(
      (i: { projectId: string | null; assigneeId: string | null }) => i.projectId && i.assigneeId,
    )
    expect(sample).toBeDefined()
    const filtered = await json(
      'GET',
      `/workspaces/${SLUG}/issues?status=${sample.status}&projectId=${sample.projectId}&assigneeId=${sample.assigneeId}`,
    )
    expect(filtered.body.length).toBeGreaterThan(0)
    for (const issue of filtered.body) {
      expect(issue).toMatchObject({
        status: sample.status,
        projectId: sample.projectId,
        assigneeId: sample.assigneeId,
      })
    }
    expect((await json('GET', '/workspaces/nope/issues')).status).toBe(404)
  })

  it('applies defaults on create, keeps labels typed, and lands new issues on top of the column', async () => {
    const created = await json('POST', `/workspaces/${SLUG}/issues`, {
      title: '',
      labels: ['bug', 42, 'ui'],
      assigneeId: 'u1',
      projectId: 'p1',
      status: 'not-a-status',
      priority: 'not-a-priority',
    })
    expect(created.status).toBe(201)
    expect(created.body).toMatchObject({
      title: 'Untitled issue',
      status: 'backlog',
      priority: 'none',
      labels: ['bug', 'ui'],
      assigneeId: 'u1',
      projectId: 'p1',
      commentCount: 0,
    })
    const column = await json('GET', `/workspaces/${SLUG}/issues?status=backlog`)
    expect(column.body[0].id).toBe(created.body.id)
    expect((await json('POST', '/workspaces/nope/issues', {})).status).toBe(404)
  })

  it('patches only known fields, reads, and deletes an issue', async () => {
    const created = await json('POST', `/workspaces/${SLUG}/issues`, { title: 'To patch' })
    const id = created.body.id

    const patched = await json('PATCH', `/workspaces/${SLUG}/issues/${id}`, {
      title: 'Patched',
      description: 'more',
      status: 'in_review',
      priority: 'urgent',
    })
    expect(patched.body).toMatchObject({
      title: 'Patched',
      description: 'more',
      status: 'in_review',
      priority: 'urgent',
    })
    const unchanged = await json('PATCH', `/workspaces/${SLUG}/issues/${id}`, { status: 'bogus' })
    expect(unchanged.body.status).toBe('in_review')

    expect((await json('GET', `/workspaces/${SLUG}/issues/${id}`)).body.id).toBe(id)
    expect((await json('DELETE', `/workspaces/${SLUG}/issues/${id}`)).status).toBe(204)
    expect((await json('GET', `/workspaces/${SLUG}/issues/${id}`)).status).toBe(404)
    expect((await json('PATCH', `/workspaces/${SLUG}/issues/${id}`, {})).status).toBe(404)
    expect((await json('DELETE', `/workspaces/${SLUG}/issues/${id}`)).status).toBe(404)
  })
})

describe('mock workspace and inbox handlers', () => {
  it('reads a workspace and its members joined with user records', async () => {
    const ws = await json('GET', `/workspaces/${SLUG}`)
    expect(ws.body.slug).toBe(SLUG)
    const members = await json('GET', `/workspaces/${SLUG}/members`)
    expect(members.status).toBe(200)
    expect(members.body.length).toBeGreaterThan(0)
    expect(members.body[0]).toMatchObject({ role: expect.any(String), name: expect.any(String) })
    expect((await json('GET', '/workspaces/nope/members')).status).toBe(404)
  })

  it('lists inbox items newest first, patches read state and marks all read', async () => {
    const list = await json('GET', `/workspaces/${SLUG}/inbox`)
    expect(list.status).toBe(200)
    const dates = list.body.map((i: { createdAt: string }) => i.createdAt)
    expect(dates).toEqual(dates.toSorted((a: string, b: string) => b.localeCompare(a)))
    expect((await json('GET', '/workspaces/nope/inbox')).status).toBe(404)

    const first = list.body[0]
    const patched = await json('PATCH', `/workspaces/${SLUG}/inbox/${first.id}`, { read: false })
    expect(patched.body.read).toBe(false)
    const ignored = await json('PATCH', `/workspaces/${SLUG}/inbox/${first.id}`, { read: 'yes' })
    expect(ignored.body.read).toBe(false)
    expect((await json('PATCH', `/workspaces/${SLUG}/inbox/missing`, {})).status).toBe(404)

    expect((await json('POST', `/workspaces/${SLUG}/inbox/mark-all-read`)).status).toBe(204)
    const after = await json('GET', `/workspaces/${SLUG}/inbox`)
    expect(after.body.every((i: { read: boolean }) => i.read)).toBe(true)
    expect((await json('POST', '/workspaces/nope/inbox/mark-all-read')).status).toBe(404)
  })
})

describe('mock handler helpers', () => {
  it('narrow untrusted values at the boundary', () => {
    expect(pathParam({ slug: 'a' }, 'slug')).toBe('a')
    expect(pathParam({ slug: ['b'] }, 'slug')).toBe('b')
    expect(() => pathParam({ slug: ['a', 'b'] }, 'slug')).toThrow(/missing path parameter/)
    expect(() => pathParam({}, 'slug')).toThrow(/missing path parameter/)
    expect(jsonObject({ a: 1 })).toEqual({ a: 1 })
    expect(jsonObject([1])).toEqual({})
    expect(jsonObject(null)).toEqual({})
    expect(stringField(1)).toBeUndefined()
    expect(booleanField('true')).toBeUndefined()
    expect(booleanField(true)).toBe(true)
    expect(runtimeAction('start')).toBe('start')
    expect(runtimeAction('restart')).toBeUndefined()
    expect(requireWorkspace('nope')).toBeNull()
  })

  it('joins a member with its user record and returns null for strangers', () => {
    const member = db.members[0]
    if (!member) throw new Error('seed members must not be empty')
    expect(withMember(member.userId)).toMatchObject({ id: member.userId, role: member.role })
    expect(withMember('nobody')).toBeNull()
  })
})

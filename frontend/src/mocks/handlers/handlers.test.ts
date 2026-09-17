import { describe, expect, it } from 'vitest'
import { db } from '../data/store'

const BASE = '/mock-api'
const SLUG = db.workspace.slug

describe('mock API handlers', () => {
  it('returns the seeded workspace list', async () => {
    const res = await fetch(`${BASE}/workspaces`)
    expect(res.status).toBe(200)
    const body = await res.json()
    expect(body).toHaveLength(1)
    expect(body[0].slug).toBe(SLUG)
  })

  it('404s for an unknown workspace slug', async () => {
    const res = await fetch(`${BASE}/workspaces/does-not-exist`)
    expect(res.status).toBe(404)
  })

  it('creates and then reads back an issue', async () => {
    const createRes = await fetch(`${BASE}/workspaces/${SLUG}/issues`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ title: 'Fix the flaky test', status: 'todo', priority: 'high' }),
    })
    expect(createRes.status).toBe(201)
    const created = await createRes.json()
    expect(created.title).toBe('Fix the flaky test')
    expect(created.identifier).toMatch(/^MUL-\d+$/)

    const getRes = await fetch(`${BASE}/workspaces/${SLUG}/issues/${created.id}`)
    expect(getRes.status).toBe(200)
    const fetched = await getRes.json()
    expect(fetched.id).toBe(created.id)
  })

  it('patches an issue status and reflects it in the list filter', async () => {
    const issue = db.issues[0]
    const patchRes = await fetch(`${BASE}/workspaces/${SLUG}/issues/${issue.id}`, {
      method: 'PATCH',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ status: 'done' }),
    })
    expect(patchRes.status).toBe(200)

    const listRes = await fetch(`${BASE}/workspaces/${SLUG}/issues?status=done`)
    const list = await listRes.json()
    expect(list.some((i: { id: string }) => i.id === issue.id)).toBe(true)
  })

  it('logs in and authorizes the session with the returned token', async () => {
    const loginRes = await fetch(`${BASE}/auth/login`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ email: 'demo@example.com' }),
    })
    const { token } = await loginRes.json()
    expect(token).toBeTruthy()

    const unauthorized = await fetch(`${BASE}/auth/session`)
    expect(unauthorized.status).toBe(401)

    const authorized = await fetch(`${BASE}/auth/session`, {
      headers: { authorization: `Bearer ${token}` },
    })
    expect(authorized.status).toBe(200)
  })
})

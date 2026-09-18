import { describe, expect, it } from 'vitest'
import { actorById, currentUserId, db, nextId, nextIssueIdentifier } from './store'

describe('mock store seed data', () => {
  it('seeds a single workspace with a matching slug', () => {
    expect(db.workspace.slug).toBe('ora-demo')
  })

  it('seeds the demo user as a workspace member', () => {
    const member = db.members.find((m) => m.userId === currentUserId)
    expect(member).toBeDefined()
    expect(member?.role).toBe('owner')
  })

  it('gives every issue a unique id and identifier', () => {
    const ids = new Set(db.issues.map((i) => i.id))
    const identifiers = new Set(db.issues.map((i) => i.identifier))
    expect(ids.size).toBe(db.issues.length)
    expect(identifiers.size).toBe(db.issues.length)
  })

  it('assigns every agent to a squad that references it back', () => {
    for (const agent of db.agents) {
      const squad = db.squads.find((s) => s.id === agent.squadId)
      expect(squad).toBeDefined()
      expect(squad?.memberIds).toContain(agent.id)
    }
  })

  it('resolves actors by id across users and agents', () => {
    expect(actorById(currentUserId)?.type).toBe('user')
    expect(actorById(db.agents[0].id)?.type).toBe('agent')
    expect(actorById(null)).toBeUndefined()
    expect(actorById('does-not-exist')).toBeUndefined()
  })

  it('generates unique sequential ids and issue identifiers', () => {
    const a = nextId('x')
    const b = nextId('x')
    expect(a).not.toBe(b)
    const id1 = nextIssueIdentifier()
    const id2 = nextIssueIdentifier()
    expect(id1).not.toBe(id2)
  })
})

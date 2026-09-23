import { describe, expect, it } from 'vitest'
import { makeIssue, makeStatus } from '@/test/issue-fixtures'
import type { TenantMember } from './types'
import {
  assigneeName,
  assigneeType,
  columnLabel,
  issueNumber,
  memberNameById,
  orderedColumns,
} from './present'

const MEMBERS: TenantMember[] = [
  { id: 'm1', userId: 'u1', role: 'member', status: 'active', version: 1, displayName: 'Alice' },
  { id: 'm2', userId: 'u2', role: 'admin', status: 'active', version: 1, displayName: 'Bob' },
]

describe('issueNumber', () => {
  it('renders the tenant-wide number as #N', () => {
    expect(issueNumber(makeIssue('i1', 'x', { number: 42 }))).toBe('#42')
  })
})

describe('memberNameById', () => {
  it('returns an empty map for undefined', () => {
    expect(memberNameById(undefined).size).toBe(0)
  })

  it('maps userId to displayName', () => {
    const byId = memberNameById(MEMBERS)
    expect(byId.get('u1')).toBe('Alice')
    expect(byId.get('u2')).toBe('Bob')
    expect(byId.has('ghost')).toBe(false)
  })
})

describe('assigneeType', () => {
  it('resolves a user assignee from assigneeUserId', () => {
    expect(assigneeType(makeIssue('i1', 'x', { assigneeUserId: 'u1' }))).toBe('user')
  })

  it('resolves an opaque agent assignee', () => {
    expect(
      assigneeType(makeIssue('i1', 'x', { assigneeUserId: null, assigneeType: 'agent', assigneeId: 'a1' })),
    ).toBe('agent')
  })

  it('resolves an opaque team assignee', () => {
    expect(
      assigneeType(makeIssue('i1', 'x', { assigneeUserId: null, assigneeType: 'team', assigneeId: 't1' })),
    ).toBe('team')
  })

  it('returns null when unassigned', () => {
    expect(assigneeType(makeIssue('i1', 'x'))).toBeNull()
  })
})

describe('assigneeName', () => {
  const byId = memberNameById(MEMBERS)

  it('uses the member display name for a user assignee', () => {
    expect(assigneeName(makeIssue('i1', 'x', { assigneeUserId: 'u1' }), byId)).toBe('Alice')
  })

  it('falls back when the user assignee is not in the member list', () => {
    expect(assigneeName(makeIssue('i1', 'x', { assigneeUserId: 'ghost' }), byId)).toBe('用户')
  })

  it('labels an agent assignee', () => {
    expect(
      assigneeName(makeIssue('i1', 'x', { assigneeUserId: null, assigneeType: 'agent', assigneeId: 'a1' }), byId),
    ).toBe('Agent')
  })

  it('labels a team assignee', () => {
    expect(
      assigneeName(makeIssue('i1', 'x', { assigneeUserId: null, assigneeType: 'team', assigneeId: 't1' }), byId),
    ).toBe('Team')
  })

  it('labels an unassigned issue', () => {
    expect(assigneeName(makeIssue('i1', 'x'), byId)).toBe('未分配')
  })
})

describe('columnLabel', () => {
  const statuses = [makeStatus('custom', { name: 'Custom Column' })]

  it('uses the localized label for a canonical key', () => {
    expect(columnLabel(statuses, 'todo')).toBe('待办')
  })

  it('uses the catalog name for a custom key', () => {
    expect(columnLabel(statuses, 'custom')).toBe('Custom Column')
  })

  it('falls back to the raw key when absent from the catalog', () => {
    expect(columnLabel(statuses, 'other')).toBe('other')
  })
})

describe('orderedColumns', () => {
  const statuses = [makeStatus('todo'), makeStatus('done')]

  it('returns catalog keys in order', () => {
    expect([...orderedColumns(statuses, [])]).toEqual(['todo', 'done'])
  })

  it('appends keys absent from the catalog after the known ones', () => {
    expect([...orderedColumns(statuses, ['done', 'custom'])]).toEqual(['todo', 'done', 'custom'])
  })
})
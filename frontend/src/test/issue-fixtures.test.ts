import { describe, expect, it } from 'vitest'
import { makeIssue, makeStatus } from './issue-fixtures'

describe('makeIssue', () => {
  it('applies the overrides last so they win over the neutral defaults', () => {
    const issue = makeIssue('i1', 'Fix', { status: 'done', priority: 'urgent', position: 7 })

    expect(issue.id).toBe('i1')
    expect(issue.title).toBe('Fix')
    expect(issue.status).toBe('done')
    expect(issue.priority).toBe('urgent')
    expect(issue.position).toBe(7)
    expect(issue.labels).toEqual([])
  })
})

describe('makeStatus', () => {
  it('derives the id from the key and defaults to a system column', () => {
    const column = makeStatus('in_progress')

    expect(column.id).toBe('st-in_progress')
    expect(column.key).toBe('in_progress')
    expect(column.isSystem).toBe(true)
  })
})

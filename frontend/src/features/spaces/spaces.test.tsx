import { describe, expect, it } from 'vitest'
import { idempotencyKeyFor, normalizeSpaceRole } from '@/features/spaces/api'
import { parseSSEFrames } from '@/features/spaces/use-space-events'

describe('idempotencyKeyFor', () => {
  it('mints one key per logical mutation and replays it for the same variables', () => {
    const pending: { current: { variables: unknown; key: string } | null } = { current: null }
    const input = { name: 'Team', slug: 'team', description: '' }

    const first = idempotencyKeyFor(pending, input)
    expect(first).not.toBe('')
    // A retry re-enters mutationFn with the very same variables object.
    expect(idempotencyKeyFor(pending, input)).toBe(first)
    // A separate mutate() call carries a fresh object and must get a fresh key.
    const second = idempotencyKeyFor(pending, { name: 'Other', slug: 'other', description: '' })
    expect(second).not.toBe(first)
  })

  it('keeps the archive key stable across a retry of the same version', () => {
    const pending: { current: { variables: unknown; key: string } | null } = { current: null }
    expect(idempotencyKeyFor(pending, 1)).toBe(idempotencyKeyFor(pending, 1))
  })
})

describe('parseSSEFrames', () => {
  it('splits complete frames into typed events and keeps partial tails', () => {
    const stream =
      'data: {"type":"project.updated","spaceId":"s","projectId":"p","version":3}\n\n' +
      'data: {"type":"space.updated","spaceId":"s"}\n\n' +
      'data: {"type":"project.cr'
    const { events, rest } = parseSSEFrames(stream)
    expect(events).toEqual([
      { type: 'project.updated', spaceId: 's', projectId: 'p', version: 3 },
      { type: 'space.updated', spaceId: 's' },
    ])
    expect(rest).toBe('data: {"type":"project.cr')
  })

  it('ignores malformed data payloads and non-data lines', () => {
    const stream =
      'retry: 1000\n\ndata: not-json\n\ndata: {"type":"space.updated","spaceId":"s"}\n\n'
    const { events } = parseSSEFrames(stream)
    expect(events).toEqual([{ type: 'space.updated', spaceId: 's' }])
  })
})

describe('normalizeSpaceRole', () => {
  it('keeps known roles and falls back to member for unknown values', () => {
    expect(normalizeSpaceRole('owner')).toBe('owner')
    expect(normalizeSpaceRole('admin')).toBe('admin')
    expect(normalizeSpaceRole('member')).toBe('member')
    expect(normalizeSpaceRole('superuser')).toBe('member')
  })
})

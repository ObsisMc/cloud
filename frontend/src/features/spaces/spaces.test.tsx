import { describe, expect, it } from 'vitest'
import { normalizeSpaceRole } from '@/features/spaces/api'
import { parseSSEFrames } from '@/features/spaces/use-space-events'

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

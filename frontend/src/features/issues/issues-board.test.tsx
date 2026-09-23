import { screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'
import { makeIssue, makeStatus } from '@/test/issue-fixtures'
import { renderWithProviders } from '@/test/render'
import { server } from '@/test/msw-server'
import { IssuesBoard, insertsAfter, resolveDrop } from './issues-board'

describe('IssuesBoard', () => {
  it('renders a column per status with its cards', () => {
    server.use(
      http.get('/api/v1/tenants/t1/issues', () => HttpResponse.json({ items: [], nextCursor: '' })),
    )
    renderWithProviders(
      <IssuesBoard
        issues={[
          makeIssue('b1', 'Fix the login', { status: 'backlog' }),
          makeIssue('t1', 'Ship the board', { status: 'todo' }),
        ]}
        slug="t1"
        statuses={[makeStatus('backlog'), makeStatus('todo')]}
        members={[]}
      />,
    )

    expect(screen.getByText('Fix the login')).toBeInTheDocument()
    expect(screen.getByText('Ship the board')).toBeInTheDocument()
    // Each status label appears twice: once in the column header and once on the card.
    expect(screen.getAllByText('待规划')).toHaveLength(2)
    expect(screen.getAllByText('待办')).toHaveLength(2)
  })

  it('shows a sub-issue count badge on parent cards', () => {
    server.use(
      http.get('/api/v1/tenants/t1/issues', () => HttpResponse.json({ items: [], nextCursor: '' })),
    )
    renderWithProviders(
      <IssuesBoard
        issues={[
          makeIssue('p1', 'Parent epic', { status: 'backlog', parentIssueId: null }),
          makeIssue('c1', 'Child one', { status: 'backlog', parentIssueId: 'p1' }),
          makeIssue('c2', 'Child two', { status: 'backlog', parentIssueId: 'p1' }),
        ]}
        slug="t1"
        statuses={[makeStatus('backlog'), makeStatus('todo')]}
        members={[]}
      />,
    )

    expect(screen.getByText('2 子任务')).toBeInTheDocument()
  })

  it('omits the assignee label when the issue is unassigned', () => {
    server.use(
      http.get('/api/v1/tenants/t1/issues', () => HttpResponse.json({ items: [], nextCursor: '' })),
    )
    renderWithProviders(
      <IssuesBoard
        issues={[makeIssue('b1', 'Unassigned task', { status: 'backlog', assigneeUserId: null })]}
        slug="t1"
        statuses={[makeStatus('backlog')]}
        members={[]}
      />,
    )

    expect(screen.queryByText('未分配')).not.toBeInTheDocument()
  })

  it('reflects the issue status on the card label', () => {
    server.use(
      http.get('/api/v1/tenants/t1/issues', () => HttpResponse.json({ items: [], nextCursor: '' })),
    )
    renderWithProviders(
      <IssuesBoard
        issues={[makeIssue('t1', 'In-progress task', { status: 'in_progress' })]}
        slug="t1"
        statuses={[makeStatus('in_progress')]}
        members={[]}
      />,
    )

    // Column header + card status label both read the localized label.
    expect(screen.getAllByText('进行中')).toHaveLength(2)
  })
})

describe('resolveDrop', () => {
  // backlog: b1(0), b2(10), b3(20) — todo: t1(0)
  const issues = [
    { id: 'b1', status: 'backlog', position: 0 },
    { id: 'b2', status: 'backlog', position: 10 },
    { id: 'b3', status: 'backlog', position: 20 },
    { id: 't1', status: 'todo', position: 0 },
  ]

  it('drops at the end of an empty column with no anchors', () => {
    expect(
      resolveDrop({ issues, activeId: 'b1', overId: 'in_progress', insertAfter: false }),
    ).toEqual({ id: 'b1', status: 'in_progress' })
  })

  it('drops at the end of a column via the background, after its last card', () => {
    expect(resolveDrop({ issues, activeId: 't1', overId: 'backlog', insertAfter: false })).toEqual({
      id: 't1',
      status: 'backlog',
      beforeId: 'b3',
    })
  })

  it('anchors before the hovered card', () => {
    expect(resolveDrop({ issues, activeId: 't1', overId: 'b2', insertAfter: false })).toEqual({
      id: 't1',
      status: 'backlog',
      beforeId: 'b1',
      afterId: 'b2',
    })
  })

  it('anchors after the hovered card', () => {
    expect(resolveDrop({ issues, activeId: 't1', overId: 'b2', insertAfter: true })).toEqual({
      id: 't1',
      status: 'backlog',
      beforeId: 'b2',
      afterId: 'b3',
    })
  })

  it('reorders within the same column via the two neighboring anchors', () => {
    expect(resolveDrop({ issues, activeId: 'b3', overId: 'b2', insertAfter: false })).toEqual({
      id: 'b3',
      status: 'backlog',
      beforeId: 'b1',
      afterId: 'b2',
    })
  })

  it('returns null when the drop lands on the same neighbors (no-op)', () => {
    expect(resolveDrop({ issues, activeId: 'b3', overId: 'b2', insertAfter: true })).toBeNull()
  })

  it('returns null when dropped outside any droppable', () => {
    expect(
      resolveDrop({ issues, activeId: 'b1', overId: undefined, insertAfter: false }),
    ).toBeNull()
  })

  it('returns null for an issue id that is not in the list', () => {
    expect(
      resolveDrop({ issues, activeId: 'missing-issue', overId: 'todo', insertAfter: false }),
    ).toBeNull()
  })

  it('returns null when dropped on itself', () => {
    expect(resolveDrop({ issues, activeId: 'b1', overId: 'b1', insertAfter: false })).toBeNull()
  })
})

function eventWith(
  activeRect: { top: number; height: number } | null,
  overRect: { top: number; height: number } | null,
) {
  return {
    active: { rect: { current: { translated: activeRect } } },
    over: overRect ? { rect: overRect } : null,
  }
}

describe('insertsAfter', () => {
  it('is false when the dragged card center sits above the hovered card center', () => {
    expect(insertsAfter(eventWith({ top: 0, height: 40 }, { top: 100, height: 40 }))).toBe(false)
  })

  it('is true when the dragged card center sits below the hovered card center', () => {
    expect(insertsAfter(eventWith({ top: 150, height: 40 }, { top: 100, height: 40 }))).toBe(true)
  })

  it('is false when there is nothing to compare against', () => {
    expect(insertsAfter(eventWith(null, { top: 100, height: 40 }))).toBe(false)
    expect(insertsAfter(eventWith({ top: 0, height: 40 }, null))).toBe(false)
  })
})

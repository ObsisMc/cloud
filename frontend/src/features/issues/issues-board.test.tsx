import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { STATUS_ORDER, statusLabelText } from '@/components/common/issue-badges'
import { db } from '@/mocks/data/store'
import { renderWithProviders } from '@/test/render'
import { IssuesBoard, resolveDrop } from './issues-board'

describe('IssuesBoard', () => {
  it('renders one column per status with a matching card count and titles', () => {
    const issues = db.issues.filter((i) => i.workspaceId === db.workspace.id)
    renderWithProviders(<IssuesBoard issues={issues} slug={db.workspace.slug} />)

    for (const status of STATUS_ORDER) {
      const group = issues.filter((i) => i.status === status)
      expect(screen.getByText(statusLabelText(status))).toBeInTheDocument()
      if (group.length > 0) {
        expect(screen.getByText(group[0].identifier)).toBeInTheDocument()
        expect(screen.getByText(group[0].title)).toBeInTheDocument()
      }
    }

    const total = issues.length
    expect(screen.getAllByRole('link')).toHaveLength(total)
  })
})

describe('resolveDrop', () => {
  // backlog: b1(0), b2(10), b3(20) — todo: t1(0)
  const issues = [
    { id: 'b1', status: 'backlog', order: 0 },
    { id: 'b2', status: 'backlog', order: 10 },
    { id: 'b3', status: 'backlog', order: 20 },
    { id: 't1', status: 'todo', order: 0 },
  ] as never[]

  it('drops at the end of an empty column', () => {
    expect(resolveDrop({ issues, activeId: 'b1', overId: 'in_progress', insertAfter: false })).toEqual({
      id: 'b1',
      status: 'in_progress',
      order: 0,
    })
  })

  it('drops at the end of a column via the column background, after its last card', () => {
    expect(resolveDrop({ issues, activeId: 't1', overId: 'backlog', insertAfter: false })).toEqual({
      id: 't1',
      status: 'backlog',
      order: 21, // after b3 (order 20), no upper neighbor
    })
  })

  it('inserts before the hovered card', () => {
    expect(resolveDrop({ issues, activeId: 't1', overId: 'b2', insertAfter: false })).toEqual({
      id: 't1',
      status: 'backlog',
      order: 5, // midpoint of b1 (0) and b2 (10)
    })
  })

  it('inserts after the hovered card', () => {
    expect(resolveDrop({ issues, activeId: 't1', overId: 'b2', insertAfter: true })).toEqual({
      id: 't1',
      status: 'backlog',
      order: 15, // midpoint of b2 (10) and b3 (20)
    })
  })

  it('reorders within the same column without renumbering the rest', () => {
    // Move b3 to just before b2.
    expect(resolveDrop({ issues, activeId: 'b3', overId: 'b2', insertAfter: false })).toEqual({
      id: 'b3',
      status: 'backlog',
      order: 5, // midpoint of b1 (0) and b2 (10) — b1/b2 untouched
    })
  })

  it('returns null when dropped outside any droppable', () => {
    expect(resolveDrop({ issues, activeId: 'b1', overId: undefined, insertAfter: false })).toBeNull()
  })

  it('returns null for an issue id that is not in the list', () => {
    expect(resolveDrop({ issues, activeId: 'missing-issue', overId: 'todo', insertAfter: false })).toBeNull()
  })

  it('returns null when dropped on itself', () => {
    expect(resolveDrop({ issues, activeId: 'b1', overId: 'b1', insertAfter: false })).toBeNull()
  })
})

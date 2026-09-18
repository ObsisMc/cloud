import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { STATUS_ORDER } from '@/components/common/issue-badges'
import { db } from '@/mocks/data/store'
import { renderWithProviders } from '@/test/render'
import { IssuesBoard, resolveDrop } from './issues-board'

const STATUS_LABELS: Record<string, string> = {
  backlog: 'Backlog',
  todo: 'Todo',
  in_progress: 'In Progress',
  in_review: 'In Review',
  blocked: 'Blocked',
  done: 'Done',
}

describe('IssuesBoard', () => {
  it('renders one column per status with a matching card count and titles', () => {
    const issues = db.issues.filter((i) => i.workspaceId === db.workspace.id)
    renderWithProviders(<IssuesBoard issues={issues} slug={db.workspace.slug} />)

    for (const status of STATUS_ORDER) {
      const group = issues.filter((i) => i.status === status)
      expect(screen.getByText(STATUS_LABELS[status])).toBeInTheDocument()
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
  const issues = [
    { id: 'issue-1', status: 'backlog' } as never,
    { id: 'issue-2', status: 'done' } as never,
  ]

  it('returns a status update when dropped on a different column', () => {
    expect(resolveDrop(issues, 'issue-1', 'in_progress')).toEqual({ id: 'issue-1', status: 'in_progress' })
  })

  it('returns null when dropped back on its own column', () => {
    expect(resolveDrop(issues, 'issue-1', 'backlog')).toBeNull()
  })

  it('returns null when dropped outside any droppable', () => {
    expect(resolveDrop(issues, 'issue-1', undefined)).toBeNull()
  })

  it('returns null for an issue id that is not in the list', () => {
    expect(resolveDrop(issues, 'missing-issue', 'todo')).toBeNull()
  })
})

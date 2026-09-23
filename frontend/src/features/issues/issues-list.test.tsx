import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'
import { makeIssue, makeStatus } from '@/test/issue-fixtures'
import { renderWithProviders } from '@/test/render'
import { server } from '@/test/msw-server'
import type { Issue, IssueStatusColumn, TenantMember } from './types'
import { IssuesList } from './issues-list'

function serveIssues(
  issues: Issue[] = [],
  statuses: IssueStatusColumn[] = [],
  members: TenantMember[] = [],
) {
  server.use(
    http.get('/api/v1/tenants/t1/issues', () =>
      HttpResponse.json({ items: issues, nextCursor: '' }),
    ),
    http.get('/api/v1/tenants/t1/issue-statuses', () =>
      HttpResponse.json({ items: statuses, nextCursor: '' }),
    ),
    http.get('/api/v1/tenants/t1/members', () =>
      HttpResponse.json({ items: members, nextCursor: '' }),
    ),
  )
}

describe('IssuesList', () => {
  it('shows the empty state when there are no issues', async () => {
    serveIssues([], [makeStatus('backlog')], [])
    renderWithProviders(<IssuesList slug="t1" title="Issues" />)
    expect(await screen.findByText('暂无任务。')).toBeInTheDocument()
  })

  it('renders the board view by default', async () => {
    serveIssues(
      [makeIssue('i1', 'Fix the login', { status: 'backlog' })],
      [makeStatus('backlog')],
      [],
    )
    renderWithProviders(<IssuesList slug="t1" title="Issues" />)
    expect(await screen.findByText('Fix the login')).toBeInTheDocument()
  })

  it('switches to the list view', async () => {
    serveIssues(
      [makeIssue('i1', 'Fix the login', { status: 'backlog' })],
      [makeStatus('backlog')],
      [],
    )
    const user = userEvent.setup()
    renderWithProviders(<IssuesList slug="t1" title="Issues" />)
    await screen.findByText('Fix the login')

    await user.click(screen.getByRole('button', { name: '列表视图' }))

    expect(await screen.findByText('Fix the login')).toBeInTheDocument()
  })

  it('filters to the assigned user when assigneeUserId is provided', async () => {
    serveIssues(
      [
        makeIssue('mine', 'My task', { status: 'backlog', assigneeUserId: 'u1' }),
        makeIssue('theirs', 'Their task', { status: 'backlog', assigneeUserId: 'u2' }),
      ],
      [makeStatus('backlog')],
      [],
    )
    renderWithProviders(<IssuesList slug="t1" title="Issues" assigneeUserId="u1" />)

    expect(await screen.findByText('My task')).toBeInTheDocument()
    expect(screen.queryByText('Their task')).not.toBeInTheDocument()
  })
})

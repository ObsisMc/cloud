import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { installTenantIssueHandlers } from '@/test/cloud-handlers'
import { makeIssue, makeStatus } from '@/test/issue-fixtures'
import { renderWithProviders } from '@/test/render'
import { IssuesList } from './issues-list'

describe('IssuesList', () => {
  it('shows the empty state when there are no issues', async () => {
    installTenantIssueHandlers([], [makeStatus('backlog')], [])
    renderWithProviders(<IssuesList slug="t1" title="Issues" />)
    expect(await screen.findByText('暂无任务。')).toBeInTheDocument()
  })

  it('renders the board view by default', async () => {
    installTenantIssueHandlers(
      [makeIssue('i1', 'Fix the login', { status: 'backlog' })],
      [makeStatus('backlog')],
      [],
    )
    renderWithProviders(<IssuesList slug="t1" title="Issues" />)
    expect(await screen.findByText('Fix the login')).toBeInTheDocument()
  })

  it('switches to the list view', async () => {
    installTenantIssueHandlers(
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
    installTenantIssueHandlers(
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

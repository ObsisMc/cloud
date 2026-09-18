import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '@/test/render'
import { db } from '@/mocks/data/store'
import { IssuesPage } from './issues-page'

describe('IssuesPage', () => {
  it('groups seeded issues by status', async () => {
    renderWithProviders(<IssuesPage slug={db.workspace.slug} />)

    const backlogIssue = db.issues.find((i) => i.status === 'backlog')
    if (!backlogIssue) throw new Error('issue seed data must contain a backlog issue')
    expect(await screen.findByText(backlogIssue.title)).toBeInTheDocument()
    expect(screen.getByText('待规划')).toBeInTheDocument()
  })

  it('creates a new issue through the dialog and lists it in Backlog', async () => {
    const user = userEvent.setup()
    renderWithProviders(<IssuesPage slug={db.workspace.slug} />)
    await screen.findByText('待规划')

    await user.click(screen.getByRole('button', { name: '新建任务' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByPlaceholderText('任务标题'), 'A brand new mock issue')
    await user.click(within(dialog).getByRole('button', { name: '创建任务' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(await screen.findByText('A brand new mock issue')).toBeInTheDocument()
  })
})

import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '@/test/render'
import { db } from '@/mocks/data/store'
import { IssuesPage } from './issues-page'

describe('IssuesPage', () => {
  it('groups seeded issues by status', async () => {
    renderWithProviders(<IssuesPage slug={db.workspace.slug} />)

    const backlogIssue = db.issues.find((i) => i.status === 'backlog')!
    expect(await screen.findByText(backlogIssue.title)).toBeInTheDocument()
    expect(screen.getByText('Backlog')).toBeInTheDocument()
  })

  it('creates a new issue through the dialog and lists it in Backlog', async () => {
    const user = userEvent.setup()
    renderWithProviders(<IssuesPage slug={db.workspace.slug} />)
    await screen.findByText('Backlog')

    await user.click(screen.getByRole('button', { name: 'New issue' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByPlaceholderText(/issue title/i), 'A brand new mock issue')
    await user.click(within(dialog).getByRole('button', { name: /create issue/i }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(await screen.findByText('A brand new mock issue')).toBeInTheDocument()
  })
})

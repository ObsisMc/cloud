import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { SidebarProvider } from '@/components/ui/sidebar'
import { db } from '@/mocks/data/store'
import { IssueDetailPage } from './issue-detail-page'

function renderIssueDetail(issueId: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const router = createMemoryRouter(
    [{ path: '/:workspaceSlug/issues/:issueId', element: <IssueDetailPage slug={db.workspace.slug} /> }],
    { initialEntries: [`/${db.workspace.slug}/issues/${issueId}`] },
  )
  return render(
    <QueryClientProvider client={queryClient}>
      <SidebarProvider>
        <RouterProvider router={router} />
      </SidebarProvider>
    </QueryClientProvider>,
  )
}

describe('IssueDetailPage', () => {
  it('renders the issue title and lets the status be changed', async () => {
    const issue = db.issues.find((i) => i.status !== 'done')!
    const user = userEvent.setup()
    renderIssueDetail(issue.id)

    expect(await screen.findByText(issue.title)).toBeInTheDocument()

    const [statusTrigger] = screen.getAllByRole('combobox')
    await user.click(statusTrigger)
    await user.click(await screen.findByRole('option', { name: /^done$/i }))

    await waitFor(() => {
      expect(within(statusTrigger).getByText(/^done$/i)).toBeInTheDocument()
    })
  })
})

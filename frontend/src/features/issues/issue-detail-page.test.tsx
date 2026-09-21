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
    [
      { path: '/w/:workspaceSlug/issues', element: <div>Issues list screen</div> },
      {
        path: '/w/:workspaceSlug/issues/:issueId',
        element: <IssueDetailPage slug={db.workspace.slug} />,
      },
    ],
    { initialEntries: [`/w/${db.workspace.slug}/issues/${issueId}`] },
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
    const issue = db.issues.find((i) => i.status !== 'done')
    if (!issue) throw new Error('issue seed data must contain an unfinished issue')
    const user = userEvent.setup()
    renderIssueDetail(issue.id)

    expect(await screen.findByText(issue.title)).toBeInTheDocument()

    const [statusTrigger] = screen.getAllByRole('combobox')
    if (!statusTrigger) throw new Error('issue detail must render a status selector')
    await user.click(statusTrigger)
    await user.click(await screen.findByRole('option', { name: '已完成' }))

    await waitFor(() => {
      expect(within(statusTrigger).getByText('已完成')).toBeInTheDocument()
    })
  })

  it('navigates back to the issues list via the breadcrumb', async () => {
    const issue = db.issues[0]
    if (!issue) throw new Error('issue seed data must not be empty')
    const user = userEvent.setup()
    renderIssueDetail(issue.id)
    await screen.findByText(issue.title)

    await user.click(screen.getByRole('link', { name: '任务' }))

    expect(await screen.findByText('Issues list screen')).toBeInTheDocument()
  })
})

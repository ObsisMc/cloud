import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'
import { makeIssue, makeStatus } from '@/test/issue-fixtures'
import { server } from '@/test/msw-server'
import { renderWithProviders } from '@/test/render'
import type { Issue } from './types'
import { IssuesPage } from './issues-page'

const statuses = [makeStatus('backlog'), makeStatus('todo'), makeStatus('done')]

/** Serves the three tenant queries the board mounts (issues, statuses, members). */
function serveBoard(issues: Issue[]) {
  server.use(
    http.get('/api/v1/tenants/t1/issues', () => HttpResponse.json({ items: issues, nextCursor: '' })),
    http.get('/api/v1/tenants/t1/issue-statuses', () =>
      HttpResponse.json({ items: statuses, nextCursor: '' }),
    ),
    http.get('/api/v1/tenants/t1/members', () => HttpResponse.json({ items: [], nextCursor: '' })),
  )
}

describe('IssuesPage', () => {
  it('renders the board with catalog columns and issue titles', async () => {
    serveBoard([makeIssue('b1', 'Fix the login', { status: 'backlog' })])

    renderWithProviders(<IssuesPage slug="t1" />)

    expect(await screen.findByText('Fix the login')).toBeInTheDocument()
    // Column header + card status label both show the localized label.
    expect(screen.getAllByText('待规划')).toHaveLength(2)
  })

  it('creates a new issue through the dialog and lists it', async () => {
    const issues: Issue[] = [makeIssue('b1', 'Seeded issue', { status: 'backlog' })]
    serveBoard(issues)
    server.use(
      http.post('/api/v1/tenants/t1/issues', async ({ request }) => {
        const body = (await request.json()) as { title: string }
        const created = makeIssue('new', body.title, { status: 'backlog' })
        issues.unshift(created)
        return HttpResponse.json({ resource: created })
      }),
    )

    const user = userEvent.setup()
    renderWithProviders(<IssuesPage slug="t1" />)
    await screen.findByText('Seeded issue')

    await user.click(screen.getByRole('button', { name: '新建任务' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByPlaceholderText('任务标题'), 'A brand new issue')
    await user.click(within(dialog).getByRole('button', { name: '创建任务' }))

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(await screen.findByText('A brand new issue')).toBeInTheDocument()
  })
})

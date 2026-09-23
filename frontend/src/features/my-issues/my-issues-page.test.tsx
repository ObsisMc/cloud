import { screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'
import { installSignedInSession, TEST_USER_ID } from '@/test/cloud-handlers'
import { makeIssue, makeStatus } from '@/test/issue-fixtures'
import { renderWithProviders } from '@/test/render'
import { server } from '@/test/msw-server'
import { MyIssuesPage } from './my-issues-page'

function serveIssues() {
  server.use(
    http.get('/api/v1/tenants/t1/issues', () =>
      HttpResponse.json({
        items: [makeIssue('i1', 'My task', { status: 'backlog', assigneeUserId: TEST_USER_ID })],
        nextCursor: '',
      }),
    ),
    http.get('/api/v1/tenants/t1/issue-statuses', () =>
      HttpResponse.json({ items: [makeStatus('backlog')], nextCursor: '' }),
    ),
    http.get('/api/v1/tenants/t1/members', () => HttpResponse.json({ items: [], nextCursor: '' })),
  )
}

describe('MyIssuesPage', () => {
  it('renders the unfiltered task list when signed out', async () => {
    serveIssues()
    renderWithProviders(<MyIssuesPage slug="t1" />)
    expect(await screen.findByText('我的任务')).toBeInTheDocument()
    expect(await screen.findByText('My task')).toBeInTheDocument()
  })

  it('scopes the list to the signed-in user', async () => {
    installSignedInSession()
    serveIssues()
    renderWithProviders(<MyIssuesPage slug="t1" />)
    expect(await screen.findByText('My task')).toBeInTheDocument()
  })
})

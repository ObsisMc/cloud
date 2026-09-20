import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { setCloudCredentials } from '@/lib/cloud-session'
import { db } from '@/mocks/data/store'
import { useAuthStore } from '@/state/auth-store'
import { TEST_CLOUD_CREDENTIALS, TEST_TENANT_ID } from '@/test/cloud-handlers'
import { server } from '@/test/msw-server'
import { DashboardLayout } from './dashboard-layout'

function renderRouter(initialPath: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const router = createMemoryRouter(
    [
      { path: '/login', element: <div>Login screen</div> },
      {
        path: '/:workspaceSlug',
        element: <DashboardLayout />,
        children: [{ path: 'issues', element: <div>Issues screen</div> }],
      },
    ],
    { initialEntries: [initialPath] },
  )
  return render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
}

describe('DashboardLayout', () => {
  beforeEach(() => {
    useAuthStore.getState().clear()
  })

  afterEach(() => {
    sessionStorage.clear()
  })

  it('redirects to /login when there is no session', async () => {
    renderRouter(`/${db.workspace.slug}/issues`)
    expect(await screen.findByText('Login screen')).toBeInTheDocument()
  })

  it('redirects an unknown workspace slug back to the real workspace', async () => {
    useAuthStore.getState().setSession('token', db.users[0])
    renderRouter('/some-other-workspace/issues')
    expect(await screen.findByText('Issues screen')).toBeInTheDocument()
  })

  it('renders the matched child route once authenticated', async () => {
    useAuthStore.getState().setSession('token', db.users[0])
    renderRouter(`/${db.workspace.slug}/issues`)
    expect(await screen.findByText('Issues screen')).toBeInTheDocument()
  })
})

describe('DashboardLayout cloud mode', () => {
  afterEach(() => {
    sessionStorage.clear()
  })

  it('shows an empty state instead of demo data for a session with no joined space', async () => {
    setCloudCredentials(TEST_CLOUD_CREDENTIALS)
    server.use(
      http.get('/api/v1/me/tenants', () =>
        HttpResponse.json({
          items: [{ id: TEST_TENANT_ID, name: '研发组织', status: 'active', role: 'admin' }],
          nextCursor: '',
        }),
      ),
      http.get(`/api/v1/tenants/${TEST_TENANT_ID}/spaces`, () =>
        HttpResponse.json({ items: [], nextCursor: '' }),
      ),
    )
    renderRouter('/default/issues')
    expect(await screen.findByText(/尚未加入任何工作区/)).toBeInTheDocument()
    expect(screen.queryByText('Issues screen')).not.toBeInTheDocument()
  })
})

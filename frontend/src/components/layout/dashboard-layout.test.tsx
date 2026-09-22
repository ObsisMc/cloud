import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { useAuthStore } from '@/state/auth-store'
import { server } from '@/test/msw-server'
import { DashboardLayout } from './dashboard-layout'

const session = {
  user: { id: 'u1', displayName: 'Alice', subject: 'subj' },
  tenantId: 't1',
  tenantName: 'Acme',
}

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

  it('redirects to /login when there is no session', async () => {
    renderRouter('/t1/issues')
    expect(await screen.findByText('Login screen')).toBeInTheDocument()
  })

  it('renders the matched child route once authenticated', async () => {
    useAuthStore.getState().setSession(session)
    renderRouter('/t1/issues')
    expect(await screen.findByText('Issues screen')).toBeInTheDocument()
  })

  it('shows the onboarding create-workspace CTA for a cloud session with zero workspaces', async () => {
    useAuthStore.getState().setSession(session)
    // A legal 0-workspace state: an empty space list renders the onboarding
    // empty state, not a crash, a redirect or a fake workspace.
    server.use(
      http.get(`/api/v1/tenants/${session.tenantId}/spaces`, () =>
        HttpResponse.json({ items: [], nextCursor: '' }),
      ),
    )
    renderRouter('/t1/issues')

    expect(await screen.findByText('你还没有加入任何工作区')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '退出登录' })).toBeInTheDocument()

    // The primary CTA opens the create-space dialog (creator becomes owner).
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: '创建工作区' }))
    expect(await screen.findByText('新建空间')).toBeInTheDocument()
    expect(screen.getByText('创建后你自动成为所有者（owner）。')).toBeInTheDocument()
  })
})

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { DashboardLayout } from '@/components/layout/dashboard-layout'
import { useAuthStore } from '@/state/auth-store'

const session = {
  user: { id: 'u1', displayName: 'Alice', subject: 'subj' },
  tenantId: 't1',
  tenantName: 'Acme',
}

function renderDashboard(initialPath: string) {
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

describe('AppSidebar', () => {
  it('shows the tenant and the signed-in user in the header dropdown', async () => {
    useAuthStore.getState().setSession(session)
    const user = userEvent.setup()
    renderDashboard('/t1/issues')
    await screen.findByText('Issues screen')

    expect(screen.getByText('Acme')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /Acme/ }))

    expect(await screen.findByText('Alice')).toBeInTheDocument()
    expect(screen.getByText('subj')).toBeInTheDocument()
    expect(screen.getByText('退出登录')).toBeInTheDocument()
  })
})

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { db } from '@/mocks/data/store'
import { useAuthStore } from '@/state/auth-store'
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

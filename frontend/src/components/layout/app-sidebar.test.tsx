import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { DashboardLayout } from '@/components/layout/dashboard-layout'
import { db } from '@/mocks/data/store'
import { useAuthStore } from '@/state/auth-store'

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

describe('AppSidebar workspace switcher', () => {
  it('opens without crashing and lists every workspace', async () => {
    useAuthStore.getState().setSession('token', db.users[0])
    const user = userEvent.setup()
    renderDashboard(`/${db.workspace.slug}/issues`)
    await screen.findByText('Issues screen')

    await user.click(screen.getByRole('button', { name: new RegExp(db.workspace.name) }))

    const menu = await screen.findByRole('menu')
    await Promise.all(
      db.workspaces.map(async (ws) => {
        expect(
          await screen.findByRole('menuitem', { name: new RegExp(ws.name) }),
        ).toBeInTheDocument()
      }),
    )
    expect(menu).toBeInTheDocument()
  })

  it('switches to a different workspace and lands on its issues page', async () => {
    useAuthStore.getState().setSession('token', db.users[0])
    const user = userEvent.setup()
    const target = db.workspaces.find((w) => w.id !== db.workspace.id)
    if (!target) throw new Error('workspace seed data must contain a second workspace')

    renderDashboard(`/${db.workspace.slug}/issues`)
    await screen.findByText('Issues screen')

    await user.click(screen.getByRole('button', { name: new RegExp(db.workspace.name) }))
    await user.click(await screen.findByRole('menuitem', { name: new RegExp(target.name) }))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: new RegExp(target.name) })).toBeInTheDocument()
    })
  })
})

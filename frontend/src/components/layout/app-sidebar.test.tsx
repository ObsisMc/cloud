import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { afterEach, describe, expect, it } from 'vitest'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { DashboardLayout } from '@/components/layout/dashboard-layout'
import type { SpaceListItem } from '@/api/generated.schemas'
import { db } from '@/mocks/data/store'
import { setCloudSession, TEST_SPACE_ID, TEST_TENANT_ID } from '@/test/cloud-session'
import { server } from '@/test/msw-server'
import { useAuthStore } from '@/state/auth-store'
import { useDemoAuthStore } from '@/state/demo-auth-store'

const CLOUD_SLUG = 'team'
const OTHER_SLUG = 'platform'

function spaceItem(id: string, name: string, slug: string, role: string): SpaceListItem {
  return {
    id,
    tenantId: TEST_TENANT_ID,
    name,
    slug,
    description: '',
    role,
    createdBy: 'u1',
    version: 1,
    createdAt: '2026-09-21T10:00:00+08:00',
    updatedAt: '2026-09-21T10:00:00+08:00',
    archivedAt: null,
  }
}

function renderDashboard(initialPath: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const router = createMemoryRouter(
    [
      { path: '/login', element: <div>Login screen</div> },
      {
        path: '/:workspaceSlug',
        element: <DashboardLayout />,
        children: [
          { path: 'issues', element: <div>Issues screen</div> },
          { path: 'projects', element: <div>Projects screen</div> },
        ],
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
  afterEach(() => {
    useAuthStore.getState().clear()
    useDemoAuthStore.getState().clear()
  })

  it('lists every demo workspace with the active one selected', async () => {
    useDemoAuthStore.getState().setSession('token', {
      id: 'u1',
      type: 'user',
      name: 'Demo',
      email: 'demo@example.com',
      avatarColor: '#3b82f6',
      initials: 'DE',
      role: 'owner',
    })
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

  it('switches to another demo workspace and lands on its issues page', async () => {
    useDemoAuthStore.getState().setSession('token', {
      id: 'u1',
      type: 'user',
      name: 'Demo',
      email: 'demo@example.com',
      avatarColor: '#3b82f6',
      initials: 'DE',
      role: 'owner',
    })
    const user = userEvent.setup()
    const target = db.workspaces.find((w) => w.slug !== db.workspace.slug)
    if (!target) throw new Error('workspace seed data must contain a second workspace')

    renderDashboard(`/${db.workspace.slug}/issues`)
    await screen.findByText('Issues screen')

    await user.click(screen.getByRole('button', { name: new RegExp(db.workspace.name) }))
    await user.click(await screen.findByRole('menuitem', { name: new RegExp(target.name) }))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: new RegExp(target.name) })).toBeInTheDocument()
    })
  })

  it('lists real joined spaces in cloud mode and switches to projects', async () => {
    setCloudSession()
    server.use(
      http.get(`/api/v1/tenants/${TEST_TENANT_ID}/spaces`, () =>
        HttpResponse.json({
          items: [
            spaceItem(TEST_SPACE_ID, 'Team Space', CLOUD_SLUG, 'admin'),
            spaceItem('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'Platform', OTHER_SLUG, 'member'),
          ],
          nextCursor: '',
        }),
      ),
    )
    const user = userEvent.setup()
    renderDashboard(`/${CLOUD_SLUG}/projects`)
    await screen.findByText('Projects screen')

    await user.click(screen.getByRole('button', { name: new RegExp('Team Space') }))

    expect(
      await screen.findByRole('menuitem', { name: new RegExp('Platform') }),
    ).toBeInTheDocument()
    expect(screen.getByText('新建工作区')).toBeInTheDocument()

    await user.click(screen.getByRole('menuitem', { name: new RegExp('Platform') }))

    await waitFor(() => {
      expect(screen.getByText('Projects screen')).toBeInTheDocument()
      expect(screen.getByRole('button', { name: new RegExp('Platform') })).toBeInTheDocument()
    })
  })
})

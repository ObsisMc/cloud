import { screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'
import { RequireSession } from '@/features/auth/require-session'
import {
  installCloudSpaceHandlers,
  installSignedInSession,
  TEST_TENANT_ID,
} from '@/test/cloud-handlers'
import { renderRoutes } from '@/test/render'
import { server } from '@/test/msw-server'
import { DashboardLayout } from './dashboard-layout'

function renderRouter(initialPath: string) {
  return renderRoutes(
    [
      { path: '/login', element: <div>Login screen</div> },
      { path: '/onboarding', element: <div>Onboarding screen</div> },
      {
        path: '/:workspaceSlug',
        element: (
          <RequireSession>
            <DashboardLayout />
          </RequireSession>
        ),
        children: [{ path: 'issues', element: <div>Issues screen</div> }],
      },
    ],
    initialPath,
  )
}

describe('DashboardLayout', () => {
  it('redirects to /login with returnTo when there is no session', async () => {
    renderRouter('/cloud-dev/issues')
    expect(await screen.findByText('Login screen')).toBeInTheDocument()
  })

  it('renders the matched child route for a joined space', async () => {
    installCloudSpaceHandlers('member')
    renderRouter('/cloud-dev/issues')
    expect(await screen.findByText('Issues screen')).toBeInTheDocument()
  })

  it('redirects an unknown slug to the first joined space', async () => {
    installCloudSpaceHandlers('member')
    renderRouter('/some-other-workspace/issues')
    expect(await screen.findByText('Issues screen')).toBeInTheDocument()
  })

  it('sends a member with no tenant to onboarding instead of showing demo data', async () => {
    installSignedInSession()
    server.use(
      http.get('/api/v1/me/tenants', () => HttpResponse.json({ items: [], nextCursor: '' })),
    )
    renderRouter('/default/issues')
    expect(await screen.findByText('Onboarding screen')).toBeInTheDocument()
    expect(screen.queryByText('Issues screen')).not.toBeInTheDocument()
  })

  it('sends a member whose tenant has no live space to onboarding', async () => {
    installSignedInSession()
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
    expect(await screen.findByText('Onboarding screen')).toBeInTheDocument()
  })
})

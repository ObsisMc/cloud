import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'
import {
  installCloudSpaceHandlers,
  installSignedInSession,
  TEST_SPACE_ID,
  TEST_TENANT_ID,
  TEST_USER_ID,
} from '@/test/cloud-handlers'
import { renderRoutes } from '@/test/render'
import { server } from '@/test/msw-server'
import { OnboardingPage } from './onboarding-page'

function renderOnboarding() {
  return renderRoutes(
    [
      { path: '/onboarding', element: <OnboardingPage /> },
      { path: '/w/:workspaceSlug/issues', element: <div>Issues screen</div> },
    ],
    '/onboarding',
  )
}

function noTenant() {
  installSignedInSession()
  server.use(http.get('/api/v1/me/tenants', () => HttpResponse.json({ items: [], nextCursor: '' })))
}

function createdSpace(name: string, slug: string) {
  return {
    id: TEST_SPACE_ID,
    tenantId: TEST_TENANT_ID,
    name,
    slug,
    description: '',
    createdBy: TEST_USER_ID,
    version: 1,
    createdAt: '2026-09-20T10:00:00+08:00',
    updatedAt: '2026-09-20T10:00:00+08:00',
    archivedAt: null,
  }
}

describe('OnboardingPage', () => {
  it('sends a member who already has a workspace straight to it', async () => {
    installCloudSpaceHandlers('member')
    renderOnboarding()
    expect(await screen.findByText('Issues screen')).toBeInTheDocument()
  })

  it('derives the slug from the name until the slug is edited, and previews the URL', async () => {
    noTenant()
    const user = userEvent.setup()
    renderOnboarding()

    await user.type(await screen.findByLabelText('工作区名称'), 'Acme Inc')
    expect(screen.getByLabelText(/工作区地址/)).toHaveValue('acme-inc')
    // The reserved prefix is fixed in front of the input and repeated in the full address.
    expect(screen.getByText('localhost:3000/w/')).toBeInTheDocument()
    expect(screen.getByText('完整地址：localhost:3000/w/acme-inc')).toBeInTheDocument()

    await user.clear(screen.getByLabelText(/工作区地址/))
    await user.type(screen.getByLabelText(/工作区地址/), 'Team')
    await user.type(screen.getByLabelText('工作区名称'), ' Ltd')
    expect(screen.getByLabelText(/工作区地址/)).toHaveValue('team')
  })

  it('provisions a tenant with the first workspace for a member without one', async () => {
    noTenant()
    let body: unknown = null
    server.use(
      http.post('/api/v1/tenants', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json(
          {
            tenant: { id: TEST_TENANT_ID, name: 'Acme', status: 'active', role: 'admin' },
            space: createdSpace('Acme', 'acme'),
          },
          { status: 201 },
        )
      }),
    )
    const user = userEvent.setup()
    renderOnboarding()

    await user.type(await screen.findByLabelText('工作区名称'), 'Acme')
    await user.click(screen.getByRole('button', { name: '创建工作区' }))

    expect(await screen.findByText('Issues screen')).toBeInTheDocument()
    expect(body).toEqual({ name: 'Acme', slug: 'acme' })
  })

  it('creates a space in the existing tenant when the member has one but no workspace', async () => {
    installSignedInSession()
    let posted = false
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
      http.post(`/api/v1/tenants/${TEST_TENANT_ID}/spaces`, () => {
        posted = true
        return HttpResponse.json(createdSpace('Ops', 'ops'))
      }),
      http.post('/api/v1/tenants', () => HttpResponse.error()),
    )
    const user = userEvent.setup()
    renderOnboarding()

    await user.type(await screen.findByLabelText('工作区名称'), 'Ops')
    await user.click(screen.getByRole('button', { name: '创建工作区' }))

    expect(await screen.findByText('Issues screen')).toBeInTheDocument()
    expect(posted).toBe(true)
  })

  it('shows the backend fault code and keeps the form when creation fails', async () => {
    noTenant()
    server.use(
      http.post('/api/v1/tenants', () =>
        HttpResponse.json({ code: 'invalid_slug', params: {}, requestId: 'r' }, { status: 400 }),
      ),
    )
    const user = userEvent.setup()
    renderOnboarding()

    await user.type(await screen.findByLabelText('工作区名称'), 'Acme')
    await user.click(screen.getByRole('button', { name: '创建工作区' }))

    expect(await screen.findByText(/创建失败：invalid_slug/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '创建工作区' })).toBeEnabled()
  })

  it('disables submission for an invalid slug', async () => {
    noTenant()
    const user = userEvent.setup()
    renderOnboarding()

    await user.type(await screen.findByLabelText('工作区名称'), 'Acme')
    await user.clear(screen.getByLabelText(/工作区地址/))
    await user.type(screen.getByLabelText(/工作区地址/), '-bad')

    expect(screen.getByRole('button', { name: '创建工作区' })).toBeDisabled()
    expect(screen.getByText(/小写字母、数字与连字符/)).toBeInTheDocument()
  })

  it('lets a member without a workspace sign out', async () => {
    noTenant()
    let loggedOut = false
    server.use(
      http.post('/auth/logout', () => {
        loggedOut = true
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const user = userEvent.setup()
    renderOnboarding()

    await user.click(await screen.findByRole('button', { name: '退出登录' }))

    await waitFor(() => expect(loggedOut).toBe(true))
  })
})

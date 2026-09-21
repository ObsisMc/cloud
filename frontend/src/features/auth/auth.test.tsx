import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import type { ReactNode } from 'react'
import { useLocation } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import { AXIOS_INSTANCE } from '@/lib/api-client'
import { installSignedInSession, TEST_USER } from '@/test/cloud-handlers'
import { installFakeNavigation } from '@/test/navigation'
import { renderRoutes, renderWithProviders } from '@/test/render'
import { server } from '@/test/msw-server'
import { fetchLoginProviders, fetchSessionUser, logoutSession, startLogin } from './api'
import { LoginPage } from './login-page'
import { RequireSession } from './require-session'
import { SessionProvider, useSession } from './session'

function wrapper({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return (
    <QueryClientProvider client={queryClient}>
      <SessionProvider>{children}</SessionProvider>
    </QueryClientProvider>
  )
}

describe('gateway auth api', () => {
  it('starts a login through the gateway and sends the browser to the provider', async () => {
    let body: unknown = null
    server.use(
      http.post('/auth/login', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json({ authorizationUrl: 'https://github.com/login/oauth/authorize?x' })
      }),
    )
    const navigation = installFakeNavigation()

    await startLogin('github', '/acme/issues')

    expect(body).toEqual({ provider: 'github', returnTo: '/acme/issues' })
    expect(navigation.destinations).toEqual(['https://github.com/login/oauth/authorize?x'])
  })

  it('does not leave the page when the gateway refuses the login start', async () => {
    server.use(
      http.post('/auth/login', () =>
        HttpResponse.json(
          { code: 'origin_forbidden', params: {}, requestId: 'r' },
          { status: 403 },
        ),
      ),
    )
    const navigation = installFakeNavigation()

    await expect(startLogin('github', '/')).rejects.toBeDefined()
    expect(navigation.destinations).toEqual([])
  })

  it('lists the providers the gateway offers and drops ones this build cannot present', async () => {
    server.use(
      http.get('/auth/providers', () =>
        HttpResponse.json({ providers: ['saml-future', 'dev', 'github'] }),
      ),
    )
    await expect(fetchLoginProviders()).resolves.toEqual(['github', 'dev'])
  })

  it('treats a 401 from the session probe as signed out and any other failure as an error', async () => {
    await expect(fetchSessionUser()).resolves.toBeNull()

    installSignedInSession()
    await expect(fetchSessionUser()).resolves.toEqual(TEST_USER)

    server.use(
      http.get('/api/v1/me', () =>
        HttpResponse.json({ code: 'internal_error', params: {}, requestId: 'r' }, { status: 500 }),
      ),
    )
    await expect(fetchSessionUser()).rejects.toBeDefined()
  })

  it('logs out through the gateway', async () => {
    let called = 0
    server.use(
      http.post('/auth/logout', () => {
        called += 1
        return new HttpResponse(null, { status: 204 })
      }),
    )
    await logoutSession()
    expect(called).toBe(1)
  })
})

describe('SessionProvider', () => {
  it('reports signed out when the probe answers 401', async () => {
    const { result } = renderHook(() => useSession(), { wrapper })
    expect(result.current.session).toEqual({ status: 'loading' })
    await waitFor(() => expect(result.current.session).toEqual({ status: 'signed-out' }))
  })

  it('reports the member when the probe succeeds', async () => {
    installSignedInSession()
    const { result } = renderHook(() => useSession(), { wrapper })
    await waitFor(() =>
      expect(result.current.session).toEqual({ status: 'signed-in', user: TEST_USER }),
    )
  })

  it('reports unavailable, not signed out, when the probe fails for another reason', async () => {
    server.use(
      http.get('/api/v1/me', () =>
        HttpResponse.json({ code: 'internal_error', params: {}, requestId: 'r' }, { status: 500 }),
      ),
    )
    const { result } = renderHook(() => useSession(), { wrapper })
    await waitFor(() => expect(result.current.session).toEqual({ status: 'unavailable' }))
  })

  it('ends the session as soon as any request answers 401', async () => {
    installSignedInSession()
    server.use(
      http.get('/api/v1/me/tenants', () =>
        HttpResponse.json({ code: 'unauthenticated', params: {}, requestId: 'r' }, { status: 401 }),
      ),
    )
    const { result } = renderHook(() => useSession(), { wrapper })
    await waitFor(() => expect(result.current.session.status).toBe('signed-in'))

    await act(async () => {
      await AXIOS_INSTANCE.get('/api/v1/me/tenants').catch(() => undefined)
    })

    await waitFor(() => expect(result.current.session).toEqual({ status: 'signed-out' }))
  })

  it('signs out through the gateway and forgets the member', async () => {
    installSignedInSession()
    server.use(http.post('/auth/logout', () => new HttpResponse(null, { status: 204 })))
    const { result } = renderHook(() => useSession(), { wrapper })
    await waitFor(() => expect(result.current.session.status).toBe('signed-in'))

    await act(() => result.current.signOut())

    await waitFor(() => expect(result.current.session).toEqual({ status: 'signed-out' }))
  })
})

describe('RequireSession', () => {
  const routes = [
    { path: '/login', element: <div>Login screen</div> },
    {
      path: '/private',
      element: (
        <RequireSession>
          <div>Private screen</div>
        </RequireSession>
      ),
    },
  ]

  it('redirects a signed-out tab to the login page carrying returnTo', async () => {
    renderRoutes(
      [{ path: '/login', element: <LocationEcho /> }, ...routes.slice(1)],
      '/private?tab=2',
    )
    expect(await screen.findByText('/login?returnTo=%2Fprivate%3Ftab%3D2')).toBeInTheDocument()
  })

  it('renders the protected screen for a member', async () => {
    installSignedInSession()
    renderRoutes(routes, '/private')
    expect(await screen.findByText('Private screen')).toBeInTheDocument()
  })

  it('reports an unreachable backend instead of redirecting', async () => {
    server.use(http.get('/api/v1/me', () => HttpResponse.error()))
    renderRoutes(routes, '/private')
    expect(await screen.findByText(/无法连接服务端/)).toBeInTheDocument()
    expect(screen.queryByText('Login screen')).not.toBeInTheDocument()
  })
})

function LocationEcho() {
  const location = useLocation()
  return <div>{`${location.pathname}${location.search}`}</div>
}

describe('LoginPage', () => {
  it('starts the GitHub login with the requested returnTo', async () => {
    let body: unknown = null
    server.use(
      http.post('/auth/login', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json({ authorizationUrl: 'https://github.com/login/oauth/authorize' })
      }),
    )
    const navigation = installFakeNavigation()
    const user = userEvent.setup()
    renderWithProviders(<LoginPage />, { route: '/login?returnTo=%2Facme%2Fprojects' })

    await user.click(await screen.findByRole('button', { name: '使用 GitHub 登录' }))

    await waitFor(() => expect(navigation.destinations).toHaveLength(1))
    expect(body).toEqual({ provider: 'github', returnTo: '/acme/projects' })
  })

  it('falls back to / for a returnTo that is not a same-origin path', async () => {
    let body: unknown = null
    server.use(
      http.post('/auth/login', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json({ authorizationUrl: 'https://github.com/login/oauth/authorize' })
      }),
    )
    installFakeNavigation()
    const user = userEvent.setup()
    renderWithProviders(<LoginPage />, { route: '/login?returnTo=//evil.example' })

    await user.click(await screen.findByRole('button', { name: '使用 GitHub 登录' }))

    await waitFor(() => expect(body).toEqual({ provider: 'github', returnTo: '/' }))
  })

  it('reports a gateway failure and re-enables the button', async () => {
    server.use(http.post('/auth/login', () => HttpResponse.error()))
    const user = userEvent.setup()
    renderWithProviders(<LoginPage />, { route: '/login' })

    await user.click(await screen.findByRole('button', { name: '使用 GitHub 登录' }))

    expect(await screen.findByText(/无法开始登录/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '使用 GitHub 登录' })).toBeEnabled()
  })

  it('offers the developer login only when the gateway registers it, and starts it as "dev"', async () => {
    let body: unknown = null
    server.use(
      http.get('/auth/providers', () => HttpResponse.json({ providers: ['dev', 'github'] })),
      http.post('/auth/login', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json({ authorizationUrl: 'http://localhost:5173/auth/dev/authorize?s' })
      }),
    )
    const navigation = installFakeNavigation()
    const user = userEvent.setup()
    renderWithProviders(<LoginPage />, { route: '/login?returnTo=%2Fw%2Facme%2Fissues' })

    await user.click(await screen.findByRole('button', { name: '开发者登录（仅本地）' }))

    await waitFor(() => expect(navigation.destinations).toHaveLength(1))
    expect(body).toEqual({ provider: 'dev', returnTo: '/w/acme/issues' })
    expect(screen.getByRole('button', { name: '使用 GitHub 登录' })).toBeInTheDocument()
  })

  it('shows no developer login against a production-shaped gateway', async () => {
    renderWithProviders(<LoginPage />, { route: '/login' })
    expect(await screen.findByRole('button', { name: '使用 GitHub 登录' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /开发者登录/ })).not.toBeInTheDocument()
  })

  it('reports an unreachable gateway when the provider list cannot be loaded', async () => {
    server.use(http.get('/auth/providers', () => HttpResponse.error()))
    renderWithProviders(<LoginPage />, { route: '/login' })
    expect(await screen.findByText(/认证网关不可用/)).toBeInTheDocument()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('sends an already signed-in tab to returnTo', async () => {
    installSignedInSession()
    renderRoutes(
      [
        { path: '/login', element: <LoginPage /> },
        { path: '/acme/issues', element: <div>Issues screen</div> },
      ],
      '/login?returnTo=%2Facme%2Fissues',
    )
    expect(await screen.findByText('Issues screen')).toBeInTheDocument()
  })
})

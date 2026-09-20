import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '@/test/render'
import { useAuthStore } from '@/state/auth-store'
import { LoginPage } from './login-page'

const CLOUD_CREDENTIALS = {
  serviceToken: 'svc-token',
  userToken: 'usr-token',
  expiresAt: '2026-09-20T12:00:00+08:00',
}

describe('LoginPage', () => {
  beforeEach(() => {
    useAuthStore.getState().clear()
  })

  afterEach(() => {
    sessionStorage.clear()
    vi.restoreAllMocks()
  })

  it('renders the sign-in form with both flows', () => {
    renderWithProviders(<LoginPage />, { route: '/login' })
    expect(screen.getByRole('heading', { name: '登录 Ora' })).toBeInTheDocument()
    // The cloud flow is the default tab; the demo flow is one click away.
    expect(screen.getByLabelText('账号标识（subject）')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '演示账号' })).toBeInTheDocument()
  })

  it('signs the demo user in and stores the session on submit', async () => {
    const user = userEvent.setup()
    renderWithProviders(<LoginPage />, { route: '/login' })

    await user.click(screen.getByRole('tab', { name: '演示账号' }))
    await user.click(screen.getByRole('button', { name: '继续' }))

    await waitFor(() => {
      expect(useAuthStore.getState().token).toBeTruthy()
      expect(useAuthStore.getState().user?.email).toBeTruthy()
    })
  })

  it('signs the cloud user in through devgateway and stores dual credentials', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({ ok: true, json: async () => CLOUD_CREDENTIALS }),
    )
    const user = userEvent.setup()
    renderWithProviders(<LoginPage />, { route: '/login' })

    await user.click(screen.getByRole('button', { name: '连接后端登录' }))

    await waitFor(() => {
      expect(sessionStorage.getItem('ora-cloud-session')).toBeTruthy()
    })
    expect(JSON.parse(sessionStorage.getItem('ora-cloud-session') ?? '{}')).toEqual(
      CLOUD_CREDENTIALS,
    )
  })

  it('surfaces a devgateway failure without storing a session', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 502 }))
    const user = userEvent.setup()
    renderWithProviders(<LoginPage />, { route: '/login' })

    await user.click(screen.getByRole('button', { name: '连接后端登录' }))

    expect(await screen.findByText(/登录失败/)).toBeInTheDocument()
    expect(sessionStorage.getItem('ora-cloud-session')).toBeNull()
  })
})

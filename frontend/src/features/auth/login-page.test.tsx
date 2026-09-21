import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import { installFakeHttp } from '@/test/http'
import { renderWithProviders } from '@/test/render'
import { useAuthStore } from '@/state/auth-store'
import { useDemoAuthStore } from '@/state/demo-auth-store'
import { LoginPage } from './login-page'

const SESSION = {
  user: { id: 'u1', displayName: 'Alice', subject: 'alice' },
  tenantId: 't1',
  tenantName: 'Acme',
}

describe('LoginPage', () => {
  beforeEach(() => {
    useAuthStore.getState().clear()
    useDemoAuthStore.getState().clear()
  })

  it('renders the sign-in form with both session tabs', () => {
    renderWithProviders(<LoginPage />, { route: '/login' })
    expect(screen.getByRole('heading', { name: '登录 Ora' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '真实账号' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '演示账号' })).toBeInTheDocument()
    expect(screen.getByLabelText('邮箱')).toBeInTheDocument()
  })

  it('signs the real account in through the edge server and stores the session', async () => {
    installFakeHttp(SESSION)
    const user = userEvent.setup()
    renderWithProviders(<LoginPage />, { route: '/login' })

    await user.type(screen.getByLabelText('邮箱'), 'alice@example.com')
    await user.click(screen.getByRole('button', { name: '连接后端登录' }))

    await waitFor(() => {
      expect(useAuthStore.getState().user?.displayName).toBe('Alice')
      expect(useAuthStore.getState().tenantId).toBe('t1')
    })
  })

  it('shows a failure hint when the edge server is unreachable', async () => {
    installFakeHttp({ message: 'down' }, 500)
    const user = userEvent.setup()
    renderWithProviders(<LoginPage />, { route: '/login' })

    await user.type(screen.getByLabelText('邮箱'), 'alice@example.com')
    await user.click(screen.getByRole('button', { name: '连接后端登录' }))

    expect(await screen.findByText('登录失败：请确认后端已启动后重试。')).toBeInTheDocument()
    expect(useAuthStore.getState().tenantId).toBeNull()
  })

  it('signs a demo account in against the mock store', async () => {
    const user = userEvent.setup()
    renderWithProviders(<LoginPage />, { route: '/login' })

    await user.click(screen.getByRole('tab', { name: '演示账号' }))
    await user.clear(screen.getByLabelText('邮箱'))
    await user.type(screen.getByLabelText('邮箱'), 'demo@example.com')
    await user.click(screen.getByRole('button', { name: '继续' }))

    await waitFor(() => {
      expect(useDemoAuthStore.getState().token).not.toBeNull()
      expect(useDemoAuthStore.getState().user?.email).toBe('demo@example.com')
    })
  })

  it('redirects an already-signed-in real account to the default workspace', () => {
    useAuthStore.getState().setSession(SESSION)
    renderWithProviders(<LoginPage />, { route: '/login' })
    expect(screen.queryByRole('heading', { name: '登录 Ora' })).not.toBeInTheDocument()
  })

  it('redirects an already-signed-in demo account to its issue board', () => {
    useDemoAuthStore.getState().setSession('token', {
      id: 'u1',
      type: 'user',
      name: 'Demo',
      email: 'demo@example.com',
      avatarColor: '#3b82f6',
      initials: 'DE',
      role: 'owner',
    })
    renderWithProviders(<LoginPage />, { route: '/login' })
    expect(screen.queryByRole('heading', { name: '登录 Ora' })).not.toBeInTheDocument()
  })
})

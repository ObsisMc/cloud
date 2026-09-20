import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import { installFakeHttp } from '@/test/http'
import { renderWithProviders } from '@/test/render'
import { useAuthStore } from '@/state/auth-store'
import { LoginPage } from './login-page'

describe('LoginPage', () => {
  beforeEach(() => {
    useAuthStore.getState().clear()
  })

  it('renders the sign-in form', () => {
    renderWithProviders(<LoginPage />, { route: '/login' })
    expect(screen.getByRole('heading', { name: '登录 Ora' })).toBeInTheDocument()
    expect(screen.getByLabelText('邮箱')).toBeInTheDocument()
  })

  it('signs in through the edge server and stores the session', async () => {
    installFakeHttp({
      user: { id: 'u1', displayName: 'Alice', subject: 'subj' },
      tenantId: 't1',
      tenantName: 'Acme',
    })
    const user = userEvent.setup()
    renderWithProviders(<LoginPage />, { route: '/login' })

    await user.type(screen.getByLabelText('邮箱'), 'alice@example.com')
    await user.click(screen.getByRole('button', { name: '继续' }))

    await waitFor(() => {
      expect(useAuthStore.getState().user?.displayName).toBe('Alice')
      expect(useAuthStore.getState().tenantId).toBe('t1')
    })
  })
})

import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
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

  it('signs the user in and stores the session on submit', async () => {
    const user = userEvent.setup()
    renderWithProviders(<LoginPage />, { route: '/login' })

    await user.click(screen.getByRole('button', { name: '继续' }))

    await waitFor(() => {
      expect(useAuthStore.getState().token).toBeTruthy()
      expect(useAuthStore.getState().user?.email).toBeTruthy()
    })
  })
})

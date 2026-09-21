import { useMutation } from '@tanstack/react-query'
import { AXIOS_INSTANCE } from '@/lib/api-client'
import { mockApi } from '@/lib/mock-api-client'
import { useAuthStore, type SessionUser } from '@/state/auth-store'
import { useDemoAuthStore } from '@/state/demo-auth-store'
import type { User } from '@/mocks/data/types'

interface LoginResponse {
  user: SessionUser
  tenantId: string
  tenantName: string
}

/**
 * Signs the email in through the edge server, which provisions the identity into the
 * bootstrap tenant and sets the `ora_subject` session cookie (HttpOnly). The user JWT
 * is minted server-side per request and never reaches JavaScript.
 */
export function useLogin() {
  const setSession = useAuthStore((s) => s.setSession)
  return useMutation({
    mutationFn: async (email: string) => {
      const { data } = await AXIOS_INSTANCE.post<LoginResponse>('/auth/login', { email })
      return data
    },
    onSuccess: ({ user, tenantId, tenantName }) => setSession({ user, tenantId, tenantName }),
  })
}

/** Clears the server session cookie and the local session state. */
export function useLogout() {
  const clear = useAuthStore((s) => s.clear)
  return useMutation({
    mutationFn: async () => {
      await AXIOS_INSTANCE.post('/auth/logout')
    },
    onSettled: () => clear(),
  })
}

/**
 * Demo-plane sign-in against the MSW-mocked store (`/mock-api/auth/login`); any
 * email works. The token lives only in the demo session and never touches the
 * real backend.
 */
export function useDemoLogin() {
  const setSession = useDemoAuthStore((s) => s.setSession)
  return useMutation({
    mutationFn: async (email: string) => {
      const { data } = await mockApi.post<{ token: string; user: User }>('/auth/login', { email })
      return data
    },
    onSuccess: ({ token, user }) => setSession(token, user),
  })
}

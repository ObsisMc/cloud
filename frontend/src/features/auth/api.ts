import { useMutation } from '@tanstack/react-query'
import { AXIOS_INSTANCE } from '@/lib/api-client'
import { useAuthStore, type SessionUser } from '@/state/auth-store'

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

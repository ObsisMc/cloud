import { useMutation } from '@tanstack/react-query'
import { mockApi } from '@/lib/mock-api-client'
import { useAuthStore } from '@/state/auth-store'
import type { User } from '@/mocks/data/types'

export function useLogin() {
  const setSession = useAuthStore((s) => s.setSession)
  return useMutation({
    mutationFn: async (email: string) => {
      const { data } = await mockApi.post<{ token: string; user: User }>('/auth/login', { email })
      return data
    },
    onSuccess: ({ token, user }) => setSession(token, user),
  })
}

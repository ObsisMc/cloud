import { create } from 'zustand'
import { persist } from 'zustand/middleware'

/** Signed-in user profile returned by the edge server's `/auth/login`. */
export interface SessionUser {
  id: string
  displayName: string
  subject: string
}

/** Session state. The raw JWT never reaches the browser — only the cookie + profile. */
interface AuthState {
  user: SessionUser | null
  tenantId: string | null
  tenantName: string | null
  setSession: (session: { user: SessionUser; tenantId: string; tenantName: string }) => void
  clear: () => void
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      user: null,
      tenantId: null,
      tenantName: null,
      setSession: ({ user, tenantId, tenantName }) => set({ user, tenantId, tenantName }),
      clear: () => set({ user: null, tenantId: null, tenantName: null }),
    }),
    { name: 'ora-auth' },
  ),
)

import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import type { User } from '@/mocks/data/types'

/**
 * Demo-plane session for the MSW-mocked store (`/mock-api/*`), kept separate
 * from the cloud session in `auth-store.ts` (which is backed by the ora-web
 * HttpOnly `ora_subject` cookie). The demo session is purely local mock state;
 * it never reaches the real backend.
 */
interface DemoAuthState {
  token: string | null
  user: User | null
  setSession: (token: string, user: User) => void
  clear: () => void
}

export const useDemoAuthStore = create<DemoAuthState>()(
  persist(
    (set) => ({
      token: null,
      user: null,
      setSession: (token, user) => set({ token, user }),
      clear: () => set({ token: null, user: null }),
    }),
    { name: 'ora-mock-auth' },
  ),
)

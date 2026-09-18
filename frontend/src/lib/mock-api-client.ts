import { create } from 'axios'
import { useAuthStore } from '@/state/auth-store'

/**
 * Client for the MSW-mocked domain (`/mock-api/*`), kept separate from the
 * orval-generated client in `src/api` which talks to the real Go backend.
 */
export const mockApi = create({ baseURL: '/mock-api' })

mockApi.interceptors.request.use((config) => {
  const token = useAuthStore.getState().token
  if (token) {
    config.headers.set('Authorization', `Bearer ${token}`)
  }
  return config
})

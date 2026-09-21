import { create } from 'axios'
import { db } from '@/mocks/data/store'
import { useAuthStore } from '@/state/auth-store'
import { useDemoAuthStore } from '@/state/demo-auth-store'

/**
 * Client for the MSW-mocked domain (`/mock-api/*`), kept separate from the
 * orval-generated client in `src/api` (and the issue hooks) which talk to the
 * real Go backend.
 *
 * Demo sessions attach their mock token; in a cloud session (ora-web cookie) the
 * mock store has no token, and mock pages remain usable by rewriting the real
 * space slug in the URL to the seeded workspace before the request reaches MSW.
 * Mock pages therefore show demo data regardless of which space the tab is in —
 * the planned behavior until they get real backend counterparts.
 */
export const mockApi = create({ baseURL: '/mock-api' })

mockApi.interceptors.request.use((config) => {
  const token = useDemoAuthStore.getState().token
  if (token) {
    config.headers.set('Authorization', `Bearer ${token}`)
  }
  const cloudMode = useAuthStore.getState().tenantId != null
  if (!token && cloudMode && config.url?.startsWith('/workspaces/')) {
    config.url = config.url.replace(/^\/workspaces\/[^/]+/, `/workspaces/${db.workspace.slug}`)
  }
  return config
})

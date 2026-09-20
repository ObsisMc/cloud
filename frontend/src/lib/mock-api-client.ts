import { create } from 'axios'
import { hasCloudSession } from '@/lib/cloud-session'
import { db } from '@/mocks/data/store'
import { useAuthStore } from '@/state/auth-store'

/**
 * Client for the MSW-mocked domain (`/mock-api/*`), kept separate from the
 * orval-generated client in `src/api` which talks to the real Go backend.
 *
 * Mock pages stay usable in cloud sessions: their routes carry the real space
 * slug, which the demo store does not know, so the slug is rewritten to the
 * seeded workspace before the request reaches MSW. Mock pages therefore show
 * demo data regardless of which space the tab is in — the planned behavior
 * until they get real backend counterparts.
 */
export const mockApi = create({ baseURL: '/mock-api' })

mockApi.interceptors.request.use((config) => {
  const token = useAuthStore.getState().token
  if (token) {
    config.headers.set('Authorization', `Bearer ${token}`)
  }
  if (!token && hasCloudSession() && config.url?.startsWith('/workspaces/')) {
    config.url = config.url.replace(/^\/workspaces\/[^/]+/, `/workspaces/${db.workspace.slug}`)
  }
  return config
})

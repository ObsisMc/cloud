import { create } from 'axios'

/**
 * Client for the MSW-mocked preview domain (`/mock-api/*`), kept separate from the
 * real backend client in `src/api` and the issue hooks (which use `AXIOS_INSTANCE`
 * against `/api/v1/*`). The mocked features carry no auth — the real session cookie
 * only ever reaches same-origin `/api` and `/auth` requests.
 */
export const mockApi = create({ baseURL: '/mock-api' })

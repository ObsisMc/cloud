import { http } from './api-client'

// The Gateway's browser login routes. They are not part of api/openapi.json, so they are written by
// hand; v2-server serves the same shapes with only the development provider.

export interface Providers {
  providers: string[]
  default?: string
}

export const getProviders = () => http.get<Providers>('/auth/providers').then((r) => r.data)

/**
 * Starts a login; the browser then navigates to the returned URL. The email provider needs the
 * address it signs in; every other provider carries its own flow.
 */
export const startLogin = (provider: string, returnTo: string, email?: string) =>
  http
    .post<{ authorizationUrl: string }>('/auth/login', { provider, returnTo, ...(email === undefined ? {} : { email }) })
    .then((r) => r.data.authorizationUrl)

export const logout = () => http.post('/auth/logout').then(() => undefined)

import type { User } from '@/api/generated.schemas'
import { getApiV1Me } from '@/api/me/me'
import { customInstance, isUnauthorizedError } from '@/lib/api-client'
import { navigateExternal } from '@/lib/navigation'

/**
 * Gateway authentication boundary.
 *
 * The gateway (not the cloud) owns login: `POST /auth/login` records a login
 * attempt and returns the provider's authorization URL, the provider sends
 * the browser back to the gateway's callback, and the gateway answers with
 * an HttpOnly session cookie plus a redirect to `returnTo`. These routes are
 * outside the cloud's OpenAPI document, so they are the one hand-written
 * HTTP surface; everything else goes through the generated client.
 */

/** Login providers the gateway is configured with. */
export type LoginProvider = 'github'

/**
 * Starts an external login and leaves the page. The gateway validates
 * `returnTo` (a same-origin path); after a successful callback the browser
 * lands there with the session cookie set. Rejects when the login could not
 * be started, e.g. the gateway is down or refused the origin.
 */
export async function startLogin(provider: LoginProvider, returnTo: string): Promise<void> {
  const { authorizationUrl } = await customInstance<{ authorizationUrl: string }>({
    url: '/auth/login',
    method: 'POST',
    data: { provider, returnTo },
  })
  navigateExternal(authorizationUrl)
}

/**
 * Revokes the gateway session. Idempotent on the gateway side, so calling it
 * without a live session is not an error.
 */
export async function logoutSession(): Promise<void> {
  await customInstance<void>({ url: '/auth/logout', method: 'POST' })
}

/**
 * Resolves the signed-in user, or `null` when the browser holds no valid
 * session. A 401 is the session's normal "signed out" answer, not a failure;
 * every other error propagates so the UI can distinguish "not signed in"
 * from "backend unreachable".
 */
export async function fetchSessionUser(signal?: AbortSignal): Promise<User | null> {
  try {
    return await getApiV1Me(undefined, signal)
  } catch (error) {
    if (isUnauthorizedError(error)) return null
    throw error
  }
}

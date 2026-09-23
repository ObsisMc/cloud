import type { User } from '@/api/generated.schemas'
import { getApiV1Me } from '@/api/me/me'
import { customInstance, isForbiddenError, isUnauthorizedError } from '@/lib/api-client'
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

/**
 * Login providers this frontend knows how to present. `huawei-idaas` and
 * `github` are external logins (a deployment configures one of them); `dev`
 * is the gateway's development-only form, registered solely on loopback
 * development origins, where any typed identity signs in.
 */
export type LoginProvider = 'huawei-idaas' | 'github' | 'dev'

const KNOWN_PROVIDERS: readonly LoginProvider[] = ['huawei-idaas', 'github', 'dev']

/** True for a provider a member signs in with for real, as opposed to the development form. */
export function isExternalProvider(provider: LoginProvider): boolean {
  return provider !== 'dev'
}

/**
 * Asks the gateway which logins it offers, so the sign-in screen shows
 * exactly the buttons that can succeed (no GitHub button on a machine without
 * an OAuth App, no developer login in production). Providers this build has
 * no button for are dropped.
 */
export async function fetchLoginProviders(signal?: AbortSignal): Promise<LoginProvider[]> {
  const { providers } = await customInstance<{ providers: string[] }>({
    url: '/auth/providers',
    method: 'GET',
    signal,
  })
  return KNOWN_PROVIDERS.filter((known) => providers.includes(known))
}

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
 * GitHub's own sign-out page (public github.com). It asks the member to
 * confirm and then ends the github.com session, so the next "sign in with
 * GitHub" starts from GitHub's login form instead of silently reusing the
 * account the browser was holding. Ora cannot end that session itself: the
 * gateway discards the GitHub token right after reading the profile and never
 * holds one.
 */
export const GITHUB_SIGN_OUT_URL = 'https://github.com/logout'

/** What the session probe learned: a member, no session, or a member Cloud has disabled. */
export type SessionProbe =
  { kind: 'signed-in'; user: User } | { kind: 'signed-out' } | { kind: 'disabled' }

/**
 * Probes the session. A 401 is the normal "signed out" answer and a 403 means
 * the gateway session is valid but Cloud has disabled the user, so signing in
 * again would not help; neither is a failure. Every other error propagates so
 * the UI can distinguish those from "backend unreachable".
 */
export async function fetchSessionUser(signal?: AbortSignal): Promise<SessionProbe> {
  try {
    return { kind: 'signed-in', user: await getApiV1Me(undefined, signal) }
  } catch (error) {
    if (isUnauthorizedError(error)) return { kind: 'signed-out' }
    if (isForbiddenError(error)) return { kind: 'disabled' }
    throw error
  }
}

/**
 * Cloud credential session for the real Go backend.
 *
 * In development the browser signs in through `devgateway` (cmd/devgateway),
 * which holds the signing keys and returns short-lived dual JWTs; in
 * production a real gateway performs the same exchange. This module only
 * stores what the gateway returns and never handles keys or secrets itself.
 * Credentials live in sessionStorage so each browser tab can sign in as a
 * different member — exactly what multi-account verification needs.
 */

export interface CloudCredentials {
  serviceToken: string
  userToken: string
  expiresAt: string
}

const SESSION_KEY = 'ora-cloud-session'

/**
 * Narrows an unknown value to a property bag; storage is untrusted, and a
 * type guard is the boundary check before field access.
 */
function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

/**
 * Validates an untrusted stored value as a credential session. Storage is
 * writable by anything in the tab, so shape-checking at the boundary keeps
 * malformed entries indistinguishable from no session.
 */
function isCloudCredentials(value: unknown): value is CloudCredentials {
  if (!isRecord(value)) return false
  return (
    typeof value['serviceToken'] === 'string' &&
    typeof value['userToken'] === 'string' &&
    typeof value['expiresAt'] === 'string'
  )
}

type Listener = () => void
const listeners = new Set<Listener>()

function notifyListeners(): void {
  for (const listener of listeners) listener()
}

/**
 * Subscribes to session changes so React can re-render when a tab signs in or
 * out. Used with `useSyncExternalStore` by session-aware providers.
 */
export function subscribeCloudSession(listener: Listener): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

/**
 * Snapshot for `useSyncExternalStore`: true while this tab holds cloud
 * credentials. Reading storage here keeps the session synchronous with the
 * tab's actual state.
 */
export function getCloudSessionSnapshot(): boolean {
  return getCloudCredentials() !== null
}

/**
 * Reads the stored credentials, tolerating an empty or corrupted storage
 * entry: a broken session is indistinguishable from no session.
 */
export function getCloudCredentials(): CloudCredentials | null {
  try {
    const raw = sessionStorage.getItem(SESSION_KEY)
    if (!raw) return null
    const parsed: unknown = JSON.parse(raw)
    return isCloudCredentials(parsed) ? parsed : null
  } catch {
    return null
  }
}

/** Persists credentials for the lifetime of the tab. */
export function setCloudCredentials(credentials: CloudCredentials): void {
  sessionStorage.setItem(SESSION_KEY, JSON.stringify(credentials))
  notifyListeners()
}

/** Removes stored credentials; callers redirect to sign-in afterwards. */
export function clearCloudCredentials(): void {
  sessionStorage.removeItem(SESSION_KEY)
  notifyListeners()
}

/** True when this tab has an active cloud session. */
export function hasCloudSession(): boolean {
  return getCloudCredentials() !== null
}

/**
 * Signs in through devgateway with the given identity and stores the returned
 * dual credentials. source/subject mirror the stable IDaaS mapping; display
 * becomes the user's displayName.
 */
export async function loginCloud(
  source: string,
  subject: string,
  display: string,
): Promise<CloudCredentials> {
  const params = new URLSearchParams({ source, subject, display })
  // The devgateway call must settle even when the mock service worker has
  // wedged a passthrough (a broken SW state hangs fetch indefinitely); a
  // bounded timeout surfaces the failure instead of pinning the button.
  const response = await fetch(`/devgateway/login?${params.toString()}`, {
    signal: AbortSignal.timeout(10000),
  })
  if (!response.ok) {
    throw new Error(`devgateway login failed: ${response.status}`)
  }
  const parsed: unknown = await response.json()
  if (!isCloudCredentials(parsed)) {
    throw new Error('devgateway login returned an invalid session')
  }
  setCloudCredentials(parsed)
  return parsed
}

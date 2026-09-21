import { useQuery, useQueryClient } from '@tanstack/react-query'
import { createContext, useCallback, useContext, useEffect, useMemo, type ReactNode } from 'react'
import type { User } from '@/api/generated.schemas'
import { fetchSessionUser, GITHUB_SIGN_OUT_URL, logoutSession } from '@/features/auth/api'
import { onUnauthorized } from '@/lib/api-client'
import { openExternalTab } from '@/lib/navigation'

/**
 * The tab's authentication state. `loading` lasts until the first
 * `GET /api/v1/me` settles; `unavailable` means that probe failed for a
 * reason other than 401, so the UI must not pretend the user is signed out.
 */
export type Session =
  | { status: 'loading' }
  | { status: 'signed-out' }
  | { status: 'unavailable' }
  | { status: 'signed-in'; user: User }

/** Session plus the actions every screen may take on it. */
export interface SessionValue {
  session: Session
  /** Revokes the gateway session and drops every cached query. */
  signOut: () => Promise<void>
  /**
   * `signOut`, then GitHub's own sign-out page in a new tab so this tab stays
   * on Ora (it lands on the login screen) and the member can come straight
   * back with another GitHub account. Ora is signed out first, so cancelling
   * on GitHub's page still leaves Ora signed out.
   */
  signOutOfGitHub: () => Promise<void>
}

/** Query key of the session probe; other modules invalidate it after login. */
export const SESSION_QUERY_KEY = ['session'] as const

const SessionContext = createContext<SessionValue | null>(null)

/**
 * Owns the session probe and the 401 policy. Mount it once above the router:
 * any 401 from the shared HTTP client flips the session to signed out, so
 * screens redirect instead of retrying with a dead cookie. There is no client
 * side token to refresh; the gateway cookie is the whole credential.
 */
export function SessionProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()
  const query = useQuery({
    queryKey: SESSION_QUERY_KEY,
    queryFn: ({ signal }) => fetchSessionUser(signal),
    retry: false,
    staleTime: 5 * 60 * 1000,
  })

  useEffect(
    () => onUnauthorized(() => queryClient.setQueryData(SESSION_QUERY_KEY, null)),
    [queryClient],
  )

  const signOut = useCallback(async () => {
    await logoutSession()
    queryClient.setQueryData(SESSION_QUERY_KEY, null)
    // Everything else in the cache belongs to the member who just left.
    queryClient.removeQueries({
      predicate: (cached) => cached.queryKey[0] !== SESSION_QUERY_KEY[0],
    })
  }, [queryClient])

  const signOutOfGitHub = useCallback(async () => {
    await signOut()
    openExternalTab(GITHUB_SIGN_OUT_URL)
  }, [signOut])

  const value = useMemo<SessionValue>(
    () => ({
      session: toSession(query.data, query.isPending, query.isError),
      signOut,
      signOutOfGitHub,
    }),
    [query.data, query.isPending, query.isError, signOut, signOutOfGitHub],
  )
  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>
}

function toSession(user: User | null | undefined, pending: boolean, failed: boolean): Session {
  if (pending) return { status: 'loading' }
  if (failed) return { status: 'unavailable' }
  if (!user) return { status: 'signed-out' }
  return { status: 'signed-in', user }
}

/** Reads the tab's session; throws outside {@link SessionProvider}. */
export function useSession(): SessionValue {
  const value = useContext(SessionContext)
  if (!value) throw new Error('useSession must be used within a SessionProvider')
  return value
}

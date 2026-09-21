import type { ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { useSession } from '@/features/auth/session'
import { loginPath } from '@/lib/paths'

/**
 * Route gate: renders `children` only for a signed-in session. While the
 * probe is pending nothing is rendered, so a page never flashes with an
 * unknown identity; a signed-out tab goes to the login page with the current
 * location as `returnTo`; an unreachable backend is reported in place.
 */
export function RequireSession({ children }: { children: ReactNode }) {
  const { session } = useSession()
  const location = useLocation()
  if (session.status === 'loading') return null
  if (session.status === 'signed-out') {
    return <Navigate to={loginPath(location.pathname + location.search)} replace />
  }
  if (session.status === 'unavailable') {
    return (
      <div className="flex h-svh items-center justify-center bg-muted/30">
        <p className="text-sm text-muted-foreground">无法连接服务端，请稍后刷新重试</p>
      </div>
    )
  }
  return children
}

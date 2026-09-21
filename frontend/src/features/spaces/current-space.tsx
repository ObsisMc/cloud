import { createContext, useContext, useMemo, type ReactNode } from 'react'
import type { SpaceListItem } from '@/api/generated.schemas'
import { useSpaces } from '@/features/spaces/api'
import { useAuthStore } from '@/state/auth-store'

/**
 * Current-workspace context: resolves the route's `:workspaceSlug` (a
 * collaboration-space slug, e.g. `/default`) against the real spaces list when
 * a cloud session exists, and exposes the tenant id every tenant-scoped API
 * needs. Pages use {@link useCurrentSpace} to decide between cloud data and the
 * mock store without touching routing.
 *
 * Cloud mode is judged from the persisted ora-web cookie session (`tenantId`),
 * not from separately stored credentials: the cookie is the authoritative
 * session, and the browser never holds JWTs.
 */
export interface CurrentSpaceValue {
  /** True while the tab holds an ora-web cookie session. */
  cloudMode: boolean
  /** Tenant id of the cloud session, present only in cloud mode. */
  tenantId: string | undefined
  /** Spaces the signed-in member joined (undefined while loading). */
  spaces: SpaceListItem[] | undefined
  /** The space matching the active route slug, when one exists. */
  space: SpaceListItem | undefined
}

const CurrentSpaceContext = createContext<CurrentSpaceValue | null>(null)

export function CurrentSpaceProvider({ slug, children }: { slug: string; children: ReactNode }) {
  const tenantId = useAuthStore((s) => s.tenantId) ?? undefined
  const cloudMode = tenantId != null
  const spacesQuery = useSpaces(tenantId)
  const spaces = spacesQuery.data?.items
  const space = spaces?.find((candidate) => candidate.slug === slug)
  const value = useMemo(
    () => ({ cloudMode, tenantId, spaces, space }),
    [cloudMode, tenantId, spaces, space],
  )
  return <CurrentSpaceContext.Provider value={value}>{children}</CurrentSpaceContext.Provider>
}

export function useCurrentSpace(): CurrentSpaceValue {
  const value = useContext(CurrentSpaceContext)
  if (!value) throw new Error('useCurrentSpace must be used within a CurrentSpaceProvider')
  return value
}

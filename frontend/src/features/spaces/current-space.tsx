import { createContext, useContext, useMemo, useSyncExternalStore, type ReactNode } from 'react'
import { useGetApiV1MeTenants } from '@/api/me/me'
import type { SpaceListItem } from '@/api/generated.schemas'
import { getCloudSessionSnapshot, subscribeCloudSession } from '@/lib/cloud-session'
import { useSpaces } from '@/features/spaces/api'

/**
 * Current-space context: resolves the route's `:workspaceSlug` against the
 * real spaces list when a cloud session exists, and exposes the tenant id
 * every tenant-scoped API needs. Pages use {@link useCurrentSpace} to decide
 * between cloud data and the mock store without touching routing.
 */
export interface CurrentSpaceValue {
  /** True while the tab holds devgateway-issued cloud credentials. */
  cloudMode: boolean
  /** Tenant id of the cloud session (development uses the first tenant). */
  tenantId: string | undefined
  /** Spaces the signed-in member joined. */
  spaces: SpaceListItem[] | undefined
  /** The space matching the active route slug, when one exists. */
  space: SpaceListItem | undefined
}

const CurrentSpaceContext = createContext<CurrentSpaceValue | null>(null)

export function CurrentSpaceProvider({ slug, children }: { slug: string; children: ReactNode }) {
  const cloudMode = useSyncExternalStore(subscribeCloudSession, getCloudSessionSnapshot)
  const tenants = useGetApiV1MeTenants(undefined, { query: { enabled: cloudMode } })
  const tenantId = tenants.data?.items[0]?.id
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

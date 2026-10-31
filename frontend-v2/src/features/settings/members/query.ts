import { useQuery } from '@tanstack/react-query'
import { getApiV1TenantsTidMembers } from '@/api/members/members'

/** One workspace roster, shared by the page and the invite form's invalidation. */
export const membersKey = (tenantId: string) =>
  ['tenant-members', tenantId] as const

export function useMembers(tenantId: string) {
  return useQuery({
    queryKey: membersKey(tenantId),
    queryFn: () => getApiV1TenantsTidMembers(tenantId),
    enabled: tenantId !== '',
  })
}

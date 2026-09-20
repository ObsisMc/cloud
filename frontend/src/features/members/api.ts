/**
 * Tenant member directory. The single real implementation of the members query
 * lives in `features/issues/api.ts` (it is needed there for assignee name
 * resolution); this module re-exports it so the settings page keeps a coherent
 * import surface without a second, drifting copy.
 */
export { useMembers } from '@/features/issues/api'
export type { TenantMember } from '@/features/issues/types'

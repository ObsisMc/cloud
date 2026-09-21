import type { SpaceMemberListItem } from '@/api/generated.schemas'
import { useCurrentSpace } from '@/features/spaces/current-space'
import { useSpaceMembers } from '@/features/spaces/api'
import type { User, WorkspaceMember } from '@/mocks/data/types'

export type MemberWithUser = User &
  Pick<WorkspaceMember, 'role' | 'status' | 'joinedAt'> & {
    /** Optimistic version of the membership row, required for updates. */
    version?: number
  }

const AVATAR_COLORS = [
  '#f97316',
  '#f43f5e',
  '#8b5cf6',
  '#3b82f6',
  '#10b981',
  '#eab308',
  '#06b6d4',
  '#ec4899',
]

/** Deterministic avatar color so a member looks stable across reloads. */
function colorFor(seed: string): string {
  let hash = 0
  for (let i = 0; i < seed.length; i++) hash = (hash * 31 + seed.charCodeAt(i)) >>> 0
  return AVATAR_COLORS[hash % AVATAR_COLORS.length] ?? '#3b82f6'
}

/**
 * Narrows the cloud's free-form role string to the UI union, falling back to
 * member for unknown values so the table always renders a known label.
 */
function normalizeRole(role: string): MemberWithUser['role'] {
  if (role === 'owner' || role === 'admin' || role === 'member') return role
  return 'member'
}

/**
 * Maps a cloud membership row onto the UI member shape. The cloud has no
 * avatar or email fields; display name drives the initials and the id drives
 * a stable color. A disabled membership renders as the invited status.
 */
export function cloudMemberToUI(m: SpaceMemberListItem): MemberWithUser {
  const name = m.displayName || m.userId.slice(0, 8)
  return {
    id: m.userId,
    type: 'user',
    name,
    email: '',
    avatarColor: colorFor(m.userId),
    initials: name.slice(0, 2).toUpperCase(),
    role: normalizeRole(m.role),
    status: m.status === 'disabled' ? 'invited' : 'active',
    joinedAt: m.joinedAt,
    version: m.version,
  }
}

/**
 * Members of the space at `slug`, read through the generated client once the
 * slug resolved to a joined space; pending until then.
 */
export function useMembers(slug: string): {
  data: MemberWithUser[] | undefined
  isPending: boolean
  isError: boolean
} {
  const { tenantId, space } = useCurrentSpace()
  const cloud = useSpaceMembers(tenantId, space?.slug === slug ? space.id : undefined)
  return {
    data: cloud.data?.items.map(cloudMemberToUI),
    isPending: cloud.isLoading,
    isError: cloud.isError,
  }
}

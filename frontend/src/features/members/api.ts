import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { Error as ApiError, SpaceMember, SpaceMemberListItem } from '@/api/generated.schemas'
import { getGetApiV1TenantsTidSpacesQueryKey } from '@/api/spaces/spaces'
import { AXIOS_INSTANCE, type ErrorType } from '@/lib/api-client'
import { useCurrentSpace } from '@/features/spaces/current-space'
import { mutationHeaders, useIdempotencyKeys, useSpaceMembers } from '@/features/spaces/api'
import type { User, WorkspaceMember } from '@/mocks/data/types'

/** Add-by-email inputs; the backend resolves and normalizes the address. */
export interface AddMemberByEmailInput {
  email: string
}

export type MemberWithUser = User &
  Pick<WorkspaceMember, 'role' | 'status' | 'joinedAt'> & {
    /** Cloud-only optimistic version; absent on mock members. */
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

/**
 * Adds an already-registered user to the space as a plain member by email. The
 * backend resolves the address in the caller's identity source (404
 * `user_not_registered` when unknown), atomically ensures tenant membership, and
 * returns the membership — idempotent for an existing member. The POST requires
 * an `Idempotency-Key`, so one logical add reuses a single key across retries.
 * The membership list is invalidated on success so the new row appears
 * immediately.
 */
export function useAddSpaceMemberByEmail(tenantId: string, spaceId: string) {
  const queryClient = useQueryClient()
  const keyFor = useIdempotencyKeys()
  return useMutation<SpaceMember, ErrorType<ApiError>, AddMemberByEmailInput>({
    mutationFn: async (input: AddMemberByEmailInput) => {
      const { data } = await AXIOS_INSTANCE.post<SpaceMember>(
        `/api/v1/tenants/${tenantId}/spaces/${spaceId}/members`,
        { email: input.email },
        { headers: mutationHeaders(keyFor(input)) },
      )
      return data
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: [`/api/v1/tenants/${tenantId}/spaces/${spaceId}/members`],
      })
    },
  })
}

/**
 * Removes a member's Workspace membership (DELETE, owner only). Removal is a
 * hard delete of the workspace membership alone — the user account, their
 * tenant membership and any resources they created are untouched and remain in
 * the workspace. The backend rejects removing an owner row (409
 * `cannot_remove_workspace_owner`) and any non-owner actor (403). DELETE
 * requires an `Idempotency-Key`, so one logical removal reuses a single key
 * across retries; the member list and the space list are invalidated on
 * success.
 */
export function useRemoveSpaceMember(tenantId: string, spaceId: string) {
  const queryClient = useQueryClient()
  const keyFor = useIdempotencyKeys()
  return useMutation<SpaceMember, ErrorType<ApiError>, { userId: string; version: number }>({
    mutationFn: async (input: { userId: string; version: number }) => {
      const { data } = await AXIOS_INSTANCE.delete<SpaceMember>(
        `/api/v1/tenants/${tenantId}/spaces/${spaceId}/members/${input.userId}`,
        // The DELETE route's optimistic lock reads `version` from the JSON body
        // (the router requires a body on non-GET), not from the query string.
        { headers: mutationHeaders(keyFor(input)), data: { version: input.version } },
      )
      return data
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: [`/api/v1/tenants/${tenantId}/spaces/${spaceId}/members`],
      })
      void queryClient.invalidateQueries({
        queryKey: getGetApiV1TenantsTidSpacesQueryKey(tenantId),
      })
    },
  })
}

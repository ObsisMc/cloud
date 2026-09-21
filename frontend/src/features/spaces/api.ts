import { useRef } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { Error as ApiError, Space, SpaceListItem, SpaceMember } from '@/api/generated.schemas'
import {
  deleteApiV1TenantsTidSpacesSpaceId,
  getApiV1TenantsTidSpaces,
  getApiV1TenantsTidSpacesSpaceIdMembers,
  getApiV1TenantsTidSpacesSpaceIdProjects,
  getGetApiV1TenantsTidSpacesQueryKey,
  patchApiV1TenantsTidSpacesSpaceId,
  postApiV1TenantsTidSpaces,
  putApiV1TenantsTidSpacesSpaceIdMembersUid,
} from '@/api/spaces/spaces'
import type { ErrorType } from '@/lib/api-client'

/** Fresh idempotency key; the backend requires one on every POST/DELETE. */
function idempotencyKey(): string {
  return crypto.randomUUID()
}

/**
 * Returns the idempotency key for one logical mutation: minted the first time
 * `variables` is seen and reused for every later call with that same object.
 *
 * React Query re-invokes `mutationFn` for each retry of a `mutate()` call but
 * always passes the same variables object, so a retry replays the original key
 * and the backend dedupes it instead of running the mutation twice. A new
 * `mutate()` call carries fresh variables and therefore mints a fresh key, and
 * two overlapping calls can never share one because they never share variables.
 */
export function idempotencyKeyFor(
  pending: { current: { variables: unknown; key: string } | null },
  variables: unknown,
): string {
  const current = pending.current
  if (current !== null && current.variables === variables) return current.key
  const key = idempotencyKey()
  pending.current = { variables, key }
  return key
}

/** Per-hook slot backing {@link idempotencyKeyFor}. */
function useIdempotencyKeys() {
  const pending = useRef<{ variables: unknown; key: string } | null>(null)
  return (variables: unknown): string => idempotencyKeyFor(pending, variables)
}

/**
 * Headers for one mutating request. The backend rejects a POST/DELETE without a
 * non-empty `Idempotency-Key` (400 `idempotency_key_required`) and dedupes a
 * replay that carries the same key, so the key must stay stable across retries
 * of the same logical mutation. `Content-Type` is repeated here because the
 * orval mutator spreads these options over the generated request config, which
 * replaces its headers object outright.
 */
function mutationHeaders(key: string): Record<string, string> {
  return { 'Content-Type': 'application/json', 'Idempotency-Key': key }
}

/** Space membership roles; mirrors the backend owner/admin/member model. */
export type SpaceRole = 'owner' | 'admin' | 'member'

/**
 * Narrows the cloud's free-form role string to the UI union, falling back to
 * member for unknown values so role checks always render a known label.
 */
export function normalizeSpaceRole(role: string): SpaceRole {
  if (role === 'owner' || role === 'admin' || role === 'member') return role
  return 'member'
}

export type SpaceMemberStatus = 'active' | 'disabled'

/** Input for creating a collaboration space; slug is lowercase and immutable. */
export interface CreateSpaceInput {
  name: string
  slug: string
  description: string
}

/** Input for updating space settings; version guards concurrent edits. */
export interface UpdateSpaceInput {
  name: string
  description: string
  version: number
}

/** Input for upserting one membership; new members use version 0. */
export interface UpdateSpaceMemberInput {
  userId: string
  role: SpaceRole
  status: SpaceMemberStatus
  version: number
}

/**
 * Space API hooks wrap the generated orval client so pages talk domain terms
 * instead of endpoint paths. Mutations invalidate the affected lists on
 * success. All functions are disabled until a tenant id is known.
 */

export function useSpaces(tenantId: string | undefined) {
  return useQuery({
    queryKey: tenantId ? getGetApiV1TenantsTidSpacesQueryKey(tenantId) : ['spaces', 'no-tenant'],
    queryFn: ({ signal }) => getApiV1TenantsTidSpaces(tenantId ?? '', undefined, undefined, signal),
    enabled: !!tenantId,
  })
}

export function useSpaceMembers(tenantId: string | undefined, spaceId: string | undefined) {
  return useQuery({
    queryKey:
      tenantId && spaceId
        ? [`/api/v1/tenants/${tenantId}/spaces/${spaceId}/members`]
        : ['space-members', 'disabled'],
    queryFn: ({ signal }) =>
      getApiV1TenantsTidSpacesSpaceIdMembers(
        tenantId ?? '',
        spaceId ?? '',
        undefined,
        undefined,
        signal,
      ),
    enabled: !!tenantId && !!spaceId,
  })
}

export function useSpaceProjects(tenantId: string | undefined, spaceId: string | undefined) {
  return useQuery({
    queryKey:
      tenantId && spaceId
        ? [`/api/v1/tenants/${tenantId}/spaces/${spaceId}/projects`]
        : ['space-projects', 'disabled'],
    queryFn: ({ signal }) =>
      getApiV1TenantsTidSpacesSpaceIdProjects(
        tenantId ?? '',
        spaceId ?? '',
        undefined,
        undefined,
        signal,
      ),
    enabled: !!tenantId && !!spaceId,
  })
}

export function useCreateSpace(tenantId: string | undefined) {
  const queryClient = useQueryClient()
  const keyFor = useIdempotencyKeys()
  return useMutation<Space, ErrorType<ApiError>, CreateSpaceInput>({
    mutationFn: (input: CreateSpaceInput) =>
      postApiV1TenantsTidSpaces(tenantId ?? '', input, {
        headers: mutationHeaders(keyFor(input)),
      }),
    onSuccess: () => {
      if (!tenantId) return
      void queryClient.invalidateQueries({
        queryKey: getGetApiV1TenantsTidSpacesQueryKey(tenantId),
      })
    },
  })
}

export function useUpdateSpace(tenantId: string | undefined, spaceId: string | undefined) {
  const queryClient = useQueryClient()
  return useMutation<Space, ErrorType<ApiError>, UpdateSpaceInput>({
    mutationFn: (input: UpdateSpaceInput) =>
      patchApiV1TenantsTidSpacesSpaceId(tenantId ?? '', spaceId ?? '', input),
    onSuccess: (space: Space) => {
      if (!tenantId) return
      void queryClient.invalidateQueries({
        queryKey: getGetApiV1TenantsTidSpacesQueryKey(tenantId),
      })
      void queryClient.invalidateQueries({
        queryKey: [`/api/v1/tenants/${tenantId}/spaces/${space.id}`],
      })
    },
  })
}

export function useArchiveSpace(tenantId: string | undefined, spaceId: string | undefined) {
  const queryClient = useQueryClient()
  const keyFor = useIdempotencyKeys()
  return useMutation<Space, ErrorType<ApiError>, number>({
    mutationFn: (version: number) =>
      deleteApiV1TenantsTidSpacesSpaceId(
        tenantId ?? '',
        spaceId ?? '',
        { version },
        { headers: mutationHeaders(keyFor(version)) },
      ),
    onSuccess: () => {
      if (!tenantId) return
      void queryClient.invalidateQueries({
        queryKey: getGetApiV1TenantsTidSpacesQueryKey(tenantId),
      })
    },
  })
}

export function useUpdateSpaceMember(tenantId: string | undefined, spaceId: string | undefined) {
  const queryClient = useQueryClient()
  return useMutation<SpaceMember, ErrorType<ApiError>, UpdateSpaceMemberInput>({
    mutationFn: (input: UpdateSpaceMemberInput) =>
      putApiV1TenantsTidSpacesSpaceIdMembersUid(tenantId ?? '', spaceId ?? '', input.userId, {
        role: input.role,
        status: input.status,
        version: input.version,
      }),
    onSuccess: () => {
      if (!tenantId || !spaceId) return
      void queryClient.invalidateQueries({
        queryKey: [`/api/v1/tenants/${tenantId}/spaces/${spaceId}/members`],
      })
    },
  })
}

export type { Space, SpaceListItem }

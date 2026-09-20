import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AXIOS_INSTANCE } from '@/lib/api-client'
import type {
  ContextRef,
  Issue,
  IssueComment,
  IssuePriority,
  IssueRun,
  IssueStatusColumn,
  Label,
  Page,
  TenantMember,
} from './types'

/**
 * Cloud Issue data layer. Every hook talks to the real `/api/v1/tenants/:tid`
 * backend through the shared `AXIOS_INSTANCE` (same-origin, so the edge server's
 * session cookie carries auth). Nothing here touches the MSW mocks.
 */

/** Field names the PUT endpoint accepts; sent verbatim, with `version` for concurrency. */
export interface UpdateIssueInput {
  title?: string
  description?: string
  status?: string
  priority?: IssuePriority
  assigneeType?: 'user' | 'agent' | 'team'
  assigneeId?: string
  parentIssueId?: string
  projectRef?: string
  properties?: Record<string, unknown>
}

/** Fields the create endpoint accepts. */
export interface CreateIssueInput {
  title: string
  description?: string
  status?: string
  priority?: IssuePriority
  assigneeType?: 'user' | 'agent' | 'team'
  assigneeId?: string
  parentIssueId?: string
}

/** Anchor-based move the board drag maps onto. */
export interface MoveIssueInput {
  status?: string
  beforeId?: string
  afterId?: string
}

/** Fresh idempotency key for every POST/DELETE; the backend dedupes replays by it. */
function idempotencyKey(): string {
  return crypto.randomUUID()
}

/** Unwraps a `Page<T>` list envelope into its `items` array. */
async function listPage<T>(url: string, params?: Record<string, string>): Promise<T[]> {
  const { data } = await AXIOS_INSTANCE.get<Page<T>>(url, { params })
  return data.items
}

/** Unwraps a `{ resource }` creation envelope. */
async function created<T>(url: string, body: unknown): Promise<T> {
  const { data } = await AXIOS_INSTANCE.post<{ resource: T }>(url, body, {
    headers: { 'Idempotency-Key': idempotencyKey() },
  })
  return data.resource
}

export function issuesKey(tid: string) {
  return ['issues', tid] as const
}

/** Lists the tenant board (optionally filtered by a free-text `q`). */
export function useIssues(tid: string, q?: string) {
  return useQuery({
    queryKey: [...issuesKey(tid), q ?? ''],
    queryFn: () => listPage<Issue>(`/api/v1/tenants/${tid}/issues`, q ? { q } : undefined),
    enabled: !!tid,
  })
}

export function useIssue(tid: string, id: string | undefined) {
  return useQuery({
    queryKey: ['issue', tid, id],
    queryFn: async () => {
      const { data } = await AXIOS_INSTANCE.get<Issue>(`/api/v1/tenants/${tid}/issues/${id}`)
      return data
    },
    enabled: !!tid && !!id,
  })
}

/** Loads the tenant's status catalog (columns the board renders, in position order). */
export function useIssueStatuses(tid: string) {
  return useQuery({
    queryKey: ['issue-statuses', tid],
    queryFn: () => listPage<IssueStatusColumn>(`/api/v1/tenants/${tid}/issue-statuses`),
    enabled: !!tid,
  })
}

/** Loads tenant members for resolving user assignee display names. */
export function useMembers(tid: string) {
  return useQuery({
    queryKey: ['members', tid],
    queryFn: () => listPage<TenantMember>(`/api/v1/tenants/${tid}/members`),
    enabled: !!tid,
  })
}

export function useCreateIssue(tid: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: CreateIssueInput) =>
      created<Issue>(`/api/v1/tenants/${tid}/issues`, input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: issuesKey(tid) })
    },
  })
}

export function useUpdateIssue(tid: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, version, patch }: { id: string; version: number; patch: UpdateIssueInput }) => {
      const { data } = await AXIOS_INSTANCE.put<Issue>(
        `/api/v1/tenants/${tid}/issues/${id}`,
        { ...patch, version },
      )
      return data
    },
    onSuccess: (data) => {
      queryClient.setQueryData(['issue', tid, data.id], data)
      void queryClient.invalidateQueries({ queryKey: issuesKey(tid) })
    },
  })
}

export function useMoveIssue(tid: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, version, move }: { id: string; version: number; move: MoveIssueInput }) => {
      const { data } = await AXIOS_INSTANCE.post<Issue>(
        `/api/v1/tenants/${tid}/issues/${id}/move`,
        { ...move, version },
        { headers: { 'Idempotency-Key': idempotencyKey() } },
      )
      return data
    },
    onSuccess: (data) => {
      queryClient.setQueryData(['issue', tid, data.id], data)
      void queryClient.invalidateQueries({ queryKey: issuesKey(tid) })
    },
  })
}

export function useDeleteIssue(tid: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, version }: { id: string; version: number }) => {
      await AXIOS_INSTANCE.delete(`/api/v1/tenants/${tid}/issues/${id}`, {
        data: { version },
        headers: { 'Idempotency-Key': idempotencyKey() },
      })
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: issuesKey(tid) })
    },
  })
}

export function useComments(tid: string, issueId: string) {
  return useQuery({
    queryKey: ['issue-comments', tid, issueId],
    queryFn: () => listPage<IssueComment>(`/api/v1/tenants/${tid}/issues/${issueId}/comments`),
    enabled: !!tid && !!issueId,
  })
}

export function useCreateComment(tid: string, issueId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (body: { body: string; parentId?: string }) =>
      created<IssueComment>(`/api/v1/tenants/${tid}/issues/${issueId}/comments`, body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['issue-comments', tid, issueId] })
    },
  })
}

export function useRuns(tid: string, issueId: string) {
  return useQuery({
    queryKey: ['issue-runs', tid, issueId],
    queryFn: () => listPage<IssueRun>(`/api/v1/tenants/${tid}/issues/${issueId}/runs`),
    enabled: !!tid && !!issueId,
  })
}

export function useCreateRun(tid: string, issueId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: { executorType: 'agent' | 'team' | 'workflow'; executorId: string; input?: Record<string, unknown> }) =>
      created<IssueRun>(`/api/v1/tenants/${tid}/issues/${issueId}/runs`, input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['issue-runs', tid, issueId] })
    },
  })
}

export function useContextRefs(tid: string, issueId: string) {
  return useQuery({
    queryKey: ['issue-context-refs', tid, issueId],
    queryFn: () => listPage<ContextRef>(`/api/v1/tenants/${tid}/issues/${issueId}/context-refs`),
    enabled: !!tid && !!issueId,
  })
}

export function useCreateContextRef(tid: string, issueId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: { refType: ContextRef['refType']; refId: string }) =>
      created<ContextRef>(`/api/v1/tenants/${tid}/issues/${issueId}/context-refs`, input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['issue-context-refs', tid, issueId] })
    },
  })
}

export function useDeleteContextRef(tid: string, issueId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      AXIOS_INSTANCE.delete(`/api/v1/tenants/${tid}/issues/${issueId}/context-refs/${id}`, {
        headers: { 'Idempotency-Key': idempotencyKey() },
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['issue-context-refs', tid, issueId] })
    },
  })
}

/** Re-export the label type used by issue cards; keeps components on one import surface. */
export type { Label }

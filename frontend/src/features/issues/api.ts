import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { mockApi } from '@/lib/mock-api-client'
import type { Issue } from '@/mocks/data/types'

export function issuesKey(slug: string, filters?: Record<string, string | undefined>) {
  return ['issues', slug, filters ?? {}] as const
}

export function useIssues(slug: string, filters?: { status?: string; projectId?: string; assigneeId?: string }) {
  return useQuery({
    queryKey: issuesKey(slug, filters),
    queryFn: async () => {
      const { data } = await mockApi.get<Issue[]>(`/workspaces/${slug}/issues`, { params: filters })
      return data
    },
  })
}

export function useIssue(slug: string, id: string | undefined) {
  return useQuery({
    queryKey: ['issue', slug, id],
    queryFn: async () => {
      const { data } = await mockApi.get<Issue>(`/workspaces/${slug}/issues/${id}`)
      return data
    },
    enabled: !!id,
  })
}

export function useCreateIssue(slug: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: Partial<Issue>) => {
      const { data } = await mockApi.post<Issue>(`/workspaces/${slug}/issues`, input)
      return data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['issues', slug] })
    },
  })
}

export function useUpdateIssue(slug: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, patch }: { id: string; patch: Partial<Issue> }) => {
      const { data } = await mockApi.patch<Issue>(`/workspaces/${slug}/issues/${id}`, patch)
      return data
    },
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['issues', slug] })
      queryClient.setQueryData(['issue', slug, data.id], data)
    },
  })
}

export function useDeleteIssue(slug: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) => {
      await mockApi.delete(`/workspaces/${slug}/issues/${id}`)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['issues', slug] })
    },
  })
}

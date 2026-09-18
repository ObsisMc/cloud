import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { mockApi } from '@/lib/mock-api-client'
import type { Issue } from '@/mocks/data/types'

export function issuesKey(slug: string, filters?: Record<string, string | undefined>) {
  return ['issues', slug, filters ?? {}] as const
}

export function useIssues(
  slug: string,
  filters?: { status?: string; projectId?: string; assigneeId?: string },
) {
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
      void queryClient.invalidateQueries({ queryKey: ['issues', slug] })
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
    // Applied synchronously so e.g. a board drag moves the card immediately
    // instead of snapping back to its old column until the refetch lands.
    onMutate: async ({ id, patch }) => {
      await queryClient.cancelQueries({ queryKey: ['issues', slug] })
      const previousLists = queryClient.getQueriesData<Issue[]>({ queryKey: ['issues', slug] })
      queryClient.setQueriesData<Issue[]>({ queryKey: ['issues', slug] }, (issues) =>
        issues?.map((issue) => (issue.id === id ? { ...issue, ...patch } : issue)),
      )
      const previousDetail = queryClient.getQueryData<Issue>(['issue', slug, id])
      if (previousDetail) {
        queryClient.setQueryData<Issue>(['issue', slug, id], { ...previousDetail, ...patch })
      }
      return { previousLists, previousDetail, id }
    },
    onError: (_err, _vars, context) => {
      context?.previousLists.forEach(([key, data]) => queryClient.setQueryData(key, data))
      if (context?.previousDetail) {
        queryClient.setQueryData(['issue', slug, context.id], context.previousDetail)
      }
    },
    onSuccess: (data) => {
      queryClient.setQueryData(['issue', slug, data.id], data)
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: ['issues', slug] })
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
      void queryClient.invalidateQueries({ queryKey: ['issues', slug] })
    },
  })
}

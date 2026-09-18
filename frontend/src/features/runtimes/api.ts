import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { mockApi } from '@/lib/mock-api-client'
import type { Runtime } from '@/mocks/data/types'

export function useRuntimes(slug: string) {
  return useQuery({
    queryKey: ['runtimes', slug],
    queryFn: async () => {
      const { data } = await mockApi.get<Runtime[]>(`/workspaces/${slug}/runtimes`)
      return data
    },
  })
}

export function useRuntimeAction(slug: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, action }: { id: string; action: 'start' | 'stop' }) => {
      const { data } = await mockApi.post<Runtime>(`/workspaces/${slug}/runtimes/${id}/action`, { action })
      return data
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['runtimes', slug] }),
  })
}

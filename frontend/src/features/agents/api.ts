import { useQuery } from '@tanstack/react-query'
import { mockApi } from '@/lib/mock-api-client'
import type { Agent } from '@/mocks/data/types'

export function useAgents(slug: string) {
  return useQuery({
    queryKey: ['agents', slug],
    queryFn: async () => {
      const { data } = await mockApi.get<Agent[]>(`/workspaces/${slug}/agents`)
      return data
    },
  })
}

export function useAgent(slug: string, id: string | undefined) {
  return useQuery({
    queryKey: ['agent', slug, id],
    queryFn: async () => {
      const { data } = await mockApi.get<Agent>(`/workspaces/${slug}/agents/${id}`)
      return data
    },
    enabled: !!id,
  })
}

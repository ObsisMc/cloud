import { useQuery } from '@tanstack/react-query'
import { mockApi } from '@/lib/mock-api-client'
import type { Squad } from '@/mocks/data/types'

export function useSquads(slug: string) {
  return useQuery({
    queryKey: ['squads', slug],
    queryFn: async () => {
      const { data } = await mockApi.get<Squad[]>(`/workspaces/${slug}/squads`)
      return data
    },
  })
}

export function useSquad(slug: string, id: string | undefined) {
  return useQuery({
    queryKey: ['squad', slug, id],
    queryFn: async () => {
      const { data } = await mockApi.get<Squad>(`/workspaces/${slug}/squads/${id}`)
      return data
    },
    enabled: !!id,
  })
}

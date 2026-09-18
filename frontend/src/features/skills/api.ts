import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { mockApi } from '@/lib/mock-api-client'
import type { Skill } from '@/mocks/data/types'

export function useSkills(slug: string) {
  return useQuery({
    queryKey: ['skills', slug],
    queryFn: async () => {
      const { data } = await mockApi.get<Skill[]>(`/workspaces/${slug}/skills`)
      return data
    },
  })
}

export function useToggleSkill(slug: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, enabled }: { id: string; enabled: boolean }) => {
      const { data } = await mockApi.patch<Skill>(`/workspaces/${slug}/skills/${id}`, { enabled })
      return data
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['skills', slug] }),
  })
}

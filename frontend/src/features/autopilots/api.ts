import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { mockApi } from '@/lib/mock-api-client'
import type { Autopilot } from '@/mocks/data/types'

export function useAutopilots(slug: string) {
  return useQuery({
    queryKey: ['autopilots', slug],
    queryFn: async () => {
      const { data } = await mockApi.get<Autopilot[]>(`/workspaces/${slug}/autopilots`)
      return data
    },
  })
}

export function useToggleAutopilot(slug: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, status }: { id: string; status: Autopilot['status'] }) => {
      const { data } = await mockApi.patch<Autopilot>(`/workspaces/${slug}/autopilots/${id}`, { status })
      return data
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['autopilots', slug] }),
  })
}

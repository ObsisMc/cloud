import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { mockApi } from '@/lib/mock-api-client'
import type { Project } from '@/mocks/data/types'

export function useProjects(slug: string) {
  return useQuery({
    queryKey: ['projects', slug],
    queryFn: async () => {
      const { data } = await mockApi.get<Project[]>(`/workspaces/${slug}/projects`)
      return data
    },
  })
}

export function useProject(slug: string, id: string | undefined) {
  return useQuery({
    queryKey: ['project', slug, id],
    queryFn: async () => {
      const { data } = await mockApi.get<Project>(`/workspaces/${slug}/projects/${id}`)
      return data
    },
    enabled: !!id,
  })
}

export function useCreateProject(slug: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (input: Partial<Project>) => {
      const { data } = await mockApi.post<Project>(`/workspaces/${slug}/projects`, input)
      return data
    },
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['projects', slug] }),
  })
}

export function useUpdateProject(slug: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, patch }: { id: string; patch: Partial<Project> }) => {
      const { data } = await mockApi.patch<Project>(`/workspaces/${slug}/projects/${id}`, patch)
      return data
    },
    onSuccess: (data) => {
      void queryClient.invalidateQueries({ queryKey: ['projects', slug] })
      queryClient.setQueryData(['project', slug, data.id], data)
    },
  })
}

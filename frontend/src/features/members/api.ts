import { useQuery } from '@tanstack/react-query'
import { mockApi } from '@/lib/mock-api-client'
import type { User, WorkspaceMember } from '@/mocks/data/types'

export type MemberWithUser = User & Pick<WorkspaceMember, 'role' | 'status' | 'joinedAt'>

export function useMembers(slug: string) {
  return useQuery({
    queryKey: ['members', slug],
    queryFn: async () => {
      const { data } = await mockApi.get<MemberWithUser[]>(`/workspaces/${slug}/members`)
      return data
    },
  })
}

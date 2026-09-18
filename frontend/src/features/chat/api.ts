import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { mockApi } from '@/lib/mock-api-client'
import type { ChatMessage, ChatSession } from '@/mocks/data/types'

export function useChatSessions(slug: string) {
  return useQuery({
    queryKey: ['chat-sessions', slug],
    queryFn: async () => {
      const { data } = await mockApi.get<ChatSession[]>(`/workspaces/${slug}/chat/sessions`)
      return data
    },
    refetchInterval: 15_000,
  })
}

export function useChatMessages(slug: string, sessionId: string | undefined) {
  return useQuery({
    queryKey: ['chat-messages', slug, sessionId],
    queryFn: async () => {
      const { data } = await mockApi.get<ChatMessage[]>(`/workspaces/${slug}/chat/sessions/${sessionId}/messages`)
      return data
    },
    enabled: !!sessionId,
  })
}

export function useSendChatMessage(slug: string, sessionId: string | undefined) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (content: string) => {
      const { data } = await mockApi.post<ChatMessage[]>(
        `/workspaces/${slug}/chat/sessions/${sessionId}/messages`,
        { content },
      )
      return data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['chat-messages', slug, sessionId] })
      queryClient.invalidateQueries({ queryKey: ['chat-sessions', slug] })
    },
  })
}

import { http, HttpResponse } from 'msw'
import { db, nextId } from '../data/store'
import { MOCK_BASE, notFound, requireWorkspace } from './shared'

const CANNED_REPLIES = [
  '收到，我这边看一下情况，稍后同步给你。',
  '好的，已经安排上了，完成后会通知你。',
  '看了一下，目前一切正常，无需处理。',
  '找到问题了，已经修复，下一次运行就能看到。',
  '这个问题我需要再确认一下，稍等。',
]

export const chatHandlers = [
  http.get(`${MOCK_BASE}/workspaces/:slug/chat/sessions`, ({ params }) => {
    const ws = requireWorkspace(params['slug'] as string)
    if (!ws) return notFound('workspace not found')
    const list = [...db.chatSessions.filter((s) => s.workspaceId === ws.id)].sort((a, b) =>
      b.updatedAt.localeCompare(a.updatedAt),
    )
    return HttpResponse.json(list)
  }),

  http.get(`${MOCK_BASE}/workspaces/:slug/chat/sessions/:id/messages`, ({ params }) => {
    const list = db.chatMessages
      .filter((m) => m.sessionId === params['id'])
      .sort((a, b) => a.createdAt.localeCompare(b.createdAt))
    return HttpResponse.json(list)
  }),

  http.post(
    `${MOCK_BASE}/workspaces/:slug/chat/sessions/:id/messages`,
    async ({ params, request }) => {
      const session = db.chatSessions.find((s) => s.id === params['id'])
      if (!session) return notFound('session not found')
      const body = (await request.json()) as { content: string }
      const now = new Date()
      const userMessage = {
        id: nextId('msg'),
        sessionId: session.id,
        authorId: db.users[0].id,
        authorType: 'user' as const,
        content: body.content,
        createdAt: now.toISOString(),
      }
      db.chatMessages.push(userMessage)
      const replyContent = CANNED_REPLIES[db.chatMessages.length % CANNED_REPLIES.length]
      if (!replyContent) throw new Error('chat reply seed data must not be empty')
      const reply = {
        id: nextId('msg'),
        sessionId: session.id,
        authorId: session.agentId,
        authorType: 'agent' as const,
        content: replyContent,
        createdAt: new Date(now.getTime() + 1200).toISOString(),
      }
      db.chatMessages.push(reply)
      session.updatedAt = reply.createdAt
      return HttpResponse.json([userMessage, reply], { status: 201 })
    },
  ),
]

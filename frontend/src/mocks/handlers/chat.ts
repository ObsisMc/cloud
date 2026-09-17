import { http, HttpResponse } from 'msw'
import { db, nextId } from '../data/store'
import { MOCK_BASE, notFound, requireWorkspace } from './shared'

const CANNED_REPLIES = [
  "On it — I'll pull the details and get back to you shortly.",
  "Got it. I've queued that up and will report back once it's done.",
  'Looked into this: everything checks out, no action needed.',
  "I found the issue and opened a fix — you'll see it in the next run.",
  'Good question — let me dig a bit deeper before I answer that.',
]

export const chatHandlers = [
  http.get(`${MOCK_BASE}/workspaces/:slug/chat/sessions`, ({ params }) => {
    const ws = requireWorkspace(params.slug as string)
    if (!ws) return notFound('workspace not found')
    const list = [...db.chatSessions.filter((s) => s.workspaceId === ws.id)].sort((a, b) =>
      b.updatedAt.localeCompare(a.updatedAt),
    )
    return HttpResponse.json(list)
  }),

  http.get(`${MOCK_BASE}/workspaces/:slug/chat/sessions/:id/messages`, ({ params }) => {
    const list = db.chatMessages
      .filter((m) => m.sessionId === params.id)
      .sort((a, b) => a.createdAt.localeCompare(b.createdAt))
    return HttpResponse.json(list)
  }),

  http.post(`${MOCK_BASE}/workspaces/:slug/chat/sessions/:id/messages`, async ({ params, request }) => {
    const session = db.chatSessions.find((s) => s.id === params.id)
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
    const reply = {
      id: nextId('msg'),
      sessionId: session.id,
      authorId: session.agentId,
      authorType: 'agent' as const,
      content: CANNED_REPLIES[db.chatMessages.length % CANNED_REPLIES.length],
      createdAt: new Date(now.getTime() + 1200).toISOString(),
    }
    db.chatMessages.push(reply)
    session.updatedAt = reply.createdAt
    return HttpResponse.json([userMessage, reply], { status: 201 })
  }),
]

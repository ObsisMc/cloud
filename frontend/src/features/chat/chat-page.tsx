import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { Send } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { ActorAvatar } from '@/components/common/actor-avatar'
import { PageHeader } from '@/components/layout/page-header'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'
import { workspacePaths } from '@/lib/paths'
import { useChatMessages, useChatSessions, useSendChatMessage } from '@/features/chat/api'
import { actorById, currentUserId } from '@/mocks/data/store'

export function ChatPage({ slug }: { slug: string }) {
  const { sessionId } = useParams<{ sessionId: string }>()
  const navigate = useNavigate()
  const p = workspacePaths(slug)
  const { data: sessions } = useChatSessions(slug)
  const activeSessionId = sessionId ?? sessions?.[0]?.id
  const { data: messages } = useChatMessages(slug, activeSessionId)
  const sendMessage = useSendChatMessage(slug, activeSessionId)
  const [draft, setDraft] = useState('')
  const bottomRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ block: 'end' })
  }, [messages])

  function handleSend(e: React.FormEvent) {
    e.preventDefault()
    if (!draft.trim() || !activeSessionId) return
    sendMessage.mutate(draft.trim())
    setDraft('')
  }

  const activeSession = sessions?.find((s) => s.id === activeSessionId)
  const activeAgent = activeSession ? actorById(activeSession.agentId) : undefined

  return (
    <div className="flex h-full min-h-0">
      <div className="flex w-64 shrink-0 flex-col border-r">
        <PageHeader title="聊天" />
        <div className="min-h-0 flex-1 overflow-y-auto">
          {sessions?.map((session) => {
            const agent = actorById(session.agentId)
            return (
              <button
                key={session.id}
                onClick={() => navigate(`${p.chat}/${session.id}`)}
                className={cn(
                  'flex w-full items-center gap-2 border-b px-3 py-2.5 text-left text-sm hover:bg-muted/50',
                  session.id === activeSessionId && 'bg-muted',
                )}
              >
                <ActorAvatar actor={agent} size="sm" />
                <span className="min-w-0 flex-1 truncate">{session.title}</span>
                {session.unreadCount > 0 && (
                  <span className="rounded-full bg-primary px-1.5 text-[10px] text-primary-foreground">
                    {session.unreadCount}
                  </span>
                )}
              </button>
            )
          })}
        </div>
      </div>

      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        <PageHeader title={activeSession?.title ?? '请选择一个会话'} />
        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-4">
          {messages?.map((message) => {
            const isUser = message.authorType === 'user'
            const author = isUser ? actorById(currentUserId) : activeAgent
            return (
              <div key={message.id} className={cn('flex gap-2', isUser && 'flex-row-reverse')}>
                <ActorAvatar actor={author} size="sm" className="mt-0.5" />
                <div className={cn('max-w-[70%] space-y-1', isUser && 'items-end text-right')}>
                  <div
                    className={cn(
                      'rounded-lg px-3 py-2 text-sm',
                      isUser ? 'bg-primary text-primary-foreground' : 'bg-muted',
                    )}
                  >
                    {message.content}
                  </div>
                  <p className="text-[10px] text-muted-foreground">
                    {formatDistanceToNow(new Date(message.createdAt), { addSuffix: true, locale: zhCN })}
                  </p>
                </div>
              </div>
            )
          })}
          <div ref={bottomRef} />
        </div>
        {activeSessionId && (
          <form onSubmit={handleSend} className="flex items-center gap-2 border-t p-3">
            <Input
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              placeholder="给智能体发消息…"
              className="flex-1"
            />
            <Button
              type="submit"
              size="icon"
              aria-label="发送消息"
              disabled={!draft.trim() || sendMessage.isPending}
            >
              <Send className="size-4" />
            </Button>
          </form>
        )}
      </div>
    </div>
  )
}

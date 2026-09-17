import { formatDistanceToNow } from 'date-fns'
import { AtSign, Bell, MessageCircle, Sparkles, UserPlus } from 'lucide-react'
import { ActorAvatar } from '@/components/common/actor-avatar'
import { PageHeader } from '@/components/layout/page-header'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useInboxItems, useMarkAllInboxRead, useMarkInboxRead } from '@/features/inbox/api'
import { cn } from '@/lib/utils'
import { actorById } from '@/mocks/data/store'
import type { InboxItemType } from '@/mocks/data/types'

const TYPE_ICON: Record<InboxItemType, React.ComponentType<{ className?: string }>> = {
  mention: AtSign,
  assignment: Bell,
  comment: MessageCircle,
  invite: UserPlus,
  run_complete: Sparkles,
}

export function InboxPage({ slug }: { slug: string }) {
  const { data: items, isPending } = useInboxItems(slug)
  const markRead = useMarkInboxRead(slug)
  const markAllRead = useMarkAllInboxRead(slug)
  const unreadCount = items?.filter((i) => !i.read).length ?? 0

  return (
    <div className="flex h-full flex-col">
      <PageHeader
        title="Inbox"
        actions={
          unreadCount > 0 && (
            <Button variant="outline" size="sm" onClick={() => markAllRead.mutate()}>
              Mark all read
            </Button>
          )
        }
      />
      <div className="flex-1 overflow-y-auto">
        {isPending && (
          <div className="space-y-2 p-4">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-14 w-full" />
            ))}
          </div>
        )}
        {items?.length === 0 && <p className="p-8 text-center text-sm text-muted-foreground">You're all caught up.</p>}
        {items?.map((item) => {
          const Icon = TYPE_ICON[item.type]
          const actor = actorById(item.actorId)
          return (
            <button
              key={item.id}
              onClick={() => markRead.mutate({ id: item.id, read: !item.read })}
              className={cn(
                'flex w-full items-start gap-3 border-b px-4 py-3 text-left hover:bg-muted/50',
                !item.read && 'bg-primary/5',
              )}
            >
              <Icon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
              <ActorAvatar actor={actor} size="sm" />
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">{item.title}</p>
                <p className="truncate text-xs text-muted-foreground">{item.body}</p>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <span className="text-xs text-muted-foreground">
                  {formatDistanceToNow(new Date(item.createdAt), { addSuffix: true })}
                </span>
                {!item.read && <span className="size-2 rounded-full bg-primary" />}
              </div>
            </button>
          )
        })}
      </div>
    </div>
  )
}

import { Bot } from 'lucide-react'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { cn } from '@/lib/utils'
import type { Agent, User } from '@/mocks/data/types'

const SIZE_CLASSES = {
  sm: 'size-5 text-[10px]',
  md: 'size-7 text-xs',
  lg: 'size-10 text-sm',
} as const

export function ActorAvatar({
  actor,
  size = 'md',
  className,
}: {
  actor: User | Agent | undefined
  size?: keyof typeof SIZE_CLASSES
  className?: string
}) {
  if (!actor) {
    return (
      <Avatar className={cn(SIZE_CLASSES[size], className)}>
        <AvatarFallback className="bg-muted text-muted-foreground">?</AvatarFallback>
      </Avatar>
    )
  }
  return (
    <Avatar className={cn(SIZE_CLASSES[size], className)}>
      <AvatarFallback
        className="font-medium text-white"
        style={{ backgroundColor: actor.avatarColor }}
      >
        {actor.type === 'agent' ? <Bot className="size-[60%]" /> : actor.initials}
      </AvatarFallback>
    </Avatar>
  )
}

import type { ReactNode } from 'react'
import { Bot, Users } from 'lucide-react'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { cn } from '@/lib/utils'

const SIZE_CLASSES = {
  sm: 'size-5 text-[10px]',
  md: 'size-7 text-xs',
  lg: 'size-10 text-sm',
} as const

/** Minimal actor shape: mock users and real cloud users both satisfy it. */
export interface AvatarActor {
  name?: string
  type?: 'user' | 'agent' | 'team'
  avatarColor?: string
  initials?: string
}

const PALETTE = [
  '#f97316',
  '#f43f5e',
  '#8b5cf6',
  '#3b82f6',
  '#10b981',
  '#eab308',
  '#06b6d4',
  '#ec4899',
] as const

/** Deterministic color for actors that only carry a name (real cloud users). */
function colorFor(name: string): string {
  let hash = 0
  for (let i = 0; i < name.length; i++) hash = (hash * 31 + name.charCodeAt(i)) >>> 0
  return PALETTE[hash % PALETTE.length] ?? PALETTE[0]
}

function initialsOf(name: string): string {
  return name
    .trim()
    .split(/\s+/)
    .slice(0, 2)
    .map((p) => p[0]?.toUpperCase() ?? '')
    .join('')
}

export function ActorAvatar({
  actor,
  size = 'md',
  className,
}: {
  actor: AvatarActor | undefined
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
  const type = actor.type ?? 'user'
  const name = actor.name ?? ''
  let fallback: ReactNode
  if (type === 'agent') fallback = <Bot className="size-[60%]" />
  else if (type === 'team') fallback = <Users className="size-[60%]" />
  else fallback = actor.initials ?? initialsOf(name)
  return (
    <Avatar className={cn(SIZE_CLASSES[size], className)}>
      <AvatarFallback
        className="font-medium text-white"
        style={{ backgroundColor: actor.avatarColor ?? colorFor(name) }}
      >
        {fallback}
      </AvatarFallback>
    </Avatar>
  )
}

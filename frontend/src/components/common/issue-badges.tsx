import {
  ArrowUp,
  Ban,
  Circle,
  CircleCheck,
  CircleDashed,
  CircleDot,
  Minus,
  SignalHigh,
  SignalLow,
  SignalMedium,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import type { IssuePriority, IssueStatus } from '@/mocks/data/types'

const STATUS_META: Record<IssueStatus, { label: string; className: string; icon: React.ComponentType<{ className?: string }> }> = {
  backlog: { label: 'Backlog', className: 'text-muted-foreground', icon: CircleDashed },
  todo: { label: 'Todo', className: 'text-muted-foreground', icon: Circle },
  in_progress: { label: 'In Progress', className: 'text-amber-500', icon: CircleDot },
  in_review: { label: 'In Review', className: 'text-violet-500', icon: CircleDot },
  done: { label: 'Done', className: 'text-emerald-500', icon: CircleCheck },
  cancelled: { label: 'Cancelled', className: 'text-muted-foreground', icon: Ban },
}

export function StatusIcon({ status, className }: { status: IssueStatus; className?: string }) {
  const meta = STATUS_META[status]
  const Icon = meta.icon
  return <Icon className={cn('size-4', meta.className, className)} />
}

export function StatusLabel({ status }: { status: IssueStatus }) {
  return <span className="text-sm">{STATUS_META[status].label}</span>
}

const PRIORITY_META: Record<IssuePriority, { label: string; icon: React.ComponentType<{ className?: string }>; className: string }> = {
  none: { label: 'No priority', icon: Minus, className: 'text-muted-foreground' },
  low: { label: 'Low', icon: SignalLow, className: 'text-muted-foreground' },
  medium: { label: 'Medium', icon: SignalMedium, className: 'text-foreground' },
  high: { label: 'High', icon: SignalHigh, className: 'text-foreground' },
  urgent: { label: 'Urgent', icon: ArrowUp, className: 'text-orange-500' },
}

export function PriorityIcon({ priority, className }: { priority: IssuePriority; className?: string }) {
  const meta = PRIORITY_META[priority]
  const Icon = meta.icon
  return <Icon className={cn('size-4', meta.className, className)} />
}

export function PriorityLabel({ priority }: { priority: IssuePriority }) {
  return <span className="text-sm">{PRIORITY_META[priority].label}</span>
}

export const STATUS_ORDER: IssueStatus[] = ['backlog', 'todo', 'in_progress', 'in_review', 'done', 'cancelled']

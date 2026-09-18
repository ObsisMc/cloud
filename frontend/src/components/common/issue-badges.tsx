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

const STATUS_META: Record<
  IssueStatus,
  { label: string; className: string; icon: React.ComponentType<{ className?: string }> }
> = {
  backlog: { label: '待规划', className: 'text-muted-foreground', icon: CircleDashed },
  todo: { label: '待办', className: 'text-muted-foreground', icon: Circle },
  in_progress: { label: '进行中', className: 'text-amber-500', icon: CircleDot },
  in_review: { label: '审核中', className: 'text-violet-500', icon: CircleDot },
  blocked: { label: '已阻塞', className: 'text-red-500', icon: Ban },
  done: { label: '已完成', className: 'text-emerald-500', icon: CircleCheck },
}

export function StatusIcon({ status, className }: { status: IssueStatus; className?: string }) {
  const meta = STATUS_META[status]
  const Icon = meta.icon
  return <Icon className={cn('size-4', meta.className, className)} />
}

export function StatusLabel({ status }: { status: IssueStatus }) {
  return <span className="text-sm">{STATUS_META[status].label}</span>
}

export function statusLabelText(status: IssueStatus): string {
  return STATUS_META[status].label
}

const PRIORITY_META: Record<
  IssuePriority,
  { label: string; icon: React.ComponentType<{ className?: string }>; className: string }
> = {
  none: { label: '无优先级', icon: Minus, className: 'text-muted-foreground' },
  low: { label: '低', icon: SignalLow, className: 'text-muted-foreground' },
  medium: { label: '中', icon: SignalMedium, className: 'text-foreground' },
  high: { label: '高', icon: SignalHigh, className: 'text-foreground' },
  urgent: { label: '紧急', icon: ArrowUp, className: 'text-orange-500' },
}

export function PriorityIcon({
  priority,
  className,
}: {
  priority: IssuePriority
  className?: string
}) {
  const meta = PRIORITY_META[priority]
  const Icon = meta.icon
  return <Icon className={cn('size-4', meta.className, className)} />
}

export function PriorityLabel({ priority }: { priority: IssuePriority }) {
  return <span className="text-sm">{PRIORITY_META[priority].label}</span>
}

export function priorityLabelText(priority: IssuePriority): string {
  return PRIORITY_META[priority].label
}

export const STATUS_ORDER: IssueStatus[] = [
  'backlog',
  'todo',
  'in_progress',
  'in_review',
  'blocked',
  'done',
]
export const PRIORITY_ORDER: IssuePriority[] = ['none', 'low', 'medium', 'high', 'urgent']

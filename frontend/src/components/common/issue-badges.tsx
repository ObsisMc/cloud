import {
  ArrowUp,
  Ban,
  Circle,
  CircleCheck,
  CircleDashed,
  CircleDot,
  CircleSlash,
  Minus,
  SignalHigh,
  SignalLow,
  SignalMedium,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import type { IssuePriority } from '@/features/issues/types'

type StatusMeta = {
  label: string
  className: string
  icon: React.ComponentType<{ className?: string }>
}

/**
 * Labels/icons for the 7 canonical system status keys. Custom tenant statuses have no
 * fixed presentation here, so they fall back to the raw key; the live catalog
 * (`/issue-statuses`) is what the board actually renders, this only prettifies known keys.
 */
const STATUS_META: Record<string, StatusMeta> = {
  backlog: { label: '待规划', className: 'text-muted-foreground', icon: CircleDashed },
  todo: { label: '待办', className: 'text-muted-foreground', icon: Circle },
  in_progress: { label: '进行中', className: 'text-amber-500', icon: CircleDot },
  in_review: { label: '审核中', className: 'text-violet-500', icon: CircleDot },
  blocked: { label: '已阻塞', className: 'text-red-500', icon: Ban },
  done: { label: '已完成', className: 'text-emerald-500', icon: CircleCheck },
  cancelled: { label: '已取消', className: 'text-muted-foreground', icon: CircleSlash },
}

const UNKNOWN_STATUS: StatusMeta = { label: '', className: 'text-muted-foreground', icon: Circle }

function statusMeta(status: string): StatusMeta {
  return STATUS_META[status] ?? { ...UNKNOWN_STATUS, label: status }
}

export function StatusIcon({ status, className }: { status: string; className?: string }) {
  const meta = statusMeta(status)
  const Icon = meta.icon
  return <Icon className={cn('size-4', meta.className, className)} />
}

export function statusLabelText(status: string): string {
  return statusMeta(status).label
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

export function priorityLabelText(priority: IssuePriority): string {
  return PRIORITY_META[priority].label
}

export const PRIORITY_ORDER: IssuePriority[] = ['none', 'low', 'medium', 'high', 'urgent']

/** Converts untrusted select input into a known issue priority. */
export function parseIssuePriority(value: unknown): IssuePriority | undefined {
  return PRIORITY_ORDER.find((priority) => priority === value)
}

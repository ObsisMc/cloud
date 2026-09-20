import { useSortable } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { Link } from 'react-router-dom'
import { ActorAvatar } from '@/components/common/actor-avatar'
import { PriorityIcon, StatusIcon } from '@/components/common/issue-badges'
import { cn } from '@/lib/utils'
import { workspacePaths } from '@/lib/paths'
import { assigneeName, assigneeType, columnLabel, issueNumber } from '@/features/issues/present'
import type { Issue, IssueStatusColumn } from '@/features/issues/types'

/** Stable empty catalog so the status-label fallback doesn't allocate a fresh array each render. */
const NO_STATUSES: readonly IssueStatusColumn[] = []

export function IssueCard({
  issue,
  slug,
  members,
  draggable = true,
  statuses = NO_STATUSES,
  subCount = 0,
}: {
  issue: Issue
  slug: string
  members: ReadonlyMap<string, string>
  /** DragOverlay renders its own static copy — disable dragging on that one. */
  draggable?: boolean
  /** Status catalog, used to resolve a custom column's display name for the status label. */
  statuses?: readonly IssueStatusColumn[]
  /** Number of direct sub-issues; shown as a badge so a parent reads at a glance. */
  subCount?: number
}) {
  const p = workspacePaths(slug)
  const type = assigneeType(issue)
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: issue.id,
    disabled: !draggable,
    attributes: { role: 'link', roleDescription: 'draggable issue card' },
  })

  return (
    <Link
      ref={setNodeRef}
      to={p.issueDetail(issue.id)}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={cn(
        'block rounded-md border bg-card p-2.5 text-sm shadow-xs touch-none hover:border-ring/50 hover:shadow-sm',
        isDragging && 'opacity-40',
      )}
      {...attributes}
      {...listeners}
    >
      <div className="mb-1.5 flex items-center gap-1.5 text-xs text-muted-foreground">
        <PriorityIcon priority={issue.priority} className="size-3.5" />
        <span>{issueNumber(issue)}</span>
        {subCount > 0 && (
          <span className="ml-auto rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
            {subCount} 子任务
          </span>
        )}
      </div>
      <p className="mb-2 line-clamp-2 font-medium">{issue.title}</p>
      <div className="flex items-center justify-between gap-2 text-xs">
        <span className="flex min-w-0 shrink-0 items-center gap-1">
          <StatusIcon status={issue.status} className="size-3.5" />
          <span className="truncate">{columnLabel(statuses, issue.status)}</span>
        </span>
        <span className="flex min-w-0 items-center gap-1.5 text-muted-foreground">
          {type && (
            <>
              <ActorAvatar actor={{ name: assigneeName(issue, members), type }} size="sm" />
              <span className="truncate">{assigneeName(issue, members)}</span>
            </>
          )}
          <span className="shrink-0 text-[10px]">
            {formatDistanceToNow(new Date(issue.updatedAt), { addSuffix: true, locale: zhCN })}
          </span>
        </span>
      </div>
    </Link>
  )
}
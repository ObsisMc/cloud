import { useDraggable } from '@dnd-kit/core'
import { CSS } from '@dnd-kit/utilities'
import { formatDistanceToNow } from 'date-fns'
import { FolderClosed } from 'lucide-react'
import { Link } from 'react-router-dom'
import { ActorAvatar } from '@/components/common/actor-avatar'
import { PriorityIcon } from '@/components/common/issue-badges'
import { cn } from '@/lib/utils'
import { workspacePaths } from '@/lib/paths'
import { actorById, db } from '@/mocks/data/store'
import type { Issue } from '@/mocks/data/types'

export function IssueCard({
  issue,
  slug,
  draggable = true,
}: {
  issue: Issue
  slug: string
  /** DragOverlay renders its own static copy — disable dragging on that one. */
  draggable?: boolean
}) {
  const p = workspacePaths(slug)
  const assignee = actorById(issue.assigneeId)
  const project = db.projects.find((pr) => pr.id === issue.projectId)
  const { attributes, listeners, setNodeRef, transform, isDragging } = useDraggable({
    id: issue.id,
    disabled: !draggable,
    // The card is a real <a href>, not a button — keep that semantic instead
    // of dnd-kit's default role="button" override.
    attributes: { role: 'link', roleDescription: 'draggable issue card' },
  })

  return (
    <Link
      ref={setNodeRef}
      to={p.issueDetail(issue.id)}
      style={transform ? { transform: CSS.Translate.toString(transform) } : undefined}
      className={cn(
        'block rounded-md border bg-card p-2.5 text-sm shadow-xs touch-none hover:border-ring/50 hover:shadow-sm',
        isDragging && 'z-10 opacity-40',
      )}
      {...attributes}
      {...listeners}
    >
      <div className="mb-1.5 flex items-center gap-1.5 text-xs text-muted-foreground">
        <PriorityIcon priority={issue.priority} className="size-3.5" />
        <span>{issue.identifier}</span>
      </div>
      <p className="mb-2 line-clamp-2 font-medium">{issue.title}</p>
      {project && (
        <div className="mb-2 flex items-center gap-1 text-xs text-muted-foreground">
          <FolderClosed className="size-3" />
          <span className="truncate">{project.title}</span>
        </div>
      )}
      <div className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-1.5">
          <ActorAvatar actor={assignee} size="sm" />
          <span className="truncate text-xs text-muted-foreground">{assignee?.name ?? 'Unassigned'}</span>
        </div>
        <span className="shrink-0 text-[10px] text-muted-foreground">
          {formatDistanceToNow(new Date(issue.updatedAt), { addSuffix: true })}
        </span>
      </div>
    </Link>
  )
}

import { formatDistanceToNow } from 'date-fns'
import { FolderClosed } from 'lucide-react'
import { Link } from 'react-router-dom'
import { ActorAvatar } from '@/components/common/actor-avatar'
import { PriorityIcon } from '@/components/common/issue-badges'
import { workspacePaths } from '@/lib/paths'
import { actorById, db } from '@/mocks/data/store'
import type { Issue } from '@/mocks/data/types'

export function IssueCard({ issue, slug }: { issue: Issue; slug: string }) {
  const p = workspacePaths(slug)
  const assignee = actorById(issue.assigneeId)
  const project = db.projects.find((pr) => pr.id === issue.projectId)

  return (
    <Link
      to={p.issueDetail(issue.id)}
      className="block rounded-md border bg-card p-2.5 text-sm shadow-xs hover:border-ring/50 hover:shadow-sm"
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

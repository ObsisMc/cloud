import { Link } from 'react-router-dom'
import { ActorAvatar } from '@/components/common/actor-avatar'
import { PriorityIcon, StatusIcon } from '@/components/common/issue-badges'
import { Badge } from '@/components/ui/badge'
import { workspacePaths } from '@/lib/paths'
import { actorById, db } from '@/mocks/data/store'
import type { Issue } from '@/mocks/data/types'

export function IssueRow({ issue, slug }: { issue: Issue; slug: string }) {
  const p = workspacePaths(slug)
  const assignee = actorById(issue.assigneeId)
  const project = db.projects.find((pr) => pr.id === issue.projectId)

  return (
    <Link
      to={p.issueDetail(issue.id)}
      className="flex items-center gap-3 border-b px-4 py-2.5 text-sm hover:bg-muted/50"
    >
      <PriorityIcon priority={issue.priority} className="shrink-0" />
      <span className="w-16 shrink-0 text-xs text-muted-foreground">{issue.identifier}</span>
      <StatusIcon status={issue.status} className="shrink-0" />
      <span className="min-w-0 flex-1 truncate">{issue.title}</span>
      {issue.labels.slice(0, 2).map((label) => (
        <Badge key={label} variant="secondary" className="hidden shrink-0 sm:inline-flex">
          {label}
        </Badge>
      ))}
      {project && (
        <span className="hidden shrink-0 truncate text-xs text-muted-foreground sm:inline">{project.title}</span>
      )}
      <ActorAvatar actor={assignee} size="sm" className="shrink-0" />
    </Link>
  )
}

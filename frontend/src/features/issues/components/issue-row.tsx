import { Link, useParams } from 'react-router-dom'
import { ActorAvatar } from '@/components/common/actor-avatar'
import { PriorityIcon, StatusIcon } from '@/components/common/issue-badges'
import { Badge } from '@/components/ui/badge'
import { workspacePaths } from '@/lib/paths'
import { assigneeName, assigneeType, issueNumber } from '@/features/issues/present'
import type { Issue } from '@/features/issues/types'

export function IssueRow({
  issue,
  slug,
  members,
}: {
  issue: Issue
  slug: string
  members: ReadonlyMap<string, string>
}) {
  // The `slug` prop is the tenant id (forwarded by CloudScope); nav links must
  // carry the space slug from the route.
  const { workspaceSlug } = useParams<{ workspaceSlug: string }>()
  const p = workspacePaths(workspaceSlug ?? slug)
  const type = assigneeType(issue)

  return (
    <Link
      to={p.issueDetail(issue.id)}
      className="flex items-center gap-3 border-b px-4 py-2.5 text-sm hover:bg-muted/50"
    >
      <PriorityIcon priority={issue.priority} className="shrink-0" />
      <span className="w-16 shrink-0 text-xs text-muted-foreground">{issueNumber(issue)}</span>
      <StatusIcon status={issue.status} className="shrink-0" />
      <span className="min-w-0 flex-1 truncate">{issue.title}</span>
      {issue.labels.slice(0, 2).map((label) => (
        <Badge key={label.id} variant="secondary" className="hidden shrink-0 sm:inline-flex">
          {label.name}
        </Badge>
      ))}
      <ActorAvatar
        actor={{ name: assigneeName(issue, members), type: type ?? 'user' }}
        size="sm"
        className="shrink-0"
      />
    </Link>
  )
}

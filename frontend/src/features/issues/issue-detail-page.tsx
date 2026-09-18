import { MessageSquare, Trash2 } from 'lucide-react'
import { useNavigate, useParams } from 'react-router-dom'
import { ActorAvatar } from '@/components/common/actor-avatar'
import { PriorityIcon, StatusIcon } from '@/components/common/issue-badges'
import { PageHeader } from '@/components/layout/page-header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { useDeleteIssue, useIssue, useUpdateIssue } from '@/features/issues/api'
import { workspacePaths } from '@/lib/paths'
import { actorById, db } from '@/mocks/data/store'
import type { IssuePriority, IssueStatus } from '@/mocks/data/types'

const STATUS_OPTIONS: IssueStatus[] = ['backlog', 'todo', 'in_progress', 'in_review', 'blocked', 'done']
const PRIORITY_OPTIONS: IssuePriority[] = ['none', 'low', 'medium', 'high', 'urgent']

export function IssueDetailPage({ slug }: { slug: string }) {
  const { issueId } = useParams<{ issueId: string }>()
  const navigate = useNavigate()
  const { data: issue, isPending } = useIssue(slug, issueId)
  const updateIssue = useUpdateIssue(slug)
  const deleteIssue = useDeleteIssue(slug)
  const p = workspacePaths(slug)

  if (isPending || !issue) {
    return (
      <div className="flex h-full flex-col">
        <PageHeader title="Issue" breadcrumb={{ label: 'Issues', to: p.issues }} />
        <div className="space-y-3 p-6">
          <Skeleton className="h-6 w-2/3" />
          <Skeleton className="h-24 w-full" />
        </div>
      </div>
    )
  }

  const assignee = actorById(issue.assigneeId)
  const project = db.projects.find((pr) => pr.id === issue.projectId)

  return (
    <div className="flex h-full flex-col">
      <PageHeader
        title={issue.identifier}
        breadcrumb={{ label: 'Issues', to: p.issues }}
        actions={
          <Button
            variant="ghost"
            size="icon"
            onClick={() => deleteIssue.mutate(issue.id, { onSuccess: () => navigate(p.issues) })}
          >
            <Trash2 className="size-4" />
          </Button>
        }
      />
      <div className="flex flex-1 flex-col gap-6 overflow-y-auto p-6 md:flex-row">
        <div className="min-w-0 flex-1 space-y-4">
          <h1 className="text-xl font-semibold">{issue.title}</h1>
          <p className="whitespace-pre-wrap text-sm text-muted-foreground">
            {issue.description || 'No description.'}
          </p>
          <div className="flex items-center gap-1.5 pt-4 text-sm text-muted-foreground">
            <MessageSquare className="size-4" />
            {issue.commentCount} comments
          </div>
        </div>
        <div className="w-full shrink-0 space-y-4 md:w-64">
          <div className="space-y-1.5">
            <p className="text-xs font-medium text-muted-foreground">Status</p>
            <Select
              value={issue.status}
              onValueChange={(v) => updateIssue.mutate({ id: issue.id, patch: { status: v as IssueStatus } })}
            >
              <SelectTrigger className="w-full">
                <span className="flex items-center gap-2">
                  <StatusIcon status={issue.status} />
                  <SelectValue>{(value: unknown) => String(value).replace('_', ' ')}</SelectValue>
                </span>
              </SelectTrigger>
              <SelectContent>
                {STATUS_OPTIONS.map((s) => (
                  <SelectItem key={s} value={s}>{s.replace('_', ' ')}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <p className="text-xs font-medium text-muted-foreground">Priority</p>
            <Select
              value={issue.priority}
              onValueChange={(v) => updateIssue.mutate({ id: issue.id, patch: { priority: v as IssuePriority } })}
            >
              <SelectTrigger className="w-full">
                <span className="flex items-center gap-2">
                  <PriorityIcon priority={issue.priority} />
                  <SelectValue />
                </span>
              </SelectTrigger>
              <SelectContent>
                {PRIORITY_OPTIONS.map((pr) => (
                  <SelectItem key={pr} value={pr}>{pr}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <p className="text-xs font-medium text-muted-foreground">Assignee</p>
            <div className="flex items-center gap-2 rounded-md border px-2.5 py-1.5 text-sm">
              <ActorAvatar actor={assignee} size="sm" />
              {assignee?.name ?? 'Unassigned'}
            </div>
          </div>
          {project && (
            <div className="space-y-1.5">
              <p className="text-xs font-medium text-muted-foreground">Project</p>
              <p className="text-sm">{project.title}</p>
            </div>
          )}
          {issue.labels.length > 0 && (
            <div className="space-y-1.5">
              <p className="text-xs font-medium text-muted-foreground">Labels</p>
              <div className="flex flex-wrap gap-1.5">
                {issue.labels.map((l) => (
                  <Badge key={l} variant="secondary">{l}</Badge>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

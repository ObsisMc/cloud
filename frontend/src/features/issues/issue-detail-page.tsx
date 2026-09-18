import { MessageSquare, Trash2 } from 'lucide-react'
import { useNavigate, useParams } from 'react-router-dom'
import { ActorAvatar } from '@/components/common/actor-avatar'
import {
  PRIORITY_ORDER,
  PriorityIcon,
  STATUS_ORDER,
  StatusIcon,
  priorityLabelText,
  statusLabelText,
} from '@/components/common/issue-badges'
import { PageHeader } from '@/components/layout/page-header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { useDeleteIssue, useIssue, useUpdateIssue } from '@/features/issues/api'
import { workspacePaths } from '@/lib/paths'
import { actorById, db } from '@/mocks/data/store'
import type { IssuePriority, IssueStatus } from '@/mocks/data/types'

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
        <PageHeader title="任务" breadcrumb={{ label: '任务', to: p.issues }} />
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
        breadcrumb={{ label: '任务', to: p.issues }}
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
            {issue.description || '暂无描述。'}
          </p>
          <div className="flex items-center gap-1.5 pt-4 text-sm text-muted-foreground">
            <MessageSquare className="size-4" />
            {issue.commentCount} 条评论
          </div>
        </div>
        <div className="w-full shrink-0 space-y-4 md:w-64">
          <div className="space-y-1.5">
            <p className="text-xs font-medium text-muted-foreground">状态</p>
            <Select
              value={issue.status}
              onValueChange={(v) => updateIssue.mutate({ id: issue.id, patch: { status: v as IssueStatus } })}
            >
              <SelectTrigger className="w-full">
                <span className="flex items-center gap-2">
                  <StatusIcon status={issue.status} />
                  <SelectValue>{(value: unknown) => statusLabelText(value as IssueStatus)}</SelectValue>
                </span>
              </SelectTrigger>
              <SelectContent>
                {STATUS_ORDER.map((s) => (
                  <SelectItem key={s} value={s}>{statusLabelText(s)}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <p className="text-xs font-medium text-muted-foreground">优先级</p>
            <Select
              value={issue.priority}
              onValueChange={(v) => updateIssue.mutate({ id: issue.id, patch: { priority: v as IssuePriority } })}
            >
              <SelectTrigger className="w-full">
                <span className="flex items-center gap-2">
                  <PriorityIcon priority={issue.priority} />
                  <SelectValue>{(value: unknown) => priorityLabelText(value as IssuePriority)}</SelectValue>
                </span>
              </SelectTrigger>
              <SelectContent>
                {PRIORITY_ORDER.map((pr) => (
                  <SelectItem key={pr} value={pr}>{priorityLabelText(pr)}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <p className="text-xs font-medium text-muted-foreground">负责人</p>
            <div className="flex items-center gap-2 rounded-md border px-2.5 py-1.5 text-sm">
              <ActorAvatar actor={assignee} size="sm" />
              {assignee?.name ?? '未分配'}
            </div>
          </div>
          {project && (
            <div className="space-y-1.5">
              <p className="text-xs font-medium text-muted-foreground">项目</p>
              <p className="text-sm">{project.title}</p>
            </div>
          )}
          {issue.labels.length > 0 && (
            <div className="space-y-1.5">
              <p className="text-xs font-medium text-muted-foreground">标签</p>
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

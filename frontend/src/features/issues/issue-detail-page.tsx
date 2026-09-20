import { Trash2 } from 'lucide-react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ActorAvatar } from '@/components/common/actor-avatar'
import {
  PRIORITY_ORDER,
  PriorityIcon,
  StatusIcon,
  priorityLabelText,
  parseIssuePriority,
} from '@/components/common/issue-badges'
import { PageHeader } from '@/components/layout/page-header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { ActivityPanel } from '@/features/issues/components/activity-panel'
import { ContextRefsPanel } from '@/features/issues/components/context-refs-panel'
import {
  useDeleteIssue,
  useIssue,
  useIssues,
  useIssueStatuses,
  useMembers,
  useUpdateIssue,
  type UpdateIssueInput,
} from '@/features/issues/api'
import {
  assigneeName,
  assigneeType,
  columnLabel,
  issueNumber,
  memberNameById,
  orderedColumns,
} from '@/features/issues/present'
import { workspacePaths } from '@/lib/paths'
import type { Issue } from '@/features/issues/types'

function PropertyLabel({ children }: { children: React.ReactNode }) {
  return <p className="text-xs font-medium text-muted-foreground">{children}</p>
}

function StatusField({
  issue,
  statuses,
  onCommit,
}: {
  issue: Issue
  statuses: ReturnType<typeof useIssueStatuses>['data']
  onCommit: (patch: UpdateIssueInput) => void
}) {
  const keys = orderedColumns(statuses ?? [], [issue.status])
  return (
    <div className="space-y-1.5">
      <PropertyLabel>状态</PropertyLabel>
      <Select
        value={issue.status}
        onValueChange={(v) => {
          if (v !== null) onCommit({ status: v })
        }}
      >
        <SelectTrigger className="w-full">
          <span className="flex items-center gap-2">
            <StatusIcon status={issue.status} />
            <SelectValue>
              {(value: unknown) => columnLabel(statuses ?? [], typeof value === 'string' ? value : issue.status)}
            </SelectValue>
          </span>
        </SelectTrigger>
        <SelectContent>
          {keys.map((key) => (
            <SelectItem key={key} value={key}>
              {columnLabel(statuses ?? [], key)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}

function PriorityField({
  issue,
  onCommit,
}: {
  issue: Issue
  onCommit: (patch: UpdateIssueInput) => void
}) {
  return (
    <div className="space-y-1.5">
      <PropertyLabel>优先级</PropertyLabel>
      <Select
        value={issue.priority}
        onValueChange={(v) => {
          const next = parseIssuePriority(v)
          if (next !== undefined) onCommit({ priority: next })
        }}
      >
        <SelectTrigger className="w-full">
          <span className="flex items-center gap-2">
            <PriorityIcon priority={issue.priority} />
            <SelectValue>
              {(value: unknown) => priorityLabelText(parseIssuePriority(value) ?? issue.priority)}
            </SelectValue>
          </span>
        </SelectTrigger>
        <SelectContent>
          {PRIORITY_ORDER.map((pr) => (
            <SelectItem key={pr} value={pr}>
              {priorityLabelText(pr)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}

function ParentField({
  issue,
  issues,
  onCommit,
}: {
  issue: Issue
  issues: Issue[]
  onCommit: (patch: UpdateIssueInput) => void
}) {
  const candidates = issues.filter((i) => i.id !== issue.id)
  return (
    <div className="space-y-1.5">
      <PropertyLabel>父任务</PropertyLabel>
      <Select
        value={issue.parentIssueId ?? 'none'}
        onValueChange={(v) => {
          if (v !== null) onCommit({ parentIssueId: v === 'none' ? '' : v })
        }}
      >
        <SelectTrigger className="w-full">
          <SelectValue placeholder="无父任务">
            {(value: unknown) =>
              value === 'none' || value == null
                ? '无父任务'
                : (issues.find((i) => i.id === value)?.title ?? '父任务')
            }
          </SelectValue>
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="none">无父任务</SelectItem>
          {candidates.map((i) => (
            <SelectItem key={i.id} value={i.id}>
              {issueNumber(i)} {i.title}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}

// oxlint-disable-next-line max-lines-per-function -- the detail screen keeps its issue controls and two-column layout transaction together.
export function IssueDetailPage({ slug }: { slug: string }) {
  const { issueId } = useParams<{ issueId: string }>()
  const navigate = useNavigate()
  const { data: issue, isPending } = useIssue(slug, issueId)
  const { data: statuses } = useIssueStatuses(slug)
  const { data: members = [] } = useMembers(slug)
  const { data: allIssues = [] } = useIssues(slug)
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

  const names = memberNameById(members)
  const type = assigneeType(issue)
  const subIssues = allIssues.filter((i) => i.parentIssueId === issue.id)
  const commit = (patch: UpdateIssueInput) =>
    updateIssue.mutate({ id: issue.id, version: issue.version, patch })

  return (
    <div className="flex h-full flex-col">
      <PageHeader
        title={issueNumber(issue)}
        breadcrumb={{ label: '任务', to: p.issues }}
        actions={
          <Button
            variant="ghost"
            size="icon"
            onClick={() =>
              deleteIssue.mutate(
                { id: issue.id, version: issue.version },
                { onSuccess: () => navigate(p.issues) },
              )
            }
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
          <ActivityPanel slug={slug} issueId={issue.id} members={members} />
        </div>
        <div className="w-full shrink-0 space-y-4 md:w-64">
          <StatusField issue={issue} statuses={statuses} onCommit={commit} />
          <PriorityField issue={issue} onCommit={commit} />
          <div className="space-y-1.5">
            <PropertyLabel>负责人</PropertyLabel>
            <div className="flex items-center gap-2 rounded-md border px-2.5 py-1.5 text-sm">
              <ActorAvatar actor={type ? { name: assigneeName(issue, names), type } : undefined} size="sm" />
              <span className="truncate">{assigneeName(issue, names)}</span>
              {type === 'agent' || type === 'team' ? (
                <span className="shrink-0 text-xs text-muted-foreground">暂不可用</span>
              ) : null}
            </div>
          </div>
          <ParentField issue={issue} issues={allIssues} onCommit={commit} />
          <div className="space-y-1.5">
            <PropertyLabel>子任务</PropertyLabel>
            {subIssues.length === 0 ? (
              <p className="text-sm text-muted-foreground">暂无子任务</p>
            ) : (
              <ul className="space-y-1">
                {subIssues.map((sub) => (
                  <li key={sub.id}>
                    <Link
                      to={p.issueDetail(sub.id)}
                      className="flex items-center gap-1.5 text-sm hover:underline"
                    >
                      <span className="shrink-0 text-xs text-muted-foreground">
                        {issueNumber(sub)}
                      </span>
                      <span className="truncate">{sub.title}</span>
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </div>
          <div className="space-y-1.5">
            <PropertyLabel>项目</PropertyLabel>
            {issue.projectRef ? (
              <div className="text-sm">
                <p className="text-muted-foreground">项目详情暂不可用</p>
                <p className="truncate font-mono text-xs text-muted-foreground">{issue.projectRef}</p>
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">未关联项目</p>
            )}
          </div>
          {issue.labels.length > 0 && (
            <div className="space-y-1.5">
              <PropertyLabel>标签</PropertyLabel>
              <div className="flex flex-wrap gap-1.5">
                {issue.labels.map((label) => (
                  <Badge key={label.id} variant="secondary">
                    {label.name}
                  </Badge>
                ))}
              </div>
            </div>
          )}
          {Object.keys(issue.properties).length > 0 && (
            <div className="space-y-1.5">
              <PropertyLabel>属性</PropertyLabel>
              <dl className="space-y-1 text-sm">
                {Object.entries(issue.properties).map(([key, value]) => (
                  <div key={key} className="flex justify-between gap-2">
                    <dt className="text-muted-foreground">{key}</dt>
                    <dd className="min-w-0 truncate">{String(value)}</dd>
                  </div>
                ))}
              </dl>
            </div>
          )}
          <div className="space-y-1.5">
            <PropertyLabel>上下文引用</PropertyLabel>
            <ContextRefsPanel slug={slug} issueId={issue.id} />
          </div>
          <div className="space-y-1.5">
            <PropertyLabel>执行</PropertyLabel>
            <Button variant="outline" size="sm" className="w-full" disabled>
              运行（暂不可用）
            </Button>
            <p className="text-xs text-muted-foreground">Agent / Team / Workflow 执行尚未接入。</p>
          </div>
        </div>
      </div>
    </div>
  )
}

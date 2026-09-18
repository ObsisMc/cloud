import { Plus } from 'lucide-react'
import { STATUS_ORDER, StatusIcon } from '@/components/common/issue-badges'
import { Button } from '@/components/ui/button'
import { CreateIssueDialog } from '@/features/issues/components/create-issue-dialog'
import { IssueCard } from '@/features/issues/components/issue-card'
import type { Issue, IssueStatus } from '@/mocks/data/types'

const STATUS_LABELS: Record<IssueStatus, string> = {
  backlog: 'Backlog',
  todo: 'Todo',
  in_progress: 'In Progress',
  in_review: 'In Review',
  blocked: 'Blocked',
  done: 'Done',
}

export function IssuesBoard({ issues, slug }: { issues: Issue[]; slug: string }) {
  return (
    <div className="flex h-full min-h-0 gap-3 overflow-x-auto p-3">
      {STATUS_ORDER.map((status) => {
        const group = issues.filter((i) => i.status === status)
        return (
          <div key={status} className="flex h-full w-72 shrink-0 flex-col rounded-lg bg-muted/40">
            <div className="flex items-center gap-2 px-2.5 py-2">
              <StatusIcon status={status} />
              <span className="text-sm font-medium">{STATUS_LABELS[status]}</span>
              <span className="text-xs text-muted-foreground">{group.length}</span>
              <CreateIssueDialog
                slug={slug}
                defaultStatus={status}
                trigger={
                  <Button variant="ghost" size="icon" className="ml-auto size-6" aria-label={`New issue in ${STATUS_LABELS[status]}`}>
                    <Plus className="size-3.5" />
                  </Button>
                }
              />
            </div>
            <div className="min-h-0 flex-1 space-y-2 overflow-y-auto px-2 pb-2">
              {group.length === 0 && (
                <p className="py-6 text-center text-xs text-muted-foreground">No issues</p>
              )}
              {group.map((issue) => (
                <IssueCard key={issue.id} issue={issue} slug={slug} />
              ))}
            </div>
          </div>
        )
      })}
    </div>
  )
}

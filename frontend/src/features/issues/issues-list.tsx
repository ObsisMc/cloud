import { LayoutGrid, List as ListIcon } from 'lucide-react'
import { useState } from 'react'
import { PageHeader } from '@/components/layout/page-header'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { STATUS_ORDER } from '@/components/common/issue-badges'
import { CreateIssueDialog } from '@/features/issues/components/create-issue-dialog'
import { IssueRow } from '@/features/issues/components/issue-row'
import { IssuesBoard } from '@/features/issues/issues-board'
import { useIssues } from '@/features/issues/api'
import { cn } from '@/lib/utils'
import type { Issue } from '@/mocks/data/types'

const STATUS_LABELS: Record<Issue['status'], string> = {
  backlog: 'Backlog',
  todo: 'Todo',
  in_progress: 'In Progress',
  in_review: 'In Review',
  blocked: 'Blocked',
  done: 'Done',
}

type ViewMode = 'board' | 'list'

export function IssuesList({
  slug,
  title,
  assigneeId,
}: {
  slug: string
  title: string
  assigneeId?: string
}) {
  const { data: issues, isPending } = useIssues(slug, assigneeId ? { assigneeId } : undefined)
  const [view, setView] = useState<ViewMode>('board')

  return (
    <div className="flex h-full flex-col">
      <PageHeader
        title={title}
        actions={
          <div className="flex items-center gap-2">
            <div className="flex items-center rounded-md border p-0.5">
              <Button
                variant={view === 'board' ? 'secondary' : 'ghost'}
                size="icon"
                className="size-6"
                aria-label="Board view"
                onClick={() => setView('board')}
              >
                <LayoutGrid className="size-3.5" />
              </Button>
              <Button
                variant={view === 'list' ? 'secondary' : 'ghost'}
                size="icon"
                className="size-6"
                aria-label="List view"
                onClick={() => setView('list')}
              >
                <ListIcon className="size-3.5" />
              </Button>
            </div>
            <CreateIssueDialog slug={slug} />
          </div>
        }
      />
      {isPending && (
        <div className="space-y-2 p-4">
          {Array.from({ length: 8 }).map((_, i) => (
            <Skeleton key={i} className="h-9 w-full" />
          ))}
        </div>
      )}
      {issues && issues.length === 0 && (
        <p className="p-8 text-center text-sm text-muted-foreground">No issues here yet.</p>
      )}
      {issues && issues.length > 0 && view === 'board' && (
        <div className="min-h-0 flex-1">
          <IssuesBoard issues={issues} slug={slug} />
        </div>
      )}
      {issues && issues.length > 0 && view === 'list' && (
        <div className={cn('min-h-0 flex-1 overflow-y-auto')}>
          {STATUS_ORDER.map((status) => {
            const group = issues.filter((i) => i.status === status)
            if (group.length === 0) return null
            return (
              <div key={status}>
                <div className="sticky top-0 flex items-center gap-2 border-b bg-background px-4 py-1.5 text-xs font-medium text-muted-foreground">
                  {STATUS_LABELS[status]}
                  <span className="text-muted-foreground/70">{group.length}</span>
                </div>
                {group.map((issue) => (
                  <IssueRow key={issue.id} issue={issue} slug={slug} />
                ))}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

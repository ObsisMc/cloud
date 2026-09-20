import { LayoutGrid, List as ListIcon } from 'lucide-react'
import { useState } from 'react'
import { PageHeader } from '@/components/layout/page-header'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { CreateIssueDialog } from '@/features/issues/components/create-issue-dialog'
import { IssueRow } from '@/features/issues/components/issue-row'
import { IssuesBoard } from '@/features/issues/issues-board'
import { useIssueStatuses, useIssues, useMembers } from '@/features/issues/api'
import { columnLabel, memberNameById, orderedColumns } from '@/features/issues/present'
import { cn } from '@/lib/utils'

type ViewMode = 'board' | 'list'
const SKELETON_KEYS = ['one', 'two', 'three', 'four', 'five', 'six', 'seven', 'eight']

export function IssuesList({
  slug,
  title,
  assigneeUserId,
}: {
  slug: string
  title: string
  /** When set, only issues assigned to this user are shown (client-side "my issues"). */
  assigneeUserId?: string
}) {
  const { data: issues, isPending } = useIssues(slug)
  const { data: statuses = [] } = useIssueStatuses(slug)
  const { data: members = [] } = useMembers(slug)
  const [view, setView] = useState<ViewMode>('board')

  const visible = assigneeUserId
    ? (issues ?? []).filter((i) => i.assigneeUserId === assigneeUserId)
    : (issues ?? [])
  const memberNames = memberNameById(members)
  const columns = orderedColumns(statuses, visible.map((i) => i.status))

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
                aria-label="看板视图"
                onClick={() => setView('board')}
              >
                <LayoutGrid className="size-3.5" />
              </Button>
              <Button
                variant={view === 'list' ? 'secondary' : 'ghost'}
                size="icon"
                className="size-6"
                aria-label="列表视图"
                onClick={() => setView('list')}
              >
                <ListIcon className="size-3.5" />
              </Button>
            </div>
            <CreateIssueDialog slug={slug} statuses={statuses} members={members} />
          </div>
        }
      />
      {isPending && (
        <div className="space-y-2 p-4">
          {SKELETON_KEYS.map((key) => (
            <Skeleton key={key} className="h-9 w-full" />
          ))}
        </div>
      )}
      {!isPending && visible.length === 0 && (
        <p className="p-8 text-center text-sm text-muted-foreground">暂无任务。</p>
      )}
      {!isPending && visible.length > 0 && view === 'board' && (
        <div className="min-h-0 flex-1">
          <IssuesBoard issues={visible} slug={slug} statuses={statuses} members={members} />
        </div>
      )}
      {!isPending && visible.length > 0 && view === 'list' && (
        <div className={cn('min-h-0 flex-1 overflow-y-auto')}>
          {columns.map((status) => {
            const group = visible.filter((i) => i.status === status)
            if (group.length === 0) return null
            return (
              <div key={status}>
                <div className="sticky top-0 flex items-center gap-2 border-b bg-background px-4 py-1.5 text-xs font-medium text-muted-foreground">
                  {columnLabel(statuses, status)}
                  <span className="text-muted-foreground/70">{group.length}</span>
                </div>
                {group.map((issue) => (
                  <IssueRow key={issue.id} issue={issue} slug={slug} members={memberNames} />
                ))}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

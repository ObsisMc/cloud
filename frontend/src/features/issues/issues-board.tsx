import {
  DndContext,
  DragOverlay,
  PointerSensor,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent,
} from '@dnd-kit/core'
import { Plus } from 'lucide-react'
import { useState } from 'react'
import { STATUS_ORDER, StatusIcon } from '@/components/common/issue-badges'
import { Button } from '@/components/ui/button'
import { CreateIssueDialog } from '@/features/issues/components/create-issue-dialog'
import { IssueCard } from '@/features/issues/components/issue-card'
import { useUpdateIssue } from '@/features/issues/api'
import { cn } from '@/lib/utils'
import type { Issue, IssueStatus } from '@/mocks/data/types'

const STATUS_LABELS: Record<IssueStatus, string> = {
  backlog: 'Backlog',
  todo: 'Todo',
  in_progress: 'In Progress',
  in_review: 'In Review',
  blocked: 'Blocked',
  done: 'Done',
}

/**
 * Pure decision for what a drag-end should do, kept separate from the
 * DndContext wiring so it can be unit tested without simulating real
 * pointer drags (dnd-kit needs real layout measurements jsdom can't give it).
 */
export function resolveDrop(
  issues: Issue[],
  activeId: string | number | undefined,
  overId: string | number | undefined,
): { id: string; status: IssueStatus } | null {
  if (activeId == null || overId == null) return null
  const issue = issues.find((i) => i.id === activeId)
  if (!issue) return null
  const newStatus = overId as IssueStatus
  if (issue.status === newStatus) return null
  return { id: issue.id, status: newStatus }
}

function BoardColumn({
  status,
  issues,
  slug,
}: {
  status: IssueStatus
  issues: Issue[]
  slug: string
}) {
  const { setNodeRef, isOver } = useDroppable({ id: status })

  return (
    <div
      ref={setNodeRef}
      className={cn('flex h-full w-72 shrink-0 flex-col rounded-lg bg-muted/40', isOver && 'ring-2 ring-primary/50')}
    >
      <div className="flex items-center gap-2 px-2.5 py-2">
        <StatusIcon status={status} />
        <span className="text-sm font-medium">{STATUS_LABELS[status]}</span>
        <span className="text-xs text-muted-foreground">{issues.length}</span>
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
        {issues.length === 0 && <p className="py-6 text-center text-xs text-muted-foreground">No issues</p>}
        {issues.map((issue) => (
          <IssueCard key={issue.id} issue={issue} slug={slug} />
        ))}
      </div>
    </div>
  )
}

export function IssuesBoard({ issues, slug }: { issues: Issue[]; slug: string }) {
  const updateIssue = useUpdateIssue(slug)
  const [activeIssue, setActiveIssue] = useState<Issue | null>(null)
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }))

  function handleDragStart(event: DragStartEvent) {
    setActiveIssue(issues.find((i) => i.id === event.active.id) ?? null)
  }

  function handleDragEnd(event: DragEndEvent) {
    setActiveIssue(null)
    const update = resolveDrop(issues, event.active.id, event.over?.id)
    if (update) {
      updateIssue.mutate({ id: update.id, patch: { status: update.status } })
    }
  }

  return (
    <DndContext sensors={sensors} onDragStart={handleDragStart} onDragEnd={handleDragEnd} onDragCancel={() => setActiveIssue(null)}>
      <div className="flex h-full min-h-0 gap-3 overflow-x-auto p-3">
        {STATUS_ORDER.map((status) => (
          <BoardColumn key={status} status={status} issues={issues.filter((i) => i.status === status)} slug={slug} />
        ))}
      </div>
      <DragOverlay>
        {activeIssue && (
          <div className="w-72 rotate-2 opacity-90">
            <IssueCard issue={activeIssue} slug={slug} draggable={false} />
          </div>
        )}
      </DragOverlay>
    </DndContext>
  )
}

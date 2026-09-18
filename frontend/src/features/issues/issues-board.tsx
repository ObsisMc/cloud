import {
  DndContext,
  DragOverlay,
  PointerSensor,
  closestCenter,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent,
} from '@dnd-kit/core'
import { SortableContext, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { Plus } from 'lucide-react'
import { useState } from 'react'
import { STATUS_ORDER, StatusIcon, statusLabelText } from '@/components/common/issue-badges'
import { Button } from '@/components/ui/button'
import { CreateIssueDialog } from '@/features/issues/components/create-issue-dialog'
import { IssueCard } from '@/features/issues/components/issue-card'
import { useUpdateIssue } from '@/features/issues/api'
import { cn } from '@/lib/utils'
import type { Issue, IssueStatus } from '@/mocks/data/types'

/**
 * Pure decision for what a drag-end should do, kept separate from the
 * DndContext wiring so it can be unit tested without simulating real
 * pointer drags (dnd-kit needs real layout measurements jsdom can't give it).
 *
 * `overId` is either a column's status (dropped on empty space) or another
 * issue's id (dropped near a card); `insertAfter` says which side of that
 * card the drop lands on. The returned `order` is a fractional value
 * between its new neighbors, so this never needs to renumber the rest of
 * the column — the card lands exactly where it was dropped instead of
 * wherever a recency sort would put it.
 */
export function resolveDrop({
  issues,
  activeId,
  overId,
  insertAfter,
}: {
  issues: Issue[]
  activeId: string | number | undefined
  overId: string | number | undefined
  insertAfter: boolean
}): { id: string; status: IssueStatus; order: number } | null {
  if (activeId == null || overId == null) return null
  const active = issues.find((i) => i.id === activeId)
  if (!active) return null

  const isColumnDrop = (STATUS_ORDER as (string | number)[]).includes(overId)
  const targetStatus = isColumnDrop ? (overId as IssueStatus) : issues.find((i) => i.id === overId)?.status
  if (!targetStatus) return null

  const column = issues
    .filter((i) => i.status === targetStatus && i.id !== activeId)
    .sort((a, b) => a.order - b.order)

  let index: number
  if (isColumnDrop) {
    index = column.length
  } else {
    const overIndex = column.findIndex((i) => i.id === overId)
    if (overIndex === -1) return null
    index = insertAfter ? overIndex + 1 : overIndex
  }

  const before = column[index - 1]?.order
  const after = column[index]?.order
  const newOrder = before == null ? (after == null ? 0 : after - 1) : after == null ? before + 1 : (before + after) / 2

  if (active.status === targetStatus && active.order === newOrder) return null
  return { id: active.id, status: targetStatus, order: newOrder }
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
        <span className="text-sm font-medium">{statusLabelText(status)}</span>
        <span className="text-xs text-muted-foreground">{issues.length}</span>
        <CreateIssueDialog
          slug={slug}
          defaultStatus={status}
          trigger={
            <Button variant="ghost" size="icon" className="ml-auto size-6" aria-label={`在${statusLabelText(status)}中新建任务`}>
              <Plus className="size-3.5" />
            </Button>
          }
        />
      </div>
      <div className="min-h-0 flex-1 space-y-2 overflow-y-auto px-2 pb-2">
        {issues.length === 0 && <p className="py-6 text-center text-xs text-muted-foreground">暂无任务</p>}
        <SortableContext items={issues.map((i) => i.id)} strategy={verticalListSortingStrategy}>
          {issues.map((issue) => (
            <IssueCard key={issue.id} issue={issue} slug={slug} />
          ))}
        </SortableContext>
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
    const { active, over } = event
    if (!over) return

    // Which side of the hovered card the pointer is on decides insert
    // before/after it; a drop directly on a column (no card underneath)
    // has no "side" to compare, so it always lands at the end.
    let insertAfter = false
    const overRect = over.rect
    const activeRect = active.rect.current.translated
    if (overRect && activeRect) {
      insertAfter = activeRect.top + activeRect.height / 2 > overRect.top + overRect.height / 2
    }

    const update = resolveDrop({ issues, activeId: active.id, overId: over.id, insertAfter })
    if (update) {
      updateIssue.mutate({ id: update.id, patch: { status: update.status, order: update.order } })
    }
  }

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCenter}
      onDragStart={handleDragStart}
      onDragEnd={handleDragEnd}
      onDragCancel={() => setActiveIssue(null)}
    >
      <div className="flex h-full min-h-0 gap-3 overflow-x-auto p-3">
        {STATUS_ORDER.map((status) => (
          <BoardColumn key={status} status={status} issues={issues.filter((i) => i.status === status)} slug={slug} />
        ))}
      </div>
      {/*
        dropAnimation defaults to animating the overlay back to wherever the
        source node currently sits in the DOM — for a same-column reorder
        that's the right place, but here the source has just moved to a
        different column, so the default animation flies to the *old*
        column before the re-rendered board snaps it into the new one.
        Disabling it lets the drop resolve instantly, in the new column.
      */}
      <DragOverlay dropAnimation={null}>
        {activeIssue && (
          <div className="w-72 rotate-2 opacity-90">
            <IssueCard issue={activeIssue} slug={slug} draggable={false} />
          </div>
        )}
      </DragOverlay>
    </DndContext>
  )
}

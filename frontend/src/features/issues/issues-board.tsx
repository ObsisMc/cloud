import {
  DndContext,
  DragOverlay,
  PointerSensor,
  closestCorners,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragOverEvent,
  type DragStartEvent,
} from '@dnd-kit/core'
import { SortableContext, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { Plus } from 'lucide-react'
import { useMemo, useState } from 'react'
import { StatusIcon } from '@/components/common/issue-badges'
import { Button } from '@/components/ui/button'
import { CreateIssueDialog } from '@/features/issues/components/create-issue-dialog'
import { IssueCard } from '@/features/issues/components/issue-card'
import { useMoveIssue } from '@/features/issues/api'
import { columnLabel, memberNameById, orderedColumns } from '@/features/issues/present'
import { cn } from '@/lib/utils'
import type { Issue, IssueStatusColumn, TenantMember } from '@/features/issues/types'

type OrderedIssue = Pick<Issue, 'id' | 'status' | 'position'>

/** Where a dropped card lands, expressed as backend `move` anchors rather than a raw order. */
export interface DropTarget {
  id: string
  status: string
  beforeId?: string
  afterId?: string
}

/** The card immediately above and below `id` within its own column. */
function neighborsOf(issues: OrderedIssue[], id: string): { beforeId?: string; afterId?: string } {
  const active = issues.find((i) => i.id === id)
  if (!active) return {}
  const column = issues
    .filter((i) => i.status === active.status)
    .toSorted((a, b) => a.position - b.position)
  const idx = column.findIndex((i) => i.id === id)
  const before = column[idx - 1]
  const after = column[idx + 1]
  return {
    ...(before ? { beforeId: before.id } : {}),
    ...(after ? { afterId: after.id } : {}),
  }
}

/**
 * Pure decision for what a drag-end should do, kept separate from the DndContext wiring
 * so it can be unit tested without real pointer drags. `overId` is either a status key
 * (dropped on empty space) or another issue's id; the result is an anchor pair the
 * backend `/move` endpoint turns into a fractional position.
 */
export function resolveDrop({
  issues,
  activeId,
  overId,
  insertAfter,
}: {
  issues: OrderedIssue[]
  activeId: string | number | undefined
  overId: string | number | undefined
  insertAfter: boolean
}): DropTarget | null {
  if (activeId == null || overId == null) return null
  const active = issues.find((i) => i.id === activeId)
  if (!active) return null

  const isColumnDrop = !issues.some((i) => i.id === overId)
  const targetStatus = isColumnDrop
    ? String(overId)
    : issues.find((i) => i.id === overId)?.status
  if (!targetStatus) return null

  const column = issues
    .filter((i) => i.status === targetStatus && i.id !== activeId)
    .toSorted((a, b) => a.position - b.position)

  let index: number
  if (isColumnDrop) {
    index = column.length
  } else {
    const overIndex = column.findIndex((i) => i.id === overId)
    if (overIndex === -1) return null
    index = insertAfter ? overIndex + 1 : overIndex
  }

  const before = column[index - 1]
  const after = column[index]
  const target: DropTarget = {
    id: active.id,
    status: targetStatus,
    ...(before ? { beforeId: before.id } : {}),
    ...(after ? { afterId: after.id } : {}),
  }

  const current = neighborsOf(issues, active.id)
  if (
    active.status === targetStatus &&
    current.beforeId === target.beforeId &&
    current.afterId === target.afterId
  ) {
    return null
  }
  return target
}

/** Which side of `over` the pointer is currently on, for the live insertion line. */
type DropPositionEvent = {
  active: { rect: { current: { translated: { top: number; height: number } | null } } }
  over: { rect: { top: number; height: number } } | null
}

export function insertsAfter(event: DropPositionEvent): boolean {
  const { active, over } = event
  const overRect = over?.rect
  const activeRect = active.rect.current.translated
  if (!overRect || !activeRect) return false
  return activeRect.top + activeRect.height / 2 > overRect.top + overRect.height / 2
}

type DropIndicator = { overId: string | number; insertAfter: boolean }

function DropIndicatorLine() {
  return <div className="h-0.5 rounded-full bg-primary" />
}

function BoardColumn({
  status,
  label,
  issues,
  slug,
  statuses,
  members,
  childCounts,
  dropIndicator,
}: {
  status: string
  label: string
  issues: Issue[]
  slug: string
  statuses: IssueStatusColumn[]
  members: TenantMember[]
  childCounts: ReadonlyMap<string, number>
  dropIndicator: DropIndicator | null
}) {
  const { setNodeRef, isOver } = useDroppable({ id: status })
  const showEndIndicator = dropIndicator?.overId === status

  return (
    <div
      ref={setNodeRef}
      className={cn(
        'flex h-full w-72 shrink-0 flex-col rounded-lg bg-muted/40',
        isOver && 'ring-2 ring-primary/50',
      )}
    >
      <div className="flex items-center gap-2 px-2.5 py-2">
        <StatusIcon status={status} />
        <span className="text-sm font-medium">{label}</span>
        <span className="text-xs text-muted-foreground">{issues.length}</span>
        <CreateIssueDialog
          slug={slug}
          statuses={statuses}
          members={members}
          defaultStatus={status}
          trigger={
            <Button
              variant="ghost"
              size="icon"
              className="ml-auto size-6"
              aria-label={`在${label}中新建任务`}
            >
              <Plus className="size-3.5" />
            </Button>
          }
        />
      </div>
      <div className="min-h-0 flex-1 space-y-2 overflow-y-auto px-2 pb-2">
        {issues.length === 0 && !showEndIndicator && (
          <p className="py-6 text-center text-xs text-muted-foreground">暂无任务</p>
        )}
        <SortableContext items={issues.map((i) => i.id)} strategy={verticalListSortingStrategy}>
          {issues.map((issue) => (
            <div key={issue.id}>
              {dropIndicator?.overId === issue.id && !dropIndicator.insertAfter && (
                <DropIndicatorLine />
              )}
              <IssueCard
                issue={issue}
                slug={slug}
                members={memberNameById(members)}
                statuses={statuses}
                subCount={childCounts.get(issue.id) ?? 0}
              />
              {dropIndicator?.overId === issue.id && dropIndicator.insertAfter && (
                <DropIndicatorLine />
              )}
            </div>
          ))}
        </SortableContext>
        {showEndIndicator && <DropIndicatorLine />}
      </div>
    </div>
  )
}

export function IssuesBoard({
  issues,
  slug,
  statuses,
  members,
}: {
  issues: Issue[]
  slug: string
  statuses: IssueStatusColumn[]
  members: TenantMember[]
}) {
  const moveIssue = useMoveIssue(slug)
  const [activeIssue, setActiveIssue] = useState<Issue | null>(null)
  const [dropIndicator, setDropIndicator] = useState<DropIndicator | null>(null)
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }))

  const columns = orderedColumns(statuses, issues.map((issue) => issue.status))
  /** parentIssueId → count of direct sub-issues, so a parent card can show its children at a glance. */
  const childCounts = useMemo(() => {
    const counts = new Map<string, number>()
    for (const issue of issues) {
      if (issue.parentIssueId) {
        counts.set(issue.parentIssueId, (counts.get(issue.parentIssueId) ?? 0) + 1)
      }
    }
    return counts
  }, [issues])

  function handleDragStart(event: DragStartEvent) {
    setActiveIssue(issues.find((i) => i.id === event.active.id) ?? null)
  }

  function handleDragOver(event: DragOverEvent) {
    const { over } = event
    setDropIndicator(over ? { overId: over.id, insertAfter: insertsAfter(event) } : null)
  }

  function handleDragEnd(event: DragEndEvent) {
    setActiveIssue(null)
    setDropIndicator(null)
    const { active, over } = event
    if (!over) return

    const target = resolveDrop({
      issues,
      activeId: active.id,
      overId: over.id,
      insertAfter: insertsAfter(event),
    })
    const moved = issues.find((i) => i.id === target?.id)
    if (target && moved) {
      moveIssue.mutate({
        id: target.id,
        version: moved.version,
        move: {
          status: target.status,
          ...(target.beforeId ? { beforeId: target.beforeId } : {}),
          ...(target.afterId ? { afterId: target.afterId } : {}),
        },
      })
    }
  }

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCorners}
      onDragStart={handleDragStart}
      onDragOver={handleDragOver}
      onDragEnd={handleDragEnd}
      onDragCancel={() => {
        setActiveIssue(null)
        setDropIndicator(null)
      }}
    >
      <div className="flex h-full min-h-0 gap-3 overflow-x-auto p-3">
        {columns.map((status) => (
          <BoardColumn
            key={status}
            status={status}
            label={columnLabel(statuses, status)}
            issues={issues.filter((i) => i.status === status)}
            slug={slug}
            statuses={statuses}
            members={members}
            childCounts={childCounts}
            dropIndicator={dropIndicator}
          />
        ))}
      </div>
      <DragOverlay dropAnimation={null}>
        {activeIssue && (
          <div className="w-72 rotate-2 opacity-90">
            <IssueCard
              issue={activeIssue}
              slug={slug}
              members={memberNameById(members)}
              statuses={statuses}
              subCount={childCounts.get(activeIssue.id) ?? 0}
              draggable={false}
            />
          </div>
        )}
      </DragOverlay>
    </DndContext>
  )
}

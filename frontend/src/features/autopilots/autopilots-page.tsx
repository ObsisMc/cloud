import { formatDistanceToNow } from 'date-fns'
import { Sparkles } from 'lucide-react'
import { PageHeader } from '@/components/layout/page-header'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { useAutopilots, useToggleAutopilot } from '@/features/autopilots/api'
import type { Autopilot } from '@/mocks/data/types'

export function AutopilotsPage({ slug }: { slug: string }) {
  const { data: autopilots, isPending } = useAutopilots(slug)
  const toggle = useToggleAutopilot(slug)

  function handleToggle(autopilot: Autopilot, checked: boolean) {
    toggle.mutate({ id: autopilot.id, status: checked ? 'active' : 'paused' })
  }

  return (
    <div className="flex h-full flex-col">
      <PageHeader title="Autopilots" />
      <div className="flex-1 overflow-y-auto">
        {isPending && (
          <div className="space-y-2 p-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-16 w-full" />
            ))}
          </div>
        )}
        {autopilots?.map((autopilot) => (
          <div key={autopilot.id} className="flex items-center gap-3 border-b px-4 py-3">
            <Sparkles className="size-4 shrink-0 text-muted-foreground" />
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <p className="truncate text-sm font-medium">{autopilot.name}</p>
                {autopilot.status === 'draft' && <Badge variant="outline">draft</Badge>}
              </div>
              <p className="truncate text-xs text-muted-foreground">{autopilot.description}</p>
            </div>
            <div className="hidden shrink-0 text-right text-xs text-muted-foreground sm:block">
              <p>{autopilot.runsCount} runs · {autopilot.successRate}% success</p>
              <p>{autopilot.lastRunAt ? `Last run ${formatDistanceToNow(new Date(autopilot.lastRunAt), { addSuffix: true })}` : 'Never run'}</p>
            </div>
            <Switch
              checked={autopilot.status === 'active'}
              disabled={autopilot.status === 'draft'}
              onCheckedChange={(checked) => handleToggle(autopilot, checked)}
            />
          </div>
        ))}
      </div>
    </div>
  )
}

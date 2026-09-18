import { Server } from 'lucide-react'
import { PageHeader } from '@/components/layout/page-header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useRuntimeAction, useRuntimes } from '@/features/runtimes/api'
import type { Runtime } from '@/mocks/data/types'

const STATUS_VARIANT: Record<Runtime['status'], 'default' | 'secondary' | 'outline'> = {
  running: 'default',
  stopped: 'secondary',
  provisioning: 'outline',
}

const STATUS_LABELS: Record<Runtime['status'], string> = {
  running: '运行中',
  stopped: '已停止',
  provisioning: '创建中',
}

const TYPE_LABELS: Record<Runtime['type'], string> = {
  sandbox: '沙箱',
  container: '容器',
  vm: '虚拟机',
}

export function RuntimesPage({ slug }: { slug: string }) {
  const { data: runtimes, isPending } = useRuntimes(slug)
  const action = useRuntimeAction(slug)

  return (
    <div className="flex h-full flex-col">
      <PageHeader title="运行时" />
      <div className="flex-1 overflow-y-auto">
        {isPending && (
          <div className="space-y-2 p-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-16 w-full" />
            ))}
          </div>
        )}
        {runtimes?.map((runtime) => (
          <div key={runtime.id} className="flex items-center gap-3 border-b px-4 py-3">
            <Server className="size-4 shrink-0 text-muted-foreground" />
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium">{runtime.name}</p>
              <p className="text-xs text-muted-foreground">
                {TYPE_LABELS[runtime.type]} · {runtime.region} · {runtime.cpu} 核 / {runtime.memoryGb}GB
              </p>
            </div>
            <Badge variant={STATUS_VARIANT[runtime.status]}>{STATUS_LABELS[runtime.status]}</Badge>
            <Button
              size="sm"
              variant="outline"
              disabled={runtime.status === 'provisioning' || action.isPending}
              onClick={() =>
                action.mutate({ id: runtime.id, action: runtime.status === 'running' ? 'stop' : 'start' })
              }
            >
              {runtime.status === 'running' ? '停止' : '启动'}
            </Button>
          </div>
        ))}
      </div>
    </div>
  )
}

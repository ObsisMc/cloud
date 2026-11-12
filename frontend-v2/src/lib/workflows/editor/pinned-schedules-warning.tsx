import { useQuery } from '@tanstack/react-query'
import { listScheduleConfigs } from '@/lib/schedules-api/generated'
import { useContractsClient } from '../data/contracts-client'

/** Warns only about this user's fixed schedules for the workflow being published. */
export function PinnedSchedulesWarning({ workflowId }: { workflowId: string }) {
  const { tenantId } = useContractsClient()
  const schedules = useQuery({
    queryKey: ['workflow-pinned-schedules', tenantId, workflowId],
    queryFn: ({ signal }) => listScheduleConfigs(tenantId, undefined, signal),
    staleTime: 0,
    retry: false,
  })
  const consequence = <p className="text-sm text-muted-foreground">发布不会更新固定版本的定时任务；它们会继续使用原版本。如需使用新版本，请编辑任务或选择跟随最新发布版本。</p>
  if (schedules.isPending) return <div role="status">{consequence}<p className="text-sm text-muted-foreground">正在检查受影响的定时任务…</p></div>
  if (schedules.isError) return <div role="alert">{consequence}<p className="text-sm text-muted-foreground">无法检查定时任务，请稍后到定时任务页核对。</p></div>
  const affected = schedules.data.items.filter(({ target }) => target.kind === 'workflow' && target.workflowId === workflowId && typeof target.snapshotId === 'string' && target.versionPolicy !== 'latest')
  return <div role="status">{consequence}{affected.length > 0 && <p className="max-h-32 overflow-y-auto text-sm text-muted-foreground">以下定时任务将继续使用固定的旧版本：{affected.map(task => task.name).join('、')}。仅检查你在当前团队的任务。</p>}</div>
}

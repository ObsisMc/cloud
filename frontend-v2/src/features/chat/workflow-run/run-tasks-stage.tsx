import { ArrowUpRight, CheckCircle2, ClipboardCheck, Link2, Loader2, TriangleAlert } from 'lucide-react'
import { Link } from 'react-router'
import { Badge } from '@/components/ui/badge'
import { buttonVariants } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import type { GraphWorkflowNodeState } from '@/lib/workflows/runtime'

type TaskResult = {
  key?: string
  taskId?: string
  action?: string
}

function readTasks(state: GraphWorkflowNodeState | null): TaskResult[] {
  const raw = state?.output?.detail ?? state?.output?.summary ?? ''
  if (!raw) return []
  try {
    const parsed: unknown = JSON.parse(raw)
    const value = parsed !== null && typeof parsed === 'object' ? parsed as Record<string, unknown> : {}
    const rows = Array.isArray(value.tasks) ? value.tasks : Array.isArray(parsed) ? parsed : []
    return rows.filter((row): row is TaskResult => row !== null && typeof row === 'object')
  } catch {
    return []
  }
}

/** CreateTasks owns the task outcome, so its stage lists exact created or reused tasks. */
export function RunTasksStage({ nodeTitle, state }: { nodeTitle: string; state: GraphWorkflowNodeState | null }) {
  const tasks = readTasks(state)
  const status = state?.status ?? 'idle'
  const pending = status === 'running' || status === 'awaiting_input' || status === 'retry_waiting'
  return <section className="flex w-full flex-col gap-4" aria-label="任务处理结果">
    <header className="flex flex-wrap items-start justify-between gap-3">
      <div className="min-w-0">
        <p className="text-xs font-medium text-muted-foreground">协作任务</p>
        <h2 className="mt-1 text-xl font-semibold tracking-tight">{nodeTitle || '创建或关联任务'}</h2>
        <p className="mt-1 text-sm text-muted-foreground">确认结果已写入这次运行，任务由表单提交后确定性创建或关联。</p>
      </div>
      <Badge variant="outline" className={cn('gap-1.5', status === 'succeeded' && 'border-success/30 bg-success/10 text-success', status === 'failed' && 'border-destructive/30 bg-destructive/10 text-destructive', pending && 'border-info/30 bg-info/10 text-info')}>
        {pending ? <Loader2 className="size-3 animate-spin" /> : status === 'failed' ? <TriangleAlert className="size-3" /> : <CheckCircle2 className="size-3" />}
        {pending ? '处理中' : status === 'failed' ? '失败' : status === 'succeeded' ? `${tasks.length} 项` : '待执行'}
      </Badge>
    </header>
    {tasks.length > 0 ? <ul className="grid gap-3">
      {tasks.map((task, index) => <li key={`${task.taskId ?? task.key ?? index}`} className="rounded-xl border bg-card p-4 shadow-sm">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="flex min-w-0 items-start gap-3">
            <span className={cn('mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-lg', task.action === 'reused' ? 'bg-info-soft text-info' : 'bg-success-soft text-success')}>
              {task.action === 'reused' ? <Link2 className="size-4" /> : <ClipboardCheck className="size-4" />}
            </span>
            <div className="min-w-0">
              <p className="font-medium">{task.action === 'reused' ? '已关联现有任务' : '已创建任务'}</p>
              <p className="mt-0.5 truncate font-mono text-xs text-muted-foreground">{task.taskId ?? task.key ?? '任务编号不可用'}</p>
            </div>
          </div>
          {task.taskId ? <Link className={buttonVariants({ size: 'sm', variant: 'outline' })} to={`/collaboration/tasks/${task.taskId}`}>打开任务<ArrowUpRight /></Link> : null}
        </div>
      </li>)}
    </ul> : <div className="rounded-xl border border-dashed bg-muted/20 px-5 py-8 text-center">
      <ClipboardCheck className="mx-auto size-6 text-muted-foreground" />
      <p className="mt-3 text-sm font-medium">{pending ? '正在创建或关联任务' : status === 'failed' ? '任务处理失败' : '没有需要创建的任务'}</p>
      <p className="mt-1 text-sm text-muted-foreground">{pending ? '表单提交后，这里会列出每个问题的处理结果。' : status === 'failed' ? state?.errorMessage || '请回到确认节点检查提交结果后重试。' : '本次确认没有需要创建或关联的协作任务。'}</p>
    </div>}
  </section>
}

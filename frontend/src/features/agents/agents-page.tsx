import { Link } from 'react-router-dom'
import { ActorAvatar } from '@/components/common/actor-avatar'
import { PageHeader } from '@/components/layout/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { useAgents } from '@/features/agents/api'
import { workspacePaths } from '@/lib/paths'
import type { Agent } from '@/mocks/data/types'

const STATUS_DOT: Record<Agent['status'], string> = {
  online: 'bg-emerald-500',
  busy: 'bg-amber-500',
  idle: 'bg-muted-foreground',
  offline: 'bg-muted-foreground/40',
}

export function AgentsPage({ slug }: { slug: string }) {
  const { data: agents, isPending } = useAgents(slug)
  const p = workspacePaths(slug)

  return (
    <div className="flex h-full flex-col">
      <PageHeader title="智能体" />
      <div className="flex-1 overflow-y-auto p-4">
        {isPending && (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-24 w-full" />
            ))}
          </div>
        )}
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {agents?.map((agent) => (
            <Link
              key={agent.id}
              to={p.agentDetail(agent.id)}
              className="flex items-start gap-3 rounded-lg border p-4 hover:bg-muted/50"
            >
              <span className="relative shrink-0">
                <ActorAvatar actor={agent} size="lg" />
                <span
                  className={`absolute -bottom-0.5 -right-0.5 size-2.5 rounded-full ring-2 ring-background ${STATUS_DOT[agent.status]}`}
                />
              </span>
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">{agent.name}</p>
                <p className="truncate text-xs text-muted-foreground">{agent.role}</p>
                <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">
                  {agent.description}
                </p>
              </div>
            </Link>
          ))}
        </div>
      </div>
    </div>
  )
}

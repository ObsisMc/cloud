import { useParams } from 'react-router-dom'
import { ActorAvatar } from '@/components/common/actor-avatar'
import { PageHeader } from '@/components/layout/page-header'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { useAgent } from '@/features/agents/api'
import { workspacePaths } from '@/lib/paths'
import { db } from '@/mocks/data/store'

export function AgentDetailPage({ slug }: { slug: string }) {
  const { agentId } = useParams<{ agentId: string }>()
  const { data: agent, isPending } = useAgent(slug, agentId)
  const p = workspacePaths(slug)

  if (isPending || !agent) {
    return (
      <div className="flex h-full flex-col">
        <PageHeader title="Agent" breadcrumb={{ label: 'Agents', to: p.agents }} />
        <div className="space-y-3 p-6">
          <Skeleton className="h-6 w-1/2" />
          <Skeleton className="h-20 w-full" />
        </div>
      </div>
    )
  }

  const squad = db.squads.find((s) => s.id === agent.squadId)
  const assignedIssues = db.issues.filter((i) => i.assigneeId === agent.id)

  return (
    <div className="flex h-full flex-col">
      <PageHeader title={agent.name} breadcrumb={{ label: 'Agents', to: p.agents }} />
      <div className="flex-1 overflow-y-auto p-6">
        <div className="mb-6 flex items-start gap-3">
          <ActorAvatar actor={agent} size="lg" />
          <div>
            <h1 className="text-lg font-semibold">{agent.name}</h1>
            <p className="text-sm text-muted-foreground">{agent.role}</p>
            <div className="mt-2 flex items-center gap-2">
              <Badge variant="secondary">{agent.model}</Badge>
              <Badge variant="outline" className="capitalize">{agent.status}</Badge>
              {squad && <Badge variant="outline">{squad.name}</Badge>}
            </div>
          </div>
        </div>
        <p className="mb-6 text-sm text-muted-foreground">{agent.description}</p>

        <h2 className="mb-2 text-xs font-medium text-muted-foreground">Assigned issues ({assignedIssues.length})</h2>
        <div className="space-y-1.5">
          {assignedIssues.length === 0 && <p className="text-sm text-muted-foreground">No issues assigned.</p>}
          {assignedIssues.map((issue) => (
            <div key={issue.id} className="flex items-center gap-2 rounded-md border px-3 py-2 text-sm">
              <span className="text-xs text-muted-foreground">{issue.identifier}</span>
              <span className="truncate">{issue.title}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

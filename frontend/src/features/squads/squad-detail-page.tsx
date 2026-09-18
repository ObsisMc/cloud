import { useParams } from 'react-router-dom'
import { ActorAvatar } from '@/components/common/actor-avatar'
import { PageHeader } from '@/components/layout/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { useSquad } from '@/features/squads/api'
import { workspacePaths } from '@/lib/paths'
import { actorById, db } from '@/mocks/data/store'

export function SquadDetailPage({ slug }: { slug: string }) {
  const { squadId } = useParams<{ squadId: string }>()
  const { data: squad, isPending } = useSquad(slug, squadId)
  const p = workspacePaths(slug)

  if (isPending || !squad) {
    return (
      <div className="flex h-full flex-col">
        <PageHeader title="小队" breadcrumb={{ label: '小队', to: p.squads }} />
        <div className="space-y-3 p-6">
          <Skeleton className="h-6 w-1/2" />
          <Skeleton className="h-20 w-full" />
        </div>
      </div>
    )
  }

  const members = squad.memberIds.map((id) => actorById(id)).filter((a): a is NonNullable<typeof a> => !!a)
  const projects = db.projects.filter((proj) => squad.projectIds.includes(proj.id))

  return (
    <div className="flex h-full flex-col">
      <PageHeader title={squad.name} breadcrumb={{ label: '小队', to: p.squads }} />
      <div className="flex-1 overflow-y-auto p-6">
        <div className="mb-6 flex items-center gap-3">
          <span
            className="flex size-8 shrink-0 items-center justify-center rounded-md text-sm font-semibold text-white"
            style={{ backgroundColor: squad.color }}
          >
            {squad.name.charAt(0)}
          </span>
          <div>
            <h1 className="text-lg font-semibold">{squad.name}</h1>
            <p className="text-sm text-muted-foreground">{squad.description}</p>
          </div>
        </div>

        <h2 className="mb-2 text-xs font-medium text-muted-foreground">成员</h2>
        <div className="mb-6 space-y-1.5">
          {members.map((member) => (
            <div key={member.id} className="flex items-center gap-2 rounded-md border px-3 py-2 text-sm">
              <ActorAvatar actor={member} size="sm" />
              <span>{member.name}</span>
              {member.type === 'agent' && <span className="text-xs text-muted-foreground">({member.role})</span>}
            </div>
          ))}
        </div>

        {projects.length > 0 && (
          <>
            <h2 className="mb-2 text-xs font-medium text-muted-foreground">项目</h2>
            <div className="space-y-1.5">
              {projects.map((project) => (
                <div key={project.id} className="rounded-md border px-3 py-2 text-sm">
                  {project.title}
                </div>
              ))}
            </div>
          </>
        )}
      </div>
    </div>
  )
}

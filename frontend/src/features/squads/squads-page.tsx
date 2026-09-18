import { Link } from 'react-router-dom'
import { ActorAvatar } from '@/components/common/actor-avatar'
import { PageHeader } from '@/components/layout/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { useSquads } from '@/features/squads/api'
import { workspacePaths } from '@/lib/paths'
import { actorById } from '@/mocks/data/store'

export function SquadsPage({ slug }: { slug: string }) {
  const { data: squads, isPending } = useSquads(slug)
  const p = workspacePaths(slug)

  return (
    <div className="flex h-full flex-col">
      <PageHeader title="小队" />
      <div className="flex-1 overflow-y-auto p-4">
        {isPending && (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-28 w-full" />
            ))}
          </div>
        )}
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          {squads?.map((squad) => (
            <Link
              key={squad.id}
              to={p.squadDetail(squad.id)}
              className="flex flex-col gap-3 rounded-lg border p-4 hover:bg-muted/50"
            >
              <div className="flex items-center gap-2">
                <span
                  className="flex size-6 shrink-0 items-center justify-center rounded-md text-xs font-semibold text-white"
                  style={{ backgroundColor: squad.color }}
                >
                  {squad.name.charAt(0)}
                </span>
                <span className="text-sm font-medium">{squad.name}</span>
              </div>
              <p className="text-xs text-muted-foreground">{squad.description}</p>
              <div className="mt-auto flex items-center -space-x-1.5">
                {squad.memberIds.slice(0, 6).map((id) => (
                  <ActorAvatar
                    key={id}
                    actor={actorById(id)}
                    size="sm"
                    className="ring-2 ring-background"
                  />
                ))}
              </div>
            </Link>
          ))}
        </div>
      </div>
    </div>
  )
}

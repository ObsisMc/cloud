import { format } from 'date-fns'
import { Link } from 'react-router-dom'
import { ActorAvatar } from '@/components/common/actor-avatar'
import { PageHeader } from '@/components/layout/page-header'
import { Badge, badgeVariants } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import type { VariantProps } from 'class-variance-authority'
import { ProjectIcon } from '@/features/projects/components/project-icon'
import { useProjects } from '@/features/projects/api'
import { workspacePaths } from '@/lib/paths'
import { actorById, db } from '@/mocks/data/store'
import type { Project } from '@/mocks/data/types'

const STATUS_VARIANT: Record<Project['status'], NonNullable<VariantProps<typeof badgeVariants>['variant']>> = {
  planned: 'secondary',
  in_progress: 'default',
  completed: 'secondary',
  paused: 'outline',
}

export function ProjectsPage({ slug }: { slug: string }) {
  const { data: projects, isPending } = useProjects(slug)
  const p = workspacePaths(slug)

  return (
    <div className="flex h-full flex-col">
      <PageHeader title="Projects" />
      <div className="flex-1 overflow-y-auto p-4">
        {isPending && (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-32 w-full" />
            ))}
          </div>
        )}
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {projects?.map((project) => {
            const lead = actorById(project.leadId)
            const issueCount = db.issues.filter((i) => i.projectId === project.id).length
            const doneCount = db.issues.filter((i) => i.projectId === project.id && i.status === 'done').length
            return (
              <Link
                key={project.id}
                to={p.projectDetail(project.id)}
                className="flex flex-col gap-3 rounded-lg border p-4 hover:bg-muted/50"
              >
                <div className="flex items-center gap-2">
                  <ProjectIcon project={project} />
                  <span className="min-w-0 flex-1 truncate text-sm font-medium">{project.title}</span>
                </div>
                <p className="line-clamp-2 text-xs text-muted-foreground">{project.description}</p>
                <div className="mt-auto flex items-center justify-between text-xs text-muted-foreground">
                  <Badge variant={STATUS_VARIANT[project.status]}>{project.status.replace('_', ' ')}</Badge>
                  <span>
                    {doneCount}/{issueCount} issues
                  </span>
                </div>
                <div className="flex items-center justify-between">
                  <ActorAvatar actor={lead} size="sm" />
                  {project.targetDate && (
                    <span className="text-xs text-muted-foreground">{format(new Date(project.targetDate), 'MMM d')}</span>
                  )}
                </div>
              </Link>
            )
          })}
        </div>
      </div>
    </div>
  )
}

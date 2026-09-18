import { format } from 'date-fns'
import { useParams } from 'react-router-dom'
import { ActorAvatar } from '@/components/common/actor-avatar'
import { PageHeader } from '@/components/layout/page-header'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { ProjectIcon } from '@/features/projects/components/project-icon'
import { useProject } from '@/features/projects/api'
import { PROJECT_STATUS_LABELS, PROJECT_STATUS_VARIANT } from '@/features/projects/status'
import { useIssues } from '@/features/issues/api'
import { IssueRow } from '@/features/issues/components/issue-row'
import { workspacePaths } from '@/lib/paths'
import { actorById } from '@/mocks/data/store'

export function ProjectDetailPage({ slug }: { slug: string }) {
  const { projectId } = useParams<{ projectId: string }>()
  const { data: project, isPending } = useProject(slug, projectId)
  const { data: issues } = useIssues(slug, { projectId })
  const p = workspacePaths(slug)

  if (isPending || !project) {
    return (
      <div className="flex h-full flex-col">
        <PageHeader title="项目" breadcrumb={{ label: '项目', to: p.projects }} />
        <div className="space-y-3 p-6">
          <Skeleton className="h-6 w-1/2" />
          <Skeleton className="h-20 w-full" />
        </div>
      </div>
    )
  }

  const lead = actorById(project.leadId)

  return (
    <div className="flex h-full flex-col">
      <PageHeader title={project.title} breadcrumb={{ label: '项目', to: p.projects }} />
      <div className="flex-1 overflow-y-auto">
        <div className="space-y-3 border-b p-6">
          <div className="flex items-center gap-2">
            <ProjectIcon project={project} />
            <h1 className="text-xl font-semibold">{project.title}</h1>
          </div>
          <p className="text-sm text-muted-foreground">{project.description}</p>
          <div className="flex flex-wrap items-center gap-4 pt-2 text-sm">
            <Badge variant={PROJECT_STATUS_VARIANT[project.status]}>{PROJECT_STATUS_LABELS[project.status]}</Badge>
            <div className="flex items-center gap-1.5 text-muted-foreground">
              <ActorAvatar actor={lead} size="sm" />
              {lead?.name}
            </div>
            {project.targetDate && (
              <span className="text-muted-foreground">目标日期：{format(new Date(project.targetDate), 'yyyy年M月d日')}</span>
            )}
          </div>
        </div>
        <div>
          {issues?.length === 0 && (
            <p className="p-8 text-center text-sm text-muted-foreground">该项目下暂无任务。</p>
          )}
          {issues?.map((issue) => (
            <IssueRow key={issue.id} issue={issue} slug={slug} />
          ))}
        </div>
      </div>
    </div>
  )
}

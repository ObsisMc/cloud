import type { VariantProps } from 'class-variance-authority'
import type { badgeVariants } from '@/components/ui/badge'
import type { Project } from '@/mocks/data/types'

export const PROJECT_STATUS_LABELS: Record<Project['status'], string> = {
  planned: '规划中',
  in_progress: '进行中',
  completed: '已完成',
  paused: '已暂停',
}

export const PROJECT_STATUS_VARIANT: Record<
  Project['status'],
  NonNullable<VariantProps<typeof badgeVariants>['variant']>
> = {
  planned: 'secondary',
  in_progress: 'default',
  completed: 'secondary',
  paused: 'outline',
}

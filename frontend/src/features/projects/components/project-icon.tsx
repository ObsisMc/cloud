import { Boxes, Gauge, GitBranch, Layers, Rocket } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { Project } from '@/mocks/data/types'

const ICONS: Record<string, React.ComponentType<{ className?: string }>> = {
  Rocket,
  Layers,
  Boxes,
  GitBranch,
  Gauge,
}

export function ProjectIcon({ project, className }: { project: Pick<Project, 'icon' | 'color'>; className?: string }) {
  const Icon = ICONS[project.icon] ?? Boxes
  return (
    <span
      className={cn('flex size-6 shrink-0 items-center justify-center rounded-md text-white', className)}
      style={{ backgroundColor: project.color }}
    >
      <Icon className="size-3.5" />
    </span>
  )
}

import { Sparkles } from 'lucide-react'
import { PageHeader } from '@/components/layout/page-header'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { useSkills, useToggleSkill } from '@/features/skills/api'

const SKELETON_KEYS = ['one', 'two', 'three', 'four', 'five']

export function SkillsPage({ slug }: { slug: string }) {
  const { data: skills, isPending } = useSkills(slug)
  const toggle = useToggleSkill(slug)

  return (
    <div className="flex h-full flex-col">
      <PageHeader title="技能" />
      <div className="flex-1 overflow-y-auto">
        {isPending && (
          <div className="space-y-2 p-4">
            {SKELETON_KEYS.map((key) => (
              <Skeleton key={key} className="h-16 w-full" />
            ))}
          </div>
        )}
        {skills?.map((skill) => (
          <div key={skill.id} className="flex items-center gap-3 border-b px-4 py-3">
            <Sparkles className="size-4 shrink-0 text-muted-foreground" />
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <p className="truncate text-sm font-medium">{skill.name}</p>
                <Badge variant="outline">{skill.category}</Badge>
              </div>
              <p className="truncate text-xs text-muted-foreground">{skill.description}</p>
            </div>
            <span className="hidden shrink-0 text-xs text-muted-foreground sm:inline">
              已使用 {skill.usageCount.toLocaleString()} 次
            </span>
            <Switch
              checked={skill.enabled}
              onCheckedChange={(checked) => toggle.mutate({ id: skill.id, enabled: checked })}
            />
          </div>
        ))}
      </div>
    </div>
  )
}

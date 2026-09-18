import { useOutletContext } from 'react-router-dom'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { db, workspaceBySlug } from '@/mocks/data/store'

export function GeneralSettingsPage() {
  const slug = useOutletContext<string>()
  const workspace = workspaceBySlug(slug) ?? db.workspace

  return (
    <div className="max-w-lg space-y-4 p-4">
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">工作区</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex items-center gap-3">
            <span
              className="flex size-10 items-center justify-center rounded-lg text-sm font-semibold text-white"
              style={{ backgroundColor: workspace.avatarColor }}
            >
              {workspace.name.charAt(0)}
            </span>
            <div>
              <p className="text-sm font-medium">{workspace.name}</p>
              <p className="text-xs text-muted-foreground">/{workspace.slug}</p>
            </div>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="workspace-name">工作区名称</Label>
            <Input id="workspace-name" defaultValue={workspace.name} key={workspace.id} />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="workspace-slug">工作区标识（Slug）</Label>
            <Input id="workspace-slug" defaultValue={workspace.slug} key={`${workspace.id}-slug`} disabled />
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

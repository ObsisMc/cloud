import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { db } from '@/mocks/data/store'

export function GeneralSettingsPage() {
  return (
    <div className="max-w-lg space-y-4 p-4">
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Workspace</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex items-center gap-3">
            <span
              className="flex size-10 items-center justify-center rounded-lg text-sm font-semibold text-white"
              style={{ backgroundColor: db.workspace.avatarColor }}
            >
              {db.workspace.name.charAt(0)}
            </span>
            <div>
              <p className="text-sm font-medium">{db.workspace.name}</p>
              <p className="text-xs text-muted-foreground">/{db.workspace.slug}</p>
            </div>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="workspace-name">Workspace name</Label>
            <Input id="workspace-name" defaultValue={db.workspace.name} />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="workspace-slug">Slug</Label>
            <Input id="workspace-slug" defaultValue={db.workspace.slug} disabled />
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

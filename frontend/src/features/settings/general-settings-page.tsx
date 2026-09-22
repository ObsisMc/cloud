import { useState } from 'react'
import { useOutletContext } from 'react-router-dom'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { SpaceListItem } from '@/api/generated.schemas'
import { normalizeSpaceRole, useArchiveSpace, useUpdateSpace } from '@/features/spaces/api'
import { useCurrentSpace } from '@/features/spaces/current-space'

/**
 * Workspace settings: edit the space name (slug stays immutable) with the
 * optimistic version guard; owners additionally get an archive action in a
 * danger zone. Nothing renders until the route slug resolved to the space.
 */
export function GeneralSettingsPage() {
  const slug = useOutletContext<string>()
  const { tenantId, space } = useCurrentSpace()
  const cloudSpace = space?.slug === slug ? space : undefined
  if (!cloudSpace) return null
  return <CloudSettings tenantId={tenantId ?? ''} space={cloudSpace} />
}

/** Editable cloud workspace card plus the owner-only archive danger zone. */
function CloudSettings({ tenantId, space }: { tenantId: string; space: SpaceListItem }) {
  const updateSpace = useUpdateSpace(tenantId, space.id)
  const archiveSpace = useArchiveSpace(tenantId, space.id)
  const [name, setName] = useState('')
  const isOwner = normalizeSpaceRole(space.role) === 'owner'

  return (
    <div className="max-w-lg space-y-4 p-4">
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">工作区</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="space-y-1.5">
            <Label htmlFor="workspace-name">工作区名称</Label>
            <Input
              id="workspace-name"
              key={space.id}
              defaultValue={space.name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="workspace-slug">工作区标识（Slug）</Label>
            <Input
              id="workspace-slug"
              defaultValue={space.slug}
              key={`${space.id}-slug`}
              disabled
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="workspace-description">描述</Label>
            <Input id="workspace-description" defaultValue={space.description} disabled />
          </div>
          <Button
            onClick={() =>
              updateSpace.mutate({ name, description: space.description, version: space.version })
            }
            disabled={updateSpace.isPending || name.trim() === '' || name === space.name}
          >
            {updateSpace.isPending ? '保存中…' : '保存'}
          </Button>
          {updateSpace.isError && (
            <p className="text-xs text-destructive">保存失败：版本冲突或需要管理员角色</p>
          )}
        </CardContent>
      </Card>
      {isOwner && (
        <DangerZone
          space={space}
          pending={archiveSpace.isPending}
          error={archiveSpace.isError}
          onArchive={archiveSpace.mutate}
        />
      )}
    </div>
  )
}

/** Owner-only archive action with a confirmation dialog. */
function DangerZone({
  space,
  pending,
  error,
  onArchive,
}: {
  space: SpaceListItem
  pending: boolean
  error: boolean
  onArchive: (version: number) => void
}) {
  return (
    <Card className="border-destructive/40">
      <CardHeader>
        <CardTitle className="text-sm text-destructive">危险区</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <p className="text-xs text-muted-foreground">
          归档后成员将无法访问该工作区，其下项目不受影响。
        </p>
        <AlertDialog>
          <AlertDialogTrigger
            render={
              <Button variant="destructive" disabled={pending}>
                归档工作区
              </Button>
            }
          />
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>归档「{space.name}」？</AlertDialogTitle>
              <AlertDialogDescription>
                归档是软删除：空间从列表隐藏、成员访问被拒；项目数据与运行状态保持不变。
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>取消</AlertDialogCancel>
              <AlertDialogAction onClick={() => onArchive(space.version)}>
                确认归档
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
        {error && <p className="text-xs text-destructive">归档失败：需要所有者角色或版本冲突</p>}
      </CardContent>
    </Card>
  )
}

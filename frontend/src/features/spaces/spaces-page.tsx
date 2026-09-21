import { useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { CreateSpaceDialog } from '@/features/spaces/create-space-dialog'
import { normalizeSpaceRole, useArchiveSpace, useSpaces } from '@/features/spaces/api'
import { useSpaceEvents } from '@/features/spaces/use-space-events'
import { useAuthStore } from '@/state/auth-store'

/**
 * Space management page. Lists the collaboration spaces the signed-in member
 * joined, opens the create-space dialog, and lets owners archive a space.
 * Selecting a row subscribes that space's event stream so membership and
 * project changes invalidate the authoritative REST queries in real time.
 */
export function SpacesPage() {
  const tenantId = useAuthStore((s) => s.tenantId)
  const spaces = useSpaces(tenantId ?? undefined)
  const [createOpen, setCreateOpen] = useState(false)
  const [selectedId, setSelectedId] = useState<string | undefined>(undefined)
  useSpaceEvents(tenantId ?? undefined, selectedId)

  const items = spaces.data?.items ?? []

  return (
    <div className="max-w-2xl space-y-4 p-4">
      <CreateSpaceDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        tenantId={tenantId ?? undefined}
        onCreated={() => undefined}
      />
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-base font-semibold">空间</h2>
          <p className="text-sm text-muted-foreground">你已加入的协作空间</p>
        </div>
        <Button onClick={() => setCreateOpen(true)}>
          <Plus className="size-4" />
          新建空间
        </Button>
      </div>

      {items.length === 0 ? (
        <p className="text-sm text-muted-foreground">还没有空间，新建一个开始协作。</p>
      ) : (
        <div className="space-y-2">
          {items.map((space) => (
            <SpaceRow
              key={space.id}
              tenantId={tenantId ?? undefined}
              spaceId={space.id}
              name={space.name}
              slug={space.slug}
              description={space.description ?? ''}
              version={space.version}
              role={normalizeSpaceRole(space.role ?? '')}
              selected={space.id === selectedId}
              onSelect={() => setSelectedId(space.id === selectedId ? undefined : space.id)}
            />
          ))}
        </div>
      )}
    </div>
  )
}

/**
 * One space in the list. Only owners can archive — the backend requires the
 * owner role and rejects admins, and it also refuses archiving the tenant
 * default space (`default_space_protected`).
 */
function SpaceRow({
  tenantId,
  spaceId,
  name,
  slug,
  description,
  version,
  role,
  selected,
  onSelect,
}: {
  tenantId: string | undefined
  spaceId: string
  name: string
  slug: string
  description: string
  version: number
  role: string
  selected: boolean
  onSelect: () => void
}) {
  const archive = useArchiveSpace(tenantId, spaceId)
  const canArchive = role === 'owner'

  return (
    <Card className={selected ? 'ring-1 ring-primary' : undefined}>
      <CardContent className="flex items-center gap-3 p-3">
        <button type="button" onClick={onSelect} className="min-w-0 flex-1 text-left">
          <p className="truncate text-sm font-medium">{name}</p>
          <p className="truncate text-xs text-muted-foreground">
            {description || `/${slug}`} · {role}
          </p>
        </button>
        {canArchive && (
          <Button
            size="sm"
            variant="ghost"
            onClick={() => archive.mutate(version)}
            disabled={archive.isPending}
          >
            <Trash2 className="size-3.5" />
            归档
          </Button>
        )}
      </CardContent>
    </Card>
  )
}

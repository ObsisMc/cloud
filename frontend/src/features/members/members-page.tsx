import { format } from 'date-fns'
import { useState } from 'react'
import { ActorAvatar } from '@/components/common/actor-avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useMembers, type MemberWithUser } from '@/features/members/api'
import { normalizeSpaceRole, useUpdateSpaceMember, type SpaceRole } from '@/features/spaces/api'
import { useCurrentSpace } from '@/features/spaces/current-space'

const ROLE_LABELS: Record<string, string> = {
  owner: '所有者',
  admin: '管理员',
  member: '成员',
}

const STATUS_LABELS: Record<string, string> = {
  active: '已加入',
  invited: '待加入',
}
const SKELETON_KEYS = ['one', 'two', 'three', 'four', 'five']

/** Role options an actor may grant; only owners may grant owner. */
function roleOptions(canGrantOwner: boolean): SpaceRole[] {
  return canGrantOwner ? ['owner', 'admin', 'member'] : ['admin', 'member']
}

/**
 * Members page: the space's real membership list with admin/owner management
 * controls (role changes, disable/enable, add member). Renders a skeleton
 * until the route slug resolved to a joined space.
 */
export function MembersPage({ slug }: { slug: string }) {
  const { data: members, isPending } = useMembers(slug)
  const { tenantId, space } = useCurrentSpace()
  const cloudSpace = space?.slug === slug ? space : undefined

  return (
    <CloudMembersView
      tenantId={tenantId ?? ''}
      spaceId={cloudSpace?.id ?? ''}
      members={members ?? []}
      isPending={isPending || !cloudSpace}
      myRole={cloudSpace ? normalizeSpaceRole(cloudSpace.role) : 'member'}
    />
  )
}

/** Status badge and join-date cells of a member row. */
function MemberStatusCells({ member }: { member: MemberWithUser }) {
  return (
    <>
      <TableCell>
        <Badge variant={member.status === 'active' ? 'secondary' : 'outline'}>
          {STATUS_LABELS[member.status] ?? member.status}
        </Badge>
      </TableCell>
      <TableCell className="text-muted-foreground">
        {format(new Date(member.joinedAt), 'yyyy年M月d日')}
      </TableCell>
    </>
  )
}

/** Adds a member by their local user id; the backend rejects non-tenant users. */
function AddMemberForm({
  onSubmit,
  pending,
  canGrantOwner,
}: {
  onSubmit: (input: { userId: string; role: SpaceRole }) => void
  pending: boolean
  canGrantOwner: boolean
}) {
  const [userId, setUserId] = useState('')
  const [role, setRole] = useState<SpaceRole>('member')

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        if (userId.trim() === '' || pending) return
        onSubmit({ userId: userId.trim(), role })
        setUserId('')
      }}
      className="mb-4 flex items-end gap-2"
    >
      <div className="min-w-0 flex-1 space-y-1.5">
        <p className="text-xs text-muted-foreground">成员 userId（UUID，先经 /me 建号）</p>
        <Input
          value={userId}
          onChange={(e) => setUserId(e.target.value)}
          placeholder="xxxxxxxx-xxxx-…"
          aria-label="新成员 userId"
        />
      </div>
      <Select
        value={role}
        onValueChange={(value) => setRole(normalizeSpaceRole(value ?? 'member'))}
      >
        <SelectTrigger className="w-28" aria-label="新成员角色">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {roleOptions(canGrantOwner).map((option) => (
            <SelectItem key={option} value={option}>
              {ROLE_LABELS[option]}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Button type="submit" disabled={pending}>
        添加
      </Button>
    </form>
  )
}

/**
 * Cloud membership table with management controls. Actors without admin or
 * owner see a read-only table; owners alone can grant owner.
 */
function CloudMembersView({
  tenantId,
  spaceId,
  members,
  isPending,
  myRole,
}: {
  tenantId: string
  spaceId: string
  members: MemberWithUser[]
  isPending: boolean
  myRole: SpaceRole
}) {
  const updateMember = useUpdateSpaceMember(tenantId, spaceId)
  const canManage = myRole === 'admin' || myRole === 'owner'
  const errorCode = updateMember.error?.response?.data?.code

  function update(role: SpaceRole, status: 'active' | 'disabled', member: MemberWithUser) {
    updateMember.mutate({ userId: member.id, role, status, version: member.version ?? 0 })
  }

  return (
    <div className="p-4">
      {canManage && (
        <AddMemberForm
          pending={updateMember.isPending}
          canGrantOwner={myRole === 'owner'}
          onSubmit={({ userId, role }) =>
            updateMember.mutate({ userId, role, status: 'active', version: 0 })
          }
        />
      )}
      {errorCode && <p className="mb-3 text-xs text-destructive">操作失败：{errorCode}</p>}
      {isPending && (
        <div className="space-y-2">
          {SKELETON_KEYS.map((key) => (
            <Skeleton key={key} className="h-10 w-full" />
          ))}
        </div>
      )}
      {members.length > 0 && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>成员</TableHead>
              <TableHead>角色</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>加入时间</TableHead>
              {canManage && <TableHead>操作</TableHead>}
            </TableRow>
          </TableHeader>
          <TableBody>
            {members.map((member) => (
              <CloudMemberRow
                key={member.id}
                member={member}
                canManage={canManage}
                canGrantOwner={myRole === 'owner'}
                pending={updateMember.isPending}
                onUpdate={update}
              />
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  )
}

/** One cloud membership row with role selector and disable/enable controls. */
function CloudMemberRow({
  member,
  canManage,
  canGrantOwner,
  pending,
  onUpdate,
}: {
  member: MemberWithUser
  canManage: boolean
  canGrantOwner: boolean
  pending: boolean
  onUpdate: (role: SpaceRole, status: 'active' | 'disabled', member: MemberWithUser) => void
}) {
  const role = normalizeSpaceRole(member.role)
  return (
    <TableRow>
      <TableCell>
        <div className="flex items-center gap-2">
          <ActorAvatar actor={member} size="sm" />
          <div>
            <p className="text-sm font-medium">{member.name}</p>
            <p className="text-xs text-muted-foreground">{member.id}</p>
          </div>
        </div>
      </TableCell>
      <TableCell>
        {canManage ? (
          <Select
            value={role}
            onValueChange={(value) =>
              onUpdate(normalizeSpaceRole(value ?? 'member'), 'active', member)
            }
          >
            <SelectTrigger className="w-28" aria-label={`${member.name} 的角色`}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {roleOptions(canGrantOwner).map((option) => (
                <SelectItem key={option} value={option}>
                  {ROLE_LABELS[option]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          (ROLE_LABELS[role] ?? role)
        )}
      </TableCell>
      <MemberStatusCells member={member} />
      {canManage && (
        <TableCell>
          <Button
            variant="outline"
            size="sm"
            disabled={pending || role === 'owner'}
            onClick={() =>
              onUpdate(role, member.status === 'active' ? 'disabled' : 'active', member)
            }
          >
            {member.status === 'active' ? '禁用' : '启用'}
          </Button>
        </TableCell>
      )}
    </TableRow>
  )
}

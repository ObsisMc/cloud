import { format } from 'date-fns'
import { useState } from 'react'
import { ActorAvatar } from '@/components/common/actor-avatar'
import { DialogFormField } from '@/components/common/dialog-form-field'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
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
import { useAddSpaceMemberByEmail, useMembers, type MemberWithUser } from '@/features/members/api'
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
 * Members page. Cloud sessions render the real membership list with
 * admin/owner management controls (role changes, disable/enable, add member);
 * mock sessions keep the demo store table.
 */
export function MembersPage({ slug }: { slug: string }) {
  const { data: members, isPending } = useMembers(slug)
  const { tenantId, space } = useCurrentSpace()
  const cloudSpace = space?.slug === slug ? space : undefined

  if (cloudSpace) {
    return (
      <CloudMembersView
        tenantId={tenantId ?? ''}
        spaceId={cloudSpace.id}
        members={members ?? []}
        isPending={isPending}
        myRole={normalizeSpaceRole(cloudSpace.role)}
      />
    )
  }

  return (
    <div className="p-4">
      {isPending && (
        <div className="space-y-2">
          {SKELETON_KEYS.map((key) => (
            <Skeleton key={key} className="h-10 w-full" />
          ))}
        </div>
      )}
      {members && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>成员</TableHead>
              <TableHead>角色</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>加入时间</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {members.map((member) => (
              <MemberCells key={member.id} member={member} />
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  )
}

/** Read-only member row for the mock store. */
function MemberCells({ member }: { member: MemberWithUser }) {
  return (
    <TableRow>
      <TableCell>
        <div className="flex items-center gap-2">
          <ActorAvatar actor={member} size="sm" />
          <div>
            <p className="text-sm font-medium">{member.name}</p>
            <p className="text-xs text-muted-foreground">{member.email}</p>
          </div>
        </div>
      </TableCell>
      <TableCell>{ROLE_LABELS[member.role] ?? member.role}</TableCell>
      <MemberStatusCells member={member} />
    </TableRow>
  )
}

/** Status badge and join-date cells shared by both member tables. */
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

/**
 * Dialog for adding an already-registered user to the space by email. Only
 * admin/owner actors see the trigger; the new member is always created with the
 * fixed `member` role (role management is out of scope). The dialog closes on
 * success and the membership list refreshes via query invalidation. An unknown
 * email surfaces the backend `user_not_registered` fault as a friendly hint.
 */
function AddMemberDialog({
  open,
  onOpenChange,
  tenantId,
  spaceId,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  tenantId: string
  spaceId: string
}) {
  const [email, setEmail] = useState('')
  const [attempted, setAttempted] = useState(false)
  const addMember = useAddSpaceMemberByEmail(tenantId, spaceId)
  const errorCode = addMember.error?.response?.data?.code

  // Local validation only: empty or malformed addresses stop before the request.
  // `attempted` gates the hints so a pristine field stays quiet until first submit.
  let localHint: string | undefined
  if (attempted && email.trim() === '') {
    localHint = '请输入邮箱地址。'
  } else if (attempted && !email.includes('@')) {
    localHint = '请输入有效的邮箱地址。'
  }
  let serverHint: string | undefined
  if (errorCode === 'user_not_registered') {
    serverHint = '该邮箱尚未注册，请先完成注册。'
  } else if (errorCode === 'space_role_required') {
    serverHint = '你没有权限添加成员。'
  } else if (errorCode) {
    serverHint = `添加失败：${errorCode}`
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>添加成员</DialogTitle>
          <DialogDescription>输入已注册用户的邮箱，将其添加为普通成员。</DialogDescription>
        </DialogHeader>
        <form
          onSubmit={(e) => {
            e.preventDefault()
            if (!attempted) setAttempted(true)
            if (email.trim() === '' || !email.includes('@') || addMember.isPending) return
            addMember.mutate(
              { email: email.trim() },
              {
                onSuccess: () => {
                  onOpenChange(false)
                  setEmail('')
                  setAttempted(false)
                },
              },
            )
          }}
          noValidate
          className="space-y-4"
        >
          <DialogFormField
            id="new-member-email"
            label="成员邮箱"
            value={email}
            onChange={setEmail}
            placeholder="member@example.com"
            hint={localHint ?? serverHint}
            required
          />
          <Button type="submit" className="w-full" disabled={addMember.isPending}>
            {addMember.isPending ? '添加中…' : '添加'}
          </Button>
        </form>
      </DialogContent>
    </Dialog>
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
  const [addOpen, setAddOpen] = useState(false)
  const updateMember = useUpdateSpaceMember(tenantId, spaceId)
  const canManage = myRole === 'admin' || myRole === 'owner'
  const errorCode = updateMember.error?.response?.data?.code

  function update(role: SpaceRole, status: 'active' | 'disabled', member: MemberWithUser) {
    updateMember.mutate({ userId: member.id, role, status, version: member.version ?? 0 })
  }

  return (
    <div className="p-4">
      {canManage && (
        <>
          <div className="mb-4">
            <Button onClick={() => setAddOpen(true)}>添加成员</Button>
            <AddMemberDialog
              open={addOpen}
              onOpenChange={setAddOpen}
              tenantId={tenantId}
              spaceId={spaceId}
            />
          </div>
        </>
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

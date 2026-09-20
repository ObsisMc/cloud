import { ActorAvatar } from '@/components/common/actor-avatar'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useMembers } from '@/features/members/api'

const ROLE_LABELS: Record<string, string> = {
  admin: '管理员',
  member: '成员',
}

const STATUS_LABELS: Record<string, string> = {
  active: '已加入',
  disabled: '已停用',
}
const SKELETON_KEYS = ['one', 'two', 'three', 'four', 'five']

export function MembersPage({ slug }: { slug: string }) {
  const { data: members, isPending } = useMembers(slug)

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
            </TableRow>
          </TableHeader>
          <TableBody>
            {members.map((member) => (
              <TableRow key={member.userId}>
                <TableCell>
                  <div className="flex items-center gap-2">
                    <ActorAvatar actor={{ name: member.displayName }} size="sm" />
                    <p className="text-sm font-medium">{member.displayName}</p>
                  </div>
                </TableCell>
                <TableCell>{ROLE_LABELS[member.role] ?? member.role}</TableCell>
                <TableCell>
                  <Badge variant={member.status === 'active' ? 'secondary' : 'outline'}>
                    {STATUS_LABELS[member.status] ?? member.status}
                  </Badge>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  )
}

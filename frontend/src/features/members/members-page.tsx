import { format } from 'date-fns'
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
  owner: '所有者',
  admin: '管理员',
  member: '成员',
}

const STATUS_LABELS: Record<string, string> = {
  active: '已加入',
  invited: '待加入',
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
              <TableHead>加入时间</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {members.map((member) => (
              <TableRow key={member.id}>
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
                <TableCell>
                  <Badge variant={member.status === 'active' ? 'secondary' : 'outline'}>
                    {STATUS_LABELS[member.status] ?? member.status}
                  </Badge>
                </TableCell>
                <TableCell className="text-muted-foreground">
                  {format(new Date(member.joinedAt), 'yyyy年M月d日')}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  )
}

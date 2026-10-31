import type { MemberListItem } from '@/api/generated.schemas'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

/** v2 knows two roles; Cloud's `admin` vocabulary is shown only if a roster ever carries it. */
const roleLabels: Record<string, string> = {
  owner: 'owner',
  admin: '管理员',
  member: '成员',
}

/**
 * The workspace roster as the server reports it. It is read-only here: v2 has no role change,
 * disable or removal endpoint, so the table never offers an action the backend cannot perform.
 */
export function MemberRoster({
  members,
  currentUserId,
}: {
  members: MemberListItem[]
  currentUserId: string
}) {
  return (
    <div className="overflow-hidden rounded-lg border">
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
              <TableCell className="py-4">
                <div className="flex items-center gap-3">
                  <span
                    className="flex size-8 shrink-0 items-center justify-center rounded-full bg-muted text-xs"
                    aria-hidden="true"
                  >
                    {member.displayName.slice(0, 1)}
                  </span>
                  <span className="text-sm font-medium">
                    {member.displayName}
                    {member.userId === currentUserId && (
                      <span className="ml-2 text-xs text-muted-foreground">
                        你
                      </span>
                    )}
                  </span>
                </div>
              </TableCell>
              <TableCell>
                <Badge
                  variant={member.role === 'owner' ? 'default' : 'secondary'}
                >
                  {roleLabels[member.role] ?? member.role}
                </Badge>
              </TableCell>
              <TableCell>
                <Badge
                  variant={member.status === 'active' ? 'secondary' : 'outline'}
                >
                  {member.status === 'active' ? '有效' : '已停用'}
                </Badge>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

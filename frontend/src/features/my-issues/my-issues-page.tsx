import { useSession } from '@/features/auth/session'
import { IssuesList } from '@/features/issues/issues-list'

export function MyIssuesPage({ slug }: { slug: string }) {
  const { session } = useSession()
  const userId = session.status === 'signed-in' ? session.user.id : undefined
  return <IssuesList slug={slug} title="我的任务" {...(userId ? { assigneeUserId: userId } : {})} />
}

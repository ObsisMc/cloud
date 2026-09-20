import { IssuesList } from '@/features/issues/issues-list'
import { useAuthStore } from '@/state/auth-store'

export function MyIssuesPage({ slug }: { slug: string }) {
  const userId = useAuthStore((s) => s.user?.id)
  return <IssuesList slug={slug} title="我的任务" {...(userId ? { assigneeUserId: userId } : {})} />
}

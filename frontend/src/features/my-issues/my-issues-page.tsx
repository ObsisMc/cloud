import { IssuesList } from '@/features/issues/issues-list'
import { currentUserId } from '@/mocks/data/store'

export function MyIssuesPage({ slug }: { slug: string }) {
  return <IssuesList slug={slug} title="我的任务" assigneeId={currentUserId} />
}

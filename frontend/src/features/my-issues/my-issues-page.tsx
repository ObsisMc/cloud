import { IssuesList } from '@/features/issues/issues-list'
import { currentUserId } from '@/mocks/data/store'

export function MyIssuesPage({ slug }: { slug: string }) {
  return <IssuesList slug={slug} title="My Issues" assigneeId={currentUserId} />
}

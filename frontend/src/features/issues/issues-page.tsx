import { IssuesList } from '@/features/issues/issues-list'

export function IssuesPage({ slug }: { slug: string }) {
  return <IssuesList slug={slug} title="Issues" />
}

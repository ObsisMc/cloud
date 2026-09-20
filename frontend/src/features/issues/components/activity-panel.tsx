import { useState } from 'react'
import { ActorAvatar, type AvatarActor } from '@/components/common/actor-avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Textarea } from '@/components/ui/textarea'
import { useComments, useCreateComment, useRuns } from '@/features/issues/api'
import { memberNameById } from '@/features/issues/present'
import { cn } from '@/lib/utils'
import type { IssueComment, IssueRun, TenantMember } from '@/features/issues/types'

/** Human actor for a comment author; only `user` resolves to a member name. */
function commentActor(comment: IssueComment, names: ReadonlyMap<string, string>): {
  name: string
  type: NonNullable<AvatarActor['type']>
} {
  if (comment.authorUserId) return { name: names.get(comment.authorUserId) ?? '用户', type: 'user' }
  if (comment.authorType === 'agent') return { name: 'Agent', type: 'agent' }
  if (comment.authorType === 'team') return { name: 'Team', type: 'team' }
  return { name: '系统', type: 'user' }
}

function CommentItem({
  comment,
  names,
}: {
  comment: IssueComment
  names: ReadonlyMap<string, string>
}) {
  const author = commentActor(comment, names)
  return (
    <div className={cn('space-y-1', comment.parentId && 'ml-8 border-l pl-3')}>
      <div className="flex items-center gap-2">
        <ActorAvatar actor={{ name: author.name, type: author.type }} size="sm" />
        <span className="text-sm font-medium">{author.name}</span>
        <span className="text-xs text-muted-foreground">#{comment.seq}</span>
      </div>
      <p className="whitespace-pre-wrap text-sm text-muted-foreground">{comment.body}</p>
    </div>
  )
}

function RunItem({ run }: { run: IssueRun }) {
  return (
    <div className="flex items-center gap-2 rounded-md border px-2.5 py-1.5 text-sm">
      <span className="text-xs text-muted-foreground">运行</span>
      <span>{run.executorType}</span>
      <Badge variant="secondary" className="ml-auto">
        {run.status}
      </Badge>
    </div>
  )
}

/**
 * Issue activity column: comments (list + add) followed by a run summary. No
 * synthetic timeline — this wave only has real comments and the persisted runs.
 */
export function ActivityPanel({
  slug,
  issueId,
  members,
}: {
  slug: string
  issueId: string
  members: TenantMember[]
}) {
  const { data: comments, isPending: commentsPending } = useComments(slug, issueId)
  const { data: runs, isPending: runsPending } = useRuns(slug, issueId)
  const createComment = useCreateComment(slug, issueId)
  const [body, setBody] = useState('')
  const names = memberNameById(members)

  function submit(e: React.FormEvent) {
    e.preventDefault()
    const text = body.trim()
    if (!text) return
    createComment.mutate({ body: text }, { onSuccess: () => setBody('') })
  }

  return (
    <section className="space-y-4 pt-4">
      <h2 className="text-sm font-semibold">活动</h2>
      <form onSubmit={submit} className="space-y-2">
        <Textarea
          placeholder="添加评论…"
          value={body}
          onChange={(e) => setBody(e.target.value)}
          rows={2}
        />
        <div className="flex justify-end">
          <Button type="submit" size="sm" disabled={!body.trim() || createComment.isPending}>
            {createComment.isPending ? '提交中…' : '评论'}
          </Button>
        </div>
      </form>
      {commentsPending && <Skeleton className="h-16 w-full" />}
      {!commentsPending && comments?.length === 0 && (
        <p className="text-sm text-muted-foreground">暂无评论。</p>
      )}
      {comments?.map((comment) => (
        <CommentItem key={comment.id} comment={comment} names={names} />
      ))}
      {runsPending && <Skeleton className="h-10 w-full" />}
      {!runsPending && runs && runs.length > 0 && (
        <div className="space-y-2">
          <h3 className="text-xs font-medium text-muted-foreground">运行记录</h3>
          {runs.map((run) => (
            <RunItem key={run.id} run={run} />
          ))}
        </div>
      )}
    </section>
  )
}

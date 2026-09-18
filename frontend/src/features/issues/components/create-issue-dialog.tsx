import { useState } from 'react'
import { PRIORITY_ORDER, STATUS_ORDER, priorityLabelText, statusLabelText } from '@/components/common/issue-badges'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { useCreateIssue } from '@/features/issues/api'
import { db } from '@/mocks/data/store'
import type { IssuePriority, IssueStatus } from '@/mocks/data/types'

export function CreateIssueDialog({
  slug,
  defaultStatus = 'backlog',
  trigger,
}: {
  slug: string
  defaultStatus?: IssueStatus
  trigger?: React.ReactElement
}) {
  const [open, setOpen] = useState(false)
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [status, setStatus] = useState<IssueStatus>(defaultStatus)
  const [priority, setPriority] = useState<IssuePriority>('none')
  const [projectId, setProjectId] = useState<string>('none')
  const createIssue = useCreateIssue(slug)

  function reset() {
    setTitle('')
    setDescription('')
    setStatus(defaultStatus)
    setPriority('none')
    setProjectId('none')
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!title.trim()) return
    createIssue.mutate(
      { title, description, status, priority, projectId: projectId === 'none' ? null : projectId },
      { onSuccess: () => { setOpen(false); reset() } },
    )
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger render={trigger ?? <Button size="sm">新建任务</Button>} />
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>新建任务</DialogTitle>
          </DialogHeader>
          <div className="space-y-3 py-4">
            <Input
              placeholder="任务标题"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              autoFocus
              required
            />
            <Textarea
              placeholder="添加描述…"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={3}
            />
            <div className="flex flex-wrap gap-2">
              <Select value={status} onValueChange={(v) => setStatus(v as IssueStatus)}>
                <SelectTrigger className="w-36">
                  <SelectValue>{(value: unknown) => statusLabelText(value as IssueStatus)}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {STATUS_ORDER.map((s) => (
                    <SelectItem key={s} value={s}>{statusLabelText(s)}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select value={priority} onValueChange={(v) => setPriority(v as IssuePriority)}>
                <SelectTrigger className="w-36">
                  <SelectValue>{(value: unknown) => priorityLabelText(value as IssuePriority)}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {PRIORITY_ORDER.map((pr) => (
                    <SelectItem key={pr} value={pr}>{priorityLabelText(pr)}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select value={projectId} onValueChange={(v) => setProjectId(v ?? 'none')}>
                <SelectTrigger className="w-40">
                  <SelectValue placeholder="项目">
                    {(value: unknown) =>
                      value === 'none' ? '无项目' : (db.projects.find((p) => p.id === value)?.title ?? '项目')
                    }
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="none">无项目</SelectItem>
                  {db.projects.map((p) => (
                    <SelectItem key={p.id} value={p.id}>{p.title}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <DialogFooter>
            <DialogClose render={<Button type="button" variant="outline">取消</Button>} />
            <Button type="submit" disabled={!title.trim() || createIssue.isPending}>
              {createIssue.isPending ? '创建中…' : '创建任务'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

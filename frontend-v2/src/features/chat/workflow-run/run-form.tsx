import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { isAxiosError } from 'axios'
import { useGetApiV1TenantsTidMembers } from '@/api/members/members'
import { DtsReceipt } from './dts-receipt'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router'
import {
  ArrowUpRight,
  CheckCircle2,
  ChevronDown,
  CircleDashed,
  ClipboardCheck,
  FileText,
  ListFilter,
  RotateCcw,
  Save,
  Send,
  ShieldCheck,
  TriangleAlert,
  XCircle,
} from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel, FieldError } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Progress } from '@/components/ui/progress'
import { Textarea } from '@/components/ui/textarea'
import { getWorkflowForm, saveWorkflowFormDraft, submitWorkflowForm, retryWorkflowFormTasks, type WorkflowForm } from '@/lib/workflows/api/generated'
import { TaskAssigneePicker } from '@/components/task-assignee-picker'
import type { TaskAssigneeRef } from '@/features/collaboration/tasks-api/generated'
import { listSquads } from '@/lib/squads-api/generated'
import { cn } from '@/lib/utils'

type Answer = { key: string; decision: string; assignee?: TaskAssigneeRef; values: Record<string, string> }
type FindingFilter = 'all' | 'pending' | 'confirmed' | 'rejected'
const text = (v: unknown) => typeof v === 'string' ? v : ''
const assigneeRef = (value: { type?: string; id?: string } | null | undefined): TaskAssigneeRef | undefined => {
  if (value?.id === undefined || (value.type !== 'human' && value.type !== 'agent' && value.type !== 'squad')) return undefined
  return { type: value.type, id: value.id }
}
const wireAnswers = (answers: Answer[]) => answers.map(answer => ({
  key: answer.key,
  decision: answer.decision,
  ...(answer.assignee === undefined ? {} : { assignee: { type: answer.assignee.type, id: answer.assignee.id } }),
  values: answer.values,
}))

function readAnswers(form: WorkflowForm): Answer[] {
  return form.findings.map(row => {
    const saved = form.answers.find(a => a.key === row.key)
    const existing = form.existingTasks?.find(t => t.key === row.key)
    return {
      key: row.key,
      decision: saved?.decision || 'pending',
      values: saved?.values ?? {},
      assignee: assigneeRef(saved?.assignee) ?? assigneeRef(existing?.assignee),
    }
  })
}

const severityTone = (severity: string) => {
  if (severity.includes('高') || /critical|high/i.test(severity)) return 'border-destructive/30 bg-destructive/10 text-destructive'
  if (severity.includes('中') || /medium/i.test(severity)) return 'border-warning/35 bg-warning/10 text-warning'
  if (severity.includes('低') || /low/i.test(severity)) return 'border-info/30 bg-info/10 text-info'
  return 'border-border bg-muted text-muted-foreground'
}

const decisionLabel = (decision: string) => decision === 'confirmed' ? '是问题' : decision === 'rejected' ? '不是问题' : '待确认'

/** Reads a durable review; refreshing never invents a new form or task. */
export function RunForm({ tenantId, runId, runStatus }: { tenantId: string; runId: string; runStatus: string }) {
  const query = useQuery({ queryKey: ['workflow-form', tenantId, runId], queryFn: () => getWorkflowForm(tenantId, runId), refetchInterval: 3000, retry: false })
  if (query.isPending) return <div className="flex min-h-48 items-center justify-center text-sm text-muted-foreground" role="status">正在读取确认表单…</div>
  if (!query.data) return <div className="flex min-h-48 flex-col items-center justify-center gap-3 text-sm text-muted-foreground" role="status"><CircleDashed className="size-6" />表单尚未生成，运行到确认步骤后会自动显示。<Button variant="outline" onClick={() => void query.refetch()}>刷新</Button></div>
  return <FormEditor key={query.data.id} form={query.data} tenantId={tenantId} runStatus={runStatus} />
}

function FormEditor({ form, tenantId, runStatus }: { form: WorkflowForm; tenantId: string; runStatus: string }) {
  const roster = useGetApiV1TenantsTidMembers(tenantId, undefined, { query: { enabled: !!tenantId } })
  const client = useQueryClient()
  const [version, setVersion] = useState(form.version)
  const [answers, setAnswers] = useState<Answer[]>(() => readAnswers(form))
  const [dirty, setDirty] = useState(false)
  const [locallySubmitted, setLocallySubmitted] = useState(false)
  if (!dirty && form.version > version) {
    setVersion(form.version)
    setAnswers(readAnswers(form))
  }
  const displayAnswers = form.status === 'open' ? answers : readAnswers(form)
  const [notice, setNotice] = useState('')
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [filter, setFilter] = useState<FindingFilter>('all')
  const [submitKey] = useState(() => crypto.randomUUID())
  const squads = useQuery({ queryKey: ['squads', tenantId], queryFn: () => listSquads(tenantId) })
  const mutation = useMutation({
    mutationFn: (final: boolean) => final
      ? submitWorkflowForm(tenantId, form.runId, { version, answers: wireAnswers(answers) }, { headers: { 'Idempotency-Key': submitKey } })
      : saveWorkflowFormDraft(tenantId, form.runId, { version, answers: wireAnswers(answers) }),
    onError: (error) => {
      if (isAxiosError(error)) {
        const data = error.response?.data as { key?: string; field?: string } | undefined
        if (data?.key && data.field) setErrors({ [`${data.key}:${data.field}`]: '请检查这一项的输入。' })
      }
    },
    onSuccess: (saved, final) => {
      setDirty(false)
      if (final) setLocallySubmitted(true)
      setVersion(saved.version)
      setNotice(final ? '确认已提交，正在创建或关联任务。' : '草稿已保存。')
      void client.invalidateQueries({ queryKey: ['workflow-form', tenantId, form.runId] })
      void client.invalidateQueries({ queryKey: ['workflow-runs', tenantId] })
    },
  })
  const retry = useMutation({
    mutationFn: () => retryWorkflowFormTasks(tenantId, form.runId),
    onSuccess: () => client.invalidateQueries({ queryKey: ['workflow-form', tenantId, form.runId] }),
  })
  const readOnly = !form.canEdit || mutation.isPending || locallySubmitted
  const remaining = displayAnswers.filter(a => a.decision === 'pending').length
  const complete = remaining === 0 && answers.every(a => a.decision !== 'confirmed' || a.assignee || form.existingTasks?.some(t => t.key === a.key))
  const fields = form.config.fields
  const counts = {
    confirmed: displayAnswers.filter(a => a.decision === 'confirmed').length,
    rejected: displayAnswers.filter(a => a.decision === 'rejected').length,
    pending: remaining,
  }
  const total = displayAnswers.length
  const progress = total === 0 ? 100 : Math.round(((total - remaining) / total) * 100)
  const visibleFindings = form.findings.filter(row => {
    if (filter === 'all') return true
    return (displayAnswers.find(a => a.key === row.key)?.decision || 'pending') === filter
  })
  const title = text(form.config.title) || '问题确认'
  const submittedBy = roster.data?.items.find(m => m.userId === form.submittedBy)?.displayName ?? '确认人'

  function patch(key: string, change: Partial<Answer>) {
    setDirty(true)
    setErrors({})
    setAnswers(current => current.map(a => a.key === key ? { ...a, ...change } : a))
    setNotice('有未保存的修改')
  }

  function save(final: boolean) {
    const found: Record<string, string> = {}
    if (final) {
      for (const a of answers) {
        if (a.decision === 'pending') found[`${a.key}:decision`] = '请选择确认结果。'
        if (a.decision === 'confirmed') {
          if (!a.assignee && !form.existingTasks?.some(t => t.key === a.key)) found[`${a.key}:assignee`] = '请选择负责人。'
          for (const f of fields) if (f.required === true && f.type !== 'readonly' && !a.values[text(f.name)]?.trim()) found[`${a.key}:${text(f.name)}`] = '请填写这一项。'
        }
      }
    }
    setErrors(found)
    if (Object.keys(found).length) return
    mutation.mutate(final)
  }

  const statusLine = form.status === 'cancelled'
    ? '运行已取消，表单不可提交'
    : form.status === 'submitted'
      ? runStatus === 'failed'
        ? '确认已保存 · 提单失败，可重试'
        : runStatus === 'succeeded'
          ? '确认完成 · 查看下方处理结果'
          : '确认已保存 · 正在生成任务'
      : `${remaining} 项待确认 · 已处理 ${total - remaining}/${total}`

  return <section className="flex w-full flex-col gap-5" aria-label="报告与问题确认">
    <header className="flex flex-wrap items-start justify-between gap-4">
      <div className="min-w-0">
        <p className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground"><ClipboardCheck className="size-3.5" />确认步骤</p>
        <h2 className="mt-1 text-xl font-semibold tracking-tight">{title}</h2>
        <p className="mt-1 text-sm text-muted-foreground">{statusLine}</p>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant="outline" className={cn('gap-1.5', form.status === 'submitted' ? 'border-success/30 bg-success/10 text-success' : form.status === 'cancelled' ? 'border-muted-foreground/30 bg-muted text-muted-foreground' : 'border-warning/35 bg-warning/10 text-warning')}>
          {form.status === 'submitted' ? <CheckCircle2 className="size-3" /> : form.status === 'cancelled' ? <XCircle className="size-3" /> : <CircleDashed className="size-3" />}
          {form.status === 'submitted' ? '已提交' : form.status === 'cancelled' ? '已取消' : '等待确认'}
        </Badge>
        <Badge variant="secondary" className="tabular-nums">{total - remaining}/{total} 已处理</Badge>
      </div>
    </header>

    <div className="grid min-w-0 gap-4 lg:grid-cols-[minmax(0,0.92fr)_minmax(0,1.08fr)] lg:items-start xl:gap-6">
      <aside className="min-w-0 lg:sticky lg:top-0">
        <div className="overflow-hidden rounded-2xl border bg-card shadow-sm">
          <div className="border-b bg-muted/25 px-5 py-4">
            <div className="flex items-center justify-between gap-3">
              <div className="flex items-center gap-2">
                <span className="flex size-8 items-center justify-center rounded-lg bg-brand-soft text-brand"><FileText className="size-4" /></span>
                <div><h3 className="font-semibold">报告</h3><p className="text-xs text-muted-foreground">内容、证据与处置建议</p></div>
              </div>
              <Badge variant="outline" className="font-mono text-[10px]">{total} 项</Badge>
            </div>
          </div>
          <div className="px-5 py-4">
            <div className="flex items-center justify-between gap-3 text-xs"><span className="text-muted-foreground">确认进度</span><span className="font-medium tabular-nums">{progress}%</span></div>
            <Progress value={progress} className="mt-2" aria-label="确认进度" />
            <dl className="mt-4 grid grid-cols-3 gap-2">
              <div className="rounded-lg bg-warning-soft px-3 py-2"><dt className="text-[10px] text-muted-foreground">待确认</dt><dd className="mt-0.5 text-lg font-semibold tabular-nums text-warning">{counts.pending}</dd></div>
              <div className="rounded-lg bg-success-soft px-3 py-2"><dt className="text-[10px] text-muted-foreground">是问题</dt><dd className="mt-0.5 text-lg font-semibold tabular-nums text-success">{counts.confirmed}</dd></div>
              <div className="rounded-lg bg-muted px-3 py-2"><dt className="text-[10px] text-muted-foreground">不是问题</dt><dd className="mt-0.5 text-lg font-semibold tabular-nums text-muted-foreground">{counts.rejected}</dd></div>
            </dl>
            <div className="mt-4 max-h-[44vh] overflow-auto rounded-xl border bg-background px-4 py-3 text-sm leading-6 lg:max-h-[52vh]">
              <div className="[&_h1]:mb-3 [&_h1]:text-lg [&_h1]:font-semibold [&_h2]:my-3 [&_h2]:text-base [&_h2]:font-semibold [&_h3]:my-2 [&_h3]:font-medium [&_li]:my-1 [&_p]:my-2 [&_pre]:overflow-x-auto [&_pre]:rounded-lg [&_pre]:bg-muted [&_pre]:p-3 [&_table]:w-full [&_table]:border-collapse [&_td]:border [&_td]:p-2 [&_td]:align-top [&_th]:border [&_th]:bg-muted/50 [&_th]:p-2 [&_th]:text-left">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{form.report}</ReactMarkdown>
              </div>
            </div>
            {form.status === 'submitted' && <p className="mt-3 text-xs text-muted-foreground">提交人：{submittedBy}{form.submittedAt ? ` · ${new Date(form.submittedAt).toLocaleString()}` : ''}</p>}
          </div>
        </div>
      </aside>

      <div className="min-w-0">
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border bg-muted/20 px-3 py-2.5">
          <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground"><ListFilter className="size-3.5" />问题列表</div>
          <div role="group" aria-label="筛选问题" className="flex flex-wrap gap-1">
            {([
              ['all', '全部', total],
              ['pending', '待确认', counts.pending],
              ['confirmed', '是问题', counts.confirmed],
              ['rejected', '不是问题', counts.rejected],
            ] as const).map(([value, label, count]) => <Button key={value} type="button" size="xs" variant={filter === value ? 'secondary' : 'ghost'} aria-pressed={filter === value} onClick={() => setFilter(value)}>{label}<span className="tabular-nums text-muted-foreground">{count}</span></Button>)}
          </div>
        </div>

        {form.findings.length === 0 && <p className="mt-4 rounded-xl border border-dashed bg-muted/20 px-4 py-8 text-center text-sm text-muted-foreground">本次未发现需要人工处理的问题。</p>}
        {form.findings.length > 0 && visibleFindings.length === 0 && <p className="mt-4 rounded-xl border border-dashed bg-muted/20 px-4 py-8 text-center text-sm text-muted-foreground">这个筛选下没有问题。</p>}

        <div className="mt-3 flex flex-col gap-3">
          {visibleFindings.map(row => {
            const key = text(row.key)
            const answer = displayAnswers.find(a => a.key === key)!
            const result = form.results.find(r => r.key === key)
            const existing = form.existingTasks?.find(r => r.key === key)
            const severity = text(row.severity)
            const decision = answer.decision
            return <article key={key} className={cn('overflow-hidden rounded-xl border bg-card shadow-sm transition-colors', decision === 'pending' && form.status === 'open' && 'border-warning/35', decision === 'confirmed' && 'border-success/30', decision === 'rejected' && 'border-border/80')}>
              <div className="flex flex-wrap items-start justify-between gap-3 border-b bg-muted/15 px-4 py-3">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <Badge variant="outline" className={cn('text-[10px]', severityTone(severity))}><TriangleAlert className="size-3" />{severity || '未定级'}</Badge>
                    <Badge variant="outline" className="text-[10px]">{text(row.module) || '未指定模块'}</Badge>
                    <Badge variant="outline" className={cn('gap-1 text-[10px]', decision === 'confirmed' ? 'border-success/30 bg-success/10 text-success' : decision === 'rejected' ? 'border-muted-foreground/30 bg-muted text-muted-foreground' : 'border-warning/35 bg-warning/10 text-warning')}>
                      {decision === 'confirmed' ? <CheckCircle2 className="size-3" /> : decision === 'rejected' ? <XCircle className="size-3" /> : <CircleDashed className="size-3" />}{decisionLabel(decision)}
                    </Badge>
                  </div>
                  <h3 className="mt-2 font-semibold leading-snug">{text(row.title)}</h3>
                  <p className="mt-0.5 text-sm text-muted-foreground">{text(row.description)}</p>
                </div>
              </div>
              <div className="grid gap-4 px-4 py-4">
                <details className="group rounded-lg border bg-muted/15">
                  <summary className="flex cursor-pointer list-none items-center justify-between gap-2 px-3 py-2 text-xs font-medium text-muted-foreground"><span className="flex items-center gap-1.5"><ShieldCheck className="size-3.5" />查看证据</span><ChevronDown className="size-3.5 transition-transform group-open:rotate-180" /></summary>
                  <p className="whitespace-pre-wrap border-t px-3 py-2.5 text-sm leading-6">{text(row.evidence) || '没有提供证据。'}</p>
                </details>

                <FieldGroup>
                  <div className="grid gap-3 sm:grid-cols-2">
                    <Field data-invalid={!!errors[`${key}:decision`]}>
                      <FieldLabel htmlFor={`${key}-decision`}>是否为问题</FieldLabel>
                      <NativeSelect id={`${key}-decision`} disabled={readOnly} aria-invalid={!!errors[`${key}:decision`]} value={decision} onChange={e => patch(key, { decision: e.target.value })}>
                        <NativeSelectOption value="pending">待确认</NativeSelectOption>
                        <NativeSelectOption value="confirmed">是问题</NativeSelectOption>
                        <NativeSelectOption value="rejected">不是问题</NativeSelectOption>
                      </NativeSelect>
                      <FieldError>{errors[`${key}:decision`]}</FieldError>
                    </Field>
                    {decision === 'confirmed' && <Field data-invalid={!!errors[`${key}:assignee`]}>
                      <FieldLabel>{existing ? '备用负责人' : '负责人'}</FieldLabel>
                      <TaskAssigneePicker tenantId={tenantId} squads={squads.data?.items ?? []} value={answer.assignee ?? null} disabled={readOnly} onAssign={assignee => patch(key, { assignee: assignee ?? undefined })} />
                      {existing && <p className="text-xs text-muted-foreground">已有任务保持原负责人；此处只在新建时使用。</p>}
                      <FieldError>{errors[`${key}:assignee`]}</FieldError>
                    </Field>}
                  </div>
                  {fields.map(f => {
                    const name = text(f.name)
                    const value = answer.values[name] ?? ''
                    if (f.type === 'readonly') return <Field key={name}><FieldLabel>{text(f.label)}</FieldLabel><p className="text-sm whitespace-pre-wrap">{text(Object.entries(row).find(([key]) => key === name)?.[1])}</p></Field>
                    const update = (value: string) => patch(key, { values: { ...answer.values, [name]: value } })
                    return <Field key={name} data-invalid={!!errors[`${key}:${name}`]}>
                      <FieldLabel htmlFor={`${key}-${name}`}>{text(f.label)}{f.required === true ? ' *' : ''}</FieldLabel>
                      {f.type === 'select'
                        ? <NativeSelect id={`${key}-${name}`} aria-invalid={!!errors[`${key}:${name}`]} disabled={readOnly} value={value} onChange={e => update(e.target.value)}><NativeSelectOption value="">请选择</NativeSelectOption>{(Array.isArray(f.options) ? f.options : []).map(o => <NativeSelectOption key={String(o)} value={String(o)}>{String(o)}</NativeSelectOption>)}</NativeSelect>
                        : f.type === 'textarea'
                          ? <Textarea id={`${key}-${name}`} aria-invalid={!!errors[`${key}:${name}`]} disabled={readOnly} value={value} onChange={e => update(e.target.value)} rows={3} />
                          : <Input id={`${key}-${name}`} aria-invalid={!!errors[`${key}:${name}`]} disabled={readOnly} value={value} onChange={e => update(e.target.value)} />}
                      <FieldError>{errors[`${key}:${name}`]}</FieldError>
                    </Field>
                  })}
                </FieldGroup>

                {existing && !result && <p className="flex items-center gap-2 rounded-lg bg-info-soft px-3 py-2 text-sm text-info">确认后将关联已有任务，沿用其负责人：<Link className="inline-flex items-center gap-1 font-medium underline underline-offset-2" to={`/collaboration/tasks/${text(existing.taskId)}`}>查看任务<ArrowUpRight className="size-3" /></Link></p>}
                {form.status === 'submitted' && decision === 'rejected' && <p className="rounded-lg bg-muted px-3 py-2 text-sm text-muted-foreground">已确认无需处理</p>}
                {result && <div className="rounded-lg border border-success/25 bg-success-soft px-3 py-2.5 text-sm"><p className="flex items-center gap-2 font-medium text-success">{result.action === 'created' ? <ClipboardCheck className="size-4" /> : <CheckCircle2 className="size-4" />}{result.action === 'created' ? '已创建任务' : '已关联现有任务'}</p><Link className={cn(buttonVariants({ size: 'sm', variant: 'outline' }), 'mt-2')} to={`/collaboration/tasks/${text(result.taskId)}`}>打开任务<ArrowUpRight /></Link></div>}
                {result && <DtsReceipt taskId={text(result.taskId)} title={text(row.title)} description={text(row.description)} severity={severity} module={text(row.module)} runId={form.runId} />}
              </div>
            </article>
          })}
        </div>

        {dirty && form.version > version && <div role="alert" className="mt-3 rounded-xl border border-warning/35 bg-warning-soft p-3 text-sm"><p className="font-medium">其他确认人已更新这份表单；当前已提交结果以服务端为准。</p><details className="mt-2"><summary className="cursor-pointer text-xs text-muted-foreground">查看未保存的本地修改</summary>{answers.map(a => <p key={a.key} className="mt-1 text-xs">{form.findings.find(f => f.key === a.key)?.title}：{decisionLabel(a.decision)} · {Object.values(a.values).join('；')}</p>)}</details>{form.status === 'open' && <Button className="mt-2" variant="outline" onClick={() => { setAnswers(readAnswers(form)); setVersion(form.version); setDirty(false); setNotice('已载入最新草稿。') }}>载入最新草稿</Button>}</div>}
        {mutation.isError && <p role="alert" className="mt-3 rounded-lg border border-destructive/25 bg-destructive/5 px-3 py-2 text-sm text-destructive">保存失败，请检查必填项或刷新核对最新版本。你的输入仍保留在页面中。</p>}
        {notice && <p role="status" className="mt-3 text-sm text-muted-foreground">{form.status === 'submitted' && form.results.length > 0 ? '任务已创建或关联，可打开查看。' : form.status === 'submitted' && !displayAnswers.some(a => a.decision === 'confirmed') ? '确认已提交，无需创建任务。' : notice}</p>}
        {runStatus === 'failed' && form.status === 'submitted' && form.results.length === 0 && displayAnswers.some(a => a.decision === 'confirmed') && <Button className="mt-3" variant="outline" disabled={retry.isPending} onClick={() => retry.mutate()}><RotateCcw />{retry.isPending ? '正在重试…' : '重试任务创建'}</Button>}
        {retry.isError && <p role="alert" className="mt-3 text-sm text-destructive">任务当前不可重试，请查看运行状态。</p>}
      </div>
    </div>

    {form.status === 'open' && <footer className="sticky bottom-0 z-20 -mx-1 flex flex-wrap items-center gap-3 border-t bg-background/95 px-1 py-3 backdrop-blur supports-[backdrop-filter]:bg-background/85">
      <div className="min-w-0 flex-1"><p className="text-sm font-medium">{remaining === 0 ? '全部问题已处理' : `还有 ${remaining} 项待确认`}</p><p className="text-xs text-muted-foreground">保存草稿不会创建任务；提交后由后续节点统一处理。</p></div>
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="outline" disabled={readOnly} onClick={() => save(false)}><Save />保存草稿</Button>
        <Button disabled={readOnly || !complete} onClick={() => save(true)}>{mutation.isPending ? <><span className="size-3.5 animate-spin rounded-full border-2 border-current border-t-transparent" />正在保存…</> : <><Send />{text(form.config.submitLabel) || '确认并生成任务'}</>}</Button>
      </div>
    </footer>}
    {form.status === 'open' && !form.canEdit && <p className="text-sm text-muted-foreground">仅指定确认人或管理员可以提交。</p>}
  </section>
}

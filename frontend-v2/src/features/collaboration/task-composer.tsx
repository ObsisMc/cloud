import { useMemo, useRef, useState } from 'react'
import { AtSign, Bot, SendHorizontal, Workflow, X } from 'lucide-react'
import { useGetApiV1TenantsTidCollaborationFormsFormRef, useGetApiV1TenantsTidCollaborationTargets } from '@/api/tenants/tenants'
import type { CollaborationTarget, FormField } from '@/api/generated.schemas'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { useWorkflowHasPersonalSkills, WORKFLOW_PERSONAL_SKILL_TEAM_VISIBILITY_HINT } from '@/lib/workflows/personal-skills'
import {
  extractWorkflowInputServerError,
  resolveFormDraftValues,
  validateAndBuildWorkflowValues,
  type FormFieldDraftValue,
} from './comment-workflow'
import { isTaskFinished } from './task-model'
import { agentReach, runsAgent } from './task-views'
import type { Task, TaskCommentWorkflowInput, TaskMentionRef } from './tasks-api/generated'

/** A comment as the composer hands it over. */
export interface CommentDraft {
  body: string
  mentions: TaskMentionRef[]
  toAgent: boolean
  workflow?: TaskCommentWorkflowInput
}

type Picked = TaskMentionRef & { displayName: string }

interface StagedWorkflow {
  workflowId: string
  displayName: string
  formRef: string
}

function WorkflowFieldControl({
  field,
  value,
  error,
  disabled,
  onChange,
}: {
  field: FormField
  value: FormFieldDraftValue
  error: string
  disabled: boolean
  onChange: (next: FormFieldDraftValue) => void
}) {
  const inputId = `comment-wf-field-${field.key}`
  const errorId = `${inputId}-error`
  const labelId = `${inputId}-label`
  const describedBy = error ? errorId : undefined

  return <div className="mc-wf-field">
    {field.type === 'multi_select' || field.type === 'boolean'
      ? <span id={labelId} className="mc-wf-label">{field.label || field.key}{field.required && <span className="mc-required" aria-hidden="true">*</span>}</span>
      : <label htmlFor={inputId} className="mc-wf-label">{field.label || field.key}{field.required && <span className="mc-required" aria-hidden="true">*</span>}</label>}
    {field.type === 'textarea' && (
      <Textarea
        id={inputId}
        rows={2}
        value={typeof value === 'string' ? value : ''}
        placeholder={field.placeholder ?? ''}
        disabled={disabled}
        aria-required={field.required}
        aria-invalid={!!error}
        aria-describedby={describedBy}
        onChange={e => onChange(e.target.value)}
      />
    )}
    {field.type === 'number' && (
      <Input
        id={inputId}
        type="text"
        inputMode="decimal"
        value={typeof value === 'string' ? value : ''}
        placeholder={field.placeholder ?? ''}
        disabled={disabled}
        aria-required={field.required}
        aria-invalid={!!error}
        aria-describedby={describedBy}
        onChange={e => onChange(e.target.value)}
      />
    )}
    {field.type === 'text' && (
      <Input
        id={inputId}
        type="text"
        value={typeof value === 'string' ? value : ''}
        placeholder={field.placeholder ?? ''}
        disabled={disabled}
        aria-required={field.required}
        aria-invalid={!!error}
        aria-describedby={describedBy}
        onChange={e => onChange(e.target.value)}
      />
    )}
    {field.type === 'boolean' && (
      <label className="mc-wf-checkbox">
        <Checkbox
          checked={Boolean(value)}
          disabled={disabled}
          aria-labelledby={labelId}
          aria-invalid={!!error}
          aria-describedby={describedBy}
          onCheckedChange={checked => onChange(checked === true)}
        />
        <span>{field.placeholder || '启用'}</span>
      </label>
    )}
    {field.type === 'select' && (
      <NativeSelect
        id={inputId}
        value={typeof value === 'string' ? value : ''}
        disabled={disabled}
        aria-required={field.required}
        aria-invalid={!!error}
        aria-describedby={describedBy}
        onChange={e => onChange(e.target.value)}
      >
        <NativeSelectOption value="">{field.placeholder || '请选择'}</NativeSelectOption>
        {(field.options ?? []).map(option => (
          <NativeSelectOption key={option.value} value={option.value}>{option.label}</NativeSelectOption>
        ))}
      </NativeSelect>
    )}
    {field.type === 'multi_select' && (
      <div className="mc-wf-multi" role="group" aria-labelledby={labelId}>
        {(field.options ?? []).map(option => {
          const selected = Array.isArray(value) ? value : []
          const checked = selected.includes(option.value)
          return <label key={option.value} className="mc-wf-checkbox">
            <Checkbox
              checked={checked}
              disabled={disabled}
              aria-label={option.label}
              onCheckedChange={next => {
                const updated = next === true
                  ? (checked ? selected : [...selected, option.value])
                  : selected.filter(v => v !== option.value)
                onChange(updated)
              }}
            />
            <span>{option.label}</span>
          </label>
        })}
      </div>
    )}
    {field.description && <small className="mc-muted">{field.description}</small>}
    {error && <p id={errorId} role="alert" className="mc-field-error">{error}</p>}
  </div>
}

/**
 * The task's one input. Whenever the caller may reach the task's Agent, text goes to it as a turn
 * (and to the team as a comment) unless they turn 发给 Agent off; otherwise it is a team comment.
 * Choosing a workflow target from the @ menu loads its launch form and starts a run as the commenter.
 * Only a target picked from the @ menu counts: "@name" typed by hand routes nothing.
 */
export function TaskComposer({ tenantId, task, me, inSquad, sending, error, submitError, onSend }: {
  tenantId: string
  task: Task
  me: string
  inSquad: boolean
  sending: boolean
  error: string
  submitError?: unknown
  onSend: (draft: CommentDraft) => Promise<boolean>
}) {
  const [body, setBody] = useState('')
  const [picked, setPicked] = useState<Picked[]>([])
  // null until the caller flips the switch: then the default, to the Agent whenever it is reachable.
  const [toAgent, setToAgent] = useState<boolean | null>(null)
  const [stagedWorkflow, setStagedWorkflow] = useState<StagedWorkflow | null>(null)
  const [rawValues, setRawValues] = useState<Record<string, FormFieldDraftValue>>({})
  const [clientErrors, setClientErrors] = useState<Record<string, string>>({})
  const [dirtyAfterError, setDirtyAfterError] = useState<Record<string, true>>({})
  const [suppressedError, setSuppressedError] = useState<unknown>(null)
  const [query, setQuery] = useState<string | null>(null)
  const input = useRef<HTMLTextAreaElement>(null)
  const targets = useGetApiV1TenantsTidCollaborationTargets(tenantId, { query: { enabled: !!tenantId, staleTime: 60_000 } })
  const formQuery = useGetApiV1TenantsTidCollaborationFormsFormRef(
    tenantId,
    stagedWorkflow?.formRef ?? '',
    undefined,
    { query: { enabled: !!tenantId && !!stagedWorkflow?.formRef, staleTime: 60_000 } },
  )
  const stagedWorkflowHasPersonalSkills = useWorkflowHasPersonalSkills(tenantId, stagedWorkflow?.workflowId)
  const reach = agentReach(task, me, inSquad)
  const taskFinished = isTaskFinished(task.status)
  const sendToAgent = !stagedWorkflow && reach.ok && (toAgent ?? true)
  const options = useMemo(() => {
    if (query === null) return []
    const q = query.toLocaleLowerCase()
    return (targets.data?.items ?? [])
      .filter(t => t.type !== 'team' && (t.displayName.toLocaleLowerCase().includes(q) || t.description.toLocaleLowerCase().includes(q)))
      .slice(0, 8)
  }, [query, targets.data])

  const fields = useMemo(() => formQuery.data?.fields ?? [], [formQuery.data?.fields])
  const draftValues = useMemo(() => resolveFormDraftValues(rawValues, fields), [rawValues, fields])
  const serverFieldError = submitError && submitError !== suppressedError ? extractWorkflowInputServerError(submitError) : null
  const activeServerFieldError = serverFieldError && !dirtyAfterError[serverFieldError.variable] ? serverFieldError : null
  const unmappedServerError = activeServerFieldError && !fields.some(f => f.key === activeServerFieldError.variable)
    ? activeServerFieldError.message
    : ''

  const clearStagedWorkflow = () => {
    setStagedWorkflow(null)
    setRawValues({})
    setClientErrors({})
    setDirtyAfterError({})
    setPicked(list => list.filter(x => x.type !== 'workflow'))
  }

  const typed = (value: string, caret: number) => {
    setBody(value)
    const before = value.slice(0, caret)
    const at = /(?:^|\s)@([^\s@]*)$/.exec(before)
    setQuery(at ? at[1] : null)
  }

  const choose = (target: CollaborationTarget) => {
    const el = input.current
    const caret = el?.selectionStart ?? body.length
    const before = body.slice(0, caret).replace(/@([^\s@]*)$/, `@${target.displayName} `)
    setBody(before + body.slice(caret))
    setQuery(null)
    if (target.type === 'agent') {
      clearStagedWorkflow()
      setToAgent(true)
    } else if (target.type === 'workflow') {
      setToAgent(false)
      setStagedWorkflow({
        workflowId: target.id,
        displayName: target.displayName,
        formRef: target.interactionDescriptor.formRef || target.id,
      })
      setRawValues({})
      setClientErrors({})
      setDirtyAfterError({})
      setSuppressedError(submitError ?? null)
    }
    if (target.type === 'user' || target.type === 'agent' || target.type === 'workflow') {
      const type = target.type
      setPicked(list => {
        const base = type === 'workflow' ? list.filter(p => p.type !== 'workflow') : list
        return base.some(p => p.type === type && p.id === target.id)
          ? base
          : [...base, { type, id: target.id, displayName: target.displayName }]
      })
    }
    el?.focus()
  }

  const targetEnabled = (target: CollaborationTarget) => {
    if (target.type === 'agent') return task.assignee?.type === 'agent' && task.assignee.id === target.id && reach.ok
    if (target.type === 'workflow') return !taskFinished
    return true
  }

  const targetHint = (target: CollaborationTarget) => {
    if (target.type === 'agent') {
      return targetEnabled(target) ? '发给 Agent' : task.assignee?.id === target.id ? reach.reason : '没有指派给这个任务'
    }
    if (target.type === 'workflow') {
      return taskFinished ? '任务已结束，不能启动工作流' : (target.description || '运行工作流')
    }
    return '提及'
  }

  const submit = async () => {
    const text = body.trim()
    if (!text || sending) return
    if (stagedWorkflow && (formQuery.isPending || formQuery.isError || !formQuery.data)) return
    setSuppressedError(null)
    setDirtyAfterError({})
    let workflowInput: TaskCommentWorkflowInput | undefined
    if (stagedWorkflow && formQuery.data) {
      const validated = validateAndBuildWorkflowValues(formQuery.data.fields, draftValues)
      if (Object.keys(validated.errors).length > 0) {
        setClientErrors(validated.errors)
        return
      }
      setClientErrors({})
      workflowInput = {
        workflowId: stagedWorkflow.workflowId,
        values: validated.values as TaskCommentWorkflowInput['values'],
      }
    }
    const ok = await onSend({
      body: text,
      mentions: picked.map(({ type, id }) => ({ type, id })),
      toAgent: sendToAgent,
      ...(workflowInput ? { workflow: workflowInput } : {}),
    })
    if (ok) {
      setBody('')
      setPicked([])
      setToAgent(null)
      setStagedWorkflow(null)
      setRawValues({})
      setClientErrors({})
      setDirtyAfterError({})
    }
  }

  const workflowBlocked = !!stagedWorkflow && (formQuery.isPending || formQuery.isError || !formQuery.data)

  return <div className="mc-composer">
    {picked.length > 0 && <div className="mc-chips" aria-label="已 @ 的对象">
      {picked.map(p => <span key={`${p.type}:${p.id}`} className={`mc-chip ${p.type}`}>
        {p.type === 'agent' ? <Bot size={12} /> : p.type === 'workflow' ? <Workflow size={12} /> : <AtSign size={12} />}{p.displayName}
        <button type="button" aria-label={`取消 @${p.displayName}`} onClick={() => {
          if (p.type === 'workflow' && stagedWorkflow?.workflowId === p.id) clearStagedWorkflow()
          else setPicked(list => list.filter(x => x !== p))
          if (p.type === 'agent') setToAgent(false)
        }}><X size={11} /></button>
      </span>)}
    </div>}
    <div className="mc-composer-box">
      <Textarea ref={input} aria-label="评论或给 Agent 的消息" placeholder={stagedWorkflow ? `输入评论并启动「${stagedWorkflow.displayName}」…` : sendToAgent ? '告诉 Agent 下一步做什么…' : '写评论，输入 @ 提到成员、Agent 或工作流…'} value={body} rows={3}
        onChange={e => typed(e.target.value, e.target.selectionStart)}
        onKeyDown={e => {
          if (e.key === 'Escape') setQuery(null)
          if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); void submit() }
        }} />
      {query !== null && options.length > 0 && <ul className="mc-mentions" role="listbox" aria-label="@ 对象">
        {options.map(t => <li key={`${t.type}:${t.id}`}>
          <button type="button" role="option" aria-selected="false" disabled={!targetEnabled(t)} onMouseDown={e => e.preventDefault()} onClick={() => choose(t)}>
            {t.type === 'agent' ? <Bot size={14} /> : t.type === 'workflow' ? <Workflow size={14} /> : <AtSign size={14} />}
            <span><b>{t.displayName}</b><small>{targetHint(t)}</small></span>
          </button>
        </li>)}
      </ul>}
    </div>
    {stagedWorkflow && <section className="mc-composer-workflow" aria-label={`启动工作流：${stagedWorkflow.displayName}`}>
      <div className="mc-composer-workflow-head">
        <span className="mc-composer-workflow-title"><Workflow size={14} aria-hidden="true" /><b>{formQuery.data?.title || stagedWorkflow.displayName}</b></span>
        <button type="button" className="mc-link" aria-label={`取消启动 ${stagedWorkflow.displayName}`} onClick={clearStagedWorkflow}>取消</button>
      </div>
      {formQuery.isPending && <p className="mc-muted text-xs" role="status">正在加载启动表单…</p>}
      {formQuery.isError && <div className="mc-composer-workflow-retry" role="alert">
        <span>启动表单加载失败。</span>
        <button type="button" className="mc-link" onClick={() => void formQuery.refetch()}>重试</button>
      </div>}
      {formQuery.data && <>
        {formQuery.data.description && <p className="mc-muted text-xs">{formQuery.data.description}</p>}
        {stagedWorkflowHasPersonalSkills && <p className="mc-muted text-xs">{WORKFLOW_PERSONAL_SKILL_TEAM_VISIBILITY_HINT}</p>}
        {fields.length > 0
          ? <div className="mc-wf-fields">
            {fields.map(field => {
              const fieldError = clientErrors[field.key] ?? (activeServerFieldError?.variable === field.key ? activeServerFieldError.message : '')
              return <WorkflowFieldControl
                key={field.key}
                field={field}
                value={draftValues[field.key]}
                error={fieldError}
                disabled={sending}
                onChange={next => {
                  setRawValues(prev => ({ ...prev, [field.key]: next }))
                  setClientErrors(prev => {
                    if (!prev[field.key]) return prev
                    const copy = { ...prev }
                    delete copy[field.key]
                    return copy
                  })
                  setDirtyAfterError(prev => ({ ...prev, [field.key]: true }))
                }}
              />
            })}
          </div>
          : <p className="mc-muted text-xs">无需额外启动参数，发送评论后将立即启动运行。</p>}
        {unmappedServerError && <p role="alert" className="mc-error">{unmappedServerError}</p>}
      </>}
    </section>}
    <div className="mc-composer-foot">
      {stagedWorkflow
        ? <small className="mc-muted">将以你的身份启动工作流「{stagedWorkflow.displayName}」。</small>
        : runsAgent(task, me, inSquad)
          ? <label className="mc-switch-line" title={reach.reason}><Switch checked={sendToAgent} disabled={!reach.ok} onCheckedChange={setToAgent} aria-label="发给 Agent" /><span className="mc-composer-toggle-label">发给 Agent</span><small>{reach.reason}</small></label>
          : <small className="mc-muted">评论全团队可见。</small>}
      <Button type="button" size="sm" disabled={!body.trim() || sending || workflowBlocked} onClick={() => void submit()}>
        <SendHorizontal data-icon="inline-start" />{sending ? '发送中…' : stagedWorkflow ? '发送并运行' : sendToAgent ? '发给 Agent' : '评论'}
      </Button>
    </div>
    {error && <p role="alert" className="mc-error">{error}</p>}
  </div>
}

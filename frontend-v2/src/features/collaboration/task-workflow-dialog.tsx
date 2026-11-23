import { useRef, useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { I18nextProvider } from 'react-i18next'
import { Workflow } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { PanelDialog } from '@/components/panel/panel-dialog'
import { getWorkflow, listWorkflows, type WorkflowSnapshot } from '@/lib/workflows/api/generated'
import { parseWorkflowGraph } from '@/lib/workflows/runtime'
import { createWorkflowI18n } from '@/lib/workflows/host/i18n'
import { WorkflowStartVariableFields } from '@/lib/workflows/host/start-variable-fields'
import { workflowStartDrafts, parseWorkflowStartDrafts } from '@/lib/workflows/host/start-variable-drafts'
import { graphHasEnabledPersonalSkills, WORKFLOW_PERSONAL_SKILL_TEAM_VISIBILITY_HINT } from '@/lib/workflows/personal-skills'
import { workflowRunTranslations } from '@/features/chat/workflow-run/translations'
import { codeOf } from '@/lib/api-client'
import { taskRunMessageFor } from './task-model'
import { startTaskWorkflow } from './task-workflows-api/generated'
import type { Task } from './tasks-api/generated'

/** Picks a published snapshot explicitly before collecting its typed Start values. */
export function TaskWorkflowDialog({ tenantId, task, onClose, onStarted }: {
  tenantId: string; task: Task; onClose: () => void; onStarted: (workflowRunId: string) => void
}) {
  const [i18n] = useState(()=>createWorkflowI18n(['zh-CN'], {'zh-CN':workflowRunTranslations['zh-CN']}))
  const [picked, setPicked] = useState('')
  const workflows = useQuery({ queryKey: ['workflows', tenantId], queryFn: () => listWorkflows(tenantId) })
  const published = (workflows.data?.workflows ?? []).filter(w => w.publishedVersion)
  const detail = useQuery({ queryKey: ['task-workflow-snapshot', tenantId, picked], queryFn: () => getWorkflow(tenantId, picked), enabled: !!picked })
  return <I18nextProvider i18n={i18n}><PanelDialog icon={<Workflow size={14} />} eyebrow="用工作流处理" title={`处理 ${task.title}`}
    subtitle="选择已发布的工作流，填写需求与开始变量。" onClose={onClose}>
    <div className="p-6"><Field>
      <FieldLabel htmlFor="mc-handle-workflow">工作流</FieldLabel>
      <NativeSelect id="mc-handle-workflow" value={picked} disabled={!published.length} onChange={e => setPicked(e.target.value)}>
        <NativeSelectOption value="">{workflows.isLoading ? '正在加载…' : published.length ? '请选择工作流' : '没有可用的工作流'}</NativeSelectOption>
        {published.map(w => <NativeSelectOption key={w.id} value={w.id}>{w.name}（{w.publishedVersion}）</NativeSelectOption>)}
      </NativeSelect>
    </Field></div>
    {detail.isError && <p role="alert">工作流读取失败，请重新选择。</p>}
    {detail.data?.published
      ? <HandlingForm key={detail.data.published.id} tenantId={tenantId} task={task} snapshot={detail.data.published} onClose={onClose} onStarted={onStarted} />
      : <footer className="mc-form-actions"><Button variant="outline" onClick={onClose}>取消</Button><Button disabled>启动工作流</Button></footer>}
  </PanelDialog></I18nextProvider>
}

function HandlingForm({tenantId,task,snapshot,onClose,onStarted}:{tenantId:string;task:Task;snapshot:WorkflowSnapshot;onClose:()=>void;onStarted:(id:string)=>void}){
  const client=useQueryClient()
  const [variables]=useState(()=>parseWorkflowGraph(snapshot.graph).nodes.find(n=>n.data.kind==='start' && !n.parentId)?.data.inputVariables ?? [])
  const [hasPersonalSkills]=useState(()=>graphHasEnabledPersonalSkills(snapshot.graph))
  const [drafts,setDrafts]=useState(()=>workflowStartDrafts(variables))
  const [prompt,setPrompt]=useState(()=>[task.title,task.description].filter(Boolean).join('\n\n'))
  const [attempted,setAttempted]=useState(false)
  // A network failure is ambiguous: only an identical body may reuse that attempt's key.
  const attempt=useRef<{body:string;key:string}|null>(null)
  const parsed=parseWorkflowStartDrafts(variables,drafts)
  const invalid=parsed.some(p=>!p.result.valid || p.missingRequired || p.exceedsMaxLength)
  const start=useMutation({
    mutationFn:()=>{
      const values=Object.fromEntries(parsed.map(p=>[p.variable.name,p.result.valid && p.result.value!==undefined ? p.result.value : null]))
      const body={workflowId:snapshot.workflowId,snapshotId:snapshot.id,input:prompt.trim(),values}
      const identity=JSON.stringify(body)
      if(attempt.current?.body!==identity)attempt.current={body:identity,key:crypto.randomUUID()}
      return startTaskWorkflow(tenantId,task.id,body,{headers:{'Idempotency-Key':attempt.current!.key}})
    },
    onError:error=>{if(typeof error==='object' && error!==null && 'response' in error && error.response)attempt.current=null},
    onSuccess:async result=>{await client.invalidateQueries({queryKey:['task-workflow-runs',tenantId,task.id]});onStarted(result.workflowRunId)},
  })
  const submit=(event:FormEvent)=>{event.preventDefault();setAttempted(true);if(!invalid)start.mutate()}
  return <form className="mc-form" onSubmit={submit} noValidate>
    <div className="mc-form-scroll"><FieldGroup>
      {hasPersonalSkills && <p className="text-xs leading-relaxed text-muted-foreground">{WORKFLOW_PERSONAL_SKILL_TEAM_VISIBILITY_HINT}</p>}
      <WorkflowStartVariableFields variables={variables} drafts={drafts} setDrafts={next=>{attempt.current=null;setDrafts(next)}} attemptedStart={attempted}/>
      <Field><FieldLabel htmlFor="mc-handle-prompt">需求</FieldLabel><Textarea id="mc-handle-prompt" rows={6} value={prompt} onChange={e=>{attempt.current=null;setPrompt(e.target.value)}}/></Field>
    </FieldGroup></div>
    {start.isError && <p role="alert" className="mc-form-error">{(() => {
      if (codeOf(start.error) === 'not_assignee') return '任务派给了别人，只有负责人本人或所属小队成员能用工作流处理。'
      const msg = taskRunMessageFor(start.error)
      return msg === '操作失败，请重试。' ? '工作流启动失败，请检查输入后重试。' : msg
    })()}</p>}
    <footer className="mc-form-actions"><Button type="button" variant="outline" onClick={onClose} disabled={start.isPending}>取消</Button>
      <Button type="submit" disabled={start.isPending}>{start.isPending?'启动中…':'启动工作流'}</Button></footer>
  </form>
}

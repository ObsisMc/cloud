import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import * as workflowsApi from '@/lib/workflows/api/generated'
import { isTerminalActionRejection, reasonText, scheduleError } from './runtime'
import { ScheduleForm } from './schedule-form'

vi.mock('@/lib/workflows/api/generated', () => ({ getWorkflow: vi.fn().mockResolvedValue({ published: null }) }))

it('validates weekly scheduling and accepts unscoped chat', async () => {
  const user = userEvent.setup()
  const onSave = vi.fn()
  render(<QueryClientProvider client={new QueryClient()}><ScheduleForm onSave={onSave} onCancel={() => {}} data={{ projects: [] }} /></QueryClientProvider>)
  fireEvent.change(screen.getByLabelText('任务名称'), { target: { value: '每日简报' } })
  fireEvent.change(screen.getByLabelText('任务指令'), { target: { value: '总结资讯' } })
  expect(screen.getByRole('combobox', { name: '关联项目' })).toHaveValue('')
  await user.selectOptions(screen.getByLabelText('重复频率'), 'weekly')
  await user.click(within(screen.getByRole('group', { name: '执行星期' })).getByRole('button', { name: '星期一' }))
  await user.click(screen.getByRole('button', { name: '创建并启用' }))
  expect(onSave).not.toHaveBeenCalled()
  expect(screen.getByText('请至少选择一天')).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '星期五' }))
  await user.click(screen.getByRole('button', { name: '创建并启用' }))
  expect(onSave).toHaveBeenCalledWith(expect.objectContaining({ kind: 'chat', projectId: '', rule: expect.objectContaining({ days: [5] }) }), expect.any(Object))
})

it('selects an Agent and one of its models independently', async () => {
  const user = userEvent.setup()
  const onSave = vi.fn()
  const data = {
    projects: [{ id: 'project', name: 'Demo', description: '' }],
    models: [{ id: 'direct', name: 'Direct model' }],
    agents: [
      { id: 'official/opencode', name: 'OpenCode', version: '1.0.0', models: [{ id: 'openai', name: 'OpenAI model' }] },
      { id: 'official/claude', name: 'Claude Code', version: '1.0.0', models: [{ id: 'messages', name: 'Anthropic model' }] },
    ],
    available: { projects: true, models: true, agents: true, workflows: false },
  }
  render(<QueryClientProvider client={new QueryClient()}><ScheduleForm onSave={onSave} onCancel={() => {}} data={data} /></QueryClientProvider>)
  fireEvent.change(screen.getByLabelText('任务名称'), { target: { value: '项目任务' } })
  fireEvent.change(screen.getByLabelText('任务指令'), { target: { value: '检查代码' } })
  expect(screen.queryByLabelText('执行器')).not.toBeInTheDocument()
  expect(screen.getByLabelText('Agent')).toBeDisabled()
  await user.selectOptions(screen.getByLabelText('关联项目'), 'project')
  await user.selectOptions(screen.getByLabelText('Agent'), 'official/claude')
  expect(screen.getByRole('option', { name: 'Anthropic model' })).toBeInTheDocument()
  expect(screen.queryByRole('option', { name: 'OpenAI model' })).not.toBeInTheDocument()
  await user.selectOptions(screen.getByLabelText('模型'), 'messages')
  await user.click(screen.getByRole('button', { name: '创建并启用' }))
  expect(onSave).toHaveBeenCalledWith(expect.objectContaining({ agentId: 'official/claude', agent: 'Claude Code', model: 'messages' }), data)
})

it('requires a project for a workflow while keeping projectless chat', async () => {
  const user = userEvent.setup()
  const onSave = vi.fn()
  const data = {
    projects: [{ id: 'project', name: 'Demo', description: '' }],
    workflows: [{ id: 'workflow', name: '周报流程', version: 'v1', snapshotId: 'snapshot' }],
    available: { projects: true, models: true, agents: true, workflows: true },
  }
  render(<QueryClientProvider client={new QueryClient()}><ScheduleForm onSave={onSave} onCancel={() => {}} data={data} /></QueryClientProvider>)
  fireEvent.change(screen.getByLabelText('任务名称'), { target: { value: '定时周报' } })
  await user.click(screen.getByRole('button', { name: /运行工作流/ }))
  await user.selectOptions(screen.getByLabelText('工作流'), 'workflow')
  const project = screen.getByLabelText('关联项目') as HTMLSelectElement
  expect(project).toHaveValue('')
  expect(project.selectedOptions[0]?.textContent).toBe('请选择项目')
  expect(within(project).getByText('请选择项目')).toHaveAttribute('hidden')
  expect(within(project).queryByRole('option', { name: '请选择项目' })).not.toBeInTheDocument()
  expect(within(project).queryByRole('option', { name: '不关联项目' })).not.toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: '创建并启用' }))
  expect(onSave).not.toHaveBeenCalled()
  expect(screen.getByText('工作流要在项目中运行，请选择项目')).toBeInTheDocument()

  await user.selectOptions(project, 'project')
  await user.click(screen.getByRole('button', { name: '创建并启用' }))
  expect(onSave).toHaveBeenCalledWith(expect.objectContaining({ kind: 'workflow', projectId: 'project' }), data)

  await user.click(screen.getByRole('button', { name: /直接对话/ }))
  expect(within(project).getByRole('option', { name: '不关联项目' })).toBeEnabled()
  expect(within(project).getByRole('option', { name: '不关联项目' })).not.toHaveAttribute('hidden')
})

it('lets a workflow explicitly follow the latest release while explaining frozen history', async () => {
 const user=userEvent.setup()
 const onSave=vi.fn()
 const data={projects:[{id:'p',name:'Demo',description:''}],workflows:[{id:'wf',name:'流程',version:'v1',snapshotId:'s1'}],available:{projects:true,models:true,agents:true,workflows:true}}
 render(<QueryClientProvider client={new QueryClient()}><ScheduleForm onSave={onSave} onCancel={()=>{}} data={data}/></QueryClientProvider>)
 await user.type(screen.getByLabelText('任务名称'),'跟随计划')
 await user.click(screen.getByRole('button',{name:/运行工作流/}))
 await user.selectOptions(screen.getByLabelText('工作流'),'wf')
 await user.selectOptions(screen.getByLabelText('关联项目'),'p')
 await user.click(screen.getByRole('button',{name:'跟随最新发布版本'}))
 expect(screen.getByText(/历史运行始终保留当次实际使用的快照/)).toBeInTheDocument()
 await user.click(screen.getByRole('button',{name:'创建并启用'}))
 expect(onSave).toHaveBeenCalledWith(expect.objectContaining({versionPolicy:'latest',workflowId:'wf'}),data)
})

it('shows the personal Skill team visibility hint when a scheduled workflow references personal Skills', async () => {
 const user=userEvent.setup()
 vi.mocked(workflowsApi.getWorkflow).mockResolvedValueOnce({
  published:{
   id:'s-skill',workflowId:'wf-skill',version:'v1',createdAt:0,updatedAt:0,
   graph:JSON.stringify({nodes:[{id:'a1',type:'workflow',position:{x:0,y:0},data:{kind:'agent',title:'审查',agentConfig:{skills:[{skillId:'my-personal-skill',enabled:true}]}}}],edges:[]}),
  },
 } as never)
 const data={tenantId:'tenant',projects:[{id:'p',name:'Demo',description:''}],workflows:[{id:'wf-skill',name:'带技能流程',version:'v1',snapshotId:'s-skill'}],available:{projects:true,models:true,agents:true,workflows:true}}
 render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><ScheduleForm onSave={()=>{}} onCancel={()=>{}} data={data}/></QueryClientProvider>)
 await user.click(screen.getByRole('button',{name:/运行工作流/}))
 await user.selectOptions(screen.getByLabelText('工作流'),'wf-skill')
 expect(await screen.findByText('运行时读取发起人同名的个人 Skill，其内容在节点会话中对团队可见。')).toBeInTheDocument()
 expect(reasonText('workflow_skill_not_found')).toBe('工作流需要的技能不可用，请确认发起人的个人技能已导入或市场技能已下发到项目')
 expect(reasonText('workflow_role_not_found')).toBe('工作流引用的角色在工作区角色目录里不存在，请先在设置的角色页新建同名角色')
 for (const [code, expected] of [
  ['workflow_skill_not_found', '工作流需要的技能不可用，请确认发起人的个人技能已导入或市场技能已下发到项目。'],
  ['workflow_role_not_found', '工作流引用的角色在工作区角色目录里不存在，请先在设置的角色页新建同名角色。'],
  ['workflow_run_not_startable', '工作流不能启动：图里有当前版本还不能执行的节点，或输入不满足开始节点。'],
 ] as const) {
  const err = { isAxiosError: true, response: { status: 400, data: { code, params: { occurrenceId: 'occ-1' } } } }
  expect(scheduleError(err, 'fallback')).toBe(expected)
  expect(isTerminalActionRejection(err)).toBe(true)
 }
})

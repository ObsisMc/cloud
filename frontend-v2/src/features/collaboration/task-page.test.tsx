import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { MemoryRouter, Route, Routes } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { UserContext } from '@/app/auth/user'
import { AxiosError, AxiosHeaders } from 'axios'
import type { CollaborationTarget, FormDescriptor, User } from '@/api/generated.schemas'
import { type Conversation, emptyConversation } from '@/components/agent-thread/model/types'
import * as workflowsApi from '@/lib/workflows/api/generated'
import * as decompositionApi from './decomposition-api/generated'
import * as workspaceApi from '@/lib/workspace-api/generated'
import * as api from './tasks-api/generated'
import * as taskWorkflows from './task-workflows-api/generated'
import type { TaskWorkflowRun } from './task-workflows-api/generated'
import type { Task, TaskDecomposition, TaskRun, TaskTimeline } from './tasks-api/generated'
import TaskPage from './task-page'

const targets = vi.hoisted(() => ({ items: [] as CollaborationTarget[] }))
const forms = vi.hoisted(() => ({
  byRef: {} as Record<string, FormDescriptor>,
  pending: false,
  error: false,
  refetch: vi.fn(),
}))
const session = vi.hoisted(() => ({ conversation: null as Conversation | null }))
const workspace = vi.hoisted(() => ({ role: 'owner' }))
vi.mock('@/api/me/me', () => ({ useGetApiV1MeTenants: () => ({ data: { items: [{ id: 'tenant', role: workspace.role }] }, isLoading: false, isError: false }) }))
vi.mock('@/api/members/members', () => ({ useGetApiV1TenantsTidMembers: () => ({ data: { items: [] }, isLoading: false, isError: false }) }))
vi.mock('@/api/spaces/spaces', () => ({
  useGetApiV1TenantsTidSpaces: () => ({ data: { items: [], nextCursor: '' }, isLoading: false, isError: false }),
  useGetApiV1TenantsTidSpacesSpaceIdPlugins: () => ({ data: { items: [] }, isLoading: false, isError: false }),
}))
vi.mock('@/api/tenants/tenants', () => ({
  useGetApiV1TenantsTidCollaborationTargets: () => ({ data: targets }),
  useGetApiV1TenantsTidCollaborationFormsFormRef: (_tid: string, formRef: string, _params?: unknown, options?: { query?: { enabled?: boolean } }) => {
    const enabled = options?.query?.enabled ?? true
    return {
      data: enabled && !forms.pending && !forms.error ? forms.byRef[formRef] : undefined,
      isPending: enabled && forms.pending,
      isError: enabled && forms.error,
      refetch: forms.refetch,
    }
  },
}))
vi.mock('@/lib/squads-api/generated', () => ({ listSquads: vi.fn().mockResolvedValue({ items: [], nextCursor: '' }) }))
vi.mock('@/lib/projects-api/generated', () => ({ getApiV2TenantsTidProjects: vi.fn().mockResolvedValue({ items: [], nextCursor: '' }) }))
// The live thread has its own tests; here a run shows the session its stream would fold.
vi.mock('./run-session', () => ({ useRunSession: () => ({ conversation: session.conversation, refused: false }) }))
vi.mock('./tasks-api/generated', () => ({
  getTask: vi.fn(), getTaskTimeline: vi.fn(), listTaskExecutors: vi.fn(), startTaskRun: vi.fn(), stopTaskRun: vi.fn(),
  createTaskComment: vi.fn(), setTaskStatus: vi.fn(), setTaskAssignee: vi.fn(), updateTask: vi.fn(),
  listTasks: vi.fn(), listTaskDecompositions: vi.fn(), startTaskDecomposition: vi.fn(),
}))
vi.mock('./decomposition-api/generated', () => ({ listDecompositionWorkflows: vi.fn() }))
vi.mock('@/lib/workflows/api/generated', () => ({
  cancelWorkflowRun: vi.fn(), getWorkflow: vi.fn(), listWorkflows: vi.fn(), listWorkflowRuns: vi.fn(), createWorkflowRun: vi.fn(), updateWorkflowRunInput: vi.fn(), startWorkflowRun: vi.fn(),
}))
vi.mock('./task-workflows-api/generated', () => ({ listTaskWorkflowRuns: vi.fn(), startTaskWorkflow: vi.fn() }))
vi.mock('@/lib/workspace-api/generated', () => ({ getTaskRunWorkspaceChanges: vi.fn(), getWorkflowRunWorkspaceChanges: vi.fn() }))
// The run view has its own tests; here it shows which run the page opened and how.
vi.mock('@/features/chat/workflow-run/run-pane', () => ({
  default: ({ runId, tenantId, readOnly, leading }: { runId: string; tenantId: string; readOnly: boolean; leading: ReactNode }) => <div>{leading}<p>运行视图 {runId} {tenantId} {readOnly ? '只读' : '可写'}</p></div>,
}))

const me: User = { id: 'user-me', displayName: '演示用户', status: 'active', version: 1, createdAt: '2026-10-01T00:00:00Z', deletedAt: null }
const agent = { type: 'agent' as const, id: 'agent-1', displayName: 'Claude Code', identifier: 'official/ora-space.claude' }
const task = (overrides: Partial<Task> = {}): Task => ({
  id: 'task-1', tenantId: 'tenant', title: '修复登录页', description: '按钮错位', projectId: 'project-1',
  assignee: agent, status: 'pending', createdBy: 'user-me', createdByName: '演示用户', version: 1,
  createdAt: '2026-10-30T00:00:00Z', updatedAt: '2026-10-30T00:00:00Z', activeRun: null, ...overrides,
})
const run = (overrides: Partial<TaskRun> = {}): TaskRun => ({
  demoSeed: false, id: 'run-1', taskId: 'task-1', startedBy: 'user-me', startedByName: '演示用户', executorId: 'claude:fixture', executorName: 'Claude Code / Fixture',
  state: 'running', lastResult: '', failureCode: '', endReason: '', createdAt: '2026-10-30T01:00:00Z', endedAt: null, ...overrides,
})
const empty: TaskTimeline = { runs: [], comments: [] }

function renderPage(query='', suppliedClient?: QueryClient) {
  const client = suppliedClient ?? new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(<QueryClientProvider client={client}><UserContext.Provider value={me}>
    <MemoryRouter initialEntries={['/collaboration/tasks/task-1'+query]}>
      <Routes><Route path="/collaboration/tasks/:taskId" element={<TaskPage />} /></Routes>
    </MemoryRouter>
  </UserContext.Provider></QueryClientProvider>)
}

beforeEach(() => {
  HTMLDialogElement.prototype.showModal = function () { this.setAttribute('open', '') }
  HTMLDialogElement.prototype.close = function () { this.removeAttribute('open') }
  vi.clearAllMocks()
  vi.mocked(workflowsApi.getWorkflow).mockResolvedValue({published:{id:'snapshot-1',workflowId:'wf-dev',version:'v1',graph:'{"nodes":[],"edges":[]}',createdAt:0,updatedAt:0}} as never)
  session.conversation = null
  workspace.role = 'owner'
  targets.items = [
    { type: 'user', id: 'user-2', displayName: '鲍勃', description: '工作区成员', interactionDescriptor: { mode: 'mention', requiresTask: false, formRef: null } },
    { type: 'agent', id: 'agent-1', displayName: 'Claude Code', description: 'official/ora-space.claude', interactionDescriptor: { mode: 'task', requiresTask: true, formRef: null } },
    { type: 'workflow', id: 'wf-1', displayName: '代码评审流程', description: '已发布工作流 · v1', interactionDescriptor: { mode: 'form', requiresTask: false, formRef: 'wf-1' } },
  ]
  forms.byRef = {}
  forms.pending = false
  forms.error = false
  forms.refetch = vi.fn()
  vi.mocked(api.getTask).mockResolvedValue(task())
  vi.mocked(api.getTaskTimeline).mockResolvedValue(empty)
  vi.mocked(api.listTasks).mockResolvedValue({ items: [], nextCursor: '' })
  vi.mocked(api.listTaskDecompositions).mockResolvedValue({ items: [] })
  vi.mocked(taskWorkflows.listTaskWorkflowRuns).mockImplementation(async () => ({ items: (await api.listTaskDecompositions('tenant', 'task-1')).items.map(d => ({ ...d, kind: 'decomposition' as const, version: 'v1' })) }))
  vi.mocked(api.listTaskExecutors).mockResolvedValue({ items: [
    { id: 'claude:fixture', name: 'Claude Code / Fixture', agentId: 'official/ora-space.claude', agentName: 'Claude Code', modelId: 'messages-fixture', modelName: 'Fixture' },
    { id: 'claude:configured', name: 'Claude Code / Sonnet', agentId: 'official/ora-space.claude', agentName: 'Claude Code', modelId: 'messages-configured', modelName: 'Sonnet' },
  ] })
})

it('starts the Agent with the configured model and shows the run being prepared', async () => {
  // Once started, the page reads the task and its timeline again and finds the run.
  let started = false
  vi.mocked(api.startTaskRun).mockImplementation(async () => { started = true; return run({ executorId: 'claude:configured' }) })
  vi.mocked(api.getTask).mockImplementation(async () => (started
    ? task({ status: 'running', activeRun: { id: 'run-1', state: 'running', startedBy: 'user-me', startedByName: '演示用户', executorName: 'Claude Code / Sonnet' } })
    : task()))
  vi.mocked(api.getTaskTimeline).mockImplementation(async () => (started ? { runs: [run()], comments: [] } : empty))
  renderPage()
  expect(await screen.findByText(/还没有运行/)).toBeInTheDocument()
  // An Agent task enters 进行中 by running, not by hand: its one 开始处理 starts the Agent.
  await waitFor(() => expect(screen.getByLabelText('执行模型')).toHaveValue('claude:configured'))
  expect(screen.getAllByRole('button', { name: '开始处理' })).toHaveLength(1)
  expect(within(screen.getByLabelText('执行')).getByRole('button', { name: '开始处理' })).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: '开始处理' }))
  await waitFor(() => expect(api.startTaskRun).toHaveBeenCalledWith('tenant', 'task-1', { executorId: 'claude:configured' }, expect.objectContaining({ headers: expect.anything() })))

  expect(await screen.findByText('正在准备…')).toBeInTheDocument()
  expect(await screen.findByText('运行正在执行，结束后再推进。')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '停止' })).toBeInTheDocument()
})

it('shows a running run\'s preparation stages before its first turn reaches the history', async () => {
  session.conversation = { ...emptyConversation, preparation: { stages: [
    { id: 'runtime', startedAt: '2026-11-05T00:00:00.000Z', endedAt: '2026-11-05T00:00:01.500Z' },
    { id: 'workspace', startedAt: '2026-11-05T00:00:01.500Z' },
  ] } }
  vi.mocked(api.getTask).mockResolvedValue(task({ status: 'running', activeRun: { id: 'run-1', state: 'running', startedBy: 'user-me', startedByName: '演示用户', executorName: 'Claude Code / Sonnet' } }))
  vi.mocked(api.getTaskTimeline).mockResolvedValue({ runs: [run()], comments: [] })
  renderPage()
  expect(await screen.findByText('正在准备工作目录…（第 2/5 步）')).toBeInTheDocument()
  expect(screen.getByText('1.5 秒')).toBeInTheDocument()
  expect(screen.queryByText('正在准备…')).not.toBeInTheDocument()
})

it('says an ended run left no record instead of showing it as still preparing', async () => {
  vi.mocked(api.getTaskTimeline).mockResolvedValue({ runs: [run({ state: 'ended', endReason: 'stopped' })], comments: [] })
  renderPage()
  expect(await screen.findByText('这次运行没有留下过程记录。')).toBeInTheDocument()
  expect(screen.queryByText('正在准备…')).not.toBeInTheDocument()
})

it('shows an empty executor state instead of a mock model', async () => {
  vi.mocked(api.listTaskExecutors).mockResolvedValue({ items: [] })
  renderPage()
  const select = await screen.findByLabelText('执行模型')
  await waitFor(() => expect(select).toBeDisabled())
  expect(within(select).getByRole('option', { name: '暂无可用的执行器' })).toBeInTheDocument()
})

it('lets the assignee of a person\'s task pick an Agent to handle it, and nobody else', async () => {
  const mine = task({ assignee: { type: 'human', id: 'user-me', displayName: '演示用户' } })
  vi.mocked(api.getTask).mockResolvedValue(mine)
  vi.mocked(api.listTaskExecutors).mockResolvedValue({ items: [
    { id: 'opencode:configured', name: 'OpenCode / DeepSeek', agentId: 'official/ora-space.opencode', agentName: 'OpenCode', modelId: 'chat-configured', modelName: 'DeepSeek' },
    { id: 'claude:configured', name: 'Claude Code / Sonnet', agentId: 'official/ora-space.claude', agentName: 'Claude Code', modelId: 'messages-configured', modelName: 'Sonnet' },
  ] })
  vi.mocked(api.startTaskRun).mockResolvedValue(run({ executorId: 'claude:configured' }))
  const { unmount } = renderPage()
  const section = await screen.findByLabelText('执行')
  expect(within(section).getByText('Agent 与模型')).toBeInTheDocument()
  await waitFor(() => expect(screen.getByLabelText('执行模型')).toHaveValue('opencode:configured'))
  await userEvent.selectOptions(screen.getByLabelText('执行模型'), 'claude:configured')
  await userEvent.click(within(section).getByRole('button', { name: '用 Agent 处理' }))
  await waitFor(() => expect(api.startTaskRun).toHaveBeenCalledWith('tenant', 'task-1', { executorId: 'claude:configured' }, expect.anything()))
  // The person still moves their own task by hand when they work without an Agent.
  expect(screen.getByRole('button', { name: '开始处理' })).toBeInTheDocument()
  unmount()

  // Someone else's task: no Agent controls, and the input only comments.
  vi.mocked(api.listTaskExecutors).mockClear()
  vi.mocked(api.getTask).mockResolvedValue(task({ assignee: { type: 'human', id: 'user-2', displayName: '鲍勃' } }))
  renderPage()
  expect(await screen.findByText('评论全团队可见。')).toBeInTheDocument()
  expect(screen.queryByLabelText('执行')).not.toBeInTheDocument()
  expect(api.listTaskExecutors).not.toHaveBeenCalled()
})

it('keeps the run for a follow-up when stopping its turn left the session live', async () => {
  let stopped = false
  const active = () => ({ id: 'run-1', state: stopped ? 'idle' as const : 'running' as const, startedBy: 'user-me', startedByName: '演示用户', executorName: 'Claude Code / Fixture' })
  vi.mocked(api.getTask).mockImplementation(async () => task({ status: stopped ? 'pending' : 'running', activeRun: active() }))
  vi.mocked(api.getTaskTimeline).mockImplementation(async () => ({ runs: [run({ state: stopped ? 'idle' : 'running', lastResult: stopped ? 'cancelled' : '' })], comments: [] }))
  vi.mocked(api.stopTaskRun).mockImplementation(async () => { stopped = true; return run({ state: 'idle', lastResult: 'cancelled' }) })
  renderPage()
  await userEvent.click(await screen.findByRole('button', { name: '停止' }))
  await waitFor(() => expect(api.stopTaskRun).toHaveBeenCalledWith('tenant', 'task-1', 'run-1'))
  // The session waits for the starter's follow-up; ending it is now a separate step.
  expect(await screen.findByRole('button', { name: '结束会话' })).toBeInTheDocument()
  expect(screen.getByText('演示用户 的会话在等追问；重新执行会结束它，用你的身份开一次新运行。')).toBeInTheDocument()
  expect(await screen.findByRole('switch', { name: '发给 Agent' })).toHaveAttribute('aria-checked', 'true')
})

it('sends to the Agent by default while it is reachable, and to the team once switched off', async () => {
  vi.mocked(api.getTask).mockResolvedValue(task({ status: 'review', activeRun: { id: 'run-1', state: 'idle', startedBy: 'user-me', startedByName: '演示用户', executorName: 'Claude Code / Fixture' } }))
  vi.mocked(api.getTaskTimeline).mockResolvedValue({ runs: [run({ state: 'idle', lastResult: 'completed' })], comments: [] })
  vi.mocked(api.createTaskComment).mockResolvedValue({ id: 'c1', taskId: 'task-1', authorId: 'user-me', authorName: '演示用户', body: '再改改', mentions: [], runId: 'run-1', createdAt: '2026-10-30T02:00:00Z' })
  renderPage()
  const toAgent = await screen.findByRole('switch', { name: '发给 Agent' })
  await waitFor(() => expect(toAgent).toHaveAttribute('aria-checked', 'true'))
  const input = screen.getByLabelText('评论或给 Agent 的消息')
  await userEvent.type(input, '再改改')
  await userEvent.click(screen.getByRole('button', { name: '发给 Agent' }))
  await waitFor(() => expect(api.createTaskComment).toHaveBeenCalledWith('tenant', 'task-1', {
    body: '再改改', mentions: [], toAgent: true, executorId: 'claude:configured',
  }, expect.anything()))

  await userEvent.click(toAgent)
  expect(toAgent).toHaveAttribute('aria-checked', 'false')
  await userEvent.type(input, '只给团队看')
  await userEvent.click(screen.getByRole('button', { name: '评论' }))
  await waitFor(() => expect(api.createTaskComment).toHaveBeenLastCalledWith('tenant', 'task-1', {
    body: '只给团队看', mentions: [], toAgent: false, executorId: undefined,
  }, expect.anything()))
})

it('sends a comment to the Agent when its own Agent is picked from the @ menu', async () => {
  vi.mocked(api.getTask).mockResolvedValue(task({ status: 'review', activeRun: { id: 'run-1', state: 'idle', startedBy: 'user-me', startedByName: '演示用户', executorName: 'Claude Code / Fixture' } }))
  vi.mocked(api.getTaskTimeline).mockResolvedValue({ runs: [run({ state: 'idle', lastResult: 'completed' })], comments: [] })
  vi.mocked(api.createTaskComment).mockResolvedValue({ id: 'c1', taskId: 'task-1', authorId: 'user-me', authorName: '演示用户', body: '@Claude Code 按钮再大一点', mentions: [], runId: 'run-1', createdAt: '2026-10-30T02:00:00Z' })
  renderPage()
  const input = await screen.findByLabelText('评论或给 Agent 的消息')
  await userEvent.type(input, '@Cla')
  const menu = await screen.findByRole('listbox', { name: '@ 对象' })
  await userEvent.click(within(menu).getByRole('option', { name: /Claude Code/ }))
  await userEvent.type(input, '按钮再大一点')
  await userEvent.click(screen.getByRole('button', { name: '发给 Agent' }))
  await waitFor(() => expect(api.createTaskComment).toHaveBeenCalledWith('tenant', 'task-1', {
    body: '@Claude Code 按钮再大一点', mentions: [{ type: 'agent', id: 'agent-1' }], toAgent: true, executorId: 'claude:configured',
  }, expect.anything()))
})

it('keeps another member to comments while someone else owns the run', async () => {
  vi.mocked(api.getTask).mockResolvedValue(task({ status: 'review', activeRun: { id: 'run-1', state: 'idle', startedBy: 'user-2', startedByName: '鲍勃', executorName: 'Claude Code / Fixture' } }))
  vi.mocked(api.getTaskTimeline).mockResolvedValue({ runs: [run({ state: 'idle', startedBy: 'user-2', startedByName: '鲍勃' })], comments: [] })
  vi.mocked(api.createTaskComment).mockResolvedValue({ id: 'c1', taskId: 'task-1', authorId: 'user-me', authorName: '演示用户', body: '@鲍勃 看起来不错', mentions: [{ type: 'user', id: 'user-2', displayName: '鲍勃' }], runId: '', createdAt: '2026-10-30T02:00:00Z' })
  renderPage()
  expect(await screen.findByRole('switch', { name: '发给 Agent' })).toHaveAttribute('aria-disabled', 'true')
  expect(screen.getByText(/当前运行由 鲍勃 发起，只有发起人能追问/)).toBeInTheDocument()
  const input = screen.getByLabelText('评论或给 Agent 的消息')
  await userEvent.type(input, '@')
  const menu = await screen.findByRole('listbox', { name: '@ 对象' })
  expect(within(menu).getByRole('option', { name: /Claude Code/ })).toBeDisabled()
  expect(within(menu).getByRole('option', { name: /代码评审流程/ })).toBeEnabled()
  await userEvent.type(input, '鲍')
  await userEvent.click(within(await screen.findByRole('listbox', { name: '@ 对象' })).getByRole('option', { name: /鲍勃/ }))
  await userEvent.type(input, '看起来不错')
  await userEvent.click(screen.getByRole('button', { name: '评论' }))
  await waitFor(() => expect(api.createTaskComment).toHaveBeenCalledWith('tenant', 'task-1', {
    body: '@鲍勃 看起来不错', mentions: [{ type: 'user', id: 'user-2' }], toAgent: false, executorId: undefined,
  }, expect.anything()))
})

it('says why a refused start failed instead of retrying', async () => {
  vi.mocked(api.startTaskRun).mockRejectedValue({ isAxiosError: true, response: { status: 409, data: { code: 'run_active' } } })
  renderPage()
  await userEvent.click(await screen.findByRole('button', { name: '开始处理' }))
  expect(await screen.findByText('Agent 正在执行，等这一轮结束或先停止。')).toBeInTheDocument()
})

const decomposition = (overrides: Partial<TaskDecomposition> = {}): TaskDecomposition => ({
  id: 'split-1', taskId: 'task-1', startedBy: 'user-me', startedByName: '演示用户', workflowId: 'wf-split', workflowName: '需求拆分',
  workflowRunId: 'wr-1', status: 'running', failure: '', createdCount: 0, createdAt: '2026-11-03T01:00:00Z', finishedAt: null, ...overrides,
})
const requirement = task({ title: '账号体系', description: '登录、注册和找回密码', assignee: null })

it('splits a requirement with the 需求拆分 workflow and lists the subtasks it wrote', async () => {
  vi.mocked(api.getTask).mockResolvedValue(requirement)
  vi.mocked(decompositionApi.listDecompositionWorkflows).mockResolvedValue({ items: [
    { id: 'wf-daily', name: '每日巡检', publishedVersion: 'v2', canDecompose: true, decompositionBlockedReason: '' },
    { id: 'wf-draft', name: '草稿', publishedVersion: null, canDecompose: false, decompositionBlockedReason: '工作流尚未发布。' },
    { id: 'wf-split', name: '需求拆分', publishedVersion: 'v1', canDecompose: true, decompositionBlockedReason: '' },
  ] })
  let started = false
  vi.mocked(api.startTaskDecomposition).mockImplementation(async () => { started = true; return decomposition() })
  vi.mocked(api.listTaskDecompositions).mockImplementation(async () => ({ items: started ? [decomposition({ status: 'succeeded', createdCount: 2 })] : [] }))
  vi.mocked(api.listTasks).mockImplementation(async () => ({ nextCursor: '', items: started ? [
    requirement,
    task({ id: 'sub-1', title: '登录页', parentId: 'task-1', assignee: null, createdAt: '2026-11-03T01:00:02Z' }),
    task({ id: 'sub-2', title: '注册页', parentId: 'task-1', assignee: null, createdAt: '2026-11-03T01:00:01Z' }),
  ] : [requirement] }))
  renderPage()
  await userEvent.click(await screen.findByRole('button', { name: '拆分' }))
  const dialog = await screen.findByRole('dialog')
  // Only published workflows run; the demo's 需求拆分 is picked for the Tech Lead.
  await waitFor(() => expect(within(dialog).getByLabelText('工作流')).toHaveValue('wf-split'))
  expect(within(dialog).queryByRole('option', { name: /草稿/ })).not.toBeInTheDocument()
  await userEvent.click(within(dialog).getByRole('button', { name: '开始拆分' }))
  await waitFor(() => expect(api.startTaskDecomposition).toHaveBeenCalledWith('tenant', 'task-1', { workflowId: 'wf-split' },
    expect.objectContaining({ headers: { 'Idempotency-Key': expect.any(String) } })))
  expect(await screen.findByText(/「需求拆分」拆出了 2 个子任务/)).toBeInTheDocument()
  // The run is open, so the subtasks stay folded until asked for.
  expect(await screen.findByText('运行视图 wr-1 tenant 只读')).toBeInTheDocument()
  expect(screen.queryByRole('list', { name: '子任务' })).not.toBeInTheDocument()
  await userEvent.click(await screen.findByRole('button', { name: '展开子任务' }))
  const subtasks = await screen.findByRole('list', { name: '子任务' })
  expect(within(subtasks).getAllByRole('link').map(a => a.textContent)).toEqual(['登录页', '注册页'])
  await userEvent.click(screen.getByRole('button', { name: '收起子任务' }))
  expect(screen.queryByRole('list', { name: '子任务' })).not.toBeInTheDocument()
})

const REQUIREMENT = '用于拆分的工作流，输出节点必须声明名为 tasks 的输出，内容是 {title, description} 的数组。'
const blockedWorkflow = (id: string, name: string) =>
  ({ id, name, publishedVersion: 'v1', canDecompose: false, decompositionBlockedReason: REQUIREMENT })

it('lists eligible workflows first, groups blocked ones, and states the requirement once', async () => {
  vi.mocked(api.getTask).mockResolvedValue(requirement)
  vi.mocked(decompositionApi.listDecompositionWorkflows).mockResolvedValue({ items: [
    blockedWorkflow('wf-blocked', '普通总结'),
    { id: 'wf-split', name: '需求拆分', publishedVersion: 'v1', canDecompose: true, decompositionBlockedReason: '' },
    blockedWorkflow('wf-blocked-2', '周报'),
  ] })
  vi.mocked(api.startTaskDecomposition).mockResolvedValue(decomposition())
  renderPage()
  await userEvent.click(await screen.findByRole('button', { name: '拆分' }))
  const dialog = await screen.findByRole('dialog')
  const select = within(dialog).getByLabelText('工作流')
  await waitFor(() => expect(select).toHaveValue('wf-split'))
  expect(within(select).getAllByRole('option').map(o => [o.textContent, (o as HTMLOptionElement).disabled]))
    .toEqual([['需求拆分（v1）', false], ['普通总结（v1）', true], ['周报（v1）', true]])
  const group = within(select).getByRole('group', { name: '不可拆分（缺少 tasks 输出）' })
  expect(within(group).getAllByRole('option').map(o => o.textContent)).toEqual(['普通总结（v1）', '周报（v1）'])
  expect(within(dialog).getByText('有 2 个已发布工作流不能用于拆分')).toBeInTheDocument()
  expect(within(dialog).getAllByText(REQUIREMENT)).toHaveLength(1)
  expect(within(dialog).getByText('普通总结、周报')).toBeInTheDocument()
  await userEvent.click(within(dialog).getByRole('button', { name: '开始拆分' }))
  await waitFor(() => expect(api.startTaskDecomposition).toHaveBeenCalledWith('tenant', 'task-1', { workflowId: 'wf-split' }, expect.anything()))
})

it('defaults to the first eligible workflow when none is called 需求拆分 and the newest is blocked', async () => {
  vi.mocked(api.getTask).mockResolvedValue(requirement)
  vi.mocked(decompositionApi.listDecompositionWorkflows).mockResolvedValue({ items: [
    blockedWorkflow('wf-newest', '最新总结'),
    { id: 'wf-ok', name: '按模块拆', publishedVersion: 'v3', canDecompose: true, decompositionBlockedReason: '' },
  ] })
  vi.mocked(api.startTaskDecomposition).mockResolvedValue(decomposition())
  renderPage()
  await userEvent.click(await screen.findByRole('button', { name: '拆分' }))
  const dialog = await screen.findByRole('dialog')
  await waitFor(() => expect(within(dialog).getByLabelText('工作流')).toHaveValue('wf-ok'))
  await userEvent.click(within(dialog).getByRole('button', { name: '开始拆分' }))
  await waitFor(() => expect(api.startTaskDecomposition).toHaveBeenCalledWith('tenant', 'task-1', { workflowId: 'wf-ok' }, expect.anything()))
})

it('disables the picker and explains why when every published workflow is blocked', async () => {
  vi.mocked(api.getTask).mockResolvedValue(requirement)
  vi.mocked(decompositionApi.listDecompositionWorkflows).mockResolvedValue({ items: [
    blockedWorkflow('wf-a', '普通总结'), blockedWorkflow('wf-b', '周报'),
  ] })
  renderPage()
  await userEvent.click(await screen.findByRole('button', { name: '拆分' }))
  const dialog = await screen.findByRole('dialog')
  expect(await within(dialog).findByText('已发布的工作流都不满足拆分输出要求，请先修改并重新发布。')).toBeInTheDocument()
  const select = within(dialog).getByLabelText('工作流')
  expect(select).toBeDisabled()
  expect(select).toHaveValue('')
  expect(within(dialog).getByRole('button', { name: '开始拆分' })).toBeDisabled()
  expect(api.startTaskDecomposition).not.toHaveBeenCalled()
})

it('shows the server\'s requirement when a start is refused for the tasks output', async () => {
  vi.mocked(api.getTask).mockResolvedValue(requirement)
  vi.mocked(decompositionApi.listDecompositionWorkflows).mockResolvedValue({ items: [
    { id: 'wf-split', name: '需求拆分', publishedVersion: 'v1', canDecompose: true, decompositionBlockedReason: '' },
  ] })
  vi.mocked(api.startTaskDecomposition).mockRejectedValue(Object.assign(new Error('refused'), {
    isAxiosError: true, response: { status: 400, data: { code: 'decomposition_output_required', params: { message: '服务端：刚发布的版本缺少 tasks。' }, requestId: 'r' } },
  }))
  renderPage()
  await userEvent.click(await screen.findByRole('button', { name: '拆分' }))
  const dialog = await screen.findByRole('dialog')
  await waitFor(() => expect(within(dialog).getByLabelText('工作流')).toHaveValue('wf-split'))
  await userEvent.click(within(dialog).getByRole('button', { name: '开始拆分' }))
  expect(await within(dialog).findByRole('alert')).toHaveTextContent('服务端：刚发布的版本缺少 tasks。')
})

it('keeps the subtasks open outside a run, and the workflow tab between the other two', async () => {
  vi.mocked(api.getTask).mockResolvedValue(requirement)
  vi.mocked(api.listTaskDecompositions).mockResolvedValue({ items: [decomposition({ status: 'succeeded', createdCount: 1 })] })
  vi.mocked(api.listTasks).mockResolvedValue({ nextCursor: '', items: [requirement, task({ id: 'sub-1', title: '登录页', parentId: 'task-1', assignee: null })] })
  renderPage()
  expect(await screen.findByRole('list', { name: '子任务' })).toBeInTheDocument()
  expect(screen.getAllByRole('tab', { name: /Agent 运行|工作流|讨论/ }).map(t => t.textContent?.replace(/\s*\d+$/, ''))).toEqual(['Agent 运行', '工作流', '讨论'])
})

it('opens the run as soon as a split starts, read-only, and goes back to the list', async () => {
  vi.mocked(api.getTask).mockResolvedValue(requirement)
  vi.mocked(decompositionApi.listDecompositionWorkflows).mockResolvedValue({ items: [{ id: 'wf-split', name: '需求拆分', publishedVersion: 'v1', canDecompose: true, decompositionBlockedReason: '' }] })
  let started = false
  vi.mocked(api.startTaskDecomposition).mockImplementation(async () => { started = true; return decomposition() })
  vi.mocked(api.listTaskDecompositions).mockImplementation(async () => ({ items: started ? [decomposition()] : [] }))
  renderPage()
  await userEvent.click(await screen.findByRole('button', { name: '拆分' }))
  expect(screen.getByRole('tab', { name: /工作流/ })).toBeInTheDocument()
  const dialog = await screen.findByRole('dialog')
  await waitFor(() => expect(within(dialog).getByLabelText('工作流')).toHaveValue('wf-split'))
  await userEvent.click(within(dialog).getByRole('button', { name: '开始拆分' }))
  expect(await screen.findByText('运行视图 wr-1 tenant 只读')).toBeInTheDocument()
  expect(screen.getByRole('tab', { name: '工作流 1' })).toHaveAttribute('aria-selected', 'true')
  // The run takes the room the composer would.
  expect(screen.queryByLabelText('评论或给 Agent 的消息')).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: '返回工作流列表' }))
  expect(await screen.findByRole('list', { name: '工作流运行记录' })).toHaveTextContent('运行中')
  expect(screen.getByLabelText('评论或给 Agent 的消息')).toBeInTheDocument()
})

it('lists every workflow the task ran and opens the one picked', async () => {
  vi.mocked(api.getTask).mockResolvedValue(requirement)
  vi.mocked(api.listTaskDecompositions).mockResolvedValue({ items: [
    decomposition({ id: 'split-3', workflowRunId: 'wr-3', status: 'succeeded', createdCount: 4 }),
    decomposition({ id: 'split-2', workflowRunId: 'wr-2', status: 'failed', failure: '工作流的输出里没有 tasks 数组。' }),
    decomposition({ id: 'split-1', workflowRunId: '', status: 'failed', failure: '工作流启动被拒绝。' }),
  ] })
  renderPage()
  await userEvent.click(await screen.findByRole('tab', { name: '工作流 3' }))
  const list = await screen.findByRole('list', { name: '工作流运行记录' })
  expect(within(list).getAllByRole('listitem').map(row => row.textContent)).toEqual([
    expect.stringContaining('拆出 4 个子任务'), expect.stringContaining('工作流的输出里没有 tasks 数组。'), expect.stringContaining('工作流启动被拒绝。'),
  ])
  // A run the host refused has nothing to open.
  expect(within(list).getAllByRole('button')).toHaveLength(2)
  await userEvent.click(within(list).getAllByRole('button')[1]!)
  expect(await screen.findByText('运行视图 wr-2 tenant 只读')).toBeInTheDocument()
  await userEvent.click(screen.getByRole('tab', { name: /讨论/ }))
  await userEvent.click(screen.getByRole('tab', { name: /工作流/ }))
  expect(await screen.findByRole('list', { name: '工作流运行记录' })).toBeInTheDocument()
})

it('opens a shared link on its run, and on the list when the run is gone', async () => {
  vi.mocked(api.getTask).mockResolvedValue(requirement)
  vi.mocked(api.listTaskDecompositions).mockResolvedValue({ items: [decomposition({ status: 'succeeded', createdCount: 2 })] })
  const { unmount } = renderPage('?tab=workflows&workflow=wr-1')
  expect(await screen.findByText('运行视图 wr-1 tenant 只读')).toBeInTheDocument()
  unmount()
  renderPage('?tab=workflows&workflow=gone')
  expect(await screen.findByRole('list', { name: '工作流运行记录' })).toBeInTheDocument()
})

it('shows a running split and holds the button', async () => {
  vi.mocked(api.getTask).mockResolvedValue(requirement)
  vi.mocked(api.listTaskDecompositions).mockResolvedValue({ items: [decomposition()] })
  renderPage()
  expect(await screen.findByText(/正在用「需求拆分」拆分/)).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '拆分中…' })).toBeDisabled()
})

it('explains a failed split and offers to split again', async () => {
  vi.mocked(api.getTask).mockResolvedValue(requirement)
  vi.mocked(api.listTaskDecompositions).mockResolvedValue({ items: [decomposition({ status: 'failed', failure: '工作流的输出里没有 tasks 数组。' })] })
  vi.mocked(decompositionApi.listDecompositionWorkflows).mockResolvedValue({ items: [] })
  renderPage()
  expect(await screen.findByRole('alert')).toHaveTextContent('拆分失败：工作流的输出里没有 tasks 数组。')
  await userEvent.click(screen.getByRole('button', { name: '重新拆分' }))
  const dialog = await screen.findByRole('dialog')
  expect(await within(dialog).findByText('工作区还没有已发布的工作流，先在工作流页发布一个。')).toBeInTheDocument()
  expect(within(dialog).getByRole('button', { name: '开始拆分' })).toBeDisabled()
})

it('offers to split again after a failure only while the caller may split the task', async () => {
  workspace.role = 'member'
  const failed = { items: [decomposition({ status: 'failed', failure: '工作流的输出里没有 tasks 数组。' })] }
  vi.mocked(api.listTaskDecompositions).mockResolvedValue(failed)
  // Someone else's requirement, assigned to someone else: the button stays, disabled, and says who can.
  vi.mocked(api.getTask).mockResolvedValue({ ...requirement, createdBy: 'user-lead', createdByName: '陈默', assignee: { type: 'human', id: 'user-doer', displayName: '鲍勃' } })
  const { unmount } = renderPage()
  const retry = await screen.findByRole('button', { name: '重新拆分' })
  expect(retry).toBeDisabled()
  expect(retry).toHaveAttribute('title', '只有创建人 陈默、负责人 鲍勃 或工作区管理员能拆分。')
  unmount()
  // A requirement in 待验收 is not split at all, so the failure offers no retry.
  vi.mocked(api.getTask).mockResolvedValue({ ...requirement, status: 'review' })
  renderPage()
  expect(await screen.findByRole('alert')).toHaveTextContent('拆分失败')
  expect(screen.queryByRole('button', { name: '重新拆分' })).not.toBeInTheDocument()
})

it('names the requirement a subtask came from and offers no split', async () => {
  vi.mocked(api.getTask).mockResolvedValue(task({ id: 'task-1', parentId: 'parent-1' }))
  vi.mocked(api.listTasks).mockResolvedValue({ nextCursor: '', items: [task({ id: 'parent-1', title: '账号体系' })] })
  renderPage()
  expect(await screen.findByRole('link', { name: '账号体系' })).toHaveAttribute('href', '/collaboration/tasks/parent-1')
  expect(screen.queryByRole('button', { name: '拆分' })).not.toBeInTheDocument()
})

it('opens the linked historical run instead of the newest run',async()=>{
  vi.mocked(api.getTaskTimeline).mockResolvedValue({runs:[run({id:'earlier',scheduled:true}),run({id:'later'})],comments:[]})
  const first=renderPage('?run=earlier')
  expect(await screen.findByRole('tab',{name:/运行 1/,selected:true})).toBeVisible()
  expect(screen.getByLabelText('运行概况')).toHaveTextContent('定时计划触发')
  first.unmount();renderPage('?run=missing')
  expect(await screen.findByText('这次运行已不存在或不可见。')).toBeVisible()
  expect(screen.queryByLabelText('运行概况')).not.toBeInTheDocument()
})

const handedToMe = (status: Task['status']) => task({ status, createdBy: 'user-lead', createdByName: '陈默', assignee: { type: 'human', id: 'user-me', displayName: '演示用户' } })

it('keeps 确认完成, 退回待处理 and 取消任务 from an assignee, and says who decides', async () => {
  workspace.role = 'member'
  vi.mocked(api.getTask).mockResolvedValue(handedToMe('review'))
  renderPage()
  expect(await screen.findByRole('button', { name: '确认完成' })).toBeDisabled()
  expect(screen.getByRole('button', { name: '退回待处理' })).toBeDisabled()
  expect(screen.getByRole('button', { name: '取消任务' })).toBeDisabled()
  expect(screen.getByText('等待 陈默 验收。只有创建人 陈默 或工作区管理员能确认完成、退回或取消任务。')).toBeInTheDocument()
  // Editing stays with whoever does the work.
  expect(screen.getByRole('button', { name: '编辑' })).toBeEnabled()
})

it('lets the creator send work in 待验收 back for rework', async () => {
  workspace.role = 'member'
  const askedByMe = (status: Task['status']) => task({ status, createdBy: 'user-me', createdByName: '演示用户', assignee: { type: 'human', id: 'user-doer', displayName: '鲍勃' } })
  vi.mocked(api.getTask).mockResolvedValue(askedByMe('review'))
  vi.mocked(api.setTaskStatus).mockResolvedValue(askedByMe('pending'))
  renderPage()
  await userEvent.click(await screen.findByRole('button', { name: '退回待处理' }))
  await waitFor(() => expect(api.setTaskStatus).toHaveBeenCalledWith('tenant', 'task-1', { status: 'pending', version: 1 }))
})

it('keeps editing, reassigning and stopping from a member with no part in the task, and says who can', async () => {
  workspace.role = 'member'
  vi.mocked(api.getTask).mockResolvedValue(task({ status: 'running', createdBy: 'user-lead', createdByName: '陈默',
    assignee: { type: 'human', id: 'user-doer', displayName: '鲍勃' },
    activeRun: { id: 'run-1', state: 'running', startedBy: 'user-doer', startedByName: '鲍勃', executorName: 'Claude Code / Sonnet' } }))
  renderPage()
  const edit = await screen.findByRole('button', { name: '编辑' })
  expect(edit).toBeDisabled()
  expect(edit).toHaveAttribute('title', '只有创建人 陈默、负责人 鲍勃 或工作区管理员能编辑。')
  expect(screen.getByText('只有创建人 陈默、负责人 鲍勃 或工作区管理员能改派。')).toBeInTheDocument()
  expect(screen.getByText('只有发起人 鲍勃、创建人 陈默 或工作区管理员能停止这次运行。')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '停止' })).not.toBeInTheDocument()
})

it('lets the creator stop a run on work assigned to someone else', async () => {
  workspace.role = 'member'
  vi.mocked(api.getTask).mockResolvedValue(task({ status: 'running', createdBy: 'user-me', createdByName: '演示用户',
    assignee: { type: 'human', id: 'user-doer', displayName: '鲍勃' },
    activeRun: { id: 'run-1', state: 'running', startedBy: 'user-doer', startedByName: '鲍勃', executorName: 'Claude Code / Sonnet' } }))
  renderPage()
  expect(await screen.findByRole('button', { name: '停止' })).toBeEnabled()
})

it('leaves an assignee the moves that are theirs', async () => {
  workspace.role = 'member'
  vi.mocked(api.getTask).mockResolvedValue(handedToMe('running'))
  renderPage()
  expect(await screen.findByRole('button', { name: '提交验收' })).toBeEnabled()
  expect(screen.getByRole('button', { name: '取消任务' })).toBeDisabled()
})

it('lets a workspace owner confirm work someone else asked for', async () => {
  vi.mocked(api.getTask).mockResolvedValue(handedToMe('review'))
  vi.mocked(api.setTaskStatus).mockResolvedValue(handedToMe('done'))
  renderPage()
  await userEvent.click(await screen.findByRole('button', { name: '确认完成' }))
  await waitFor(() => expect(api.setTaskStatus).toHaveBeenCalledWith('tenant', 'task-1', { status: 'done', version: 1 }))
})

it('opens the newest workflow run with its workspace from the page header', async () => {
  localStorage.clear()
  vi.mocked(api.getTask).mockResolvedValue(requirement)
  vi.mocked(api.listTaskDecompositions).mockResolvedValue({ items: [decomposition({ status: 'succeeded', createdCount: 2 })] })
  vi.mocked(workspaceApi.getWorkflowRunWorkspaceChanges).mockResolvedValue({ baseCommit: 'abc', files: [], truncated: false, deliveryState: { mode: 'delivery', inProgress: false } })
  renderPage()

  const button = await screen.findByRole('button', { name: '工作区' })
  expect(button).toHaveAttribute('aria-pressed', 'false')
  expect(screen.getByRole('complementary', { name: '任务属性' })).toBeInTheDocument()
  await userEvent.click(button)
  expect(await screen.findByText('运行视图 wr-1 tenant 只读')).toBeInTheDocument()
  expect(button).toHaveAttribute('aria-pressed', 'true')
  // The panel takes the side column's place, and closing it brings the column back.
  expect(await screen.findByRole('complementary', { name: '工作区' })).toBeInTheDocument()
  expect(screen.queryByRole('complementary', { name: '任务属性' })).not.toBeInTheDocument()
  await waitFor(() => expect(workspaceApi.getWorkflowRunWorkspaceChanges).toHaveBeenCalledWith('tenant', 'wr-1'))
  await userEvent.click(button)
  expect(button).toHaveAttribute('aria-pressed', 'false')
  expect(screen.getByRole('complementary', { name: '任务属性' })).toBeInTheDocument()
  expect(screen.getByText('运行视图 wr-1 tenant 只读')).toBeInTheDocument()
  localStorage.clear()
})

it('shows the workspace of the Agent run on screen', async () => {
  localStorage.clear()
  vi.mocked(api.getTaskTimeline).mockResolvedValue({ runs: [run({ id: 'run-1', state: 'ended' }), run({ id: 'run-2', state: 'idle' })], comments: [] })
  vi.mocked(workspaceApi.getTaskRunWorkspaceChanges).mockResolvedValue({ baseCommit: 'abc', files: [], truncated: false, deliveryState: { mode: 'delivery', inProgress: false } })
  renderPage('?run=run-1')

  await userEvent.click(await screen.findByRole('button', { name: '工作区' }))
  const panel = await screen.findByRole('complementary', { name: '工作区' })
  expect(within(panel).getByText('Agent 运行 1')).toBeInTheDocument()
  await waitFor(() => expect(workspaceApi.getTaskRunWorkspaceChanges).toHaveBeenCalledWith('tenant', 'task-1', 'run-1'))
  localStorage.clear()
})

it('offers no workspace before anything has run', async () => {
  renderPage()
  await screen.findByRole('button', { name: '编辑' })
  expect(screen.queryByRole('button', { name: '工作区' })).not.toBeInTheDocument()
})

const handlingRun = (overrides: Partial<TaskWorkflowRun> = {}): TaskWorkflowRun => ({
  id: 'handle:wf-run-1', taskId: 'task-1', workflowId: 'wf-dev', workflowRunId: 'wf-run-1', workflowName: '需求开发',
  version: 'v1', startedBy: 'user-me', startedByName: '演示用户', kind: 'handle', status: 'succeeded',
  failure: '', createdCount: 0, createdAt: '2026-11-14T00:00:00Z', ...overrides,
})

describe('handling a task with a workflow', () => {
  const human = { type: 'human' as const, id: 'user-me', displayName: '演示用户', identifier: '' }
  const workflow = (id: string, name: string) => ({ id, namespace: 'team', name, publishedVersion: 'v1', createdAt: 1, updatedAt: 1 })

  it('offers the entry without the demo flag, only to the assignee', async () => {
    vi.mocked(api.getTask).mockResolvedValue(task({ assignee: human }))
    const view = renderPage()
    expect(await screen.findByRole('button', { name: '用工作流处理' })).toBeEnabled()
    view.unmount()
    vi.mocked(api.getTask).mockResolvedValue(task({ assignee: { ...human, id: 'other' } }))
    renderPage()
    expect(await screen.findByText(/只有负责人或负责小队的成员可以处理/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '用工作流处理' })).not.toBeInTheDocument()
  })

  it('requires an explicit workflow pick and passes input and Start values in one idempotent request', async () => {
    const user = userEvent.setup()
    let started = false
    vi.mocked(api.getTask).mockImplementation(async()=>task({assignee:human,status:started?'running':'pending',version:started?2:1}))
    vi.mocked(workflowsApi.listWorkflows).mockResolvedValue({ workflows: [workflow('wf-split', '需求拆分'), workflow('wf-dev', '需求开发')] })
    vi.mocked(taskWorkflows.listTaskWorkflowRuns).mockImplementation(async () => ({ items: started ? [handlingRun({ status: 'running' })] : [] }))
    vi.mocked(taskWorkflows.startTaskWorkflow).mockImplementation(async () => { started = true; return { workflowRunId: 'wf-run-1' } })
    renderPage()
    await user.click(await screen.findByRole('button', { name: '用工作流处理' }))
    const dialog = screen.getByRole('dialog')
    await screen.findByRole('option', { name: '需求开发（v1）' })
    expect(within(dialog).getByLabelText('工作流')).toHaveValue('')
    expect(within(dialog).getByRole('button', { name: '启动工作流' })).toBeDisabled()
    await user.selectOptions(within(dialog).getByLabelText('工作流'), 'wf-dev')
    expect(await within(dialog).findByLabelText('需求')).toHaveValue('修复登录页\n\n按钮错位')
    await user.click(within(dialog).getByRole('button', { name: '启动工作流' }))
    await screen.findByText('运行视图 wf-run-1 tenant 只读')
    expect(taskWorkflows.startTaskWorkflow).toHaveBeenCalledWith('tenant', 'task-1', { workflowId: 'wf-dev', snapshotId: 'snapshot-1', input: '修复登录页\n\n按钮错位', values: {} }, expect.objectContaining({ headers: { 'Idempotency-Key': expect.any(String) } }))
    await waitFor(()=>expect(screen.getByText('进行中',{selector:'.mc-task-bar .mc-status'})).toBeInTheDocument())
    expect(api.setTaskStatus).not.toHaveBeenCalled()
  })

  it('lists all three kinds and opens inspection sources inside the tab', async () => {
    vi.mocked(taskWorkflows.listTaskWorkflowRuns).mockResolvedValue({ items: [
      handlingRun(), handlingRun({ id: 'decomposition:2', workflowRunId: 'split-2', kind: 'decomposition', createdCount: 2 }),
      handlingRun({ id: 'source:3', workflowRunId: 'source-3', kind: 'source', workflowName: '巡检' }),
    ] })
    renderPage('?tab=workflows')
    const list = await screen.findByRole('list', { name: '工作流运行记录' })
    expect(list).toHaveTextContent('处理 · v1 · 演示用户 发起')
    expect(list).toHaveTextContent('拆分 · v1 · 演示用户 发起')
    expect(list).toHaveTextContent('巡检来源 · v1 · 演示用户 发起')
    expect(within(list).getAllByRole('listitem')).toHaveLength(3)
    expect(document.querySelector('a[href^="/chat?run="]')).toBeNull()
    await userEvent.click(within(list).getByRole('button', { name: /查看工作流运行 巡检/ }))
    expect(await screen.findByText('运行视图 source-3 tenant 只读')).toBeInTheDocument()
  })
})


it.each([
  ['idle Agent',task({assignee:{type:'human',id:'user-me',displayName:'Me'},activeRun:{id:'idle',state:'idle',startedBy:'user-me',startedByName:'Me',executorName:'Test'}})],
  ['done',task({assignee:{type:'human',id:'user-me',displayName:'Me'},status:'done'})],
  ['cancelled',task({assignee:{type:'human',id:'user-me',displayName:'Me'},status:'cancelled'})],
] as const)('disables workflow handling for %s',async(_,target)=>{
 vi.mocked(api.getTask).mockResolvedValue(target)
 renderPage()
 expect(await screen.findByRole('button',{name:'用工作流处理'})).toBeDisabled()
})

it.each(['running','awaitingInput'] as const)('blocks Agent and workflow starts while handling is %s',async status=>{
 vi.mocked(api.getTask).mockResolvedValue(task({assignee:{type:'human',id:'user-me',displayName:'Me'}}))
 vi.mocked(taskWorkflows.listTaskWorkflowRuns).mockResolvedValue({items:[handlingRun({status})]})
 renderPage()
 await screen.findByText('工作流正在处理，结束后才能开始新的运行。')
 expect(screen.getByRole('button',{name:'用 Agent 处理'})).toBeDisabled()
 expect(screen.getByRole('button',{name:'用工作流处理'})).toBeDisabled()
})

it('keeps deleted task workflow records without opening their deleted runs',async()=>{
 vi.mocked(taskWorkflows.listTaskWorkflowRuns).mockResolvedValue({items:[handlingRun({deleted:true})]})
 renderPage('?tab=workflows&workflow=wf-run-1')
 expect(await screen.findByText('运行已删除')).toBeInTheDocument()
 expect(screen.queryByRole('button',{name:/查看工作流运行/})).not.toBeInTheDocument()
 expect(screen.queryByText(/运行视图/)).not.toBeInTheDocument()
})

it('reuses a key only for an identical network retry, rotates on body edits and error responses',async()=>{
 const user=userEvent.setup()
 vi.mocked(api.getTask).mockResolvedValue(task({assignee:{type:'human',id:'user-me',displayName:'Me'}}))
 vi.mocked(workflowsApi.listWorkflows).mockResolvedValue({workflows:[{id:'wf-dev',namespace:'team',name:'开发',publishedVersion:'v1',createdAt:0,updatedAt:0}]})
 vi.mocked(taskWorkflows.startTaskWorkflow).mockRejectedValue(new Error('network unavailable'))
 renderPage()
 await user.click(await screen.findByRole('button',{name:'用工作流处理'}))
 await user.selectOptions(screen.getByLabelText('工作流'),'wf-dev')
 const prompt=await screen.findByLabelText('需求')
 const submit=screen.getByRole('button',{name:'启动工作流'})
 await user.click(submit);await screen.findByRole('alert')
 const key=(n:number)=>vi.mocked(taskWorkflows.startTaskWorkflow).mock.calls[n]![3]!.headers!['Idempotency-Key']
 await user.click(submit);await waitFor(()=>expect(taskWorkflows.startTaskWorkflow).toHaveBeenCalledTimes(2))
 expect(key(1)).toBe(key(0))
 await user.type(prompt,' changed')
 await user.click(submit);await waitFor(()=>expect(taskWorkflows.startTaskWorkflow).toHaveBeenCalledTimes(3))
 expect(key(2)).not.toBe(key(1))
 vi.mocked(taskWorkflows.startTaskWorkflow).mockRejectedValue(Object.assign(new Error('invalid input'),{response:{status:400}}))
 await user.click(submit);await waitFor(()=>expect(taskWorkflows.startTaskWorkflow).toHaveBeenCalledTimes(4))
 await waitFor(()=>expect(submit).toBeEnabled())
 await user.click(submit);await waitFor(()=>expect(taskWorkflows.startTaskWorkflow).toHaveBeenCalledTimes(5))
 expect(key(4)).not.toBe(key(3))
})


it('keeps decomposition independent of handling admission and manual task controls',async()=>{
 vi.mocked(api.getTask).mockResolvedValue(handedToMe('running'))
 vi.mocked(taskWorkflows.listTaskWorkflowRuns).mockResolvedValue({items:[handlingRun({kind:'decomposition',status:'running'})]})
 renderPage()
 expect(await screen.findByRole('button',{name:'用 Agent 处理'})).toBeEnabled()
 expect(screen.getByRole('button',{name:'提交验收'})).toBeEnabled()
 expect(screen.getByRole('button',{name:'用工作流处理'})).toBeEnabled()
})

it('hides manual moves while a handling run owns the task status',async()=>{
 vi.mocked(api.getTask).mockResolvedValue(handedToMe('running'))
 vi.mocked(taskWorkflows.listTaskWorkflowRuns).mockResolvedValue({items:[handlingRun({status:'running'})]})
 renderPage()
 await screen.findByText('工作流正在处理，结束后才能开始新的运行。')
 expect(screen.queryByRole('button',{name:'提交验收'})).not.toBeInTheDocument()
 expect(screen.queryByRole('button',{name:'取消任务'})).not.toBeInTheDocument()
})

it('polls a live handling run, immediately refreshes settlement, and submits the fresh task version',async()=>{
 vi.useFakeTimers()
 const client=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 let settled=false
 vi.mocked(api.getTask).mockImplementation(async()=>task({status:settled?'review':'running',version:settled?2:1}))
 vi.mocked(taskWorkflows.listTaskWorkflowRuns).mockImplementation(async()=>({items:[handlingRun({status:settled?'succeeded':'running'})]}))
 vi.mocked(api.setTaskStatus).mockResolvedValue(task({status:'done',version:3}))
 const view=renderPage('?tab=workflows',client)
 try{
  await vi.waitFor(()=>expect(client.getQueryData(['task-workflow-runs','tenant','task-1'])).toMatchObject({items:[{status:'running'}]}))
  const before=vi.mocked(taskWorkflows.listTaskWorkflowRuns).mock.calls.length
  settled=true
  await act(async()=>{await vi.advanceTimersByTimeAsync(3100)})
  await vi.waitFor(()=>expect(client.getQueryData(['task','tenant','task-1'])).toMatchObject({status:'review',version:2}))
  expect(vi.mocked(taskWorkflows.listTaskWorkflowRuns).mock.calls.length).toBeGreaterThan(before)
  fireEvent.click(screen.getByRole('button',{name:'确认完成'}))
  await vi.waitFor(()=>expect(api.setTaskStatus).toHaveBeenCalledWith('tenant','task-1',{status:'done',version:2}))
  await act(async()=>{await vi.advanceTimersByTimeAsync(0)})
  const past=vi.mocked(taskWorkflows.listTaskWorkflowRuns).mock.calls.length
  await act(async()=>{await vi.advanceTimersByTimeAsync(3100)})
  expect(vi.mocked(taskWorkflows.listTaskWorkflowRuns).mock.calls.length).toBe(past)
 }finally{view.unmount();client.clear();vi.useRealTimers()}
})

it('lets any member stop handling inside the task tab and refreshes the returned pending task',async()=>{
 let cancelled=false
 vi.mocked(api.getTask).mockImplementation(async()=>task({status:cancelled?'pending':'running',version:cancelled?3:2}))
 vi.mocked(taskWorkflows.listTaskWorkflowRuns).mockImplementation(async()=>({items:[handlingRun({startedBy:'other',startedByName:'鲍勃',status:cancelled?'cancelled':'awaitingInput'})]}))
 vi.mocked(workflowsApi.cancelWorkflowRun).mockImplementation(async()=>{cancelled=true;return {} as never})
 renderPage('?tab=workflows&workflow=wf-run-1')
 await userEvent.click(await screen.findByRole('button',{name:'停止'}))
 await waitFor(()=>expect(screen.getByText('待处理',{selector:'.mc-task-bar .mc-status'})).toBeInTheDocument())
 expect(workflowsApi.cancelWorkflowRun).toHaveBeenCalledWith('tenant','wf-run-1')
 expect(screen.queryByRole('button',{name:'停止'})).not.toBeInTheDocument()
})

it('stops a handling admission that is still pending, settling the task back to its original status',async()=>{
 let stopped=false
 vi.mocked(api.getTask).mockImplementation(async()=>task({status:stopped?'pending':'running',version:stopped?3:2}))
 vi.mocked(taskWorkflows.listTaskWorkflowRuns).mockImplementation(async()=>({items:[handlingRun({startedBy:'other',startedByName:'鲍勃',status:stopped?'failed':'pending'})]}))
 vi.mocked(workflowsApi.cancelWorkflowRun).mockImplementation(async()=>{stopped=true;return {} as never})
 renderPage('?tab=workflows&workflow=wf-run-1')
 // A still-pending row (待启动) is stoppable: cancelling it is a start that did not happen.
 await userEvent.click(await screen.findByRole('button',{name:'停止'}))
 await waitFor(()=>expect(screen.getByText('待处理',{selector:'.mc-task-bar .mc-status'})).toBeInTheDocument())
 expect(workflowsApi.cancelWorkflowRun).toHaveBeenCalledWith('tenant','wf-run-1')
 expect(screen.queryByRole('button',{name:'停止'})).not.toBeInTheDocument()
})

it.each([
 {kind:'decomposition',startedBy:'user-me'},
 {kind:'source',startedBy:'user-me'},
 {kind:'mention',startedBy:'user-me'},
] as const)('does not offer task-tab cancellation for $kind',async record=>{
 vi.mocked(taskWorkflows.listTaskWorkflowRuns).mockResolvedValue({items:[handlingRun({...record,status:'running'})]})
 renderPage('?tab=workflows&workflow=wf-run-1')
 await screen.findByText(/运行视图/)
 expect(screen.queryByRole('button',{name:'停止'})).not.toBeInTheDocument()
})

function httpFault(status: number, code: string, params: Record<string, unknown> = {}) {
  return new AxiosError(code, 'ERR_BAD_REQUEST', undefined, undefined, {
    status,
    statusText: 'Bad Request',
    headers: new AxiosHeaders(),
    config: { headers: new AxiosHeaders() },
    data: { code, params, requestId: 'req-test' },
  })
}

const workflowTarget: CollaborationTarget = {
  type: 'workflow',
  id: 'wf-check',
  displayName: '发布检查',
  description: '已发布工作流 · v1',
  interactionDescriptor: { mode: 'form', requiresTask: false, formRef: 'wf-check' },
}

const workflowForm: FormDescriptor = {
  formRef: 'wf-check',
  title: '发布检查',
  description: '已发布版本 v1',
  fields: [
    { key: 'branch', label: '目标分支', type: 'text', required: true, placeholder: 'main' },
    { key: 'retries', label: '重试次数', type: 'number', required: false, defaultValue: 1 },
    { key: 'mode', label: '模式', type: 'select', required: false, defaultValue: 'fast', options: [{ label: 'fast', value: 'fast' }, { label: 'safe', value: 'safe' }] },
    { key: 'dryRun', label: '演练模式', type: 'boolean', required: false, defaultValue: false },
  ],
}

it('validates @ workflow inputs per field, sends typed values, reuses idempotency key on retry, and renders timeline status & output', async () => {
  const streams: { url: string; closed: boolean; listeners: Record<string, (() => void)[]> }[] = []
  class FakeEventSource {
    listeners: Record<string, (() => void)[]> = {}
    closed = false
    url: string
    constructor(url: string) {
      this.url = url
      streams.push(this)
    }
    addEventListener(event: string, fn: () => void) {
      ;(this.listeners[event] ??= []).push(fn)
    }
    close() {
      this.closed = true
    }
  }
  vi.stubGlobal('EventSource', FakeEventSource)
  try {
  targets.items.push(workflowTarget)
  forms.byRef['wf-check'] = workflowForm

  const timelineComments: TaskTimeline['comments'] = []
  vi.mocked(api.getTaskTimeline).mockImplementation(async () => ({ runs: [], comments: [...timelineComments] }))

  let attempt = 0
  vi.mocked(api.createTaskComment).mockImplementation(async (_tid, _taskId, body) => {
    attempt++
    if (attempt === 1) throw new AxiosError('Network Error')
    const created: TaskTimeline['comments'][number] = {
      id: 'c-wf-1',
      taskId: 'task-1',
      authorId: 'user-me',
      authorName: '演示用户',
      body: body.body,
      mentions: [{ type: 'workflow', id: 'wf-check', displayName: '发布检查' }],
      runId: '',
      workflowRun: {
        id: 'wf-run-99',
        workflowId: 'wf-check',
        workflowName: '发布检查',
        version: 'v1',
        status: 'running',
        output: null,
        error: null,
        deleted: false,
        createdAt: '2026-10-30T03:00:00Z',
      },
      createdAt: '2026-10-30T03:00:00Z',
    }
    timelineComments.splice(0, timelineComments.length, created)
    return created
  })

  renderPage('?tab=discussion')
  const box = await screen.findByLabelText('评论或给 Agent 的消息')
  await userEvent.type(box, '@')
  await userEvent.click(await screen.findByRole('option', { name: /发布检查/ }))

  expect(screen.getByRole('region', { name: '启动工作流：发布检查' })).toBeInTheDocument()
  expect(screen.queryByRole('switch', { name: '发给 Agent' })).not.toBeInTheDocument()

  // Invalid inputs (missing required branch + non-numeric retries) must report per field without calling API.
  const retriesInput = screen.getByLabelText(/重试次数/)
  await userEvent.clear(retriesInput)
  await userEvent.type(retriesInput, 'abc')
  await userEvent.click(screen.getByRole('button', { name: '发送并运行' }))

  expect(api.createTaskComment).not.toHaveBeenCalled()
  expect(screen.getByLabelText(/目标分支/)).toHaveAttribute('aria-invalid', 'true')
  expect(screen.getByLabelText(/重试次数/)).toHaveAttribute('aria-invalid', 'true')
  expect(screen.getAllByRole('alert').map(n => n.textContent)).toEqual(
    expect.arrayContaining(['请填写此必填字段', '请输入有效的数字']),
  )

  // Fix inputs -> first call fails transiently, second click retries with exact same Idempotency-Key.
  await userEvent.type(screen.getByLabelText(/目标分支/), 'release/v1')
  await userEvent.clear(retriesInput)
  await userEvent.type(retriesInput, '2')
  await userEvent.selectOptions(screen.getByLabelText(/^模式/), 'safe')
  await userEvent.click(screen.getByRole('checkbox', { name: /演练模式/ }))

  await userEvent.click(screen.getByRole('button', { name: '发送并运行' }))
  await waitFor(() => expect(api.createTaskComment).toHaveBeenCalledTimes(1))
  const firstCallOptions = vi.mocked(api.createTaskComment).mock.calls[0][3] as { headers: Record<string, string> }

  await userEvent.click(screen.getByRole('button', { name: '发送并运行' }))
  await waitFor(() => expect(api.createTaskComment).toHaveBeenCalledTimes(2))
  const secondCallOptions = vi.mocked(api.createTaskComment).mock.calls[1][3] as { headers: Record<string, string> }
  expect(secondCallOptions.headers['Idempotency-Key']).toBe(firstCallOptions.headers['Idempotency-Key'])

  expect(vi.mocked(api.createTaskComment).mock.calls[1][2]).toEqual({
    body: '@发布检查',
    mentions: [{ type: 'workflow', id: 'wf-check' }],
    toAgent: false,
    executorId: undefined,
    workflow: {
      workflowId: 'wf-check',
      values: { branch: 'release/v1', retries: 2, mode: 'safe', dryRun: true },
    },
  })

  // Timeline initially displays running workflow card, link, and subscribes to SSE invalidations.
  expect(await screen.findByText('运行中', { selector: '.mc-comment-workflow .mc-status' })).toBeInTheDocument()
  expect(screen.getByRole('link', { name: '查看运行' })).toHaveAttribute('href', '/collaboration/tasks/task-1?tab=workflows&workflow=wf-run-99')
  await waitFor(() => expect(streams.some(s => s.url === '/api/v2/tenants/tenant/workflow-runs/wf-run-99/events' && !s.closed)).toBe(true))

  // Simulate run completion -> workflow_run_invalidated refreshes card with final output and closes stream.
  timelineComments[0] = {
    ...timelineComments[0],
    workflowRun: {
      ...timelineComments[0].workflowRun!,
      status: 'succeeded',
      output: '{"ok":true,"branch":"release/v1"}',
    },
  }
  await act(async () => {
    for (const fn of streams[0]?.listeners.workflow_run_invalidated ?? []) fn()
  })
  await waitFor(() => expect(screen.getByText('已完成', { selector: '.mc-comment-workflow .mc-status' })).toBeInTheDocument())
  expect(screen.getByLabelText('工作流输出')).toHaveTextContent('"branch": "release/v1"')
  await waitFor(() => expect(streams[0]?.closed).toBe(true))
  } finally {
    vi.unstubAllGlobals()
  }
})

it('maps server workflow_run_input_invalid errors onto the matching field', async () => {
  targets.items.push(workflowTarget)
  forms.byRef['wf-check'] = workflowForm
  vi.mocked(api.createTaskComment).mockRejectedValueOnce(httpFault(400, 'workflow_run_input_invalid', { variable: 'branch', reason: 'length_exceeded' }))

  renderPage('?tab=discussion')
  const box = await screen.findByLabelText('评论或给 Agent 的消息')
  await userEvent.type(box, '@')
  await userEvent.click(await screen.findByRole('option', { name: /发布检查/ }))
  await userEvent.type(screen.getByLabelText(/目标分支/), 'too-long-branch')
  await userEvent.click(screen.getByRole('button', { name: '发送并运行' }))

  await waitFor(() => expect(screen.getByText('内容长度超出限制')).toBeInTheDocument())
  expect(screen.getByLabelText(/目标分支/)).toHaveAttribute('aria-invalid', 'true')
  // Composer retains input so user can fix without retyping.
  expect(screen.getByLabelText('评论或给 Agent 的消息')).toHaveValue('@发布检查 ')
})

it('disables @ workflow options when the task is closed', async () => {
  targets.items.push(workflowTarget)
  vi.mocked(api.getTask).mockResolvedValue(task({ status: 'done' }))

  renderPage('?tab=discussion')
  const box = await screen.findByLabelText('评论或给 Agent 的消息')
  await userEvent.type(box, '@')
  const opt = await screen.findByRole('option', { name: /发布检查/ })
  expect(opt).toBeDisabled()
  expect(opt).toHaveTextContent('任务已结束，不能启动工作流')
})

it('shows the missing role message when @ workflow comment returns workflow_role_not_found', async () => {
  targets.items.push(workflowTarget)
  forms.byRef['wf-check'] = workflowForm
  vi.mocked(api.createTaskComment).mockRejectedValueOnce(httpFault(400, 'workflow_role_not_found'))

  renderPage('?tab=discussion')
  const box = await screen.findByLabelText('评论或给 Agent 的消息')
  await userEvent.type(box, '@')
  await userEvent.click(await screen.findByRole('option', { name: /发布检查/ }))
  await userEvent.type(screen.getByLabelText(/目标分支/), 'main')
  await userEvent.click(screen.getByRole('button', { name: '发送并运行' }))

  expect(await screen.findByText('工作流引用的角色在工作区角色目录里不存在，请先在设置的角色页新建同名角色再启动。')).toBeInTheDocument()
  expect(screen.getByLabelText('评论或给 Agent 的消息')).toHaveValue('@发布检查 ')
})

it('shows the missing role message in TaskWorkflowDialog when workflow handling returns workflow_role_not_found', async () => {
  const user = userEvent.setup()
  const human = { type: 'human' as const, id: 'user-me', displayName: '演示用户', identifier: '' }
  vi.mocked(api.getTask).mockResolvedValue(task({ assignee: human }))
  vi.mocked(workflowsApi.listWorkflows).mockResolvedValue({
    workflows: [{ id: 'wf-dev', namespace: 'team', name: '需求开发', publishedVersion: 'v1', createdAt: 1, updatedAt: 1 }],
  })
  vi.mocked(taskWorkflows.startTaskWorkflow).mockRejectedValueOnce(httpFault(400, 'workflow_role_not_found'))

  renderPage()
  await user.click(await screen.findByRole('button', { name: '用工作流处理' }))
  const dialog = screen.getByRole('dialog')
  await screen.findByRole('option', { name: '需求开发（v1）' })
  await user.selectOptions(within(dialog).getByLabelText('工作流'), 'wf-dev')
  await user.click(await within(dialog).findByRole('button', { name: '启动工作流' }))

  expect(await within(dialog).findByText('工作流引用的角色在工作区角色目录里不存在，请先在设置的角色页新建同名角色再启动。')).toBeInTheDocument()
})

it('falls back to the workflow start failure message on unknown errors in TaskWorkflowDialog', async () => {
  const user = userEvent.setup()
  const human = { type: 'human' as const, id: 'user-me', displayName: '演示用户', identifier: '' }
  vi.mocked(api.getTask).mockResolvedValue(task({ assignee: human }))
  vi.mocked(workflowsApi.listWorkflows).mockResolvedValue({
    workflows: [{ id: 'wf-dev', namespace: 'team', name: '需求开发', publishedVersion: 'v1', createdAt: 1, updatedAt: 1 }],
  })
  vi.mocked(taskWorkflows.startTaskWorkflow).mockRejectedValueOnce(httpFault(500, 'unexpected_error'))

  renderPage()
  await user.click(await screen.findByRole('button', { name: '用工作流处理' }))
  const dialog = screen.getByRole('dialog')
  await screen.findByRole('option', { name: '需求开发（v1）' })
  await user.selectOptions(within(dialog).getByLabelText('工作流'), 'wf-dev')
  await user.click(await within(dialog).findByRole('button', { name: '启动工作流' }))

  expect(await within(dialog).findByText('工作流启动失败，请检查输入后重试。')).toBeInTheDocument()
})

it('shows workflow assignee guidance on not_assignee errors in TaskWorkflowDialog', async () => {
  const user = userEvent.setup()
  const human = { type: 'human' as const, id: 'user-me', displayName: '演示用户', identifier: '' }
  vi.mocked(api.getTask).mockResolvedValue(task({ assignee: human }))
  vi.mocked(workflowsApi.listWorkflows).mockResolvedValue({
    workflows: [{ id: 'wf-dev', namespace: 'team', name: '需求开发', publishedVersion: 'v1', createdAt: 1, updatedAt: 1 }],
  })
  vi.mocked(taskWorkflows.startTaskWorkflow).mockRejectedValueOnce(httpFault(409, 'not_assignee'))

  renderPage()
  await user.click(await screen.findByRole('button', { name: '用工作流处理' }))
  const dialog = screen.getByRole('dialog')
  await screen.findByRole('option', { name: '需求开发（v1）' })
  await user.selectOptions(within(dialog).getByLabelText('工作流'), 'wf-dev')
  await user.click(await within(dialog).findByRole('button', { name: '启动工作流' }))

  expect(await within(dialog).findByText('任务派给了别人，只有负责人本人或所属小队成员能用工作流处理。')).toBeInTheDocument()
})

const personalSkillGraph = JSON.stringify({
  nodes: [
    { id: 'start', type: 'workflow', position: { x: 0, y: 0 }, data: { kind: 'start', title: '开始' } },
    { id: 'agent-1', type: 'workflow', position: { x: 200, y: 0 }, data: { kind: 'agent', title: '代码审查', agentConfig: { skills: [{ skillId: 'my-skill', enabled: true }] } } },
  ],
  edges: [{ id: 'e1', source: 'start', target: 'agent-1' }],
})

it('shows the missing skill message and personal Skill hint when @ workflow comment uses a personal Skill', async () => {
  targets.items.push(workflowTarget)
  forms.byRef['wf-check'] = workflowForm
  vi.mocked(workflowsApi.getWorkflow).mockResolvedValue({
    published: { id: 'snapshot-skill', workflowId: 'wf-check', version: 'v1', graph: personalSkillGraph, createdAt: 0, updatedAt: 0 },
  } as never)
  vi.mocked(api.createTaskComment).mockRejectedValueOnce(
    httpFault(400, 'workflow_skill_not_found', { nodeId: 'agent-1', nodeTitle: '代码审查', skillId: 'my-skill' }),
  )

  renderPage('?tab=discussion')
  const box = await screen.findByLabelText('评论或给 Agent 的消息')
  await userEvent.type(box, '@')
  await userEvent.click(await screen.findByRole('option', { name: /发布检查/ }))

  expect(await screen.findByText('运行时读取发起人同名的个人 Skill，其内容在节点会话中对团队可见。')).toBeInTheDocument()

  await userEvent.type(screen.getByLabelText(/目标分支/), 'main')
  await userEvent.click(screen.getByRole('button', { name: '发送并运行' }))

  expect(await screen.findByText('节点「代码审查」需要的技能 my-skill 不可用，请先在个人技能页导入同名技能或确认市场技能已下发到项目后再启动。')).toBeInTheDocument()
  expect(screen.getByLabelText('评论或给 Agent 的消息')).toHaveValue('@发布检查 ')
})

it('shows the missing skill message and personal Skill hint in TaskWorkflowDialog and DecomposeDialog', async () => {
  const user = userEvent.setup()
  const human = { type: 'human' as const, id: 'user-me', displayName: '演示用户', identifier: '' }
  vi.mocked(api.getTask).mockResolvedValue(task({ assignee: human }))
  vi.mocked(workflowsApi.listWorkflows).mockResolvedValue({
    workflows: [{ id: 'wf-dev', namespace: 'team', name: '需求开发', publishedVersion: 'v1', createdAt: 1, updatedAt: 1 }],
  })
  vi.mocked(decompositionApi.listDecompositionWorkflows).mockResolvedValue({
    items: [{ id: 'wf-split', name: '需求拆分', publishedVersion: 'v1', canDecompose: true, decompositionBlockedReason: '' }],
  })
  vi.mocked(workflowsApi.getWorkflow).mockResolvedValue({
    published: { id: 'snapshot-skill', workflowId: 'wf-dev', version: 'v1', graph: personalSkillGraph, createdAt: 0, updatedAt: 0 },
  } as never)
  vi.mocked(taskWorkflows.startTaskWorkflow).mockRejectedValueOnce(
    httpFault(400, 'workflow_skill_not_found', { nodeId: 'agent-1', nodeTitle: '代码审查', skillId: 'my-skill' }),
  )

  renderPage()
  await user.click(await screen.findByRole('button', { name: '用工作流处理' }))
  const handleDialog = screen.getByRole('dialog')
  await screen.findByRole('option', { name: '需求开发（v1）' })
  await user.selectOptions(within(handleDialog).getByLabelText('工作流'), 'wf-dev')
  expect(await within(handleDialog).findByText('运行时读取发起人同名的个人 Skill，其内容在节点会话中对团队可见。')).toBeInTheDocument()
  await user.click(await within(handleDialog).findByRole('button', { name: '启动工作流' }))
  expect(await within(handleDialog).findByText('节点「代码审查」需要的技能 my-skill 不可用，请先在个人技能页导入同名技能或确认市场技能已下发到项目后再启动。')).toBeInTheDocument()
  await user.click(within(handleDialog).getByRole('button', { name: '取消' }))

  await user.click(await screen.findByRole('button', { name: '拆分' }))
  const splitDialog = screen.getByRole('dialog')
  await waitFor(() => expect(within(splitDialog).getByLabelText('工作流')).toHaveValue('wf-split'))
  expect(await within(splitDialog).findByText('运行时读取发起人同名的个人 Skill，其内容在节点会话中对团队可见。')).toBeInTheDocument()
})

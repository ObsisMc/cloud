import { expect, test, type Page } from '@playwright/test'
import { signIn } from './login.ts'

// An interactive Agent needs a real Agent session to talk to: with prepared Node assets
// (`task v2:node:prepare`, then V2_NODE_ASSETS_DIR) v2-server runs it on the fixture model. Without
// them, as in CI, the spec skips.
const realAgents = !!process.env.V2_NODE_ASSETS_DIR

/** Start → 起草 (interactive) → 审阅 → 输出: 审阅 reads what completing 起草 accepted. */
const chain = JSON.stringify({
  nodes: [
    { id: 'start', type: 'workflow', position: { x: 0, y: 0 }, data: { kind: 'start', title: '开始' } },
    { id: 'draft', type: 'workflow', position: { x: 320, y: 0 }, data: { kind: 'agent', title: '起草', agentConfig: { executor: { agentCli: 'official/ora-space.opencode', modelId: 'fixture' }, prompt: '根据需求起草方案：{{#start.input#}}', interactive: true } } },
    { id: 'review', type: 'workflow', position: { x: 640, y: 0 }, data: { kind: 'agent', title: '审阅', agentConfig: { executor: { agentCli: 'official/ora-space.opencode', modelId: 'fixture' }, prompt: '审阅上一步的方案：{{#draft.output#}}' } } },
    { id: 'out', type: 'workflow', position: { x: 960, y: 0 }, data: { kind: 'output', title: '输出', outputs: [{ name: 'review', variableSelector: ['review', 'output'] }] } },
  ],
  edges: [
    { id: 'e1', source: 'start', target: 'draft' },
    { id: 'e2', source: 'draft', target: 'review' },
    { id: 'e3', source: 'review', target: 'out' },
  ],
})

/** Start → 起草 (interactive) and Start → 旁支: a sibling runs its Agent while 起草 waits for input. */
const siblings = JSON.stringify({
  nodes: [
    { id: 'start', type: 'workflow', position: { x: 0, y: 0 }, data: { kind: 'start', title: '开始' } },
    { id: 'draft', type: 'workflow', position: { x: 320, y: 0 }, data: { kind: 'agent', title: '起草', agentConfig: { executor: { agentCli: 'official/ora-space.opencode', modelId: 'fixture' }, prompt: '根据需求起草方案：{{#start.input#}}', interactive: true } } },
    { id: 'side', type: 'workflow', position: { x: 320, y: 200 }, data: { kind: 'agent', title: '旁支', agentConfig: { executor: { agentCli: 'official/ora-space.opencode', modelId: 'fixture' }, prompt: '另写一份说明' } } },
  ],
  edges: [
    { id: 'e1', source: 'start', target: 'draft' },
    { id: 'e2', source: 'start', target: 'side' },
  ],
})

/** Start → 起草 and Start → 规划, both interactive: two parallel nodes wait for input at once. */
const parallelPair = JSON.stringify({
  nodes: [
    { id: 'start', type: 'workflow', position: { x: 0, y: 0 }, data: { kind: 'start', title: '开始' } },
    { id: 'draft', type: 'workflow', position: { x: 320, y: 0 }, data: { kind: 'agent', title: '起草', agentConfig: { executor: { agentCli: 'official/ora-space.opencode', modelId: 'fixture' }, prompt: '根据需求起草方案：{{#start.input#}}', interactive: true } } },
    { id: 'plan', type: 'workflow', position: { x: 320, y: 200 }, data: { kind: 'agent', title: '规划', agentConfig: { executor: { agentCli: 'official/ora-space.opencode', modelId: 'fixture' }, prompt: '根据需求排出开发计划：{{#start.input#}}', interactive: true } } },
  ],
  edges: [
    { id: 'e1', source: 'start', target: 'draft' },
    { id: 'e2', source: 'start', target: 'plan' },
  ],
})

/** Start → 起草 (interactive, must leave plan.md) → 审阅: completion checks the deliverable. */
const deliverable = JSON.stringify({
  nodes: [
    { id: 'start', type: 'workflow', position: { x: 0, y: 0 }, data: { kind: 'start', title: '开始' } },
    { id: 'draft', type: 'workflow', position: { x: 320, y: 0 }, data: { kind: 'agent', title: '起草', agentConfig: { schemaVersion: 4, executor: { agentCli: 'official/ora-space.opencode', modelId: 'fixture' }, prompt: '根据需求起草方案：{{#start.input#}}', interactive: true, completionSignal: 'end_turn', completion: { files: [{ path: 'plan.md' }, { path: 'docs/*.md', mustChange: true }] } } } },
    { id: 'review', type: 'workflow', position: { x: 640, y: 0 }, data: { kind: 'agent', title: '审阅', agentConfig: { executor: { agentCli: 'official/ora-space.opencode', modelId: 'fixture' }, prompt: '审阅上一步的方案：{{#draft.output#}}' } } },
  ],
  edges: [
    { id: 'e1', source: 'start', target: 'draft' },
    { id: 'e2', source: 'draft', target: 'review' },
  ],
})

/** Publishes graph, creates a run of it in Demo from the chat entry and starts it. */
async function startRun(page: Page, graph: string, label: string) {
  await page.goto('/chat')
  await signIn(page)
  const tenantId = ((await (await page.request.get('/api/v1/me/tenants')).json()) as { items: { id: string }[] }).items[0]!.id
  const suffix = Date.now().toString(36)
  const name = `${label} ${suffix}`
  const runName = `${label}运行 ${suffix}`
  const created = await page.request.post(`/api/v2/tenants/${tenantId}/workflows`, { data: { name, graph } })
  expect(created.status()).toBe(201)
  const workflowId = ((await created.json()) as { draft: { workflowId: string } }).draft.workflowId
  expect((await page.request.post(`/api/v2/tenants/${tenantId}/workflows/${workflowId}/publish`, { data: { version: 'v1' } })).ok()).toBe(true)

  await page.reload()
  await page.getByRole('button', { name: '选择项目' }).click()
  await page.getByRole('option', { name: 'Demo', exact: true }).click()
  const demo = page.getByRole('complementary', { name: '聊天记录' }).getByRole('region', { name: '项目 Demo' })
  await page.getByRole('button', { name: '运行工作流', exact: true }).click()
  await page.getByRole('menuitem', { name: new RegExp(name) }).click()
  const dialog = page.getByRole('dialog', { name: '运行工作流' })
  await dialog.getByLabel('运行名称').fill(runName)
  await dialog.getByRole('button', { name: '创建运行' }).click()
  const pane = page.getByRole('region', { name: '工作流运行' })
  await expect(pane.getByRole('heading', { name: runName })).toBeVisible()
  const row = (status: string) => demo.getByRole('button', { name: `工作流运行 ${runName}（${status}）` })
  await pane.getByRole('button', { name: '启动' }).click()
  const start = page.getByRole('dialog')
  await start.getByLabel('初始 Prompt').fill('给支付模块加一个对账接口')
  await start.getByRole('button', { name: '启动' }).click()
  const runId = async () => {
    const runs = (await (await page.request.get(`/api/v2/tenants/${tenantId}/workflows/${workflowId}/runs`)).json()) as { runs: { id: string; name: string }[] }
    return runs.runs.find((r) => r.name === runName)!.id
  }
  return { tenantId, pane, row, runId }
}

const partialNote = '由发起人确认完成。停止本轮后，会话还在就可以接着对话；没能保留会话时只能完成这一步。'

async function screenshot(page: Page, path: string) {
  await page.waitForFunction(() =>
    document
      .getAnimations()
      .filter((a) => a.timeline === document.timeline && a.effect?.getComputedTiming().endTime !== Infinity)
      .every((a) => a.playState !== 'running'),
  )
  await page.screenshot({ path })
}

test('interactive Agent: the starter talks to the parked node in its session, then completes it', async ({ page }, testInfo) => {
  test.skip(!realAgents, 'prepared Node assets are required for a real Agent session')
  test.setTimeout(240_000)
  const { tenantId, pane, row, runId: readRunId } = await startRun(page, chain, '交互起草')

  // After its first turn 起草 parks: the run waits for input and the rail says so.
  const draftChip = pane.locator('[data-path-node="draft"]')
  await expect(draftChip).toContainText('等待输入', { timeout: 150_000 })
  await expect(pane.getByText('等待输入：打开会话继续对话，或完成这一步')).toBeVisible()
  await screenshot(page, testInfo.outputPath('1-awaiting-input.png'))

  // The workflow's own first turn needs the starter's confirmation: no plain "完成" yet.
  await pane.getByRole('button', { name: '查看节点会话' }).click()
  await expect(pane.getByText(partialNote)).toBeVisible()
  await expect(pane.getByRole('button', { name: '确认并完成' })).toBeVisible()
  await expect(pane.getByText(/这是工作流发起的首轮/)).toBeVisible()
  await expect(pane.getByRole('button', { name: '仍然完成' })).toHaveCount(0)
  await screenshot(page, testInfo.outputPath('1b-confirm-first-turn.png'))

  // The starter replies in the node's own session; the answer arrives over its events stream.
  const composer = pane.getByRole('textbox', { name: '回复 Agent' })
  await composer.fill('请把接口命名改成 reconcile')
  await composer.press('Enter')
  await expect(pane.getByText('请把接口命名改成 reconcile').first()).toBeVisible({ timeout: 30_000 })
  await screenshot(page, testInfo.outputPath('2-follow-up.png'))
  await expect(draftChip).toContainText('等待输入', { timeout: 120_000 })
  await expect(pane.getByText(/V2 fixture complete\./).last()).toBeVisible()
  await screenshot(page, testInfo.outputPath('3-parked-again.png'))

  // The starter's own turn ended normally, so it completes without confirmation; 审阅 reads the
  // output and the run finishes.
  await pane.getByRole('button', { name: '完成', exact: true }).click()
  await expect(row('成功')).toBeVisible({ timeout: 150_000 })
  await screenshot(page, testInfo.outputPath('4-result.png'))
  const runId = await readRunId()
  const detail = (await (await page.request.get(`/api/v2/tenants/${tenantId}/workflow-runs/${runId}`)).json()) as { nodes: { nodeId: string; status: string; output: string | null; payload: string | null }[] }
  const draft = detail.nodes.find((n) => n.nodeId === 'draft')!
  expect(draft.status).toBe('succeeded')
  expect(JSON.parse(draft.payload!).interactive.turns).toBe(2)
  expect(detail.nodes.find((n) => n.nodeId === 'review')!.status).toBe('succeeded')
})

test('interactive Agent: 「确认并完成」 adopts the opening turn without forcing it', async ({ page }, testInfo) => {
  test.skip(!realAgents, 'prepared Node assets are required for a real Agent session')
  test.setTimeout(240_000)
  const { tenantId, pane, row, runId: readRunId } = await startRun(page, chain, '交互确认首轮')
  await expect(pane.locator('[data-path-node="draft"]')).toContainText('等待输入', { timeout: 150_000 })
  await pane.getByRole('button', { name: '查看节点会话' }).click()

  // The request must carry confirmInitialTurn: without it v2-server refuses with turn_unconfirmed.
  const completed = page.waitForResponse((r) => r.url().endsWith('/nodes/draft/complete') && r.request().method() === 'POST')
  await pane.getByRole('button', { name: '确认并完成' }).click()
  const response = await completed
  expect(response.status()).toBe(200)
  expect(response.request().postDataJSON()).toEqual({ force: false, requester: 'human', confirmInitialTurn: true })
  await expect(row('成功')).toBeVisible({ timeout: 150_000 })
  await screenshot(page, testInfo.outputPath('1c-confirmed-first-turn.png'))

  const runId = await readRunId()
  const detail = (await (await page.request.get(`/api/v2/tenants/${tenantId}/workflow-runs/${runId}`)).json()) as { nodes: { nodeId: string; status: string; payload: string | null }[] }
  const draft = detail.nodes.find((n) => n.nodeId === 'draft')!
  const payload = JSON.parse(draft.payload!)
  expect(draft.status).toBe('succeeded')
  expect(payload.interactive.turns).toBe(1)
  expect(payload.completion_forced).toBeUndefined()
  expect(payload.requester).toBe('human')
  expect(detail.nodes.find((n) => n.nodeId === 'review')!.status).toBe('succeeded')
})

test('interactive Agent: a sibling running its Agent leaves the parked session open for the next message', async ({ page }, testInfo) => {
  test.skip(!realAgents, 'prepared Node assets are required for a real Agent session')
  test.setTimeout(300_000)
  const { tenantId, pane, row, runId: readRunId } = await startRun(page, siblings, '交互旁支')

  // 起草 parks after its first turn; 旁支 then starts, runs and ends its own session on the run's
  // directory. Node keeps both sessions live on the one checkout, so 起草 still waits for input.
  const draftChip = pane.locator('[data-path-node="draft"]')
  await expect(draftChip).toContainText('等待输入', { timeout: 150_000 })
  const runId = await readRunId()
  const draftRow = async () => {
    const detail = (await (await page.request.get(`/api/v2/tenants/${tenantId}/workflow-runs/${runId}`)).json()) as { nodes: { nodeId: string; status: string; sessionId: string | null; payload: string | null }[] }
    return { draft: detail.nodes.find((n) => n.nodeId === 'draft'), side: detail.nodes.find((n) => n.nodeId === 'side') }
  }
  await expect.poll(async () => (await draftRow()).side?.status, { timeout: 150_000 }).toBe('succeeded')
  const { draft, side } = await draftRow()
  expect(draft!.status).toBe('pending')
  // The first turn awaits confirmation, so it is not completable yet.
  expect(JSON.parse(draft!.payload!).interactive).toMatchObject({ turns: 1, session_ended: false, completable: false })
  expect(side!.sessionId).not.toBe(draft!.sessionId)

  // The starter's message, sent after 旁支 ran beside it, reaches 起草's own session.
  await draftChip.click()
  await pane.getByRole('button', { name: '查看节点会话' }).click()
  const composer = pane.getByRole('textbox', { name: '回复 Agent' })
  await composer.fill('旁支跑完了，请再确认一遍方案')
  await composer.press('Enter')
  await expect(pane.getByText('旁支跑完了，请再确认一遍方案').first()).toBeVisible({ timeout: 30_000 })
  await expect.poll(async () => JSON.parse((await draftRow()).draft!.payload!).interactive.turns, { timeout: 150_000 }).toBe(2)
  await expect(draftChip).toContainText('等待输入', { timeout: 30_000 })
  const after = (await draftRow()).draft!
  expect(after.sessionId).toBe(draft!.sessionId)
  expect(JSON.parse(after.payload!).interactive).toMatchObject({ turns: 2, session_ended: false, completable: true })
  await screenshot(page, testInfo.outputPath('5-follow-up-after-sibling.png'))

  await pane.getByRole('button', { name: '完成', exact: true }).click()
  await expect(row('成功')).toBeVisible({ timeout: 60_000 })
  expect((await draftRow()).draft!.status).toBe('succeeded')
})

test('parallel nodes: the run view switches between their sessions, and each keeps its own conversation', async ({ page }, testInfo) => {
  test.skip(!realAgents, 'prepared Node assets are required for a real Agent session')
  test.setTimeout(300_000)
  const { tenantId, pane, runId: readRunId } = await startRun(page, parallelPair, '并行会话')

  // Both nodes run their first turn concurrently and park; the theater shows them side by side.
  await expect(pane.locator('[data-path-node="draft"]')).toContainText('等待输入', { timeout: 150_000 })
  await expect(pane.locator('[data-path-node="plan"]')).toContainText('等待输入', { timeout: 150_000 })
  const carousel = pane.getByRole('group', { name: '切换并行节点' })
  await expect(carousel).toBeVisible()
  await expect(pane.getByText(/2 个节点并行/)).toBeVisible()
  await screenshot(page, testInfo.outputPath('6-parallel-acts.png'))

  const openSession = async (title: string) => {
    await pane.getByRole('button', { name: `聚焦到 ${title}` }).click()
    await expect(pane.getByRole('button', { name: `聚焦到 ${title}` })).toHaveAttribute('aria-pressed', 'true')
    await pane.getByRole('button', { name: '查看节点会话' }).click()
  }
  const back = async () => {
    await pane.getByRole('button', { name: '返回阶段摘要' }).click()
    await expect(carousel).toBeVisible()
  }

  // 起草's session shows its own task, and a reply goes to that session only.
  await openSession('起草')
  await expect(pane.getByText(/根据需求起草方案/).first()).toBeVisible()
  await expect(pane.getByText(/根据需求排出开发计划/)).toHaveCount(0)
  const composer = pane.getByRole('textbox', { name: '回复 Agent' })
  await composer.fill('只回复起草这一步')
  await composer.press('Enter')
  await expect(pane.getByText('只回复起草这一步').first()).toBeVisible({ timeout: 30_000 })
  await back()

  // Switching to 规划 shows its session, without 起草's task or reply.
  await openSession('规划')
  await expect(pane.getByText(/根据需求排出开发计划/).first()).toBeVisible()
  await expect(pane.getByText(/根据需求起草方案/)).toHaveCount(0)
  await expect(pane.getByText('只回复起草这一步')).toHaveCount(0)
  await screenshot(page, testInfo.outputPath('7-switched-session.png'))
  await back()

  const runId = await readRunId()
  const turns = async () => {
    const detail = (await (await page.request.get(`/api/v2/tenants/${tenantId}/workflow-runs/${runId}`)).json()) as { nodes: { nodeId: string; sessionId: string | null; payload: string | null }[] }
    const of = (id: string) => detail.nodes.find((n) => n.nodeId === id)!
    return { draft: of('draft'), plan: of('plan') }
  }
  await expect.poll(async () => JSON.parse((await turns()).draft.payload!).interactive.turns, { timeout: 150_000 }).toBe(2)
  const { draft, plan } = await turns()
  expect(draft.sessionId).not.toBe(plan.sessionId)
  expect(JSON.parse(plan.payload!).interactive.turns).toBe(1)
})

test('interactive Agent: a missing deliverable blocks completion until the starter completes anyway', async ({ page }, testInfo) => {
  test.skip(!realAgents, 'prepared Node assets are required for a real Agent session')
  test.setTimeout(300_000)
  const { tenantId, pane, row, runId: readRunId } = await startRun(page, deliverable, '交付文件')
  const draftChip = pane.locator('[data-path-node="draft"]')
  await expect(draftChip).toContainText('等待输入', { timeout: 150_000 })
  await pane.getByRole('button', { name: '查看节点会话' }).click()
  const composer = pane.getByRole('textbox', { name: '回复 Agent' })
  await composer.fill('方案可以，写进 plan.md')
  await composer.press('Enter')
  await expect(pane.getByText('方案可以，写进 plan.md').first()).toBeVisible({ timeout: 30_000 })
  await expect(draftChip).toContainText('等待输入', { timeout: 150_000 })
  await expect(pane.getByRole('button', { name: '完成', exact: true })).toBeEnabled({ timeout: 30_000 })

  // The fixture Agent writes no file: completing names both deliverables and offers force.
  await pane.getByRole('button', { name: '完成', exact: true }).click()
  await expect(pane.getByText('缺少交付文件：plan.md', { exact: true })).toBeVisible({ timeout: 30_000 })
  await expect(pane.getByText('缺少交付文件：docs/*.md', { exact: true })).toBeVisible()
  await expect(pane.getByText('补齐或修改文件后可以再次完成。')).toBeVisible()
  await screenshot(page, testInfo.outputPath('8-deliverables-blocked.png'))

  await pane.getByRole('button', { name: '仍然完成' }).click()
  const dialog = page.getByRole('alertdialog')
  await expect(dialog.getByText('仍然完成这一步？')).toBeVisible()
  await expect(dialog).toContainText('这一轮的回答')
  await screenshot(page, testInfo.outputPath('9-complete-anyway.png'))
  await dialog.getByRole('button', { name: '仍然完成' }).click()
  await expect(row('成功')).toBeVisible({ timeout: 150_000 })

  const runId = await readRunId()
  const detail = (await (await page.request.get(`/api/v2/tenants/${tenantId}/workflow-runs/${runId}`)).json()) as { nodes: { nodeId: string; status: string; payload: string | null }[] }
  const forced = JSON.parse(detail.nodes.find((n) => n.nodeId === 'draft')!.payload!).completion_forced
  expect(forced).toMatchObject({ by: 'human', output_source: 'turn_window', without_turn: false })
  expect(forced.blockers.map((b: { kind: string }) => b.kind)).toEqual(['file_missing', 'file_missing'])
})

test('the Agent inspector says how a run applies interactive mode', async ({ page }, testInfo) => {
  test.setTimeout(120_000)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/workflows')
  await signIn(page)
  const tenantId = ((await (await page.request.get('/api/v1/me/tenants')).json()) as { items: { id: string }[] }).items[0]!.id
  const name = `交互节点说明 ${Date.now().toString(36)}`
  const created = await page.request.post(`/api/v2/tenants/${tenantId}/workflows`, { data: { name, graph: chain } })
  expect(created.status()).toBe(201)
  // v2-server parks a root-graph interactive Agent, so its analysis no longer lists the mode.
  const analysis = (await (await page.request.post(`/api/v2/tenants/${tenantId}/workflows/analyze`, { data: { graph: chain } })).json()) as {
    unappliedSettings: { nodeId: string; settings: string[] }[]
  }
  expect(analysis.unappliedSettings.find((entry) => entry.nodeId === 'draft')?.settings ?? []).not.toContain('interactive')

  await page.reload()
  await page.getByRole('complementary', { name: '工作流列表' }).getByRole('button', { name, exact: true }).click()
  await page.locator('.react-flow__node[data-id="draft"]').click()
  const inspector = page.locator('[data-workflow-inspector]')
  const note = inspector.getByTestId('workflow-interactive-partial')
  await expect(note).toContainText('交互模式')
  await expect(note).toContainText('首轮要点「确认并完成」')
  await expect(inspector.getByTestId('workflow-unapplied-interactive')).toHaveCount(0)
  await note.scrollIntoViewIfNeeded()
  await page.screenshot({ path: testInfo.outputPath('6-editor-end-turn-note.png'), animations: 'disabled' })

  // The note belongs to the mode: switching it off removes it.
  await inspector.getByRole('switch', { name: '交互模式' }).click()
  await expect(note).toHaveCount(0)
})

test('the Agent inspector edits the completion rule and deliverables, and marks neither as not applied', async ({ page }, testInfo) => {
  test.setTimeout(120_000)
  await page.setViewportSize({ width: 1440, height: 1200 })
  await page.goto('/workflows')
  await signIn(page)
  const tenantId = ((await (await page.request.get('/api/v1/me/tenants')).json()) as { items: { id: string }[] }).items[0]!.id
  const name = `完成判定 ${Date.now().toString(36)}`
  const graph = JSON.parse(deliverable)
  graph.nodes[1].data.agentConfig.completionSignal = 'agent'
  const created = await page.request.post(`/api/v2/tenants/${tenantId}/workflows`, { data: { name, graph: JSON.stringify(graph) } })
  expect(created.status()).toBe(201)

  await page.reload()
  await page.getByRole('complementary', { name: '工作流列表' }).getByRole('button', { name, exact: true }).click()
  await page.locator('.react-flow__node[data-id="draft"]').click()
  const inspector = page.locator('[data-workflow-inspector]')
  await expect(inspector.getByRole('combobox', { name: '完成判定' })).toContainText('智能体显式上报完成')
  await expect(inspector.getByRole('switch', { name: '自动接受智能体完成' })).toBeVisible()
  await expect(inspector.getByTestId('workflow-unapplied-completionSignal')).toHaveCount(0)
  await expect(inspector.getByLabel('文件 1 路径')).toHaveValue('plan.md')
  await expect(inspector.getByLabel('文件 2 路径')).toHaveValue('docs/*.md')
  await inspector.getByRole('combobox', { name: '完成判定' }).scrollIntoViewIfNeeded()
  await page.screenshot({ path: testInfo.outputPath('10-editor-completion.png'), animations: 'disabled' })
  await inspector.getByLabel('文件 2 路径').scrollIntoViewIfNeeded()
  await page.screenshot({ path: testInfo.outputPath('11-editor-deliverables.png'), animations: 'disabled' })

  // Back to end_turn: auto-accept only exists for the agent rule.
  await inspector.getByRole('combobox', { name: '完成判定' }).click()
  await page.getByRole('option', { name: '回合正常结束即可' }).click()
  await expect(inspector.getByRole('switch', { name: '自动接受智能体完成' })).toHaveCount(0)
})

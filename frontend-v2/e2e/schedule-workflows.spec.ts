/// <reference lib="dom" />
import { expect, test } from '@playwright/test'
import { signIn } from './login.ts'
import { containerWorkflowGraph, type ContainerDetail } from './workflow-container-graph.ts'

// With prepared Node assets (`task v2:node:prepare`, then V2_NODE_ASSETS_DIR) the workflow's Agent
// runs on the fixture model; without them, as in CI, there is no Agent and the run fails.
const realAgents = !!process.env.V2_NODE_ASSETS_DIR

/** Start → 起草 → 输出: one Agent that drafts from the kickoff input. */
const graph = JSON.stringify({
  nodes: [
    { id: 'start', type: 'workflow', position: { x: 0, y: 0 }, data: { kind: 'start', title: '开始' } },
    { id: 'draft', type: 'workflow', position: { x: 320, y: 0 }, data: { kind: 'agent', title: '起草', agentConfig: { executor: { agentCli: 'official/ora-space.opencode', modelId: 'fixture' }, prompt: '根据需求起草方案：{{#start.input#}}' } } },
    { id: 'out', type: 'workflow', position: { x: 640, y: 0 }, data: { kind: 'output', title: '输出', outputs: [{ name: 'draft', variableSelector: ['draft', 'output'] }] } },
  ],
  edges: [
    { id: 'e1', source: 'start', target: 'draft' },
    { id: 'e2', source: 'draft', target: 'out' },
  ],
})

for (const containers of [false, true]) {
  test(containers ? 'a scheduled container workflow preserves variables and per-round session history' : 'a scheduled workflow starts its own run of the published version and reads it back in history', async ({ page }, testInfo) => {
    test.skip(containers && !realAgents, 'needs the prepared Node fixture executor for container history')
    test.setTimeout(realAgents ? 240_000 : 90_000)
    const subscribed: string[] = []
    page.on('request', (request) => {
      const match = /\/workflow-runs\/[^/]+\/nodes\/([^/]+)\/events/.exec(request.url())
      if (match) subscribed.push(match[1]!)
    })
    await page.goto('/schedules')
    await signIn(page)

    // Publish a workflow on v2-server; names are fresh so earlier runs of this spec do not interfere.
    const tenantId = ((await (await page.request.get('/api/v1/me/tenants')).json()) as { items: { id: string }[] }).items[0]!.id
    const suffix = Date.now().toString(36)
    const workflowName = `${containers ? '定时容器' : '定时起草'} ${suffix}`
    const taskName = `${containers ? '定时容器验收' : '定时工作流'}-${suffix}`
    const created = await page.request.post(`/api/v2/tenants/${tenantId}/workflows`, { data: { name: workflowName, graph: containers ? containerWorkflowGraph : graph } })
    expect(created.status()).toBe(201)
    const workflowId = ((await created.json()) as { workflow: { id: string } }).workflow.id
    expect((await page.request.post(`/api/v2/tenants/${tenantId}/workflows/${workflowId}/publish`, { data: { version: 'v1' } })).ok()).toBe(true)
    const frozenSnapshotId = (await (await page.request.get(`/api/v2/tenants/${tenantId}/workflows/${workflowId}`)).json()).published.id as string
    await page.reload()
    await expect(page.getByRole('heading', { name: '定时任务', exact: true })).toBeVisible()

    // The form offers the published workflow; the task runs it in a project with a kickoff input.
    const dialog = page.getByRole('dialog')
    await page.locator('header').getByRole('button', { name: '新建任务', exact: true }).click()
    await dialog.getByRole('textbox', { name: '任务名称' }).fill(taskName)
    await dialog.getByRole('button', { name: /运行工作流/ }).click()
    await dialog.getByRole('combobox', { name: '工作流', exact: true }).selectOption({ label: `${workflowName}（v1）` })
    await dialog.getByRole('combobox', { name: '关联项目', exact: true }).selectOption({ label: 'Demo' })
    await dialog.getByRole('textbox', { name: '初始输入' }).fill('给支付模块加一个对账接口')
    await expect(dialog.getByText('保存时固定生效版本 v1；之后发布的新版本要重新保存任务才会使用。')).toBeVisible()
    await dialog.getByRole('button', { name: '创建并启用' }).click()
    await expect(dialog.getByRole('heading', { name: taskName })).toBeVisible()
    await expect(dialog.getByText(`${workflowName}（v1）`)).toBeVisible()
    await page.screenshot({ path: testInfo.outputPath('schedule-workflow-config.png'), fullPage: true })

    // 立即运行 creates and starts one workflow run; the history follows that run to its end.
    const runRead = page.waitForResponse((response) => /\/workflow-runs\/[^/?]+$/.test(new URL(response.url()).pathname) && response.request().method() === 'GET')
    await dialog.getByRole('button', { name: '立即运行', exact: true }).click()
    await expect(dialog.getByRole('button', { name: '运行历史' })).toHaveAttribute('aria-pressed', 'true')
    if (realAgents) {
      await expect(dialog.getByText('工作流运行 · 成功')).toBeVisible({ timeout: 200_000 })
      // Container output includes the same fixture text in a scalar and an array.
      await expect(dialog.getByText(/V2 fixture complete/).first()).toBeVisible()
    } else {
      // No Agent is installed: v2-server fails the run at its Agent node, and the history says so.
      await expect(dialog.getByText('工作流运行 · 失败')).toBeVisible({ timeout: 60_000 })
      await expect(dialog.getByText('工作流运行失败')).toBeVisible()
    }
    // The occurrence settles with its run and frees the task for the next trigger.
    await expect(dialog.getByRole('button', { name: new RegExp(`^${realAgents ? '成功' : '失败'}.*手动$`) })).toBeVisible({ timeout: 30_000 })
    const detail = await (await page.request.get((await runRead).url())).json() as ContainerDetail
    expect(detail.run.snapshotId).toBe(frozenSnapshotId)
    expect(detail.run.input).toBe('给支付模块加一个对账接口')
    expect(detail.failedAttempts).toEqual([])
    await page.screenshot({ path: testInfo.outputPath('schedule-workflow-history.png'), fullPage: true })

    // Pause it, so no later trigger of this spec's task runs on the shared e2e database.
    await dialog.getByRole('button', { name: '暂停任务' }).click()
    await expect(dialog.getByText('已暂停')).toBeVisible()

    // The history opens the occurrence's own run page directly.
    await dialog.getByRole('link', { name: '查看执行详情' }).click()
    const pane = page.getByRole('region', { name: '工作流运行' })
    await expect(pane.getByRole('heading', { name: taskName })).toBeVisible({ timeout: 15_000 })
    // Preserve the run deep link so refreshing keeps the same execution.
    await expect(page).toHaveURL(/\/chat\?run=[0-9a-f-]+$/)
    await page.screenshot({ path: testInfo.outputPath('schedule-workflow-run-page.png'), fullPage: true })

    // The run is an ordinary run of the project: the chat page lists it under Demo.
    const demo = page.getByRole('complementary', { name: '聊天记录' }).getByRole('region', { name: '项目 Demo' })
    const runRow = demo.getByRole('button', { name: `工作流运行 ${taskName}（${realAgents ? '成功' : '失败'}）` })
    await expect(runRow).toBeVisible()
    if (!containers) return

    // The merged detail keeps both the root variable projection and the container histories.
    expect(detail.scopes.map((scope) => [scope.roundIndex, scope.status])).toEqual([[1, 'succeeded'], [2, 'succeeded']])
    expect(detail.variables).toContainEqual(expect.objectContaining({ selector: ['start', 'items'], value: ['第一段', '第二段'] }))
    expect(detail.variables.some((variable) => variable.selector.join('.') === 'loop.draft')).toBe(false)
    const writers = detail.nodes.filter((node) => node.nodeId === 'writer')
    const workers = detail.nodes.filter((node) => node.nodeId === 'worker')
    const task = (node: ContainerDetail['nodes'][number]) => (JSON.parse(node.payload!) as { task: string }).task
    expect(task(writers[0]!)).toBe('改进：初稿')
    expect(task(writers[1]!)).toMatch(/^改进：V2 fixture complete/)
    expect(workers.map((node) => [node.iteration, task(node)])).toEqual([[0, '处理第 0 段：第一段'], [1, '处理第 1 段：第二段']])
    expect(detail.iterationConditionDecisions).toEqual({})
    expect(JSON.parse(detail.run.output!)).toMatchObject({ draft: expect.stringContaining('V2 fixture complete'), paragraphs: [expect.any(String), expect.any(String)] })

    await runRow.click()
    await pane.getByRole('button', { name: '全图', exact: true }).click()
    await pane.getByLabel('工作流全图俯瞰').getByTestId('rf__node-start').click()
    await pane.getByRole('button', { name: '打开节点详情', exact: true }).click()
    await expect(pane.getByLabel('阶段详情').getByRole('textbox', { name: '初始 Prompt', exact: true })).toHaveValue('给支付模块加一个对账接口')
    await expect(pane.getByLabel('阶段详情').getByRole('textbox', { name: 'items (array[string])', exact: true })).toHaveValue('["第一段","第二段"]')
    await page.screenshot({ path: testInfo.outputPath('schedule-container-start.png'), fullPage: true })

    // Refresh and reopen from the project history, then read the exact attempts' session streams.
    await page.reload()
    await runRow.click()
    await pane.getByRole('button', { name: '舞台', exact: true }).click()
    await pane.getByRole('button', { name: '反馈循环: 成功', exact: true }).click()
    await pane.getByRole('button', { name: /第 2 轮 · 达到最大轮次/ }).click()
    await pane.getByRole('region', { name: '本轮概览' }).getByRole('button', { name: /改进初稿/ }).click()
    await pane.getByRole('button', { name: '查看节点会话' }).click()
    await expect.poll(() => subscribed).toContain(writers[1]!.id)
    await expect(pane.getByText(/改进：V2 fixture complete/)).toBeVisible()
    await pane.getByRole('button', { name: '逐段处理: 成功', exact: true }).click()
    await pane.getByRole('button', { name: '选择迭代轮次' }).click()
    await page.getByRole('option', { name: '第 1 轮', exact: true }).click()
    await pane.getByRole('button', { name: '处理段落: 成功', exact: true }).click()
    await pane.getByRole('button', { name: '查看节点会话' }).click()
    await expect.poll(() => subscribed).toContain(workers[0]!.id)
    await expect(pane.getByText(/处理第 0 段：第一段/)).toBeVisible()
    await pane.getByRole('button', { name: '选择迭代轮次' }).click()
    await page.getByRole('option', { name: '第 2 轮', exact: true }).click()
    await expect.poll(() => subscribed).toContain(workers[1]!.id)
    await expect(pane.getByText(/处理第 1 段：第二段/)).toBeVisible()
    await expect(pane.getByText(/处理第 0 段：第一段/)).toBeHidden()
    const toolCall = pane.getByText(/v2 fixture tool executed/).first()
    await toolCall.scrollIntoViewIfNeeded()
    await expect(toolCall).toBeInViewport()
    await page.screenshot({ path: testInfo.outputPath('schedule-container-session.png'), fullPage: true })
  })
}

test('latest schedules adopt a republish while fixed schedules and run history keep their snapshots', async ({ page }, testInfo) => {
  test.setTimeout(realAgents ? 300_000 : 150_000)
  await page.goto('/schedules')
  await signIn(page)
  const tenant = (await (await page.request.get('/api/v1/me/tenants')).json()).items[0].id
  const base = `/api/v2/tenants/${tenant}`
  const suffix = crypto.randomUUID().slice(0, 8)
  const name = `版本策略 ${suffix}`
  const created = await page.request.post(`${base}/workflows`, { data: { name, graph } })
  expect(created.status()).toBe(201)
  const workflowId = (await created.json()).workflow.id
  const publish = async (version: string) => {
    const response = await page.request.post(`${base}/workflows/${workflowId}/publish`, { data: { version } })
    expect(response.ok()).toBe(true)
    return (await response.json()).snapshot.id as string
  }
  const v1 = await publish('v1')
  const projectId = (await (await page.request.get(`${base}/schedule-resources`)).json()).projects.find((p: { name: string }) => p.name === 'Demo').id
  const fixedResponse = await page.request.post(`${base}/schedule-configs`, { data: { name: `固定-${suffix}`, projectId, target: { kind: 'workflow', workflowId, snapshotId: v1, input: '' }, rule: { kind: 'hours', hours: 24, time: '09:00', days: [], timezone: 'UTC' } } })
  expect(fixedResponse.status()).toBe(201)
  const fixed = await fixedResponse.json()
  await page.reload()
  const dialog = page.getByRole('dialog')
  await page.locator('header').getByRole('button', { name: '新建任务', exact: true }).click()
  await dialog.getByLabel('任务名称').fill(`跟随-${suffix}`)
  await dialog.getByRole('button', { name: /运行工作流/ }).click()
  await dialog.getByLabel('工作流', { exact: true }).selectOption(workflowId)
  await dialog.getByRole('button', { name: '跟随最新发布版本' }).click()
  await dialog.getByLabel('关联项目', { exact: true }).selectOption(projectId)
  await dialog.getByRole('button', { name: '创建并启用' }).click()
  await expect(dialog.getByText(/跟随最新发布版本，当前 v1/)).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('schedule-latest-policy.png'), fullPage: true })
  const configs = (await (await page.request.get(`${base}/schedule-configs`)).json()).items
  const latest = configs.find((c: { name: string }) => c.name === `跟随-${suffix}`)
  expect(latest.target).toEqual({ kind: 'workflow', workflowId, versionPolicy: 'latest', input: '' })
  const trigger = async (id: string, snapshot: string) => {
    const response = await page.request.post(`${base}/schedule-configs/${id}/run`, { data: { requestId: crypto.randomUUID() } })
    expect(response.ok()).toBe(true)
    const occurrence = await response.json()
    await expect.poll(async () => (await (await page.request.get(`${base}/schedule-configs/${id}/runs/${occurrence.id}`)).json()).released, { timeout: 60_000 }).toBe(true)
    const history = await (await page.request.get(`${base}/schedule-configs/${id}/runs/${occurrence.id}`)).json()
    const runId = JSON.parse(history.executionRef).workflowRunId
    const run = (await (await page.request.get(`${base}/workflow-runs/${runId}`)).json()).run
    expect(run.snapshotId).toBe(snapshot)
    return runId as string
  }
  const oldRun = await trigger(latest.id, v1)
  // Give the next release a visible graph difference so the old run's UI proves it is frozen.
  const nextGraph = JSON.parse(graph)
  nextGraph.nodes.find((node: { id: string }) => node.id === 'draft').data.title = '新版起草'
  expect((await page.request.put(`${base}/workflows/${workflowId}/draft`, { data: { graph: JSON.stringify(nextGraph) } })).ok()).toBe(true)
  // The editor warns before publication and names only the fixed task for this workflow.
  await page.goto('/workflows')
  await page.getByRole('complementary', { name: '工作流列表' }).getByRole('button', { name, exact: true }).click()
  await expect(page.getByLabel('工作流画布').getByTestId('rf__node-draft')).toContainText('新版起草')
  await page.getByRole('button', { name: '发布', exact: true }).click()
  const publishDialog = page.getByRole('alertdialog', { name: '发布工作流' })
  await expect(publishDialog.getByText(new RegExp(`固定-${suffix}`))).toBeVisible()
  await expect(publishDialog.getByText(new RegExp(`跟随-${suffix}`))).toBeHidden()
  await page.screenshot({ path: testInfo.outputPath('publish-pinned-warning.png'), fullPage: true })
  await publishDialog.getByRole('textbox').fill('v2')
  await publishDialog.getByRole('button', { name: '发布', exact: true }).click()
  await expect(publishDialog).toBeHidden()
  const v2 = (await (await page.request.get(`${base}/workflows/${workflowId}`)).json()).published.id
  expect(v2).not.toBe(v1)
  // Editing and saving a latest schedule must keep its policy and omit a snapshot pin.
  await page.goto('/schedules')
  await page.getByRole('button', { name: `查看跟随-${suffix}`, exact: true }).click()
  await dialog.getByRole('button', { name: '编辑', exact: true }).click()
  await expect(dialog.getByRole('button', { name: '跟随最新发布版本', exact: true })).toHaveAttribute('aria-pressed', 'true')
  await dialog.getByLabel('初始输入').fill('编辑后的需求')
  await dialog.getByRole('button', { name: '保存修改', exact: true }).click()
  await expect(dialog.getByText(/跟随最新发布版本，当前 v2/)).toBeVisible()
  const edited = (await (await page.request.get(`${base}/schedule-configs`)).json()).items.find((config: { id: string }) => config.id === latest.id)
  expect(edited.target).toEqual({kind:'workflow',workflowId,versionPolicy:'latest',input:'编辑后的需求'})
  expect(edited.version).toBeGreaterThan(latest.version)
  await trigger(latest.id, v2)
  await trigger(fixed.id, v1)
  expect((await (await page.request.get(`${base}/workflow-runs/${oldRun}`)).json()).run.snapshotId).toBe(v1)
  await dialog.getByRole('button', { name: '运行历史', exact: true }).click()
  // This schedule has exactly two executions: the oldest is the one made before republishing.
  await expect(dialog.locator('.sp-history-list > button')).toHaveCount(2)
  await dialog.locator('.sp-history-list > button').last().click()
  const historicalLink = dialog.getByRole('link', { name: '查看执行详情' })
  await expect(historicalLink).toHaveAttribute('href', `/chat?run=${oldRun}`)
  await historicalLink.click()
  const pane = page.getByRole('region', { name: '工作流运行' })
  await expect(pane.getByRole('heading', { name: `跟随-${suffix}` })).toBeVisible()
  await pane.getByRole('button', { name: '全图', exact: true }).click()
  await expect(pane.getByTestId('rf__node-draft')).toContainText('起草')
  await expect(pane.getByTestId('rf__node-draft')).not.toContainText('新版起草')
  await page.reload()
  await expect(pane.getByTestId('rf__node-draft')).toContainText('起草')
  await expect(pane.getByTestId('rf__node-draft')).not.toContainText('新版起草')
  await page.screenshot({ path: testInfo.outputPath('schedule-latest-frozen-history.png'), fullPage: true })
  // UI edits advance the resource version; pause with freshly read versions and the enabled route.
  const current = (await (await page.request.get(`${base}/schedule-configs`)).json()).items
  for (const id of [fixed.id, latest.id]) {
    const config = current.find((item: { id: string }) => item.id === id)
    expect((await page.request.patch(`${base}/schedule-configs/${id}/enabled`, { data: { version: config.version, enabled: false } })).ok()).toBe(true)
  }
})

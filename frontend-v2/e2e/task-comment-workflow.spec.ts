import { expect, test, type Page } from '@playwright/test'
import { signIn } from './login.ts'

async function publishWorkflow(page: Page, base: string, name: string, graph: Record<string, unknown>) {
  const created = await page.request.post(`${base}/workflows`, { data: { name, graph: JSON.stringify(graph) } })
  expect(created.status(), await created.text()).toBe(201)
  const id = (await created.json()).workflow.id as string
  const published = await page.request.post(`${base}/workflows/${id}/publish`, { data: { version: 'v1' } })
  expect(published.ok(), await published.text()).toBeTruthy()
  return id
}

test('comment @ workflow validates per field, runs as commenter, updates output live, and survives reload', async ({ page, browser }) => {
  test.setTimeout(120_000)
  const suffix = crypto.randomUUID().slice(0, 8)

  // Owner prepares a task (assigned to owner, still pending) and two published workflows.
  await page.goto('/collaboration')
  await signIn(page)
  const tid = (await (await page.request.get('/api/v1/me/tenants')).json()).items[0].id as string
  const ownerId = (await (await page.request.get('/api/v1/me')).json()).id as string
  const base = `/api/v2/tenants/${tid}`
  const projectId = (await (await page.request.get(`${base}/projects`)).json()).items[0].id as string

  const memberEmail = `wf-comment-${suffix}@demo.ora-space.dev`
  const memberName = `工作流成员-${suffix}`
  const invited = await page.request.post(`${base}/members`, { data: { email: memberEmail, displayName: memberName } })
  expect(invited.ok(), await invited.text()).toBeTruthy()

  const taskRes = await page.request.post(`${base}/tasks`, {
    data: { title: `评论工作流任务 ${suffix}`, description: '验证 @ 工作流启动与时间线展示', projectId, assignee: { type: 'human', id: ownerId } },
  })
  expect(taskRes.status(), await taskRes.text()).toBe(201)
  const taskId = (await taskRes.json()).id as string

  const quickName = `发布检查-${suffix}`
  const quickWfId = await publishWorkflow(page, base, quickName, {
    nodes: [
      {
        id: 'start',
        type: 'workflow',
        position: { x: 0, y: 100 },
        data: {
          kind: 'start',
          title: 'start',
          inputVariables: [
            { name: 'branch', displayName: '目标分支', valueType: 'string', required: true },
            { name: 'retries', displayName: '重试次数', valueType: 'number', required: false, value: 1 },
            { name: 'mode', displayName: '模式', valueType: 'string', fieldType: 'select', options: ['fast', 'safe'], required: false, value: 'fast' },
            { name: 'meta', displayName: '附加参数', valueType: 'object', required: false },
            { name: 'dryRun', displayName: '演练开关', valueType: 'boolean', required: false, value: false },
          ],
        },
      },
      {
        id: 'out',
        type: 'workflow',
        position: { x: 340, y: 100 },
        data: {
          kind: 'output',
          title: 'out',
          outputs: [
            { name: 'branch', variableSelector: ['start', 'branch'] },
            { name: 'retries', variableSelector: ['start', 'retries'] },
            { name: 'mode', variableSelector: ['start', 'mode'] },
            { name: 'meta', variableSelector: ['start', 'meta'] },
            { name: 'dryRun', variableSelector: ['start', 'dryRun'] },
            { name: 'comment', variableSelector: ['start', 'input'] },
          ],
        },
      },
    ],
    edges: [{ id: 'start-out', source: 'start', target: 'out' }],
  })

  const liveName = `审批流-${suffix}`
  const liveWfId = await publishWorkflow(page, base, liveName, {
    nodes: [
      {
        id: 'start',
        type: 'workflow',
        position: { x: 0, y: 100 },
        data: {
          kind: 'start',
          title: 'start',
          inputVariables: [
            { name: 'topic', displayName: '审批主题', valueType: 'string', required: true },
            { name: 'findings', valueType: 'array[object]', value: [{ key: `item-${suffix}`, title: '核对上线清单', description: '等待确认' }] },
          ],
        },
      },
      {
        id: 'review',
        type: 'workflow',
        position: { x: 340, y: 100 },
        data: {
          kind: 'form',
          title: 'review',
          formConfig: { title: '人工确认', submitLabel: '确认', reportSelector: ['start', 'input'], itemsSelector: ['start', 'findings'], fields: [] },
        },
      },
      {
        id: 'out',
        type: 'workflow',
        position: { x: 680, y: 100 },
        data: {
          kind: 'output',
          title: 'out',
          outputs: [
            { name: 'topic', variableSelector: ['start', 'topic'] },
            { name: 'result', variableSelector: ['start', 'input'] },
          ],
        },
      },
    ],
    edges: [
      { id: 'start-review', source: 'start', target: 'review' },
      { id: 'review-out', source: 'review', target: 'out' },
    ],
  })

  const missingRoleName = `缺角色流程-${suffix}`
  await publishWorkflow(page, base, missingRoleName, {
    nodes: [
      { id: 'start', type: 'workflow', position: { x: 0, y: 100 }, data: { kind: 'start', title: 'start', inputVariables: [] } },
      { id: 'agent', type: 'workflow', position: { x: 340, y: 100 }, data: { kind: 'agent', title: 'agent', agentConfig: { executor: { agentCli: 'official/ora-space.opencode', modelId: 'fixture' }, prompt: 'Check', roleId: `missing-role-${suffix}` } } },
    ],
    edges: [{ id: 'start-agent', source: 'start', target: 'agent' }],
  })

  // Sign in as the invited non-assignee member on a fresh context.
  const memberContext = await browser.newContext()
  const memberPage = await memberContext.newPage()
  await memberPage.goto('/')
  await signIn(memberPage, memberEmail)
  await memberPage.goto(`/collaboration/tasks/${taskId}?tab=discussion`)
  const memberId = (await (await memberPage.request.get('/api/v1/me')).json()).id as string

  const screenDir = process.env.E2E_SCREENSHOT_DIR || test.info().outputPath('screens')

  // Missing workspace role on @ workflow comment must return 400 workflow_role_not_found and explain why.
  const composer = memberPage.getByLabel('评论或给 Agent 的消息')
  await composer.click()
  await composer.fill(`@${missingRoleName}`)
  await memberPage.getByRole('option', { name: new RegExp(missingRoleName) }).click()
  const missingRoleResp = memberPage.waitForResponse(r => r.url().endsWith(`/tasks/${taskId}/comments`) && r.request().method() === 'POST')
  await memberPage.getByRole('button', { name: '发送并运行' }).click()
  expect((await missingRoleResp).status()).toBe(400)
  await expect(memberPage.getByText('工作流引用的角色在工作区角色目录里不存在，请先在设置的角色页新建同名角色再启动。')).toBeVisible()
  await memberPage.screenshot({ path: `${screenDir}/00-missing-role-comment-error.png`, fullPage: true })
  await memberPage.getByRole('button', { name: `取消启动 ${missingRoleName}` }).click()

  // Trigger @ workflow picker and validate per field cleanly (both client and structured server JSON).
  await composer.fill(`@${quickName}`)
  await memberPage.getByRole('option', { name: new RegExp(quickName) }).click()

  const formCard = memberPage.getByRole('region', { name: `启动工作流：${quickName}` })
  await expect(formCard).toBeVisible()

  await formCard.getByLabel(/重试次数/).fill('abc')
  await memberPage.getByRole('button', { name: '发送并运行' }).click()
  await expect(formCard.getByText('请填写此必填字段')).toBeVisible()
  await expect(formCard.getByText('请输入有效的数字')).toBeVisible()

  await formCard.getByLabel(/目标分支/).fill('main')
  await formCard.getByLabel(/重试次数/).fill('2')
  await formCard.getByLabel(/^模式/).selectOption('safe')
  await formCard.getByLabel(/附加参数/).fill('{broken-json')
  await formCard.getByRole('checkbox', { name: /演练开关/ }).click()
  await memberPage.getByRole('button', { name: '发送并运行' }).click()
  await expect(formCard.getByText('输入类型与字段要求不符')).toBeVisible()
  await expect(formCard.getByLabel(/附加参数/)).toHaveAttribute('aria-invalid', 'true')
  // Confirm zero runs were created by invalid inputs.
  const beforeValidRuns = (await (await memberPage.request.get(`${base}/tasks/${taskId}/workflow-runs`)).json()).items
  expect(beforeValidRuns).toHaveLength(0)

  // Fix invalid structured JSON field and launch cleanly as member.
  await formCard.getByLabel(/附加参数/).fill('{"env":"prod"}')
  await memberPage.screenshot({ path: `${screenDir}/01-composer-form.png`, fullPage: true })
  await memberPage.getByRole('button', { name: '发送并运行' }).click()

  const quickCard = memberPage.getByRole('region', { name: `工作流运行：${quickName}` })
  await expect(quickCard).toBeVisible()
  await expect(quickCard.locator('.mc-status')).toHaveText('已完成')
  await expect(quickCard.getByLabel('工作流输出')).toContainText('"branch": "main"')
  await expect(quickCard.getByLabel('工作流输出')).toContainText('"env": "prod"')
  await expect(memberPage.locator('.mc-task-bar .mc-status')).toHaveText('待处理')

  // Verify idempotent retry via API creates only ONE run for a repeated key.
  const idemHeaders = { 'Idempotency-Key': `idem-e2e-${suffix}` }
  const idemPayload = {
    body: `@${quickName} 幂等验证`,
    mentions: [{ type: 'workflow', id: quickWfId }],
    workflow: { workflowId: quickWfId, values: { branch: 'idem', retries: 1, mode: 'fast', meta: '{"env":"idem"}', dryRun: false } },
  }
  const firstIdem = await memberPage.request.post(`${base}/tasks/${taskId}/comments`, {
    headers: idemHeaders,
    data: idemPayload,
  })
  expect(firstIdem.status()).toBe(201)
  const firstBody = await firstIdem.json()
  expect(firstBody.workflowRun.status).toBe('succeeded')
  const retryIdem = await memberPage.request.post(`${base}/tasks/${taskId}/comments`, {
    headers: idemHeaders,
    data: idemPayload,
  })
  expect(retryIdem.status()).toBe(201)
  expect((await retryIdem.json()).workflowRun.id).toBe(firstBody.workflowRun.id)

  // Launch interactive workflow via UI -> starts as 待确认, completes live via SSE in same tab without reload!
  await composer.fill(`@${liveName}`)
  await memberPage.getByRole('option', { name: new RegExp(liveName) }).click()
  const liveFormCard = memberPage.getByRole('region', { name: `启动工作流：${liveName}` })
  await liveFormCard.getByLabel(/审批主题/).fill(`上线前检查-${suffix}`)
  await expect(liveFormCard.getByLabel('findings')).toHaveValue(new RegExp(`item-${suffix}`))
  await memberPage.getByRole('button', { name: '发送并运行' }).click()

  const liveCard = memberPage.getByRole('region', { name: `工作流运行：${liveName}` })
  await expect(liveCard.locator('.mc-status')).toHaveText('待确认')
  const liveHref = await liveCard.getByRole('link', { name: '查看运行' }).getAttribute('href')
  const liveRunId = new URL(`http://localhost${liveHref!}`).searchParams.get('workflow')!

  // Complete the awaiting form node in a second page while memberPage keeps the timeline open (no reload).
  const helperPage = await memberContext.newPage()
  await helperPage.goto(`/chat?run=${liveRunId}`)
  const reviewRegion = helperPage.getByRole('region', { name: '报告与问题确认' })
  await expect(reviewRegion.getByText('核对上线清单', { exact: true })).toBeVisible()
  await reviewRegion.getByRole('button', { name: '是问题', exact: true }).click()
  await reviewRegion.getByRole('button', { name: '确认', exact: true }).click()
  await expect(reviewRegion.getByText('已提交', { exact: true })).toBeVisible()
  await helperPage.close()

  // Live SSE updates timeline status -> 已完成 and reveals final output without page reload.
  await expect(liveCard.locator('.mc-status')).toHaveText('已完成', { timeout: 15_000 })
  await expect(liveCard.getByLabel('工作流输出')).toContainText(`上线前检查-${suffix}`)
  await memberPage.screenshot({ path: `${screenDir}/02-timeline-live-output.png`, fullPage: true })

  // Reloading persists status and output.
  await memberPage.reload()
  await expect(memberPage.getByRole('region', { name: `工作流运行：${quickName}` }).first().locator('.mc-status')).toHaveText('已完成')
  await expect(memberPage.getByRole('region', { name: `工作流运行：${liveName}` }).locator('.mc-status')).toHaveText('已完成')
  await expect(memberPage.getByRole('region', { name: `工作流运行：${liveName}` }).getByLabel('工作流输出')).toContainText(`上线前检查-${suffix}`)

  // Clicking the timeline link opens the run inside the task's 工作流 tab and records the commenter as starter.
  await memberPage.getByRole('region', { name: `工作流运行：${quickName}` }).first().getByRole('link', { name: '查看运行' }).click()
  await expect(memberPage).toHaveURL(new RegExp(`/collaboration/tasks/${taskId}\\?tab=workflows&workflow=`))
  await memberPage.getByRole('button', { name: '返回工作流列表' }).click()
  await expect(memberPage.getByText(`评论 @ · v1 · ${memberName} 发起`).first()).toBeVisible()
  await memberPage.screenshot({ path: `${screenDir}/03-workflows-tab-mention.png`, fullPage: true })

  const allRuns = (await (await memberPage.request.get(`${base}/tasks/${taskId}/workflow-runs`)).json()).items as { workflowId: string; kind: string; startedBy: string }[]
  expect(allRuns.filter(r => r.workflowId === quickWfId)).toHaveLength(2)
  expect(allRuns.filter(r => r.workflowId === liveWfId)).toHaveLength(1)
  expect(allRuns.every(r => r.kind === 'mention' && r.startedBy === memberId)).toBe(true)

  // Verify responsive mobile layout has no horizontal overflow.
  await memberPage.setViewportSize({ width: 390, height: 844 })
  await memberPage.getByRole('tab', { name: /讨论/ }).click()
  expect(await memberPage.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await memberPage.screenshot({ path: `${screenDir}/04-mobile-timeline.png`, fullPage: true })

  // Soft deletion retains the comment card but must remove every readable run result.
  const deletedRunId = firstBody.workflowRun.id as string
  const deleted = await memberPage.request.delete(`${base}/workflow-runs/${deletedRunId}`)
  expect(deleted.ok(), await deleted.text()).toBeTruthy()
  await memberPage.reload()
  const deletedCard = memberPage.getByRole('region', { name: `工作流运行：${quickName}` }).filter({ hasText: '运行已删除' })
  await expect(deletedCard).toBeVisible()
  await expect(deletedCard.getByLabel('工作流输出')).toHaveCount(0)
  await expect(deletedCard.getByRole('link', { name: '查看运行' })).toHaveCount(0)
  const deletedTimeline = await (await memberPage.request.get(`${base}/tasks/${taskId}/timeline`)).json()
  const deletedSummary = deletedTimeline.comments.find((comment: { workflowRun?: { id: string } }) => comment.workflowRun?.id === deletedRunId).workflowRun
  expect(deletedSummary).toMatchObject({ id: deletedRunId, deleted: true, output: null, error: null })
  const replayDeleted = await memberPage.request.post(`${base}/tasks/${taskId}/comments`, { headers: idemHeaders, data: idemPayload })
  expect(replayDeleted.status()).toBe(201)
  expect((await replayDeleted.json()).workflowRun).toMatchObject({ id: deletedRunId, deleted: true, output: null, error: null })
  const deletedRunsResp = await (await memberPage.request.get(`${base}/tasks/${taskId}/workflow-runs`)).json()
  const deletedRunRecord = (deletedRunsResp.items as { workflowRunId: string; deleted: boolean; failure: string }[]).find(r => r.workflowRunId === deletedRunId)
  expect(deletedRunRecord).toMatchObject({ workflowRunId: deletedRunId, deleted: true, failure: '' })
  expect((await memberPage.request.get(`${base}/workflow-runs/${deletedRunId}`)).status()).toBe(404)
  await memberPage.screenshot({ path: `${screenDir}/05-deleted-run-hidden-output.png`, fullPage: true })

  await memberContext.close()
})

test('comment @ workflow preserves explicit Start variable named input', async ({ page }) => {
  test.setTimeout(60_000)
  const suffix = crypto.randomUUID().slice(0, 8)
  await page.goto('/collaboration')
  await signIn(page)
  const tid = (await (await page.request.get('/api/v1/me/tenants')).json()).items[0].id as string
  const ownerId = (await (await page.request.get('/api/v1/me')).json()).id as string
  const base = `/api/v2/tenants/${tid}`
  const projectId = (await (await page.request.get(`${base}/projects`)).json()).items[0].id as string

  const taskRes = await page.request.post(`${base}/tasks`, {
    data: { title: `Input 变量验证 ${suffix}`, projectId, assignee: { type: 'human', id: ownerId } },
  })
  expect(taskRes.status(), await taskRes.text()).toBe(201)
  const taskId = (await taskRes.json()).id as string

  const wfName = `输入变量流-${suffix}`
  await publishWorkflow(page, base, wfName, {
    nodes: [
      {
        id: 'start',
        type: 'workflow',
        position: { x: 0, y: 100 },
        data: {
          kind: 'start',
          title: 'start',
          inputVariables: [{ name: 'input', displayName: '自定义输入', valueType: 'string', required: true }],
        },
      },
      {
        id: 'out',
        type: 'workflow',
        position: { x: 340, y: 100 },
        data: { kind: 'output', title: 'out', outputs: [{ name: 'input', variableSelector: ['start', 'input'] }] },
      },
    ],
    edges: [{ id: 'start-out', source: 'start', target: 'out' }],
  })

  await page.goto(`/collaboration/tasks/${taskId}?tab=discussion`)
  const composer = page.getByLabel('评论或给 Agent 的消息')
  await composer.click()
  await composer.fill(`@${wfName}`)
  await page.getByRole('option', { name: new RegExp(wfName) }).click()

  const formCard = page.getByRole('region', { name: `启动工作流：${wfName}` })
  await formCard.getByLabel(/自定义输入/).fill(`explicit-input-${suffix}`)
  await page.getByRole('button', { name: '发送并运行' }).click()

  const card = page.getByRole('region', { name: `工作流运行：${wfName}` })
  await expect(card.locator('.mc-status')).toHaveText('已完成', { timeout: 15_000 })
  await expect(card.getByLabel('工作流输出')).toContainText(`"input": "explicit-input-${suffix}"`)
})

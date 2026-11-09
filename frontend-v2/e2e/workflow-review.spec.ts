// The review node end to end: what a reviewer sees and can do at the form, what the task node
// records, and how the same page behaves for a waiting schedule, a second reviewer, a member who
// may only read, a cancelled run, a broken binding and a long list.
import { expect, test } from '@playwright/test'
import { signIn } from './login.ts'

const findings = (suffix: string) => [
  { key: `src:token-${suffix}`, title: '源码中存在硬编码令牌', description: '令牌应与代码分离。', evidence: 'src/sync.mjs:1', severity: '高', module: 'src/sync.mjs' },
  { key: `src:eval-${suffix}`, title: '使用 eval 执行动态代码', description: '改为模板函数。', evidence: 'src/report.mjs:3', severity: '中', module: 'src/report.mjs' },
  { key: `test:coverage-${suffix}`, title: '模块缺少测试覆盖', description: '补上 node --test 用例。', evidence: 'src/export.mjs', severity: '中', module: 'test/' },
]

const graph = (rows: unknown[], name: string) => JSON.stringify({
  nodes: [
    { id: 'start', data: { kind: 'start', title: `开始：${name}`, inputVariables: [{ name: 'findings', valueType: 'array[object]', value: rows }] } },
    { id: 'review', data: { kind: 'form', title: '确认扫描结果', formConfig: { title: '版本质量扫描报告与问题确认', submitLabel: '确认并生成任务', reportSelector: ['start', 'input'], itemsSelector: ['start', 'findings'], fields: [{ name: 'notes', label: '备注', type: 'textarea', required: false }] } } },
    { id: 'tasks', data: { kind: 'createTasks', title: '创建或关联协作任务', createTasksConfig: { formNodeId: 'review' } } },
    { id: 'output', data: { kind: 'output', title: '输出处置结果', outputs: [{ name: 'tasks', variableSelector: ['tasks', 'structured_output', 'tasks'] }] } },
  ].map((node, index) => ({ ...node, type: 'workflow', position: { x: index * 340, y: 100 }, data: { ...node.data, description: '' } })),
  edges: [['start', 'review'], ['review', 'tasks'], ['tasks', 'output']].map(([source, target]) => ({ id: `${source}-${target}`, source, target })),
})

async function publishRun(page: import('@playwright/test').Page, name: string, rows: unknown[]) {
  const stamp = Date.now().toString(36)
  const tenant = ((await (await page.request.get('/api/v1/me/tenants')).json()) as { items: { id: string }[] }).items[0]!.id
  const projects = (await (await page.request.get(`/api/v2/tenants/${tenant}/projects`)).json()) as { items: { id: string; name: string }[] }
  const project = projects.items.find(project => project.name === 'Demo') ?? projects.items[0]!
  const created = await page.request.post(`/api/v2/tenants/${tenant}/workflows`, { data: { name: `${name} ${stamp}`, graph: graph(rows, name) } })
  expect(created.status()).toBe(201)
  const workflow = ((await created.json()) as { workflow: { id: string } }).workflow.id
  expect((await page.request.post(`/api/v2/tenants/${tenant}/workflows/${workflow}/publish`, { data: { version: 'v1' } })).ok()).toBe(true)
  const started = await page.request.post(`/api/v2/tenants/${tenant}/workflows/${workflow}/runs`, { data: { projectId: project.id, input: '# 版本质量扫描报告\n\n三处问题，等待确认。' } })
  expect(started.ok()).toBe(true)
  const run = ((await started.json()) as { run: { id: string } }).run.id
  expect((await page.request.post(`/api/v2/tenants/${tenant}/workflow-runs/${run}/start`, { data: {} })).ok()).toBe(true)
  return { tenant, projectId: project.id, workflow, run }
}

test('review → tasks → task page, with draft, bulk, filters and reuse on the second night', async ({ page }) => {
  test.setTimeout(180_000)
  await page.goto('/chat')
  await signIn(page)
  const suffix = Date.now().toString(36)
  const { workflow, run } = await publishRun(page, '巡检端到端', findings(suffix))

  await page.goto(`/chat?run=${run}`)
  const form = page.getByRole('region', { name: '报告与问题确认' })
  await expect(form.getByRole('heading', { name: '源码中存在硬编码令牌' })).toBeVisible({ timeout: 30_000 })

  // Structure: three findings, every count agrees, no owner control until a decision exists.
  await expect(form.getByRole('button', { name: '全部 3' })).toBeVisible()
  await expect(form.getByRole('button', { name: '待确认 3' })).toBeVisible()
  await expect(form.getByLabel('指派给', { exact: true })).toHaveCount(0)
  await expect(form.getByText('还有 3 项待确认')).toBeVisible()

  // Bulk marks everything and can be undone.
  await form.getByRole('button', { name: '全部标为问题' }).click()
  await expect(form.getByText('将新建 3 个任务')).toBeVisible()
  await expect(form.getByLabel('指派给', { exact: true })).toHaveCount(3)
  await form.getByRole('button', { name: '撤销' }).click()
  await expect(form.getByText('还有 3 项待确认')).toBeVisible()

  // One click decides one row and gives it the confirmer as owner.
  const groups = form.getByRole('group', { name: '是否为问题' })
  await groups.nth(0).getByRole('button', { name: '是问题', exact: true }).click()
  await expect(form.getByLabel('指派给', { exact: true })).toHaveCount(1)
  await expect(form.getByLabel('指派给', { exact: true })).toHaveValue(/^human:/)
  await expect(form.getByText('还有 2 项待确认')).toBeVisible()

  // Filtering hides what it should and keeps the counts.
  await form.getByRole('button', { name: '待确认 2' }).click()
  await expect(form.getByRole('heading', { name: '源码中存在硬编码令牌' })).toHaveCount(0)
  await form.getByRole('button', { name: '排除筛选的 2 项' }).click()
  await expect(form.getByRole('button', { name: '是问题 1' })).toBeVisible()
  await expect(form.getByRole('button', { name: '不是问题 2' })).toBeVisible()
  await form.getByRole('button', { name: '撤销' }).click()
  await form.getByRole('button', { name: '是问题 1' }).click()
  await expect(form.getByRole('heading', { name: '源码中存在硬编码令牌' })).toBeVisible()
  await form.getByRole('button', { name: '全部 3' }).click()

  // A rejection asks for nothing and the last problem gets its own picker.
  await groups.nth(1).getByRole('button', { name: '不是问题', exact: true }).click()
  await groups.nth(2).getByRole('button', { name: '是问题', exact: true }).click()
  await expect(form.getByLabel('指派给', { exact: true })).toHaveCount(2)

  // Notes are hidden until asked for and persist through a draft round trip.
  await form.getByRole('button', { name: '添加备注' }).first().click()
  await form.getByLabel('备注', { exact: true }).first().fill('令牌已轮换，本周改为环境变量。')
  await form.getByRole('button', { name: '保存草稿' }).click()
  await expect(form.getByText('草稿已保存。')).toBeVisible()
  await page.reload()
  const reloaded = page.getByRole('region', { name: '报告与问题确认' })
  await expect(reloaded.getByLabel('备注', { exact: true }).first()).toHaveValue('令牌已轮换，本周改为环境变量。')
  await expect(reloaded.getByRole('group', { name: '是否为问题' }).nth(1).getByRole('button', { name: '不是问题', exact: true })).toHaveAttribute('aria-pressed', 'true')

  // Submit turns the page into conclusions, not disabled inputs.
  await reloaded.getByRole('button', { name: '确认并生成任务' }).click()
  await expect(reloaded.getByText('已提交')).toBeVisible({ timeout: 30_000 })
  await expect(reloaded.getByRole('link', { name: '打开任务' })).toHaveCount(2)
  await expect(reloaded.getByLabel('是否为问题')).toHaveCount(0)
  await expect(reloaded.getByText('已确认无需处理，不会创建任务。')).toBeVisible()
  await expect(reloaded.getByText('已由 协作空间 Owner 确认')).toBeVisible()

  // CreateTasks lists the same two problems, by title, with links.
  await page.getByRole('button', { name: '创建或关联协作任务' }).first().click()
  const tasks = page.getByRole('region', { name: '任务处理结果' })
  await expect(tasks.getByText('源码中存在硬编码令牌')).toBeVisible()
  await expect(tasks.getByText('模块缺少测试覆盖')).toBeVisible()
  await expect(tasks.getByText('使用 eval 执行动态代码')).toHaveCount(0)
  await expect(tasks.getByText('新建 2')).toBeVisible()

  // The task page keeps the source of the inspection.
  await tasks.getByRole('link', { name: '打开任务' }).first().click()
  await expect(page.getByRole('heading', { name: '源码中存在硬编码令牌' })).toBeVisible()
  await expect(page.getByText(`/chat?run=${run}`)).toBeVisible()
  expect(page.url()).toContain('/collaboration/tasks/')

  // The same findings on a later night link to the open tasks instead of creating new ones.
  const tenant = ((await (await page.request.get('/api/v1/me/tenants')).json()) as { items: { id: string }[] }).items[0]!.id
  const projects = (await (await page.request.get(`/api/v2/tenants/${tenant}/projects`)).json()) as { items: { id: string; name: string }[] }
  const projectId = projects.items.find(project => project.name === 'Demo')!.id
  const second = await page.request.post(`/api/v2/tenants/${tenant}/workflows/${workflow}/runs`, { data: { projectId, input: '# 第二次巡检' } })
  const secondRun = ((await second.json()) as { run: { id: string } }).run.id
  await page.request.post(`/api/v2/tenants/${tenant}/workflow-runs/${secondRun}/start`, { data: {} })
  await page.goto(`/chat?run=${secondRun}`)
  const again = page.getByRole('region', { name: '报告与问题确认' })
  await expect(again.getByRole('heading', { name: '源码中存在硬编码令牌' })).toBeVisible({ timeout: 30_000 })
  await expect(again.getByText('关联到已有任务')).toHaveCount(2)
  await again.getByRole('button', { name: '全部标为问题' }).click()
  await expect(again.getByText('将新建 1 个任务、关联 2 个已有任务')).toBeVisible()
  await expect(again.getByLabel('指派给', { exact: true })).toHaveCount(3)
  await again.getByRole('button', { name: '确认并生成任务' }).click()
  await expect(again.getByText('已提交')).toBeVisible({ timeout: 30_000 })
  await page.getByRole('button', { name: '创建或关联协作任务' }).first().click()
  await expect(page.getByRole('region', { name: '任务处理结果' }).getByText('关联 2')).toBeVisible()
  await expect(page.getByRole('region', { name: '任务处理结果' }).getByText('新建 1')).toBeVisible()
})

test('a run with no findings asks for one confirmation and creates nothing', async ({ page }) => {
  test.setTimeout(90_000)
  await page.goto('/chat')
  await signIn(page)
  const { run } = await publishRun(page, '巡检无问题', [])
  await page.goto(`/chat?run=${run}`)
  const form = page.getByRole('region', { name: '报告与问题确认' })
  await expect(form.getByText('本次未发现问题')).toBeVisible({ timeout: 30_000 })
  await expect(form.getByRole('button', { name: '全部标为问题' })).toHaveCount(0)
  await form.getByRole('button', { name: '确认无问题并继续' }).click()
  await expect(form.getByText('已提交')).toBeVisible({ timeout: 30_000 })
  await expect(form.getByRole('link', { name: '打开任务' })).toHaveCount(0)
  await page.getByRole('button', { name: '创建或关联协作任务' }).first().click()
  await expect(page.getByRole('region', { name: '任务处理结果' }).getByText('没有需要创建的任务')).toBeVisible()
})

test('a member reads the form but cannot submit it', async ({ page }) => {
  test.setTimeout(120_000)
  await page.goto('/chat')
  await signIn(page)
  const { run, tenant } = await publishRun(page, '巡检权限', findings(Date.now().toString(36)))
  const email = `member-${Date.now().toString(36)}@demo.ora-space.dev`
  const invited = await page.request.post(`/api/v2/tenants/${tenant}/members`, { data: { email, displayName: `受邀成员 ${Date.now()}` } })
  expect(invited.ok()).toBe(true)

  await page.getByRole('button', { name: '退出登录', exact: true }).click()
  await signIn(page, email)
  await page.goto(`/chat?run=${run}`)
  const form = page.getByRole('region', { name: '报告与问题确认' })
  await expect(form.getByText(/只有确认人或工作区管理员可以提交/)).toBeVisible({ timeout: 30_000 })
  const deciding = form.getByRole('group', { name: '是否为问题' }).getByRole('button', { name: '是问题', exact: true })
  await expect(deciding).toHaveCount(3)
  for (const button of await deciding.all()) await expect(button).toBeDisabled()
  await expect(form.getByRole('button', { name: '保存草稿' })).toHaveCount(0)
  await expect(form.getByText('负责人')).toHaveCount(0)
})

test('cancelling a waiting run closes the form', async ({ page }) => {
  test.setTimeout(90_000)
  await page.goto('/chat')
  await signIn(page)
  const { run } = await publishRun(page, '巡检取消', findings(Date.now().toString(36)))
  await page.goto(`/chat?run=${run}`)
  await expect(page.getByRole('region', { name: '报告与问题确认' }).getByText('还有 3 项待确认')).toBeVisible({ timeout: 30_000 })
  await page.getByRole('button', { name: '停止', exact: true }).click()
  await expect(page.getByRole('region', { name: '报告与问题确认' }).getByText('运行已取消')).toBeVisible({ timeout: 30_000 })
  await expect(page.getByRole('region', { name: '报告与问题确认' }).getByRole('button', { name: '确认并生成任务' })).toHaveCount(0)
})

test('a 390px reviewer can judge, see the footer and submit', async ({ page }) => {
  test.setTimeout(120_000)
  await page.goto('/chat')
  await signIn(page)
  const { run } = await publishRun(page, '巡检窄屏', findings(Date.now().toString(36)))
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto(`/chat?run=${run}`)
  const form = page.getByRole('region', { name: '报告与问题确认' })
  await expect(form.getByRole('heading', { name: '源码中存在硬编码令牌' })).toBeVisible({ timeout: 30_000 })
  const width = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }))
  expect(width.scroll).toBe(width.client)
  await form.getByRole('group', { name: '是否为问题' }).nth(0).getByRole('button', { name: '是问题', exact: true }).click()
  await expect(form.getByLabel('指派给', { exact: true })).toBeVisible()
  await form.getByRole('group', { name: '是否为问题' }).nth(1).getByRole('button', { name: '不是问题', exact: true }).click()
  await form.getByRole('group', { name: '是否为问题' }).nth(2).getByRole('button', { name: '不是问题', exact: true }).click()
  await expect(form.getByText('将新建 1 个任务')).toBeVisible()
  await form.getByRole('button', { name: '确认并生成任务' }).click()
  await expect(form.getByText('已提交')).toBeVisible({ timeout: 30_000 })
})

test('every stage shows its own content instead of the review page', async ({ page }) => {
  test.setTimeout(120_000)
  await page.goto('/chat')
  await signIn(page)
  const { run } = await publishRun(page, '巡检节点视图', findings(Date.now().toString(36)))
  await page.goto(`/chat?run=${run}`)
  const form = page.getByRole('region', { name: '报告与问题确认' })
  await expect(form.getByText('还有 3 项待确认')).toBeVisible({ timeout: 30_000 })

  // Start keeps the kickoff input, and returning to the review keeps the form.
  await page.getByRole('button', { name: '开始：巡检节点视图' }).first().click()
  await expect(page.getByRole('region', { name: '报告与问题确认' })).toHaveCount(0)
  await expect(page.getByText('# 版本质量扫描报告').first()).toBeVisible()
  await page.getByRole('button', { name: '确认扫描结果' }).first().click()
  await expect(page.getByRole('region', { name: '报告与问题确认' })).toBeVisible()

  // Output only exists after the review; it lists what the task node produced.
  await form.getByRole('button', { name: '全部标为问题' }).click()
  await form.getByRole('button', { name: '确认并生成任务' }).click()
  await expect(form.getByText('已提交')).toBeVisible({ timeout: 30_000 })
  await page.getByRole('button', { name: '输出处置结果' }).first().click()
  await expect(page.getByText(/"taskId"/).first()).toBeVisible()
})

test('a form node that cannot read its findings explains itself', async ({ page }) => {
  test.setTimeout(90_000)
  await page.goto('/chat')
  await signIn(page)
  const suffix = Date.now().toString(36)
  const rows = findings(suffix)
  const broken = JSON.stringify({
    nodes: [
      { id: 'start', data: { kind: 'start', title: '开始：坏绑定', inputVariables: [{ name: 'findings', valueType: 'array[object]', value: rows }] } },
      { id: 'review', data: { kind: 'form', title: '确认扫描结果', formConfig: { title: '确认', submitLabel: '提交', reportSelector: ['start', 'input'], itemsSelector: ['start', 'input'], fields: [] } } },
    ].map((node, index) => ({ ...node, type: 'workflow', position: { x: index * 340, y: 100 }, data: { ...node.data, description: '' } })),
    edges: [{ id: 'e1', source: 'start', target: 'review' }],
  })
  const tenant = ((await (await page.request.get('/api/v1/me/tenants')).json()) as { items: { id: string }[] }).items[0]!.id
  const projects = (await (await page.request.get(`/api/v2/tenants/${tenant}/projects`)).json()) as { items: { id: string; name: string }[] }
  const workflow = ((await (await page.request.post(`/api/v2/tenants/${tenant}/workflows`, { data: { name: `坏绑定 ${suffix}`, graph: broken } })).json()) as { workflow: { id: string } }).workflow.id
  await page.request.post(`/api/v2/tenants/${tenant}/workflows/${workflow}/publish`, { data: { version: 'v1' } })
  const run = ((await (await page.request.post(`/api/v2/tenants/${tenant}/workflows/${workflow}/runs`, { data: { projectId: projects.items[0]!.id, input: '# 报告' } })).json()) as { run: { id: string } }).run.id
  await page.request.post(`/api/v2/tenants/${tenant}/workflow-runs/${run}/start`, { data: {} })
  await page.goto(`/chat?run=${run}`)
  const failure = page.getByRole('region', { name: '确认表单未生成' })
  await expect(failure).toBeVisible({ timeout: 30_000 })
  await expect(failure.getByText('表单处理失败', { exact: false })).toBeVisible()
  await expect(failure.getByRole('button', { name: '刷新' })).toHaveCount(0)
})

test('two reviewers editing the same form get a conflict they can resolve', async ({ page, browser }) => {
  test.setTimeout(120_000)
  await page.goto('/chat')
  await signIn(page)
  const { run } = await publishRun(page, '巡检并发', findings(Date.now().toString(36)))

  const other = await browser.newContext()
  const second = await other.newPage()
  await second.goto('/chat')
  await signIn(second)

  await page.goto(`/chat?run=${run}`)
  await second.goto(`/chat?run=${run}`)
  const first = page.getByRole('region', { name: '报告与问题确认' })
  const peer = second.getByRole('region', { name: '报告与问题确认' })
  await expect(first.getByText('还有 3 项待确认')).toBeVisible({ timeout: 30_000 })
  await expect(peer.getByText('还有 3 项待确认')).toBeVisible({ timeout: 30_000 })

  // The first reviewer saves what the second has not seen yet.
  await peer.getByRole('button', { name: '添加备注' }).first().click()
  await peer.getByLabel('备注', { exact: true }).first().fill('同伴已经保存的备注')
  await peer.getByRole('button', { name: '保存草稿' }).click()
  await expect(peer.getByText('草稿已保存。')).toBeVisible()

  // The second reviewer edits before the refresh lands, and is told to reload the draft.
  await first.getByRole('group', { name: '是否为问题' }).nth(0).getByRole('button', { name: '是问题', exact: true }).click()
  await expect(first.getByText('其他确认人已更新这份表单')).toBeVisible({ timeout: 15_000 })
  await first.getByRole('button', { name: '载入最新草稿' }).click()
  await expect(first.getByLabel('备注', { exact: true }).first()).toHaveValue('同伴已经保存的备注')
  await other.close()
})

test('thirty findings stay scannable and bulk-ready', async ({ page }) => {
  test.setTimeout(120_000)
  await page.goto('/chat')
  await signIn(page)
  const suffix = Date.now().toString(36)
  const rows = Array.from({ length: 30 }, (_, index) => ({
    key: `bulk:${suffix}:${index}`, title: `批量问题 ${index + 1}`, description: '用于检查长列表。', evidence: `src/module-${index}.ts:1`,
    severity: index % 3 === 0 ? '高' : '中', module: `src/module-${index}.ts`,
  }))
  const { run } = await publishRun(page, '巡检长列表', rows)
  await page.goto(`/chat?run=${run}`)
  const form = page.getByRole('region', { name: '报告与问题确认' })
  await expect(form.getByRole('heading', { name: '批量问题 1', exact: true })).toBeVisible({ timeout: 30_000 })
  await expect(form.getByRole('article')).toHaveCount(30)
  await expect(form.getByText('还有 30 项待确认')).toBeVisible()
  await form.getByRole('button', { name: '全部标为问题' }).click()
  await expect(form.getByText('将新建 30 个任务')).toBeVisible()
  await expect(form.getByLabel('指派给', { exact: true })).toHaveCount(30)
  const width = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }))
  expect(width.scroll).toBe(width.client)
  await form.getByRole('button', { name: '确认并生成任务' }).click()
  await expect(form.getByText('已提交')).toBeVisible({ timeout: 60_000 })
  await expect(form.getByRole('link', { name: '打开任务' })).toHaveCount(30)
})

test('a scheduled workflow parks at the form and its history links back to it', async ({ page }) => {
  test.setTimeout(150_000)
  await page.goto('/schedules')
  await signIn(page)
  const suffix = Date.now().toString(36)
  const tenant = ((await (await page.request.get('/api/v1/me/tenants')).json()) as { items: { id: string }[] }).items[0]!.id
  const name = `定时巡检 ${suffix}`
  const created = await page.request.post(`/api/v2/tenants/${tenant}/workflows`, { data: { name, graph: graph(findings(suffix), '定时巡检') } })
  const workflow = ((await created.json()) as { workflow: { id: string } }).workflow.id
  await page.request.post(`/api/v2/tenants/${tenant}/workflows/${workflow}/publish`, { data: { version: 'v1' } })
  await page.reload()

  const dialog = page.getByRole('dialog')
  await page.locator('header').getByRole('button', { name: '新建任务', exact: true }).click()
  await dialog.getByRole('textbox', { name: '任务名称' }).fill(`定时巡检-${suffix}`)
  await dialog.getByRole('button', { name: /运行工作流/ }).click()
  await dialog.getByRole('combobox', { name: '工作流', exact: true }).selectOption({ label: `${name}（v1）` })
  await dialog.getByRole('combobox', { name: '关联项目', exact: true }).selectOption({ label: 'Demo' })
  await dialog.getByRole('textbox', { name: '初始输入' }).fill('巡检演示服务')
  await dialog.getByRole('button', { name: '创建并启用' }).click()
  await expect(dialog.getByRole('heading', { name: `定时巡检-${suffix}` })).toBeVisible()
  await dialog.getByRole('button', { name: '立即运行', exact: true }).click()
  // A waiting review does not block the next trigger: the schedule can start another run.
  await expect(dialog.getByRole('button', { name: '立即运行', exact: true })).toBeEnabled()
  const waiting = dialog.getByRole('button', { name: /待确认/ })
  await expect(waiting).toHaveCount(1, { timeout: 60_000 })
  await dialog.getByRole('button', { name: '立即运行', exact: true }).click()
  await expect(dialog.getByRole('button', { name: /待确认/ })).toHaveCount(2, { timeout: 60_000 })
  await waiting.first().click()
  await dialog.getByRole('link', { name: '查看执行详情' }).click()
  const form = page.getByRole('region', { name: '报告与问题确认' })
  await expect(form.getByText('还有 3 项待确认')).toBeVisible({ timeout: 30_000 })
  await form.getByRole('button', { name: '全部标为问题' }).click()
  await form.getByRole('button', { name: '确认并生成任务' }).click()
  await expect(form.getByText('已提交')).toBeVisible({ timeout: 30_000 })
  await page.goto('/schedules')
  await page.getByRole('button', { name: new RegExp(`定时巡检-${suffix}`) }).first().click()
})

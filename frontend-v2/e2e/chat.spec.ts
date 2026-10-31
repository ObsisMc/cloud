import { expect, type Page, test } from '@playwright/test'
import { signIn } from './login.ts'

const projects = (page: Page) => page.getByRole('complementary', { name: '聊天记录' }).getByRole('region')

// With prepared Node assets (`task v2:node:prepare`, then V2_NODE_ASSETS_DIR) v2-server runs real
// Agents on the fixture model; without them, as in CI, it can run none.
const realAgents = !!process.env.V2_NODE_ASSETS_DIR

test('chat: lists only the backend\'s projects, says it cannot run Agents yet, resize the column', async ({ page }) => {
  test.skip(realAgents, 'the backend can run Agents')
  await page.goto('/chat')
  await signIn(page)
  await expect(page.getByRole('heading', { name: '今天想构建什么？' })).toBeVisible()

  // The runtime seeds a real demo project. CI has no Node assets, so the executor list remains
  // empty and a new Agent conversation stays explicitly unavailable.
  await expect(page.getByRole('status').filter({ hasText: /后端还不能运行 Agent/ })).toBeVisible()
  // Only what v2-server returns: no sample project, and the demo project has no conversations yet.
  // Other specs may leave projects of their own in a reused database, so only these two are checked.
  const labels = await projects(page).evaluateAll((els) => els.map((e) => e.getAttribute('aria-label')))
  expect(labels).toContain('项目 Demo')
  expect(labels).not.toContain('项目 示例')
  await expect(projects(page).filter({ hasText: 'Demo' }).getByText('还没有会话')).toBeVisible()

  // A send is refused up front rather than failing on the backend.
  await page.getByRole('textbox', { name: '消息' }).fill('你好')
  await page.keyboard.press('Enter')
  await expect(page.getByRole('heading', { level: 2, name: '新对话' })).toBeVisible()

  // The column's edge resizes it; dragged onto the icon rail, the column folds away.
  const column = page.getByRole('complementary', { name: '聊天记录' })
  const edge = (await page.getByRole('separator', { name: '调整对话列表宽度' }).boundingBox())!
  await page.mouse.move(edge.x + edge.width / 2, edge.y + 200)
  await page.mouse.down()
  await page.mouse.move(edge.x + 100, edge.y + 200, { steps: 5 })
  await expect.poll(async () => Math.round((await column.boundingBox())!.width)).toBeGreaterThan(370)
  await page.mouse.move(10, edge.y + 200, { steps: 10 })
  await page.mouse.up()
  await expect(column).toBeHidden()
  await page.getByRole('button', { name: '展开对话列表' }).click()
  await expect(column).toBeVisible()
})

test('chat: a real Agent answers, a stopped chat turns read-only, history survives a reload', async ({ page }) => {
  test.skip(!realAgents, 'needs V2_NODE_ASSETS_DIR for a real Node and Agent')
  // The first conversation in a project installs the Agent plugin in the project's Node.
  test.setTimeout(300_000)
  const column = page.getByRole('complementary', { name: '聊天记录' })
  const demo = projects(page).filter({ hasText: 'Demo' })
  const composer = page.getByRole('textbox', { name: '消息' })
  const answer = 'V2 fixture complete. Real Agent executed the shell tool.'
  const ended = page.getByRole('status').filter({ hasText: '这个对话已结束' })
  const stamp = Date.now().toString(36)
  const answered = `读一下 README ${stamp}`
  const stopped = `这一轮会被停止 ${stamp}`
  // The thread, not the header or the history column, which also show the title.
  const thread = page.getByRole('region', { name: '会话' }).getByRole('main')

  await page.goto('/chat')
  // The real-chain spec needs the email login this branch introduced, like every other spec.
  await signIn(page)
  await expect(page.getByRole('heading', { name: '今天想构建什么？' })).toBeVisible()

  // A new chat in the demo project: the fixture model has the real Agent run its shell tool.
  await page.getByRole('button', { name: '选择项目' }).click()
  await page.getByRole('option', { name: 'Demo' }).click()
  await composer.fill(answered)
  await composer.press('Enter')
  await expect(page.getByText(answer)).toBeVisible({ timeout: 240_000 })
  await expect(column.getByRole('button', { name: answered, exact: true })).toBeVisible()

  // A second chat, stopped while its Agent is still starting, is read-only from then on.
  await demo.getByRole('button', { name: '在 Demo 新建对话' }).click()
  await page.getByRole('menuitem', { name: '新建对话' }).click()
  await composer.fill(stopped)
  await composer.press('Enter')
  await page.getByRole('button', { name: '停止', exact: true }).click()
  await expect(page.getByText('已停止 · 这个对话已结束，不能继续追问')).toBeVisible({ timeout: 60_000 })
  await expect(ended).toBeVisible()
  await expect(composer).toBeHidden()
  await expect(thread.getByText(stopped, { exact: true })).toHaveCount(1)

  // After a reload both come back from v2-server as they were.
  await page.reload()
  await column.getByRole('button', { name: answered, exact: true }).click()
  await expect(thread.getByText(answered, { exact: true })).toHaveCount(1)
  await expect(page.getByText(answer)).toBeVisible()
  await expect(composer).toBeVisible()
  await column.getByRole('button', { name: stopped, exact: true }).click()
  // The Agent never started, so no history holds this turn; it comes back from its result.
  await expect(thread.getByText(stopped, { exact: true })).toHaveCount(1)
  await expect(page.getByText('已停止 · 这个对话已结束，不能继续追问')).toBeVisible()
  await expect(ended).toBeVisible()
})

/** The names of the projects v2-server lists for the signed-in person's team. */
async function backendProjects(page: Page): Promise<string[]> {
  const tenants = (await (await page.request.get('/api/v1/me/tenants')).json()) as { items: { id: string }[] }
  const list = (await (await page.request.get(`/api/v2/tenants/${tenants.items[0].id}/projects?limit=100`)).json()) as { items: { name: string }[] }
  return list.items.map((p) => p.name)
}

test('chat: creates a project from a public repository and starts the new chat in it', async ({ page }) => {
  // The backend clones the repository before it answers; this one is small and public.
  test.setTimeout(150_000)
  await page.goto('/chat')
  await page.getByRole('button', { name: '开发者登录' }).click()
  await expect(page.getByRole('heading', { name: '今天想构建什么？' })).toBeVisible()
  const name = `Hello-World ${Date.now()}`

  await page.getByRole('button', { name: '选择项目' }).click()
  await page.getByRole('option', { name: /新建项目/ }).click()
  const dialog = page.getByRole('dialog', { name: '新建项目' })
  await dialog.getByLabel('仓库地址').fill('https://github.com/octocat/Hello-World.git')
  await expect(dialog.getByLabel('项目名')).toHaveValue('Hello-World')

  // A branch the repository lacks is refused, and no project is recorded.
  await dialog.getByLabel('默认分支').fill('no-such-branch')
  await dialog.getByLabel('项目名').fill(name)
  await dialog.getByRole('button', { name: '新建项目' }).click()
  await expect(dialog.getByRole('alert')).toContainText('仓库里没有分支 no-such-branch', { timeout: 120_000 })
  expect(await backendProjects(page)).not.toContain(name)

  await dialog.getByLabel('默认分支').fill('master')
  await dialog.getByRole('button', { name: '新建项目' }).click()
  await expect(dialog).toBeHidden({ timeout: 120_000 })
  await expect(page.getByRole('region', { name: `项目 ${name}` })).toBeVisible()
  await expect(page.getByRole('button', { name: '选择项目' })).toHaveText(name)
  expect(await backendProjects(page)).toContain(name)
})

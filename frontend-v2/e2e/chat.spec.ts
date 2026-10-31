import { expect, type Page, test } from '@playwright/test'
import { signIn } from './login.ts'

const projects = (page: Page) => page.getByRole('complementary', { name: '聊天记录' }).getByRole('region')

// With prepared Node assets (`task v2:node:prepare`, then V2_NODE_ASSETS_DIR) v2-server runs real
// Agents on the fixture model; without them, as in CI, it can run none.
const realAgents = !!process.env.V2_NODE_ASSETS_DIR

test('chat: replay a sample, say the backend cannot run Agents yet, resize the column', async ({ page }) => {
  test.skip(realAgents, 'the backend can run Agents')
  await page.goto('/chat')
  await signIn(page)
  await expect(page.getByRole('heading', { name: '今天想构建什么？' })).toBeVisible()

  // The runtime seeds a real demo project. CI has no Node assets, so the executor list remains
  // empty and a new Agent conversation stays explicitly unavailable.
  await expect(page.getByRole('status').filter({ hasText: /后端还不能运行 Agent/ })).toBeVisible()
  expect(await projects(page).evaluateAll((els) => els.map((e) => e.getAttribute('aria-label')))).toEqual(['项目 示例', '项目 Demo'])

  // A sample replays its recording: tool calls stream in, then fold into a summary.
  await projects(page).filter({ hasText: '示例' }).getByRole('button', { name: '工具调用：读、搜、改、执行', exact: true }).click()
  await expect(page.getByText('运行 npm run check')).toBeVisible()
  await expect(page.getByText('部分操作失败')).toBeVisible({ timeout: 20_000 })
  await expect(page.getByText('修改了 1 个文件')).toBeVisible()

  // Typing into a sample gets the stand-in reply, which says it is one.
  await page.getByRole('textbox', { name: '消息' }).fill('你好')
  await page.keyboard.press('Enter')
  await expect(page.getByText(/这是演示回复/)).toBeVisible()

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
  await page.getByRole('button', { name: '开发者登录' }).click()
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

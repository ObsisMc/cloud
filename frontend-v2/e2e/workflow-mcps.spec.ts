import { expect, test } from '@playwright/test'
import { signIn } from './login.ts'

test('MCP is read-only in Settings, so workflows have no browser-local MCP choice', async ({ page }, info) => {
  test.setTimeout(120_000)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/settings/plugins')
  await signIn(page)
  // MCP is read-only in v2: no install entry, and nothing is written to browser state.
  const plugin = page.getByRole('article', { name: 'Tavily', exact: true }).filter({ has: page.getByText('official/ora-space.tavily-search', { exact: true }) })
  await expect(plugin.getByText('暂不支持安装')).toBeVisible()
  expect(await plugin.getByRole('button', { name: '安装', exact: true }).count()).toBe(0)
  const tid = (await (await page.request.get('/api/v1/me/tenants')).json()).items[0].id
  const base = `/api/v2/tenants/${tid}/workflows`
  const name = `MCP 只读-${Date.now()}`
  const response = await page.request.post(base, { data: { name, graph: null } })
  expect(response.status()).toBe(201)
  await page.goto('/workflows')
  await page.getByRole('complementary', { name: '工作流列表' }).getByRole('button', { name, exact: true }).click()
  await expect(page.getByLabel('工作流名称', { exact: true })).toHaveValue(name)
  await page.getByRole('toolbar', { name: '添加工作流节点' }).getByRole('button', { name: 'Agent', exact: true }).click()
  // No MCP is installed (Settings never records a browser-local MCP install), so the "add MCP"
  // action is disabled and the picker stays empty.
  await expect(page.getByRole('button', { name: '添加 MCP', exact: true })).toBeDisabled()
  await page.screenshot({ path: info.outputPath('workflow-mcp-picker-empty.png'), animations: 'disabled' })
})

test('an Agent with enabled MCP completes and reports mcp as unapplied', async ({ page }) => {
  test.setTimeout(180_000)
  await page.goto('/workflows')
  await signIn(page)
  const tid = (await (await page.request.get('/api/v1/me/tenants')).json()).items[0].id
  const base = `/api/v2/tenants/${tid}`
  const executors = (await (await page.request.get(`${base}/chat/executors`)).json()).items
  test.skip(executors.length === 0, 'prepared Node assets are required for Agent execution')
  const executor = executors[0]
  const graph = JSON.stringify({
    nodes: [
      { id: 'start', data: { kind: 'start' } },
      { id: 'agent', data: { kind: 'agent', agentConfig: {
        executor: { agentCli: executor.agentId, modelId: executor.modelId },
        prompt: 'Reply with a short greeting.',
        mcps: [{ mcpId: 'official/ora-space.tavily-search', enabled: true }],
      } } },
      { id: 'out', data: { kind: 'output', outputs: [{ name: 'answer', variableSelector: ['agent', 'output'] }] } },
    ],
    edges: [{ source: 'start', target: 'agent' }, { source: 'agent', target: 'out' }],
  })
  const created = await page.request.post(`${base}/workflows`, { data: { name: `MCP unapplied ${Date.now()}`, graph } })
  expect(created.status()).toBe(201)
  const wid = (await created.json()).workflow.id
  expect((await page.request.post(`${base}/workflows/${wid}/publish`, { data: { version: 'v1' } })).ok()).toBe(true)
  const project = (await (await page.request.get(`${base}/projects?limit=100`)).json()).items[0]
  const run = await page.request.post(`${base}/workflows/${wid}/runs`, { data: { projectId: project.id, name: 'MCP ignored', input: 'hello' } })
  expect(run.status()).toBe(201)
  const rid = (await run.json()).run.id
  expect((await page.request.post(`${base}/workflow-runs/${rid}/start`)).ok()).toBe(true)
  const detail = async () => (await page.request.get(`${base}/workflow-runs/${rid}`)).json()
  await expect.poll(async () => (await detail()).run.status, { timeout: 120_000 }).toBe('succeeded')
  const node = (await detail()).nodes.find((n: { nodeId: string }) => n.nodeId === 'agent')
  expect(node.sessionId).toBeTruthy()
  expect(JSON.parse(node.payload).unapplied).toContain('mcp')
})

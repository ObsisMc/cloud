import { expect, test, type Page } from '@playwright/test'
import { Client } from 'pg'
import { randomUUID } from 'node:crypto'
import { resolve } from 'node:path'
import { signIn } from './login.ts'
import { containerWorkflowGraph } from './workflow-container-graph.ts'

// PostgreSQL projection fixture: timer/dispatch behavior is covered through the real store/host
// in retry_test.go. This browser case reads persisted waits/history through the real HTTP server.
/** Publishes a one-Agent workflow and creates a pending run of it through the real HTTP server. */
async function createRun(page: Page, graph?: string): Promise<{ tid: string, rid: string }> {
  await page.goto('/chat')
  await signIn(page)
  const tid = (await (await page.request.get('/api/v1/me/tenants')).json()).items[0].id
  const base = `/api/v2/tenants/${tid}`
  const project = (await (await page.request.get(`${base}/projects?limit=100`)).json()).items[0]
  const source = graph ?? JSON.stringify({ nodes: [
    { id: 'start', type: 'workflow', position: { x: 0, y: 0 }, data: { kind: 'start', title: '开始' } },
    { id: 'agent', type: 'workflow', position: { x: 320, y: 0 }, data: { kind: 'agent', title: '重试任务', agentConfig: { executor: { agentCli: 'official/ora-space.opencode', modelId: 'fixture' }, prompt: 'retry task' } } },
    { id: 'out', type: 'workflow', position: { x: 640, y: 0 }, data: { kind: 'output', title: '输出' } },
  ], edges: [{ id: 'a', source: 'start', target: 'agent' }, { id: 'b', source: 'agent', target: 'out' }] })
  const created = await page.request.post(`${base}/workflows`, { data: { name: `重试验收 ${Date.now()}`, graph: source } })
  expect(created.status()).toBe(201)
  const wid = (await created.json()).workflow.id
  expect((await page.request.post(`${base}/workflows/${wid}/publish`, { data: { version: 'v1' } })).ok()).toBe(true)
  const run = await page.request.post(`${base}/workflows/${wid}/runs`, { data: { projectId: project.id, name: '自动重试验收' } })
  expect(run.status()).toBe(201)
  return { tid, rid: (await run.json()).run.id }
}

// The first render after a navigation waits on the page's whole request chain (session, tenants,
// projects, run list, snapshot, then the run itself), measured at 3-4s on a loaded 4-CPU machine;
// like the other run-page specs, it gets the run page's first-render budget, not the 5s default.
const firstRender = { timeout: 30_000 }

const database = () => new Client({ connectionString: process.env.E2E_DATABASE_URL ?? 'postgres://ora:ora-local@127.0.0.1:55432/ora_v2_e2e?sslmode=disable' })

/**
 * Opens a replaced attempt's session and returns the error code its node events stream answered.
 * The UI follows the stream with an EventSource, which Chromium aborts as soon as it sees a non-200
 * status, so the browser keeps that response's body only when it arrived with the headers; reading
 * it is a race. The browser's own response proves which node the UI asked for and that it was
 * refused; the code comes from the same URL requested again in the same signed-in context.
 */
async function openedSessionError(page: Page, nodeRunId: string, open: () => Promise<void>): Promise<string> {
  const stream = page.waitForResponse((res) => res.url().includes(`/nodes/${nodeRunId}/events`))
  await open()
  const response = await stream
  expect(response.status()).toBe(404)
  const again = await page.request.get(response.url())
  expect(again.status()).toBe(404)
  return (await again.json()).code
}

test('root retry wait counts down and retains replaced attempt sessions and failure causes', async ({ page }) => {
  const { tid, rid } = await createRun(page)
  const db = database()
  await db.connect()
  try {
    await db.query(`INSERT INTO v2_workflow_node_runs (tenant_id,run_id,scope_id,node_id,node_type,status,output,started_at,finished_at)
      VALUES ($1,$2,$3,'start','start','succeeded','',now(),now())`, [tid, rid, `root:${rid}`])
    const previous = randomUUID(), current = randomUUID(), now = Date.now()
    const session = `retry-history-${previous}`
    const failure = { error_detail: { kind: 'session', message: '模型请求失败', source_chain: ['上游拒绝请求', '连接超时'], attempt: 1, auto_retryable: true, recorded_at: now } }
    await db.query(`INSERT INTO v2_workflow_node_runs (id,tenant_id,run_id,scope_id,node_id,node_type,status,session_id,error,payload,started_at,finished_at,deleted_at)
      VALUES ($1,$2,$3,$4,'agent','agent','failed',$5,'模型请求失败',$6,now(),now(),now())`, [previous, tid, rid, `root:${rid}`, session, JSON.stringify(failure)])
    const payload = { retry_wait: { attempt: 2, max_attempt: 3, retry: 1, max_retries: 2, delay_ms: 120000, scheduled_at: now, due_at: now + 120000, previous_node_run_id: previous }, auto_retry: { retry: 1, max_retries: 2 }, retry_chain: [previous] }
    await db.query(`INSERT INTO v2_workflow_node_runs (id,tenant_id,run_id,scope_id,node_id,node_type,status,payload) VALUES ($1,$2,$3,$4,'agent','agent','running',$5)`, [current, tid, rid, `root:${rid}`, JSON.stringify(payload)])
    await db.query(`UPDATE v2_workflow_runs SET status='running',state='{"current_nodes":["agent"]}',started_at=now() WHERE id=$1`, [rid])
    await page.goto(`/chat?run=${rid}`)
    const pane = page.getByRole('region', { name: '工作流运行' })
    await expect(pane.locator('[data-retry-waiting]').first()).toBeVisible(firstRender)
    const countdown = pane.locator('[data-retry-waiting]').first()
    const label = await countdown.textContent()
    await expect.poll(() => countdown.textContent()).not.toBe(label)
    await pane.getByRole('button', { name: '全图', exact: true }).click()
    await pane.getByLabel('工作流全图俯瞰').getByText('重试任务', { exact: true }).click()
    await pane.getByRole('button', { name: '打开节点详情' }).click()
    const history = pane.locator('[data-slot="failed-attempts"]').filter({ visible: true })
    await expect(history).toContainText('模型请求失败')
    await expect(history).toContainText('连接超时')
    await expect(history).toContainText(session)
    await page.screenshot({ path: resolve('../.tmp/screens/root-retry-wait.png'), animations: 'disabled', fullPage: true })
    await history.scrollIntoViewIfNeeded()
    await page.screenshot({ path: resolve('../.tmp/screens/root-retry-history.png'), animations: 'disabled', fullPage: true })
    // A fixture session is absent from execution; its replaced row was found and ownership is
    // still enforced. A wrong lookup would return workflow_node_run_not_found instead.
    expect(await openedSessionError(page, previous, () => history.getByText(`查看会话 · ${session}`).click())).toBe('workflow_node_session_not_found')
    await page.reload()
    await expect(pane.locator('[data-retry-waiting]').first()).toBeVisible(firstRender)
  } finally {
    await db.query(`UPDATE v2_workflow_runs SET status='cancelled',finished_at=now() WHERE id=$1`, [rid])
    await db.query(`UPDATE v2_workflow_node_runs SET status='cancelled',payload=payload-'retry_wait' WHERE run_id=$1 AND deleted_at IS NULL`, [rid])
    await db.end()
  }
})

async function seedContainerRetry(db: Client, ids: { tid: string, rid: string, scopeId: string, nodeId: string, iteration: number | null, label: string, waiting: boolean }) {
  const previous = randomUUID(), current = randomUUID(), now = Date.now()
  const session = `${ids.label}-session-${previous}`
  const message = `${ids.label} 失败`
  const failure = { error_detail: { kind: 'session', message, source_chain: [`${ids.label} 根因`], attempt: 1, auto_retryable: true, recorded_at: now } }
  await db.query(`INSERT INTO v2_workflow_node_runs (id,tenant_id,run_id,scope_id,node_id,node_type,status,session_id,error,payload,iteration,started_at,finished_at,deleted_at)
    VALUES ($1,$2,$3,$4,$5,'agent','failed',$6,$7,$8,$9,now(),now(),now())`, [previous, ids.tid, ids.rid, ids.scopeId, ids.nodeId, session, message, JSON.stringify(failure), ids.iteration])
  const payload = { ...(ids.waiting ? { retry_wait: { attempt: 2, max_attempt: 3, retry: 1, max_retries: 2, delay_ms: 120000, scheduled_at: now, due_at: now + 120000, previous_node_run_id: previous } } : {}), auto_retry: { retry: 1, max_retries: 2 }, retry_chain: [previous] }
  if (ids.waiting) {
    await db.query(`INSERT INTO v2_workflow_node_runs (id,tenant_id,run_id,scope_id,node_id,node_type,status,payload,iteration)
      VALUES ($1,$2,$3,$4,$5,'agent','running',$6,$7)`, [current, ids.tid, ids.rid, ids.scopeId, ids.nodeId, JSON.stringify(payload), ids.iteration])
  } else {
    await db.query(`INSERT INTO v2_workflow_node_runs (id,tenant_id,run_id,scope_id,node_id,node_type,status,output,payload,iteration,started_at,finished_at)
      VALUES ($1,$2,$3,$4,$5,'agent','succeeded',$6,$7,$8,now(),now())`, [current, ids.tid, ids.rid, ids.scopeId, ids.nodeId, `${ids.label} 最终结果`, JSON.stringify(payload), ids.iteration])
  }
  return { previous, session, message }
}

test('Loop and Iteration retry waits and replaced sessions stay in their selected rounds', async ({ page }) => {
  test.setTimeout(120_000)
  await page.setViewportSize({ width: 1600, height: 1000 })
  const { tid, rid } = await createRun(page, containerWorkflowGraph)
  const db = database()
  await db.connect()
  try {
    const root = `root:${rid}`, loopParent = randomUUID(), iterParent = randomUUID()
    await db.query(`INSERT INTO v2_workflow_node_runs (tenant_id,run_id,scope_id,node_id,node_type,status,output,started_at,finished_at)
      VALUES ($1,$2,$3,'start','start','succeeded','',now(),now())`, [tid, rid, root])
    for (const [id, nodeId, kind] of [[loopParent, 'loop', 'loop'], [iterParent, 'iter', 'iteration']] as const) {
      await db.query(`INSERT INTO v2_workflow_node_runs (id,tenant_id,run_id,scope_id,node_id,node_type,status,started_at)
        VALUES ($1,$2,$3,$4,$5,$6,'running',now())`, [id, tid, rid, root, nodeId, kind])
    }
    const loopHistory = new Map<number, { previous: string, session: string, message: string }>()
    for (const round of [1, 2]) {
      const scopeId = `loop-round-${round}:${rid}`
      await db.query(`INSERT INTO v2_workflow_scopes (id,tenant_id,run_id,parent_loop_node_run_id,round_index,status,state)
        VALUES ($1,$2,$3,$4,$5,$6,$7)`, [scopeId, tid, rid, loopParent, round, round === 1 ? 'succeeded' : 'running', JSON.stringify({ conditionDecisions: {}, variablePool: { catalog: {}, values: {} } })])
      loopHistory.set(round, await seedContainerRetry(db, { tid, rid, scopeId, nodeId: 'writer', iteration: null, label: `循环第 ${round} 轮`, waiting: round === 2 }))
    }
    const iterationHistory = new Map<number, { previous: string, session: string, message: string }>()
    for (const iteration of [0, 1]) {
      iterationHistory.set(iteration, await seedContainerRetry(db, { tid, rid, scopeId: root, nodeId: 'worker', iteration, label: `迭代第 ${iteration + 1} 轮`, waiting: iteration === 1 }))
    }
    await db.query(`UPDATE v2_workflow_runs SET status='running',state='{"current_nodes":["loop"]}',started_at=now() WHERE id=$1`, [rid])
    await page.goto(`/chat?run=${rid}`)
    const pane = page.getByRole('region', { name: '工作流运行' })
    // Select the waiting round explicitly: an earlier snapshot may have focused round 1.
    await pane.getByRole('button', { name: /反馈循环:/ }).click()
    await pane.getByRole('button', { name: /第 2 轮 ·/ }).click()
    await expect(pane.locator('[data-retry-wait="compact"]').first()).toBeVisible(firstRender)

    const openLoop = async (round: number) => {
      await pane.getByRole('button', { name: /反馈循环:/ }).click()
      await pane.getByRole('button', { name: new RegExp(`第 ${round} 轮 ·`) }).click()
      await pane.getByRole('region', { name: '本轮概览' }).getByRole('button', { name: /改进初稿/ }).click()
      await pane.getByRole('button', { name: '打开节点详情' }).click()
      const history = pane.locator('[data-slot="failed-attempts"]').filter({ visible: true })
      const expected = loopHistory.get(round)!
      const other = loopHistory.get(round === 1 ? 2 : 1)!
      await expect(history).toContainText(expected.message)
      await expect(history).toContainText(expected.session)
      await expect(history).toContainText(`第 ${round} 轮`)
      await expect(history).not.toContainText(other.message)
      await expect(history).toContainText(round === 2 ? '已安排自动重试' : '已自动重试')
      return { history, expected }
    }
    const secondLoop = await openLoop(2)
    await secondLoop.history.scrollIntoViewIfNeeded()
    await page.screenshot({ path: resolve('../.tmp/screens/container-retry-loop-round-2.png'), animations: 'disabled', fullPage: true })
    expect(await openedSessionError(page, secondLoop.expected.previous, () => secondLoop.history.getByText(`查看会话 · ${secondLoop.expected.session}`).click())).toBe('workflow_node_session_not_found')
    await page.reload()
    const firstLoop = await openLoop(1)
    await firstLoop.history.scrollIntoViewIfNeeded()
    await page.screenshot({ path: resolve('../.tmp/screens/container-retry-loop-round-1.png'), animations: 'disabled', fullPage: true })

    const openIteration = async (round: number) => {
      await pane.getByRole('button', { name: /逐段处理:/ }).click()
      await pane.getByRole('button', { name: '选择迭代轮次' }).click()
      await page.getByRole('option', { name: `第 ${round} 轮`, exact: true }).click()
      await pane.locator('[data-path-node="worker"]').click()
      const history = pane.locator('[data-slot="failed-attempts"]').filter({ visible: true })
      const expected = iterationHistory.get(round - 1)!
      const other = iterationHistory.get(round === 1 ? 1 : 0)!
      await expect(history).toContainText(expected.message)
      await expect(history).toContainText(expected.session)
      await expect(history).toContainText(`第 ${round} 轮`)
      await expect(history).not.toContainText(other.message)
      await expect(history).toContainText(round === 2 ? '已安排自动重试' : '已自动重试')
      return history
    }
    const firstIteration = await openIteration(1)
    await firstIteration.scrollIntoViewIfNeeded()
    await page.screenshot({ path: resolve('../.tmp/screens/container-retry-iteration-round-1.png'), animations: 'disabled', fullPage: true })
    const secondIteration = await openIteration(2)
    await secondIteration.scrollIntoViewIfNeeded()
    await page.screenshot({ path: resolve('../.tmp/screens/container-retry-iteration-round-2.png'), animations: 'disabled', fullPage: true })
  } finally {
    await db.query(`UPDATE v2_workflow_runs SET status='cancelled',finished_at=now() WHERE id=$1`, [rid])
    await db.query(`UPDATE v2_workflow_node_runs SET status='cancelled',payload=payload-'retry_wait' WHERE run_id=$1 AND deleted_at IS NULL`, [rid])
    await db.query(`UPDATE v2_workflow_scopes SET status='cancelled' WHERE run_id=$1 AND status IN ('pending','running')`, [rid])
    await db.end()
  }
})

test('a wait left under a failed run shows neither a countdown nor a scheduled retry', async ({ page }) => {
  const { tid, rid } = await createRun(page)
  const db = database()
  await db.connect()
  try {
    const previous = randomUUID(), current = randomUUID(), now = Date.now()
    const failure = { error_detail: { kind: 'session', message: '模型请求失败', source_chain: ['连接超时'], attempt: 1, auto_retryable: true, recorded_at: now } }
    await db.query(`INSERT INTO v2_workflow_node_runs (tenant_id,run_id,scope_id,node_id,node_type,status,output,started_at,finished_at)
      VALUES ($1,$2,$3,'start','start','succeeded','',now(),now())`, [tid, rid, `root:${rid}`])
    await db.query(`INSERT INTO v2_workflow_node_runs (id,tenant_id,run_id,scope_id,node_id,node_type,status,session_id,error,payload,started_at,finished_at,deleted_at)
      VALUES ($1,$2,$3,$4,'agent','agent','failed',$5,'模型请求失败',$6,now(),now(),now())`, [previous, tid, rid, `root:${rid}`, `retry-orphan-${previous}`, JSON.stringify(failure)])
    const payload = { retry_wait: { attempt: 2, max_attempt: 3, retry: 1, max_retries: 2, delay_ms: 120000, scheduled_at: now, due_at: now + 120000, previous_node_run_id: previous }, auto_retry: { retry: 1, max_retries: 2 }, retry_chain: [previous] }
    await db.query(`INSERT INTO v2_workflow_node_runs (id,tenant_id,run_id,scope_id,node_id,node_type,status,payload) VALUES ($1,$2,$3,$4,'agent','agent','running',$5)`, [current, tid, rid, `root:${rid}`, JSON.stringify(payload)])
    await db.query(`UPDATE v2_workflow_runs SET status='failed',error='另一分支失败',state='{"current_nodes":["agent"]}',started_at=now(),finished_at=now() WHERE id=$1`, [rid])
    await page.goto(`/chat?run=${rid}`)
    const pane = page.getByRole('region', { name: '工作流运行' })
    await pane.getByRole('button', { name: '全图', exact: true }).click()
    await pane.getByLabel('工作流全图俯瞰').getByText('重试任务', { exact: true }).click()
    await pane.getByRole('button', { name: '打开节点详情' }).click()
    const history = pane.locator('[data-slot="failed-attempts"]').filter({ visible: true })
    await expect(history).toContainText('模型请求失败')
    await expect(pane.locator('[data-retry-waiting]')).toHaveCount(0)
    await expect(history).not.toContainText('已安排自动重试')
    await history.scrollIntoViewIfNeeded()
    await page.screenshot({ path: resolve('../.tmp/screens/root-retry-abandoned.png'), animations: 'disabled', fullPage: true })
  } finally {
    await db.query(`UPDATE v2_workflow_node_runs SET status='cancelled',payload=payload-'retry_wait' WHERE run_id=$1 AND deleted_at IS NULL AND status='running'`, [rid])
    await db.end()
  }
})

test('retried node exposes injected previous failure context in node details', async ({ page }, testInfo) => {
  test.skip(!process.env.V2_NODE_ASSETS_DIR, 'needs the prepared Node fixture executor')
  test.setTimeout(180_000)
  const graph = JSON.stringify({
    nodes: [
      { id: 'start', type: 'workflow', position: { x: 0, y: 0 }, data: { kind: 'start', title: '开始' } },
      {
        id: 'agent',
        type: 'workflow',
        position: { x: 320, y: 0 },
        data: {
          kind: 'agent',
          title: '重试任务',
          agentConfig: {
            executor: { agentCli: 'official/ora-space.opencode', modelId: 'fixture' },
            prompt: 'return structured summary',
            retry: { enabled: true, maxRetries: 1, initialDelaySeconds: 1 },
            outputContract: {
              type: 'structured',
              schema: {
                type: 'object',
                properties: { summary: { type: 'string' } },
                required: ['summary'],
                additionalProperties: false,
              },
            },
          },
        },
      },
      { id: 'out', type: 'workflow', position: { x: 640, y: 0 }, data: { kind: 'output', title: '输出' } },
    ],
    edges: [{ id: 'a', source: 'start', target: 'agent' }, { id: 'b', source: 'agent', target: 'out' }],
  })
  const { tid, rid } = await createRun(page, graph)
  const started = await page.request.post(`/api/v2/tenants/${tid}/workflow-runs/${rid}/start`)
  expect(started.ok(), await started.text()).toBe(true)

  await expect.poll(async () => {
    const res = await page.request.get(`/api/v2/tenants/${tid}/workflow-runs/${rid}`)
    if (!res.ok()) return 'error'
    return ((await res.json()) as { run: { status: string } }).run.status
  }, { timeout: 150_000, intervals: [500] }).toBe('failed')

  await page.goto(`/chat?run=${rid}`)
  const pane = page.getByRole('region', { name: '工作流运行' })
  await expect(pane.getByRole('button', { name: '全图', exact: true })).toBeVisible(firstRender)
  await pane.getByRole('button', { name: '全图', exact: true }).click()
  await pane.getByLabel('工作流全图俯瞰').getByText('重试任务', { exact: true }).click()
  await pane.getByRole('button', { name: '打开节点详情' }).click()
  const summary = pane.getByText('本次尝试注入的上次失败信息', { exact: true }).filter({ visible: true }).first()
  await expect(summary).toBeVisible()
  await summary.click()
  const injectedContent = pane.getByText(/上一次尝试（第 1 次）失败信息/).filter({ visible: true }).first()
  await expect(injectedContent).toBeVisible()
  await expect(pane.getByText(/结构化输出不合格/).filter({ visible: true }).first()).toBeVisible()
  await injectedContent.scrollIntoViewIfNeeded()
  await page.screenshot({ path: testInfo.outputPath('retry-injected-failure.png'), animations: 'disabled', fullPage: true })
})

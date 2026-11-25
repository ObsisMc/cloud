import { describe, expect, it } from 'vitest'
import type { WorkflowNodeRun, WorkflowRunDetail } from '@/lib/workflows/api/generated'
import { normalizeWorkflowDefinition } from '@/lib/workflows/runtime'
import { toGraphWorkflowRun } from './server-run-adapter'

const definition = normalizeWorkflowDefinition({
  id: 'wf',
  name: 'v1',
  description: '',
  updatedAt: new Date(0).toISOString(),
  viewport: { x: 0, y: 0, zoom: 1 },
  nodes: [
    { id: 'start', type: 'workflow', position: { x: 0, y: 0 }, data: { kind: 'start', title: '开始', description: '' } },
    { id: 'a', type: 'workflow', position: { x: 1, y: 0 }, data: { kind: 'agent', title: '起草', description: '' } },
    { id: 'out', type: 'workflow', position: { x: 2, y: 0 }, data: { kind: 'output', title: '输出', description: '' } },
  ],
  edges: [
    { id: 'e1', source: 'start', target: 'a' },
    { id: 'e2', source: 'a', target: 'out' },
  ],
})

function nodeRun(over: Partial<WorkflowNodeRun>): WorkflowNodeRun {
  return {
    id: 'nr', runId: 'run', scopeId: 'root:run', nodeId: 'start', nodeType: 'start', sessionId: null, status: 'succeeded',
    input: null, output: null, error: null, payload: null, iteration: null, startedAt: 1_000, finishedAt: 2_000, createdAt: 1_000, updatedAt: 2_000,
    ...over,
  }
}

function graphNode(id: string, kind: (typeof definition.nodes)[number]['data']['kind'], parentId?: string, containerId?: string): (typeof definition.nodes)[number] {
  return { id, type: 'workflow', position: { x: 0, y: 0 }, parentId, data: { kind, title: id, description: '', containerId } }
}

function detail(over: Partial<WorkflowRunDetail['run']>, nodes: WorkflowNodeRun[], conditionDecisions: Record<string, string> = {}): WorkflowRunDetail {
  return {
    run: {
      id: 'run', workflowId: 'wf', snapshotId: 'snap', projectId: 'p', name: '运行', status: 'running', startedByName: '', state: '{"current_nodes":["a"]}',
      input: '需求', output: null, error: null, startedAt: 1_000, finishedAt: null, createdAt: 500, updatedAt: 2_000,
      ...over,
    },
    nodes,
    scopes: [],
    conditionDecisions,
    variables: [],
    failedAttempts: [],
    iterationConditionDecisions: {},
    startedBy: null,
  }
}

describe('toGraphWorkflowRun', () => {
  it('retains safe MCP setup diagnostics and does not project arbitrary values', () => {
    const run = toGraphWorkflowRun(detail({ status: 'failed' }, [nodeRun({
      nodeId: 'a', status: 'failed', error: 'mcp_setting_missing',
      payload: JSON.stringify({ error_detail: {
        kind: 'missing_agent_config', code: 'mcp_setting_missing',
        plugin_id: 'official/acme.search', setting_id: 'token', transport: 'http',
        value: 'must-not-project', auto_retryable: false, resumable: true,
      } }),
    })]), definition)
    expect(run.nodeStates.a.errorDetail).toMatchObject({
      code: 'mcp_setting_missing', pluginId: 'official/acme.search',
      settingId: 'token', transport: 'http', autoRetryable: false,
    })
    expect(JSON.stringify(run.nodeStates.a.errorDetail)).not.toContain('must-not-project')
  })
  it('keeps Loop scope sessions and Condition branches separate from root and from other rounds', () => {
    const graph = normalizeWorkflowDefinition({ ...definition, nodes: [
      graphNode('start', 'start'), graphNode('loop', 'loop'),
      graphNode('entry', 'start', undefined, 'loop'), graphNode('gate', 'condition', undefined, 'loop'), graphNode('a', 'agent', undefined, 'loop'),
    ], edges: [{ id: 'outer', source: 'start', target: 'loop' }, { id: 'body', source: 'entry', target: 'gate' }, { id: 'branch', source: 'gate', sourceHandle: 'yes', target: 'a' }] })
    const wire = detail({}, [nodeRun({ id: 'parent', nodeId: 'loop', nodeType: 'loop', status: 'running' }),
      nodeRun({ id: 'gate1', scopeId: 'round1', nodeId: 'gate', nodeType: 'condition' }),
      nodeRun({ id: 'gate2', scopeId: 'round2', nodeId: 'gate', nodeType: 'condition' }),
      nodeRun({ id: 'a1', scopeId: 'round1', nodeId: 'a', nodeType: 'agent', sessionId: 'session1', output: 'first', payload: JSON.stringify({ executor: { id: 'first-executor' }, model: { name: 'first-model' }, unapplied: ['retry'] }) }),
    ], { gate: 'unrelated root decision' })
    wire.scopes = [1, 2].map((index) => ({ id: `round${index}`, runId: 'run', parentLoopNodeRunId: 'parent', roundIndex: index, status: 'succeeded', state: JSON.stringify({ conditionDecisions: { gate: index === 1 ? 'yes' : 'else' } }), createdAt: index * 1_000, updatedAt: index * 1_000 }))
    const run = toGraphWorkflowRun(wire, graph)
    expect(run.nodeStates.a).toEqual({ status: 'idle' })
    expect(run.rounds?.[0]?.nodeStates.a).toMatchObject({ nodeRunId: 'a1', sessionId: 'session1', output: { detail: 'first' }, executor: { id: 'first-executor' }, model: { name: 'first-model' }, unapplied: ['retry'] })
    expect(run.rounds?.[1]?.nodeStates.a).toEqual({ status: 'inactive' })
    expect(run.rounds?.map((round) => round.nodeStates.gate?.selectedBranchId)).toEqual(['yes', 'else'])
  })

  it('projects all Iteration members per index including skipped collect targets, without copying prior sessions', () => {
    const graph = normalizeWorkflowDefinition({ ...definition, nodes: [
      graphNode('start', 'start'), graphNode('iter', 'iteration'), graphNode('gate', 'condition', 'iter'), graphNode('a', 'agent', 'iter'),
    ], edges: [{ id: 'outer', source: 'start', target: 'iter' }, { id: 'body', source: 'iter', target: 'gate' }, { id: 'branch', source: 'gate', sourceHandle: 'yes', target: 'a' }] })
    const wire = detail({ status: 'succeeded' }, [nodeRun({ id: 'parent', nodeId: 'iter', nodeType: 'iteration' }),
      nodeRun({ id: 'gate0', nodeId: 'gate', nodeType: 'condition', iteration: 0 }),
      nodeRun({ id: 'gate1', nodeId: 'gate', nodeType: 'condition', iteration: 1 }),
      nodeRun({ id: 'a0', nodeId: 'a', nodeType: 'agent', iteration: 0, sessionId: 'first', output: 'collected', payload: JSON.stringify({ executor: { id: 'collected-executor' }, model: { name: 'collected-model' }, unapplied: ['skill'] }) }),
    ])
    wire.iterationConditionDecisions = { 'gate#0': 'yes', 'gate#1': 'else' }
    const run = toGraphWorkflowRun(wire, graph)
    expect(run.roundStates?.a).toHaveLength(2)
    expect(run.roundStates?.a?.[0]).toMatchObject({ iteration: 0, nodeRunId: 'a0', sessionId: 'first', output: { detail: 'collected' }, executor: { id: 'collected-executor' }, model: { name: 'collected-model' }, unapplied: ['skill'] })
    expect(run.roundStates?.a?.[1]).toEqual({ iteration: 1, status: 'inactive' })
    expect(run.nodeStates.a).toEqual({ iteration: 1, status: 'inactive' })
    expect(run.roundStates?.gate?.map((state) => state.selectedBranchId)).toEqual(['yes', 'else'])
  })

  it('projects the run, leaves nodes without an attempt idle and shows an Agent turn', () => {
    const run = toGraphWorkflowRun(
      detail({ status: 'succeeded', output: '{"draft":"初稿"}', finishedAt: 3_000, state: '{"current_nodes":[]}' }, [
        nodeRun({ id: 'n1', output: '需求' }),
        nodeRun({
          id: 'n2', nodeId: 'a', nodeType: 'agent', sessionId: 's-1', input: '起草：{{#start.input#}}', output: '初稿',
          payload: JSON.stringify({ stop_reason: 'end_turn', task: '起草：需求' }),
        }),
      ]),
      definition,
    )
    expect(run).toMatchObject({ id: 'run', definitionId: 'wf', snapshotId: 'snap', status: 'succeeded', kickoffInput: '需求', finalOutput: '{"draft":"初稿"}', openHitls: [] })
    expect(run.finishedAt).toBe(new Date(3_000).toISOString())
    expect(run.nodeStates.out).toEqual({ status: 'idle' })
    const a = run.nodeStates.a!
    // The attempt row is kept, so the run page can follow its session.
    expect(a).toMatchObject({ status: 'succeeded', nodeRunId: 'n2', sessionId: 's-1', stopReason: 'end_turn', output: { summary: '初稿', detail: '初稿' } })
    // The rendered task, not the template the attempt input keeps, is what the Agent was asked.
    expect(a.conversation?.map((item) => item.kind === 'message' && [item.role, item.markdown])).toEqual([
      ['user', '起草：需求'],
      ['assistant', '初稿'],
    ])
  })

  it('reads a parked interactive node\'s session summary, its answer so far and the starter', () => {
    const payload = JSON.stringify({
      task: '起草：支付', interactive: { turns: 2, session_ended: true, completable: false, answer: '初稿', answer_source: 'last_completed_turn',
        completion: { blockers: [{ kind: 'turn_stopped' }, { kind: 'structured_invalid', params: { message: 'missing approved' } }, { nokind: true }], parkKind: 'abnormal', turnOrigin: 'human', stopCause: 'user_stopped' } },
    })
    const run = toGraphWorkflowRun({ ...detail({ status: 'awaitingInput' }, [nodeRun({ id: 'nr-a', nodeId: 'a', nodeType: 'agent', status: 'pending', sessionId: 's', finishedAt: null, payload })]), startedBy: 'user-1' }, definition)
    const a = run.nodeStates.a!
    expect(run.startedBy).toBe('user-1')
    expect(a.status).toBe('awaiting_input')
    expect(a.interactiveSession).toEqual({
      turns: 2, sessionEnded: true, completable: false, answer: '初稿', answerSource: 'last_completed_turn',
      blockers: [{ kind: 'turn_stopped' }, { kind: 'structured_invalid', message: 'missing approved' }], parkKind: 'abnormal', turnOrigin: 'human', stopCause: 'user_stopped',
    })
    expect(a.conversation?.map((item) => item.kind === 'message' && [item.role, item.markdown])).toEqual([['user', '起草：支付'], ['assistant', '初稿']])
    expect(run.nodeStates.start!.interactiveSession).toBeUndefined()
  })

  it('reads that the starter\'s last message to a parked node was not delivered', () => {
    const summary = { turns: 1, session_ended: false, completable: true, answer: '初稿' }
    const parked = (interactive: Record<string, unknown>) =>
      toGraphWorkflowRun(detail({ status: 'awaitingInput' }, [nodeRun({ id: 'nr-a', nodeId: 'a', nodeType: 'agent', status: 'pending', sessionId: 's', finishedAt: null, payload: JSON.stringify({ interactive }) })]), definition).nodeStates.a!.interactiveSession
    expect(parked({ ...summary, sent: 2, last_turn_error: 'not_delivered' })).toEqual({ turns: 1, sent: 2, sessionEnded: false, completable: true, blockers: [], answer: '初稿', lastTurnError: 'not_delivered' })
    expect(parked(summary)).toEqual({ turns: 1, sessionEnded: false, completable: true, blockers: [], answer: '初稿' })
  })

  it('reads that the Agent of a parked node did not connect to its completion tool', () => {
    const summary = { turns: 1, session_ended: false, completable: false, answer: '初稿' }
    const read = (interactive: Record<string, unknown>) =>
      toGraphWorkflowRun(detail({ status: 'awaitingInput' }, [nodeRun({ id: 'nr-a', nodeId: 'a', nodeType: 'agent', status: 'pending', sessionId: 's', finishedAt: null, payload: JSON.stringify({ interactive }) })]), definition).nodeStates.a!.interactiveSession
    expect(read({ ...summary, completion_tool_unconnected: true })?.completionToolUnconnected).toBe(true)
    for (const other of [{}, { completion_tool_unconnected: false }, { completion_tool_unconnected: 'true' }]) {
      expect(read({ ...summary, ...other })).not.toHaveProperty('completionToolUnconnected')
    }
  })

  it('reads a failure detail written with the engine\'s snake_case keys', () => {
    const run = toGraphWorkflowRun(
      detail({ status: 'failed', error: '会话失败' }, [
        nodeRun({
          nodeId: 'a', nodeType: 'agent', status: 'failed', error: '会话失败',
          payload: JSON.stringify({ error_detail: { kind: 'session', message: '会话失败', source_chain: [], attempt: 1, resumable: true, injects_previous_failure: false, auto_retryable: true, recorded_at: 9 } }),
        }),
      ]),
      definition,
    )
    expect(run.status).toBe('failed')
    expect(run.nodeStates.a).toMatchObject({
      status: 'failed',
      errorMessage: '会话失败',
      errorDetail: { kind: 'session', message: '会话失败', sourceChain: [], attempt: 1, resumable: true, injectsPreviousFailure: false, autoRetryable: true, recordedAt: 9 },
    })
    expect(run.nodeStates.a!.conversation).toBeUndefined()
  })

  it('reads the executor and model an Agent ran on and the settings v2 did not apply', () => {
    const run = toGraphWorkflowRun(
      detail({ status: 'succeeded', state: '{"current_nodes":[]}' }, [
        nodeRun({
          nodeId: 'a', nodeType: 'agent', output: '初稿',
          payload: JSON.stringify({
            stop_reason: 'end_turn',
            executor: { id: 'opencode:fixture', agentId: 'ora/opencode', name: 'OpenCode', pluginVersion: '1.4.0' },
            model: { id: 'fixture', name: 'Fixture', requested: 'claude-sonnet-5-5', fallback: true },
            unapplied: ['role', 'retry', 'webhook'],
          }),
        }),
      ]),
      definition,
    )
    expect(run.nodeStates.a).toMatchObject({
      executor: { id: 'opencode:fixture', agentId: 'ora/opencode', name: 'OpenCode', pluginVersion: '1.4.0' },
      model: { id: 'fixture', name: 'Fixture', requested: 'claude-sonnet-5-5', fallback: true },
      // A name this version does not know is dropped rather than shown raw.
      unapplied: ['role', 'retry'],
    })
  })

  it('records no executor or model for an attempt without them', () => {
    const run = toGraphWorkflowRun(
      detail({}, [nodeRun({ nodeId: 'a', nodeType: 'agent', payload: JSON.stringify({ executor: { name: 'OpenCode' }, model: {}, unapplied: 'role' }) })]),
      definition,
    )
    expect(run.nodeStates.a).not.toHaveProperty('executor')
    expect(run.nodeStates.a).not.toHaveProperty('model')
    expect(run.nodeStates.a).not.toHaveProperty('unapplied')
  })

  it("overlays the run's own kickoff input and Start values on the frozen graph", () => {
    const withVariables = normalizeWorkflowDefinition({
      ...definition,
      nodes: definition.nodes.map((node) =>
        node.id === 'start'
          ? {
              ...node,
              data: {
                ...node.data,
                input: '快照里的提示',
                inputVariables: [
                  { name: 'topic', valueType: 'string' as const, value: '默认主题' },
                  { name: 'count', valueType: 'number' as const, value: 1 },
                  { name: 'note', valueType: 'string' as const, value: '默认备注' },
                ],
              },
            }
          : node,
      ),
    })
    const pending = toGraphWorkflowRun(detail({ status: 'pending', input: null }, []), withVariables)
    // Nothing of the run's own yet: the snapshot stands.
    expect(pending.definitionSnapshot).toBe(withVariables)

    const run = toGraphWorkflowRun(
      {
        ...detail({ input: '本次需求' }, []),
        variables: [
          { selector: ['start', 'topic'], valueType: 'string', sourceNodeId: 'start', value: '对账' },
          { selector: ['start', 'count'], valueType: 'number', sourceNodeId: 'start' },
          { selector: ['a', 'output'], valueType: 'string', sourceNodeId: 'a', value: '初稿' },
        ],
      },
      withVariables,
    )
    const start = run.definitionSnapshot.nodes.find((node) => node.id === 'start')!
    expect(start.data.input).toBe('本次需求')
    // A declared but unassigned variable has no value; one the pool does not declare keeps the snapshot's.
    expect(start.data.inputVariables).toEqual([
      { name: 'topic', valueType: 'string', value: '对账' },
      { name: 'count', valueType: 'number', value: undefined },
      { name: 'note', valueType: 'string', value: '默认备注' },
    ])
  })

  it('gives each root node its earlier failed attempts, oldest first', () => {
    const failed = (id: string, nodeId: string, attempt: number, over: Partial<WorkflowRunDetail['failedAttempts'][number]> = {}) => ({
      nodeRunId: id, nodeId, scopeId: 'root:run', iteration: null, sessionId: `s-${id}`, attempt, kind: 'session',
      message: `第 ${attempt} 次失败`, sourceChain: ['boom'], recordedAt: attempt, startedAt: 1_000, finishedAt: 2_000, ...over,
    })
    const run = toGraphWorkflowRun(
      {
        ...detail({}, [nodeRun({ id: 'a-3', nodeId: 'a', nodeType: 'agent', status: 'running', finishedAt: null })]),
        scopes: [{ id: 'loop-round', runId: 'run', parentLoopNodeRunId: 'l', roundIndex: 1, status: 'running', state: null, createdAt: 0, updatedAt: 0 }],
        failedAttempts: [
          failed('a-1', 'a', 1),
          failed('a-2', 'a', 2, { sessionId: null, startedAt: null }),
          // Unknown occurrences remain ignored; scoped retry history is covered below.
          failed('r-1', 'a', 1, { iteration: 0 }),
          failed('l-1', 'a', 1, { scopeId: 'loop-round' }),
          failed('gone', 'not-in-graph', 1),
        ],
      },
      definition,
    )
    expect(run.nodeStates.a!.failedAttempts).toEqual([
      { nodeRunId: 'a-1', attempt: 1, kind: 'session', errorMessage: '第 1 次失败', sourceChain: ['boom'], recordedAt: 1, sessionId: 's-a-1', startedAt: new Date(1_000).toISOString(), finishedAt: new Date(2_000).toISOString() },
      // The live row links no retry chain, so a resume replaced the newest attempt.
      { nodeRunId: 'a-2', attempt: 2, kind: 'session', errorMessage: '第 2 次失败', sourceChain: ['boom'], recordedAt: 2, finishedAt: new Date(2_000).toISOString(), replacedBy: 'manual_resume' },
    ])
    expect(run.nodeStates.out!.failedAttempts).toBeUndefined()
  })

  it('attaches retry waits and real failed sessions to their exact Loop and Iteration rounds', () => {
    const graph = normalizeWorkflowDefinition({ ...definition, nodes: [
      graphNode('start', 'start'), graphNode('loop', 'loop'), graphNode('entry', 'start', undefined, 'loop'), graphNode('writer', 'agent', undefined, 'loop'),
      graphNode('iter', 'iteration'), graphNode('worker', 'agent', 'iter'),
    ], edges: [{ id: 'outer', source: 'start', target: 'loop' }, { id: 'body', source: 'entry', target: 'writer' }, { id: 'region', source: 'iter', target: 'worker' }] })
    const wait = (previous: string) => JSON.stringify({
      retry_wait: { attempt: 2, max_attempt: 3, retry: 1, max_retries: 2, delay_ms: 10000, scheduled_at: 2000, due_at: 12000 },
      auto_retry: { retry: 1, max_retries: 2 }, retry_chain: [previous],
    })
    const wire = detail({}, [
      nodeRun({ id: 'loop-parent', nodeId: 'loop', nodeType: 'loop', status: 'running' }),
      nodeRun({ id: 'writer-1-next', scopeId: 'round-1', nodeId: 'writer', nodeType: 'agent', status: 'running', startedAt: null, finishedAt: null, payload: wait('writer-1-old') }),
      nodeRun({ id: 'writer-2-next', scopeId: 'round-2', nodeId: 'writer', nodeType: 'agent', status: 'running', startedAt: null, finishedAt: null, payload: wait('writer-2-old') }),
      nodeRun({ id: 'worker-0-next', nodeId: 'worker', nodeType: 'agent', iteration: 0, status: 'running', startedAt: null, finishedAt: null, payload: wait('worker-0-old') }),
      nodeRun({ id: 'worker-1-next', nodeId: 'worker', nodeType: 'agent', iteration: 1, status: 'running', startedAt: null, finishedAt: null, payload: wait('worker-1-old') }),
    ])
    wire.scopes = [1, 2].map((roundIndex) => ({ id: `round-${roundIndex}`, runId: 'run', parentLoopNodeRunId: 'loop-parent', roundIndex, status: 'running', state: null, createdAt: 0, updatedAt: 0 }))
    wire.failedAttempts = [
      { nodeRunId: 'writer-1-old', nodeId: 'writer', scopeId: 'round-1', iteration: null, sessionId: 'writer-session-1', attempt: 1, kind: 'session', message: 'writer failure 1', sourceChain: ['writer cause 1'], recordedAt: 1, startedAt: 1, finishedAt: 2 },
      { nodeRunId: 'writer-2-old', nodeId: 'writer', scopeId: 'round-2', iteration: null, sessionId: 'writer-session-2', attempt: 1, kind: 'session', message: 'writer failure 2', sourceChain: ['writer cause 2'], recordedAt: 2, startedAt: 2, finishedAt: 3 },
      { nodeRunId: 'worker-0-old', nodeId: 'worker', scopeId: 'root:run', iteration: 0, sessionId: 'worker-session-0', attempt: 1, kind: 'session', message: 'worker failure 0', sourceChain: ['worker cause 0'], recordedAt: 3, startedAt: 3, finishedAt: 4 },
      { nodeRunId: 'worker-1-old', nodeId: 'worker', scopeId: 'root:run', iteration: 1, sessionId: 'worker-session-1', attempt: 1, kind: 'session', message: 'worker failure 1', sourceChain: ['worker cause 1'], recordedAt: 4, startedAt: 4, finishedAt: 5 },
    ]
    const run = toGraphWorkflowRun(wire, graph)
    for (const index of [0, 1]) {
      const writer = run.rounds?.[index]?.nodeStates.writer
      expect(writer?.status).toBe('retry_waiting')
      expect(writer?.failedAttempts).toEqual([expect.objectContaining({
        nodeRunId: `writer-${index + 1}-old`, sessionId: `writer-session-${index + 1}`, errorMessage: `writer failure ${index + 1}`,
        sourceChain: [`writer cause ${index + 1}`], loopRoundIndex: index + 1, replacedBy: 'automatic_retry_scheduled',
      })])
      const worker = run.roundStates?.worker?.[index]
      expect(worker?.status).toBe('retry_waiting')
      expect(worker?.failedAttempts).toEqual([expect.objectContaining({
        nodeRunId: `worker-${index}-old`, sessionId: `worker-session-${index}`, errorMessage: `worker failure ${index}`,
        sourceChain: [`worker cause ${index}`], iteration: index, replacedBy: 'automatic_retry_scheduled',
      })])
    }
    expect(run.nodeStates.writer).toEqual({ status: 'idle' })
    expect(run.nodeStates.worker?.failedAttempts?.[0]).toMatchObject({ nodeRunId: 'worker-1-old', iteration: 1 })
  })

  it('overlays kickoff values only on the root Start, preserving Loop entry values', () => {
    const root = graphNode('start', 'start')
    root.data.inputVariables = [{ name: 'topic', valueType: 'string', value: '默认主题' }]
    const entries = [graphNode('entry-parent', 'start', 'loop'), graphNode('entry-container', 'start', undefined, 'loop')]
    for (const entry of entries) {
      entry.data.input = '轮内输入'
      entry.data.inputVariables = [{ name: 'topic', valueType: 'string', value: '轮内主题' }]
    }
    const graph = normalizeWorkflowDefinition({ ...definition, nodes: [root, graphNode('loop', 'loop'), ...entries], edges: [] })
    const run = toGraphWorkflowRun({ ...detail({ input: '本次根输入' }, []), variables: [
      { selector: ['start', 'topic'], sourceNodeId: 'start', valueType: 'string', value: '本次根主题' },
      // A root catalog entry must never be used as a Loop scope's start values.
      { selector: ['entry-parent', 'topic'], sourceNodeId: 'entry-parent', valueType: 'string', value: '错误的根值' },
    ] }, graph)
    expect(run.definitionSnapshot.nodes[0]!.data).toMatchObject({ input: '本次根输入', inputVariables: [{ value: '本次根主题' }] })
    for (const id of entries.map((entry) => entry.id)) {
      expect(run.definitionSnapshot.nodes.find((node) => node.id === id)!.data).toMatchObject({ input: '轮内输入', inputVariables: [{ value: '轮内主题' }] })
    }
    expect(graph.nodes[0]!.data.inputVariables?.[0]?.value).toBe('默认主题')
  })

  it('shows an Agent without an answer yet, and malformed payloads, without inventing anything', () => {
    const run = toGraphWorkflowRun(detail({}, [nodeRun({ nodeId: 'a', nodeType: 'agent', status: 'running', finishedAt: null, payload: '{oops' })]), definition)
    expect(run.status).toBe('running')
    expect(run.nodeStates.a).toEqual({ status: 'running', nodeRunId: 'nr', startedAt: new Date(1_000).toISOString() })
  })

  it('shows the nodes behind a Condition branch not taken as inactive, and the join they share still pending', () => {
    const branching = normalizeWorkflowDefinition({
      id: 'wf', name: 'v1', description: '', updatedAt: new Date(0).toISOString(), viewport: { x: 0, y: 0, zoom: 1 },
      nodes: [
        { id: 'start', type: 'workflow', position: { x: 0, y: 0 }, data: { kind: 'start', title: '开始', description: '' } },
        { id: 'cond', type: 'workflow', position: { x: 1, y: 0 }, data: { kind: 'condition', title: '判断', description: '', cases: [{ id: 'ok', logic: 'and', conditions: [] }] } },
        { id: 'ship', type: 'workflow', position: { x: 2, y: 0 }, data: { kind: 'agent', title: '发布', description: '' } },
        { id: 'fix', type: 'workflow', position: { x: 2, y: 1 }, data: { kind: 'agent', title: '返工', description: '' } },
        { id: 'after', type: 'workflow', position: { x: 3, y: 0 }, data: { kind: 'agent', title: '发布后', description: '' } },
        { id: 'join', type: 'workflow', position: { x: 4, y: 0 }, data: { kind: 'aggregator', title: '汇合', description: '' } },
      ],
      edges: [
        { id: 'e1', source: 'start', target: 'cond' },
        { id: 'e2', source: 'cond', sourceHandle: 'ok', target: 'ship' },
        { id: 'e3', source: 'cond', sourceHandle: 'else', target: 'fix' },
        { id: 'e4', source: 'ship', target: 'after' },
        { id: 'e5', source: 'after', target: 'join' },
        { id: 'e6', source: 'fix', target: 'join' },
      ],
    })
    const run = toGraphWorkflowRun(
      detail({}, [
        nodeRun({ id: 'n1' }),
        nodeRun({ id: 'n2', nodeId: 'cond', nodeType: 'condition' }),
        nodeRun({ id: 'n3', nodeId: 'fix', nodeType: 'agent', status: 'running', finishedAt: null }),
      ], { cond: 'else' }),
      branching,
    )
    expect(Object.fromEntries(Object.entries(run.nodeStates).map(([id, state]) => [id, state.status]))).toEqual({
      start: 'succeeded', cond: 'succeeded', ship: 'inactive', after: 'inactive', fix: 'running', join: 'idle',
    })
  })
})

it('projects retry countdown, started replacements and real failed sessions from persisted chains', () => {
  const wait = { attempt: 3, max_attempt: 3, retry: 2, max_retries: 2, delay_ms: 20000, scheduled_at: 2000, due_at: 22000 }
  const current = nodeRun({ id: 'a3', nodeId: 'a', nodeType: 'agent', status: 'running', startedAt: null, finishedAt: null,
    payload: JSON.stringify({ retry_wait: wait, auto_retry: { retry: 2, max_retries: 2 }, retry_chain: ['a1', 'a2'] }) })
  const d = detail({}, [current])
  d.failedAttempts = [1, 2].map((n) => ({ nodeRunId: `a${n}`, nodeId: 'a', scopeId: 'root:run', iteration: null,
    sessionId: `real-session-${n}`, attempt: n, kind: 'session', message: `failure ${n}`, sourceChain: [`cause ${n}`],
    recordedAt: n * 1000, startedAt: n * 1000, finishedAt: n * 1000 + 500 }))
  const state = toGraphWorkflowRun(d, definition).nodeStates.a!
  expect(state.status).toBe('retry_waiting')
  expect(state.retryWait).toEqual({ attempt: 3, maxAttempt: 3, retry: 2, maxRetries: 2, delayMs: 20000, scheduledAt: 2000, dueAt: 22000 })
  expect(state.sessionId).toBeUndefined()
  expect(state.failedAttempts?.map((a) => [a.attempt, a.sessionId, a.errorMessage, a.sourceChain, a.replacedBy])).toEqual([
    [1, 'real-session-1', 'failure 1', ['cause 1'], 'automatic_retry'],
    [2, 'real-session-2', 'failure 2', ['cause 2'], 'automatic_retry_scheduled'],
  ])
  current.startedAt = 22000
  current.payload = JSON.stringify({ auto_retry: { retry: 2, max_retries: 2 }, retry_chain: ['a1', 'a2'] })
  const started = toGraphWorkflowRun(d, definition).nodeStates.a!
  expect(started.status).toBe('running')
  expect(started.retryWait).toBeUndefined()
  expect(started.failedAttempts?.every((a) => a.replacedBy === 'automatic_retry')).toBe(true)
})

describe('attempts queued for a session slot', () => {
  const queued = (over: Partial<WorkflowNodeRun> = {}) => nodeRun({ id: 'a1', nodeId: 'a', nodeType: 'agent', status: 'running', finishedAt: null,
    payload: JSON.stringify({ session_slot_wait: { since: 1500, limit: 8 } }), ...over })

  it('shows a queued attempt as waiting for a slot, not as running', () => {
    const state = toGraphWorkflowRun(detail({}, [queued()]), definition).nodeStates.a!
    expect(state.status).toBe('session_waiting')
    expect(state.sessionSlotWait).toEqual({ since: 1500, limit: 8 })
  })

  it('shows the attempt as running once it took its slot', () => {
    const state = toGraphWorkflowRun(detail({}, [queued({ payload: '{}', sessionId: 'session-a' })]), definition).nodeStates.a!
    expect(state.status).toBe('running')
    expect(state.sessionSlotWait).toBeUndefined()
  })

  it('shows an attempt with a bound session as running even if the marker was left behind', () => {
    const state = toGraphWorkflowRun(detail({}, [queued({ sessionId: 'session-a' })]), definition).nodeStates.a!
    expect(state.status).toBe('running')
    expect(state.sessionSlotWait).toBeUndefined()
    expect(state.sessionId).toBe('session-a')
  })

  it.each(['failed', 'cancelled'] as const)('never shows a marker left under a %s run as waiting', (status) => {
    const state = toGraphWorkflowRun(detail({ status, finishedAt: 3_000 }, [queued()]), definition).nodeStates.a!
    expect(state.status).not.toBe('session_waiting')
    expect(state.sessionSlotWait).toBeUndefined()
  })

  it('ignores the marker on a row that is no longer running', () => {
    const state = toGraphWorkflowRun(detail({}, [queued({ status: 'cancelled', finishedAt: 2_000 })]), definition).nodeStates.a!
    expect(state.status).toBe('cancelled')
  })
})

describe('retry waits under a run that is no longer live', () => {
  const wait = { attempt: 2, max_attempt: 3, retry: 1, max_retries: 2, delay_ms: 10000, scheduled_at: 2000, due_at: 12000 }
  const history = [{ nodeRunId: 'a1', nodeId: 'a', scopeId: 'root:run', iteration: null, sessionId: 'real-session-1', attempt: 1,
    kind: 'session', message: 'failure 1', sourceChain: [], recordedAt: 1000, startedAt: 1000, finishedAt: 1500 }]
  const waitingRow = (over: Partial<WorkflowNodeRun> = {}) => nodeRun({ id: 'a2', nodeId: 'a', nodeType: 'agent', status: 'running', startedAt: null, finishedAt: null,
    payload: JSON.stringify({ retry_wait: wait, auto_retry: { retry: 1, max_retries: 2 }, retry_chain: ['a1'] }), ...over })

  it.each(['failed', 'cancelled', 'succeeded'] as const)('never shows a wait left under a %s run as waiting or scheduled', (status) => {
    const d = detail({ status, state: '{"current_nodes":[]}', finishedAt: 3000 }, [waitingRow()])
    d.failedAttempts = history
    const state = toGraphWorkflowRun(d, definition).nodeStates.a!
    expect(state.status).toBe('cancelled')
    expect(state.retryWait).toBeUndefined()
    expect(state.failedAttempts?.map((a) => a.replacedBy)).toEqual([undefined])
  })

  it('labels a cancelled or abandoned wait as neither scheduled nor retried', () => {
    const settled = waitingRow({ status: 'cancelled', finishedAt: 3000, error: '{"reason":"retry_abandoned"}',
      payload: JSON.stringify({ auto_retry: { retry: 1, max_retries: 2 }, retry_chain: ['a1'] }) })
    // Even a live run (e.g. one still closing) cannot schedule from a settled wait.
    const d = detail({}, [settled])
    d.failedAttempts = history
    const state = toGraphWorkflowRun(d, definition).nodeStates.a!
    expect(state.status).toBe('cancelled')
    expect(state.failedAttempts?.map((a) => [a.attempt, a.sessionId, a.replacedBy])).toEqual([[1, 'real-session-1', undefined]])
  })

  it('keeps the countdown and scheduled label while the run is live', () => {
    const d = detail({}, [waitingRow()])
    d.failedAttempts = history
    const state = toGraphWorkflowRun(d, definition).nodeStates.a!
    expect(state.status).toBe('retry_waiting')
    expect(state.failedAttempts?.map((a) => a.replacedBy)).toEqual(['automatic_retry_scheduled'])
  })
})

describe('attempts a resume from failure replaced', () => {
  const failure = (n: number) => ({ nodeRunId: `a${n}`, nodeId: 'a', scopeId: 'root:run', iteration: null, sessionId: `s${n}`, attempt: n,
    kind: 'session', message: `failure ${n}`, sourceChain: [], recordedAt: n * 1000, startedAt: n * 1000, finishedAt: n * 1000 + 500 })
  const labels = (rows: WorkflowNodeRun[], attempts: number[]) => {
    const d = detail({}, rows)
    d.failedAttempts = attempts.map(failure)
    return toGraphWorkflowRun(d, definition).nodeStates.a!.failedAttempts?.map((a) => [a.attempt, a.replacedBy])
  }

  it('labels the newest attempt a fresh resumed row replaced; older retries lost their chain with it', () => {
    // Attempts 1-3 failed (two automatic retries), then the run was resumed: attempt 4 runs.
    expect(labels([nodeRun({ id: 'a4', nodeId: 'a', nodeType: 'agent', status: 'running', finishedAt: null })], [1, 2, 3]))
      .toEqual([[1, undefined], [2, undefined], [3, 'manual_resume']])
  })

  it('labels the attempt before a resumed attempt that is now retrying automatically', () => {
    const wait = { attempt: 5, max_attempt: 6, retry: 1, max_retries: 2, delay_ms: 10000, scheduled_at: 5000, due_at: 15000 }
    const waiting = nodeRun({ id: 'a5', nodeId: 'a', nodeType: 'agent', status: 'running', startedAt: null, finishedAt: null,
      payload: JSON.stringify({ retry_wait: wait, auto_retry: { retry: 1, max_retries: 2 }, retry_chain: ['a4'] }) })
    expect(labels([waiting], [1, 2, 3, 4])).toEqual([[1, undefined], [2, undefined], [3, 'manual_resume'], [4, 'automatic_retry_scheduled']])
  })

  // L1 (merge review): an Iteration member's failures are labelled against the live row of the same
  // (node, iteration), as desktop keys them. Iteration 0 was resumed (attempt 1 failed, live attempt
  // 2); iteration 1's live attempt 3 sits after a gap, so its attempt 1 stays unlabelled even though
  // iteration 0's row would label it (F1).
  it('labels each Iteration round against its own live row', () => {
    const graph = normalizeWorkflowDefinition({ ...definition, nodes: [graphNode('start', 'start'), graphNode('iter', 'iteration'), graphNode('worker', 'agent', 'iter')],
      edges: [{ id: 'e1', source: 'start', target: 'iter' }, { id: 'e2', source: 'iter', target: 'worker' }] })
    const live = (id: string, iteration: number, attempt: number) => nodeRun({ id, nodeId: 'worker', nodeType: 'agent', iteration, status: 'failed',
      payload: JSON.stringify({ error_detail: { kind: 'session', attempt } }) })
    const d = detail({ status: 'failed', state: '{"current_nodes":[]}' }, [
      nodeRun({ id: 'iter-row', nodeId: 'iter', nodeType: 'iteration', status: 'failed' }), live('w0-2', 0, 2), live('w1-3', 1, 3),
    ])
    d.failedAttempts = [0, 1].map((iteration) => ({ nodeRunId: `w${iteration}-1`, nodeId: 'worker', scopeId: 'root:run', iteration, sessionId: `s${iteration}`, attempt: 1,
      kind: 'session', message: `failure ${iteration}`, sourceChain: [], recordedAt: 1000, startedAt: 1000, finishedAt: 1500 }))
    const rounds = toGraphWorkflowRun(d, graph).roundStates?.worker
    expect(rounds?.map((state) => [state.iteration, state.failedAttempts?.map((a) => [a.attempt, a.replacedBy])]))
      .toEqual([[0, [[1, 'manual_resume']]], [1, [[1, undefined]]]])
  })

  it('labels nothing across a gap in the attempt numbers', () => {
    // Attempt 4 was a hidden row (a cancelled wait cleared by the resume) between attempts 3 and 5.
    const again = nodeRun({ id: 'a5', nodeId: 'a', nodeType: 'agent', status: 'failed', payload: JSON.stringify({ error_detail: { kind: 'session', attempt: 5 } }) })
    expect(labels([again], [3])).toEqual([[3, undefined]])
    const adjacent = nodeRun({ id: 'a4', nodeId: 'a', nodeType: 'agent', status: 'failed', payload: JSON.stringify({ error_detail: { kind: 'session', attempt: 4 } }) })
    expect(labels([adjacent], [3])).toEqual([[3, 'manual_resume']])
  })

  it('preserves the starter identity and display name on the adapted run', () => {
    const graph = normalizeWorkflowDefinition({ ...definition, nodes: [graphNode('start', 'start'), graphNode('a', 'agent')], edges: [{ id: 'e1', source: 'start', target: 'a' }] })
    const adapted = toGraphWorkflowRun({ ...detail({ startedByName: 'Alice' }, []), startedBy: 'user-alice' }, graph)
    expect(adapted.startedBy).toBe('user-alice')
    expect(adapted.startedByName).toBe('Alice')
  })

  it('projects payload.injected_failure_context when non-empty and omits otherwise', () => {
    const injected = toGraphWorkflowRun(
      detail({}, [
        nodeRun({ id: 'a2', nodeId: 'a', nodeType: 'agent', status: 'running', finishedAt: null, payload: JSON.stringify({ injected_failure_context: '## 上一次尝试（第 1 次）失败信息' }) }),
      ]),
      definition,
    )
    expect(injected.nodeStates.a?.injectedFailureContext).toBe('## 上一次尝试（第 1 次）失败信息')

    const empty = toGraphWorkflowRun(
      detail({}, [nodeRun({ id: 'a3', nodeId: 'a', nodeType: 'agent', status: 'running', finishedAt: null, payload: JSON.stringify({ injected_failure_context: '' }) })]),
      definition,
    )
    expect(empty.nodeStates.a?.injectedFailureContext).toBeUndefined()
  })
})

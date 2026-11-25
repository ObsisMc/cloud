import type { WorkflowNodeFailedAttempt, WorkflowNodeRun, WorkflowRunDetail } from '@/lib/workflows/api/generated'
import {
  computeInactiveNodes,
  isTerminalRunStatus,
  projectNodeStatus,
  projectRunStatus,
  type GraphWorkflowNodeState,
  type GraphWorkflowRun,
  type WorkflowAgentUnappliedCapability,
  type GraphWorkflowRound,
  type WorkflowDefinition,
  type WorkflowNodeAttemptFailure,
  type WorkflowNodeConversationItem,
  type WorkflowNodeErrorDetail,
  type WorkflowNodeExecutorUsed,
  type WorkflowNodeInteractiveBlocker,
  type WorkflowNodeInteractiveSession,
  type WorkflowNodeModelUsed,
  type WorkflowNodeSessionSlotWait,
} from '@/lib/workflows/runtime'

const iso = (ms: number | null): string | undefined => (ms === null ? undefined : new Date(ms).toISOString())

function parseObject(raw: string | null): Record<string, unknown> {
  if (raw === null) return {}
  try {
    const value: unknown = JSON.parse(raw)
    return value !== null && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : {}
  } catch {
    return {}
  }
}

/** Reads `payload.error_detail`, which the engine writes with desktop's snake_case keys. */
function errorDetail(payload: Record<string, unknown>): WorkflowNodeErrorDetail | undefined {
  const d = payload.error_detail
  if (d === null || typeof d !== 'object') return undefined
  const detail = d as Record<string, unknown>
  if (typeof detail.kind !== 'string') return undefined
  return {
    kind: detail.kind,
    code: typeof detail.code === 'string' ? detail.code : undefined,
    pluginId: typeof detail.plugin_id === 'string' ? detail.plugin_id : undefined,
    settingId: typeof detail.setting_id === 'string' ? detail.setting_id : undefined,
    transport: detail.transport === 'stdio' || detail.transport === 'http' ? detail.transport : undefined,
    message: typeof detail.message === 'string' ? detail.message : '',
    sourceChain: Array.isArray(detail.source_chain) ? detail.source_chain.filter((s): s is string => typeof s === 'string') : [],
    attempt: typeof detail.attempt === 'number' ? detail.attempt : 1,
    resumable: detail.resumable === true,
    injectsPreviousFailure: typeof detail.injects_previous_failure === 'boolean' ? detail.injects_previous_failure : undefined,
    autoRetryable: typeof detail.auto_retryable === 'boolean' ? detail.auto_retryable : undefined,
    recordedAt: typeof detail.recorded_at === 'number' ? detail.recorded_at : 0,
  }
}

const text = (value: unknown): string => (typeof value === 'string' ? value : '')

const record = (value: unknown): Record<string, unknown> | undefined =>
  value !== null && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : undefined

/** Reads `payload.executor`, the executor a successful Agent attempt ran on. */
function executorUsed(payload: Record<string, unknown>): WorkflowNodeExecutorUsed | undefined {
  const e = record(payload.executor)
  if (e === undefined || text(e.id) === '') return undefined
  return { id: text(e.id), agentId: text(e.agentId), name: text(e.name), pluginVersion: text(e.pluginVersion) }
}

/** Reads `payload.model`, the model a successful Agent attempt ran on and whether it was a fallback. */
function modelUsed(payload: Record<string, unknown>): WorkflowNodeModelUsed | undefined {
  const m = record(payload.model)
  if (m === undefined || (text(m.id) === '' && text(m.name) === '')) return undefined
  return { id: text(m.id), name: text(m.name), requested: text(m.requested), fallback: m.fallback === true }
}

/** Reads a completion snapshot's blockers; malformed entries are dropped. */
function blockers(value: unknown): WorkflowNodeInteractiveBlocker[] {
  if (!Array.isArray(value)) return []
  return value.flatMap((item) => {
    const b = record(item)
    if (b === undefined || text(b.kind) === '') return []
    const params = record(b.params)
    const blocker: WorkflowNodeInteractiveBlocker = { kind: text(b.kind) }
    if (text(params?.path) !== '') blocker.path = text(params?.path)
    if (text(params?.message) !== '') blocker.message = text(params?.message)
    return [blocker]
  })
}

/** Reads `payload.interactive`, what v2 records each time an interactive Agent node parks. */
function interactiveSession(payload: Record<string, unknown>): WorkflowNodeInteractiveSession | undefined {
  const i = record(payload.interactive)
  if (i === undefined) return undefined
  const completion = record(i.completion)
  const session: WorkflowNodeInteractiveSession = {
    turns: typeof i.turns === 'number' ? i.turns : 0,
    sessionEnded: i.session_ended === true,
    completable: i.completable === true,
    blockers: blockers(completion?.blockers),
  }
  if (completion !== undefined) {
    if (text(completion.parkKind) !== '') session.parkKind = text(completion.parkKind)
    if (text(completion.turnOrigin) !== '') session.turnOrigin = text(completion.turnOrigin)
    if (text(completion.stopCause) !== '') session.stopCause = text(completion.stopCause)
    const signal = text(record(completion.agentSignal)?.signalId)
    if (signal !== '') session.signalId = signal
  }
  if (typeof i.answer === 'string') session.answer = i.answer
  if (text(i.answer_source) !== '') session.answerSource = text(i.answer_source)
  if (typeof i.sent === 'number') session.sent = i.sent
  if (text(i.last_turn_error) !== '') session.lastTurnError = text(i.last_turn_error)
  if (i.completion_tool_unconnected === true) session.completionToolUnconnected = true
  return session
}

const unappliedCapabilityMap: Readonly<Record<WorkflowAgentUnappliedCapability, true>> = {
  role: true,
  skill: true,
  mcp: true,
  retry: true,
  interactive: true,
  promptInactivity: true,
  completionSignal: true,
  autoAcceptAgentCompletion: true,
}
const unappliedNames: ReadonlySet<string> = new Set(Object.keys(unappliedCapabilityMap))

/** Reads `payload.unapplied`, the configured settings v2 ran the attempt without; unknown names are dropped. */
function unapplied(payload: Record<string, unknown>): WorkflowAgentUnappliedCapability[] | undefined {
  if (!Array.isArray(payload.unapplied)) return undefined
  return payload.unapplied.filter((name): name is WorkflowAgentUnappliedCapability => typeof name === 'string' && unappliedNames.has(name))
}

/**
 * Fallback conversation when the attempt has no readable session: its rendered task (else the
 * template input) and settled answer. The session stream, when available, replaces this fallback;
 * neither path invents messages while the node runs.
 */
function conversation(runId: string, node: WorkflowNodeRun, payload: Record<string, unknown>): WorkflowNodeConversationItem[] | undefined {
  // A parked interactive node has no output until it completes; what completing would accept
  // stands in, so members who cannot read the starter's session still see the answer so far.
  const answer = node.output ?? interactiveSession(payload)?.answer ?? null
  if (node.nodeType !== 'agent' || answer === null || answer === '') return undefined
  const base = { runId, nodeId: node.nodeId, sessionId: node.sessionId ?? '', status: 'complete' as const }
  const asked = typeof payload.task === 'string' && payload.task !== '' ? payload.task : node.input
  const askedAt = iso(node.startedAt ?? node.createdAt) ?? ''
  const answeredAt = iso(node.finishedAt ?? node.updatedAt) ?? ''
  const items: WorkflowNodeConversationItem[] = []
  if (asked) items.push({ ...base, kind: 'message', role: 'user', id: `${node.id}:task`, createdAt: askedAt, updatedAt: askedAt, markdown: asked })
  items.push({ ...base, kind: 'message', role: 'assistant', id: `${node.id}:answer`, createdAt: answeredAt, updatedAt: answeredAt, markdown: answer })
  return items
}

/** A row still waiting for its retry deadline: running, never started, with the wait marker. */
const awaitingRetry = (node: WorkflowNodeRun, payload: Record<string, unknown>): boolean =>
  node.status === 'running' && node.startedAt === null && record(payload.retry_wait) !== undefined

/**
 * Reads `payload.session_slot_wait`, the marker of a running attempt still queued for a session
 * slot. An attempt with a bound session is executing whatever the payload says: a marker the
 * server failed to clear must not hide that.
 */
function sessionSlotWait(node: WorkflowNodeRun, payload: Record<string, unknown>): WorkflowNodeSessionSlotWait | undefined {
  const w = record(payload.session_slot_wait)
  if (node.status !== 'running' || node.sessionId !== null || !w) return undefined
  return typeof w.since === 'number' && typeof w.limit === 'number' ? { since: w.since, limit: w.limit } : undefined
}

/**
 * Projects one attempt. `live` is false once the run is terminal: the server settles waits when a
 * run ends, and a wait it left behind can never fire, so it shows as cancelled, not as waiting.
 */
function nodeState(runId: string, node: WorkflowNodeRun, decisions: Record<string, string>, live: boolean): GraphWorkflowNodeState {
  const payload = parseObject(node.payload)
  const state: GraphWorkflowNodeState = { status: projectNodeStatus({ status: node.status }), nodeRunId: node.id }
  const wait = record(payload.retry_wait)
  const auto = record(payload.auto_retry)
  if (!live && awaitingRetry(node, payload)) state.status = 'cancelled'
  else if (wait && awaitingRetry(node, payload) &&
      ['attempt', 'max_attempt', 'retry', 'max_retries', 'delay_ms', 'scheduled_at', 'due_at'].every((key) => typeof wait[key] === 'number' && Number.isFinite(wait[key]))) {
    state.status = 'retry_waiting'
    state.retryWait = { attempt: wait.attempt as number, maxAttempt: wait.max_attempt as number, retry: wait.retry as number,
      maxRetries: wait.max_retries as number, delayMs: wait.delay_ms as number, scheduledAt: wait.scheduled_at as number, dueAt: wait.due_at as number }
  } else {
    // A queued attempt has no session yet: it must not read as running. A terminal run never
    // starts it, so there it keeps its stored status.
    const slot = live ? sessionSlotWait(node, payload) : undefined
    if (slot) {
      state.status = 'session_waiting'
      state.sessionSlotWait = slot
    }
  }
  if (auto && typeof auto.retry === 'number' && typeof auto.max_retries === 'number') state.autoRetry = { retry: auto.retry, maxRetries: auto.max_retries }
  if (node.iteration !== null) state.iteration = node.iteration
  if (node.nodeType === 'condition' && decisions[node.nodeId] !== undefined) state.selectedBranchId = decisions[node.nodeId]
  if (node.sessionId !== null) state.sessionId = node.sessionId
  if (node.startedAt !== null) state.startedAt = iso(node.startedAt)
  if (node.finishedAt !== null) state.finishedAt = iso(node.finishedAt)
  if (node.error !== null) state.errorMessage = node.error
  const detail = errorDetail(payload)
  if (detail) state.errorDetail = detail
  if (typeof payload.stop_reason === 'string') state.stopReason = payload.stop_reason
  if (typeof payload.injected_failure_context === 'string' && payload.injected_failure_context !== '') {
    state.injectedFailureContext = payload.injected_failure_context
  }
  const executor = executorUsed(payload)
  if (executor) state.executor = executor
  const model = modelUsed(payload)
  if (model) state.model = model
  const skipped = unapplied(payload)
  if (skipped) state.unapplied = skipped
  const session = interactiveSession(payload)
  if (session) state.interactiveSession = session
  if (node.input !== null && node.input !== '') state.input = { summary: node.input.split('\n')[0]!, detail: node.input }
  if (node.output !== null && node.output !== '') state.output = { summary: node.output.split('\n')[0]!, detail: node.output }
  const items = conversation(runId, node, payload)
  if (items) state.conversation = items
  return state
}

function currentNodes(state: string | null): string[] {
  const nodes = parseObject(state).current_nodes
  return Array.isArray(nodes) ? nodes.filter((n): n is string => typeof n === 'string') : []
}

/** Projects one exact scope/round; absent members cannot inherit another occurrence's answer. */
function scopedStates(runId: string, definition: WorkflowDefinition, rows: WorkflowNodeRun[], decisions: Record<string, string>, live: boolean, iteration?: number, owner?: string): Record<string, GraphWorkflowNodeState> {
  const states: Record<string, GraphWorkflowNodeState> = {}
  for (const node of definition.nodes) states[node.id] = { status: 'idle', ...(iteration === undefined ? {} : { iteration }) }
  if (owner) states[owner] = { status: 'succeeded' }
  for (const row of rows) states[row.nodeId] = nodeState(runId, row, decisions, live)
  const statuses = Object.fromEntries(Object.entries(states).map(([id, state]) => [id, state.status]))
  for (const id of computeInactiveNodes(definition, statuses, decisions)) states[id] = { ...states[id]!, status: 'inactive' }
  if (owner) delete states[owner]
  return states
}

function subgraph(definition: WorkflowDefinition, ids: Set<string>): WorkflowDefinition {
  return { ...definition, nodes: definition.nodes.filter((node) => ids.has(node.id)), edges: definition.edges.filter((edge) => ids.has(edge.source) && ids.has(edge.target)) }
}

function stringMap(value: unknown): Record<string, string> {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return {}
  return Object.fromEntries(Object.entries(value).filter((entry): entry is [string, string] => typeof entry[1] === 'string'))
}

/**
 * The frozen graph with this run's own start values, as desktop's run adapter overlays them: the
 * Start node shows the run's kickoff input, and each input variable the run's pool declares shows
 * the value it holds there (none while unassigned). A run that has not started has no pool yet, so
 * its variables keep the snapshot's values until then.
 */
function withRunStartValues(definition: WorkflowDefinition, detail: WorkflowRunDetail): WorkflowDefinition {
  const declared = new Map(detail.variables.map((variable) => [variable.selector.join('.'), variable]))
  const input = detail.run.input
  if (declared.size === 0 && input === null) return definition
  return {
    ...definition,
    nodes: definition.nodes.map((node) => {
      if (node.data.kind !== 'start' || node.parentId || node.data.containerId) return node
      const data = { ...node.data }
      if (input !== null) data.input = input
      if (node.data.inputVariables) {
        data.inputVariables = node.data.inputVariables.map((variable) => {
          const held = declared.get(`${node.id}.${variable.name}`)
          return held === undefined ? variable : { ...variable, value: held.value }
        })
      }
      return { ...node, data }
    }),
  }
}

function attemptFailure(attempt: WorkflowNodeFailedAttempt): WorkflowNodeAttemptFailure {
  const failure: WorkflowNodeAttemptFailure = {
    nodeRunId: attempt.nodeRunId,
    attempt: attempt.attempt,
    kind: attempt.kind,
    errorMessage: attempt.message,
    sourceChain: attempt.sourceChain,
    recordedAt: attempt.recordedAt,
  }
  if (attempt.startedAt !== null) failure.startedAt = iso(attempt.startedAt)
  if (attempt.finishedAt !== null) failure.finishedAt = iso(attempt.finishedAt)
  if (attempt.sessionId !== null) failure.sessionId = attempt.sessionId
  return failure
}

/**
 * Labels the failed attempt a resume from failure replaced, as desktop's `markReplacements`
 * (packages/app-shell/src/state/data/workflow-run-retry.ts:161-214@ebd5b725): an automatic retry
 * always links the attempt it replaced through the live row's `retry_chain`, so the newest attempt
 * older than that chain (older than the live row itself when it has none) was replaced by a manual
 * resume. When the attempt numbers are known, the label requires the next attempt to follow it
 * directly, since a gap means a hidden row (a cleared wait or region row) came between. Desktop
 * also needs the live row's own number; v2 labels a fresh resumed row that has not recorded one
 * yet too, because only a resume soft-deletes a root failure without linking it.
 */
function markManualResume(attempts: WorkflowNodeAttemptFailure[], live: WorkflowNodeRun): void {
  const payload = parseObject(live.payload)
  const chain = Array.isArray(payload.retry_chain) ? payload.retry_chain : []
  const chained = attempts.filter((attempt) => chain.includes(attempt.nodeRunId)).map((attempt) => attempt.attempt)
  const own = [record(payload.error_detail)?.attempt, record(payload.retry_wait)?.attempt].find((n): n is number => typeof n === 'number')
  const next = chained.length > 0 ? Math.min(...chained) : own
  const replaced = attempts
    .filter((attempt) => attempt.replacedBy === undefined && (next === undefined || attempt.attempt < next))
    .sort((a, b) => a.attempt - b.attempt)
    .at(-1)
  if (replaced && (next === undefined || replaced.attempt === next - 1)) replaced.replacedBy = 'manual_resume'
}

/**
 * Projects a v2-server run onto the run model Theater and Overview render, as desktop's backend
 * run adapter does. The detail lists each node's latest attempt; graph nodes without one stay idle,
 * except those behind a Condition branch not taken, which never get a row and are shown inactive,
 * projected from the decisions of that exact execution. Loop scopes and Iteration array indices
 * stay separate, preserving historical sessions even when the same node runs again. Earlier
 * failures retain their sessions and reasons in that exact root, Iteration or Loop occurrence.
 * Replacement labels come from persisted retry-chain links and, for a root node or an Iteration
 * member resumed from failure, from what those links leave out.
 */
export function toGraphWorkflowRun(detail: WorkflowRunDetail, definition: WorkflowDefinition): GraphWorkflowRun {
  const { run } = detail
  const status = projectRunStatus(run.status, currentNodes(run.state))
  const live = !isTerminalRunStatus(status)
  const nodeStates: Record<string, GraphWorkflowNodeState> = {}
  for (const node of definition.nodes) nodeStates[node.id] = { status: 'idle' }
  const loops = new Set(definition.nodes.filter((node) => node.data.kind === 'loop').map((node) => node.id))
  const iterations = new Set(definition.nodes.filter((node) => node.data.kind === 'iteration').map((node) => node.id))
  const loopOwner = (node: WorkflowDefinition['nodes'][number]) => loops.has(node.data.containerId ?? node.parentId ?? '') ? (node.data.containerId ?? node.parentId) : undefined
  const rootIds = new Set(definition.nodes.filter((node) => !loopOwner(node) && !iterations.has(node.parentId ?? '')).map((node) => node.id))
  const scopeIds = new Set(detail.scopes.map((scope) => scope.id))
  const rootRows = detail.nodes.filter((node) => !scopeIds.has(node.scopeId))
  Object.assign(nodeStates, scopedStates(run.id, subgraph(definition, rootIds), rootRows.filter((node) => node.iteration === null && rootIds.has(node.nodeId)), detail.conditionDecisions, live))
  const rounds: GraphWorkflowRound[] = []
  for (const scope of detail.scopes) {
    const parent = rootRows.find((node) => node.id === scope.parentLoopNodeRunId)
    if (!parent) continue
    const ids = new Set(definition.nodes.filter((node) => loopOwner(node) === parent.nodeId).map((node) => node.id))
    const decisions = stringMap(parseObject(scope.state).conditionDecisions)
    rounds.push({ id: scope.id, parentLoopNodeRunId: parent.id, parentLoopNodeId: parent.nodeId, roundIndex: scope.roundIndex,
      status: scope.status, nodeStates: scopedStates(run.id, subgraph(definition, ids), detail.nodes.filter((node) => node.scopeId === scope.id), decisions, live),
      createdAt: iso(scope.createdAt)!, updatedAt: iso(scope.updatedAt)!,
    })
  }
  rounds.sort((a, b) => a.roundIndex - b.roundIndex || a.id.localeCompare(b.id))
  const roundStates: Record<string, GraphWorkflowNodeState[]> = {}
  for (const owner of iterations) {
    const members = new Set(definition.nodes.filter((node) => node.parentId === owner).map((node) => node.id))
    const rows = rootRows.filter((node) => members.has(node.nodeId) && node.iteration !== null)
    const indices = [...new Set(rows.map((node) => node.iteration!))].sort((a, b) => a - b)
    for (const index of indices) {
      const suffix = `#${index}`
      const decisions = Object.fromEntries(Object.entries(detail.iterationConditionDecisions).filter(([key]) => key.endsWith(suffix)).map(([key, branch]) => [key.slice(0, -suffix.length), branch]))
      const states = scopedStates(run.id, subgraph(definition, new Set([owner, ...members])), rows.filter((node) => node.iteration === index), decisions, live, index, owner)
      for (const [id, state] of Object.entries(states)) {
        (roundStates[id] ??= []).push(state)
        nodeStates[id] = state
      }
    }
  }
  for (const attempt of detail.failedAttempts) {
    const failure = attemptFailure(attempt)
    let state: GraphWorkflowNodeState | undefined
    if (attempt.iteration !== null) {
      state = roundStates[attempt.nodeId]?.find((candidate) => candidate.iteration === attempt.iteration)
      failure.iteration = attempt.iteration
    } else {
      const round = rounds.find((candidate) => candidate.id === attempt.scopeId)
      if (round) {
        state = round.nodeStates[attempt.nodeId]
        failure.loopRoundIndex = round.roundIndex
      } else if (rootIds.has(attempt.nodeId) && !scopeIds.has(attempt.scopeId)) {
        state = nodeStates[attempt.nodeId]
      }
    }
    if (state === undefined) continue
    const replacement = detail.nodes.find((row) => {
      if (row.nodeId !== attempt.nodeId || row.scopeId !== attempt.scopeId || row.iteration !== attempt.iteration) return false
      const chain = parseObject(row.payload).retry_chain
      return Array.isArray(chain) && chain.includes(attempt.nodeRunId)
    })
    if (replacement) {
      const payload = parseObject(replacement.payload)
      const chain = payload.retry_chain as unknown[]
      // Retried once the next attempt started; scheduled only while it still waits under a live
      // run. A wait cancelled or abandoned before starting replaced nothing, so it has no label.
      if (chain.at(-1) !== attempt.nodeRunId || replacement.startedAt !== null) failure.replacedBy = 'automatic_retry'
      else if (live && awaitingRetry(replacement, payload)) failure.replacedBy = 'automatic_retry_scheduled'
    }
    state.failedAttempts = [...(state.failedAttempts ?? []), failure]
  }
  // A resume from failure soft-deletes a root or Iteration failure without linking it, so label it
  // once every failure is attached to its state, each against the live row of its own
  // (node, iteration) as desktop's projectFailedAttempts keys it. Loop members restart numbering
  // in the new rounds a resume opens, so their old rounds have no live row to compare with.
  for (const [nodeId, state] of Object.entries(nodeStates)) {
    const liveRow = rootRows.find((row) => row.nodeId === nodeId && row.iteration === null)
    if (state.failedAttempts && liveRow) markManualResume(state.failedAttempts, liveRow)
  }
  for (const [nodeId, states] of Object.entries(roundStates)) {
    for (const state of states) {
      const liveRow = rootRows.find((row) => row.nodeId === nodeId && row.iteration !== null && row.iteration === state.iteration)
      if (state.failedAttempts && liveRow) markManualResume(state.failedAttempts, liveRow)
    }
  }
  const result: GraphWorkflowRun = {
    id: run.id,
    taskId: run.taskId,
    taskLink: run.taskLink,
    projectId: run.projectId,
    definitionId: run.workflowId,
    definitionSnapshot: withRunStartValues(definition, detail),
    name: run.name,
    status,
    startedByName: run.startedByName,
    nodeStates,
    rounds,
    roundStates,
    openHitls: [],
    createdAt: iso(run.createdAt)!,
    updatedAt: iso(run.updatedAt)!,
    snapshotId: run.snapshotId,
  }
  if (run.input !== null) result.kickoffInput = run.input
  if (run.output !== null) result.finalOutput = run.output
  if (run.finishedAt !== null) result.finishedAt = iso(run.finishedAt)
  if (typeof detail.startedBy === 'string') result.startedBy = detail.startedBy
  return result
}


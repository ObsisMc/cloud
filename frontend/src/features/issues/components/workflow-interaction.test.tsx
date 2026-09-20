import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '@/test/render'
import { server } from '@/test/msw-server'
import type {
  AssistSuggestion,
  CollaborationTargetSummary,
  FormDescriptor,
  IssueInteraction,
  IssueRun,
} from '@/features/issues/types'
import { WorkflowInteractionComposer } from './workflow-interaction-composer'

const target: CollaborationTargetSummary = {
  type: 'workflow',
  id: 'wf1',
  displayName: 'Security Review Workflow',
  description: 'demo',
  interactionDescriptor: { mode: 'form', requiresTask: false, formRef: 'security-review' },
}

/**
 * A descriptor with deliberately chosen keys: the renderer must draw exactly what it declares, which is
 * how we prove no workflow id is hard-coded in the frontend.
 */
const descriptor: FormDescriptor = {
  formRef: 'security-review',
  title: 'Security Review',
  description: 'fixture descriptor',
  fields: [
    { key: 'repository', label: 'Repository', type: 'text', required: true },
    {
      key: 'scope',
      label: 'Review scope',
      type: 'select',
      required: true,
      options: [
        { value: 'current-issue', label: 'Current issue changes' },
        { value: 'changed-files', label: 'Changed files' },
        { value: 'full-repo', label: 'Full repository' },
      ],
    },
    {
      key: 'severity',
      label: 'Severity',
      type: 'select',
      required: false,
      options: [
        { value: 'low', label: 'Low' },
        { value: 'high', label: 'High' },
      ],
    },
    { key: 'notes', label: 'Notes', type: 'textarea', required: false },
  ],
}

const suggestion: AssistSuggestion = {
  suggestedValues: { scope: 'changed-files', severity: 'high' },
  suggestedContextRefs: [],
  explanations: { scope: 'because scope', severity: 'because severity' },
}

const interaction: IssueInteraction = {
  id: 'ix1',
  tenantId: 't1',
  issueId: 'i1',
  commentId: 'c1',
  targetType: 'workflow',
  targetId: 'wf1',
  mode: 'form',
  task: '',
  runId: null,
  input: {},
  createdAt: '2026-01-01T00:00:00.000Z',
}

function run(): IssueRun {
  return {
    id: 'r1',
    tenantId: 't1',
    issueId: 'i1',
    executorType: 'workflow',
    executorId: 'wf1',
    input: {},
    status: 'queued',
    createdAt: '2026-01-01T00:00:00.000Z',
    updatedAt: '2026-01-01T00:00:00.000Z',
  }
}

/** Serves the descriptor plus assist/comment/interactions/confirm; returns spies on the mutations. */
function serveComposer(overrides: { descriptor?: unknown; suggestion?: AssistSuggestion } = {}) {
  const assist = vi.fn<(body: unknown) => void>()
  const comment = vi.fn<(body: unknown) => void>()
  const confirm = vi.fn<(body: unknown) => void>()
  server.use(
    http.get('/api/v1/tenants/t1/collaboration/forms/security-review', () =>
      HttpResponse.json(overrides.descriptor ?? descriptor),
    ),
    http.post(
      '/api/v1/tenants/t1/issues/i1/collaboration/assist',
      async ({ request }) => {
        assist(await request.json())
        return HttpResponse.json(overrides.suggestion ?? suggestion)
      },
    ),
    http.post('/api/v1/tenants/t1/issues/i1/comments', async ({ request }) => {
      comment(await request.json())
      return HttpResponse.json({ resource: { id: 'c1' } })
    }),
    http.get('/api/v1/tenants/t1/issues/i1/interactions', () =>
      HttpResponse.json({ items: [interaction], nextCursor: '' }),
    ),
    http.post('/api/v1/tenants/t1/issues/i1/interactions/ix1/confirm', async ({ request }) => {
      confirm(await request.json())
      return HttpResponse.json({ resource: run() })
    }),
  )
  return { assist, comment, confirm }
}

function renderComposer() {
  const onConfirmed = vi.fn<() => void>()
  const onRemove = vi.fn<() => void>()
  renderWithProviders(
    <WorkflowInteractionComposer
      slug="t1"
      issueId="i1"
      target={target}
      onConfirmed={onConfirmed}
      onRemove={onRemove}
    />,
  )
  return { onConfirmed, onRemove }
}

/** Anchors on a suggestion's explanation text to scope assertions to one suggestion row. */
function suggestionRow(explanation: string): HTMLElement {
  const node = screen.getByText(explanation).parentElement
  if (!node) throw new Error('suggestion row not found')
  return node
}

/** The Select trigger is labelled by its field label through the generated `workflow-field-<key>` id. */
function selectTrigger(label: string): HTMLElement {
  return screen.getByLabelText(new RegExp(label))
}

async function fillRequired(user: ReturnType<typeof userEvent.setup>) {
  await user.type(await screen.findByLabelText(/Repository/), 'ora-space/cloud')
  await user.click(selectTrigger('Review scope'))
  await user.click(await screen.findByRole('option', { name: 'Changed files' }))
}

describe('WorkflowInteractionComposer', () => {
  it('renders exactly the fields the descriptor declares', async () => {
    serveComposer()
    renderComposer()

    expect(await screen.findByText('Security Review')).toBeInTheDocument()
    expect(screen.getByLabelText(/Repository/)).toBeInTheDocument()
    expect(screen.getByText('Review scope')).toBeInTheDocument()
    expect(screen.getByText('Notes')).toBeInTheDocument()
  })

  it('surfaces an unsupported field type instead of guessing at it', async () => {
    serveComposer({
      descriptor: {
        formRef: 'security-review',
        fields: [{ key: 'weird', label: 'Weird', type: 'date', required: false }],
      },
    })
    renderComposer()

    expect(await screen.findByText(/不支持的字段类型/)).toBeInTheDocument()
  })

  it('gates Review on the required fields', async () => {
    serveComposer()
    const user = userEvent.setup()
    renderComposer()

    const review = await screen.findByRole('button', { name: '审阅' })
    expect(review).toBeDisabled()
    await fillRequired(user)
    expect(review).toBeEnabled()
  })

  it('shows suggestions, applies one, ignores another, and writes nothing', async () => {
    const { assist, comment, confirm } = serveComposer()
    const user = userEvent.setup()
    renderComposer()

    await user.click(await screen.findByRole('button', { name: 'AI Assist' }))
    expect(await screen.findByText('because scope')).toBeInTheDocument()
    expect(assist).toHaveBeenCalledWith({ targetId: 'wf1', values: {} })

    await user.click(within(suggestionRow('because scope')).getByRole('button', { name: '应用' }))
    expect(await screen.findByText('Changed files')).toBeInTheDocument()
    await user.click(within(suggestionRow('because severity')).getByRole('button', { name: '忽略' }))
    expect(screen.queryByText('because severity')).not.toBeInTheDocument()

    // A draft form writes nothing: no comment, no interaction, no run.
    expect(comment).not.toHaveBeenCalled()
    expect(confirm).not.toHaveBeenCalled()
  })

  it('writes nothing when the draft is abandoned', async () => {
    const { comment, confirm } = serveComposer()
    const user = userEvent.setup()
    const { onRemove } = renderComposer()

    await fillRequired(user)
    await user.click(screen.getByRole('button', { name: '取消' }))

    expect(comment).not.toHaveBeenCalled()
    expect(confirm).not.toHaveBeenCalled()
    expect(onRemove).toHaveBeenCalled()
  })

  it('requires an explicit Review, and Confirm creates the comment + interaction + run once', async () => {
    const { comment, confirm } = serveComposer()
    const user = userEvent.setup()
    const { onConfirmed } = renderComposer()

    await fillRequired(user)
    expect(confirm).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: '审阅' }))
    expect(await screen.findByText('确认后才会创建执行（IssueRun）')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '返回修改' }))
    expect(await screen.findByRole('button', { name: '审阅' })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '审阅' }))
    await user.click(await screen.findByRole('button', { name: '确认执行' }))

    await waitFor(() => expect(comment).toHaveBeenCalledTimes(1))
    expect(comment).toHaveBeenCalledWith({
      body: '@Security Review Workflow',
      targets: [{ type: 'workflow', id: 'wf1' }],
    })
    await waitFor(() =>
      expect(confirm).toHaveBeenCalledWith({
        values: { repository: 'ora-space/cloud', scope: 'changed-files' },
      }),
    )
    await waitFor(() => expect(onConfirmed).toHaveBeenCalled())
  })
})

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import type {
  AssistSuggestion,
  ContextRefRef,
  FormDescriptor,
  FormValues,
} from '@/features/issues/types'
import { AssistSuggestions } from './assist-suggestions'

const descriptor: FormDescriptor = {
  formRef: 'f',
  fields: [
    { key: 'scope', label: 'Scope', type: 'select', required: false, options: [{ value: 'full', label: 'Full' }] },
    { key: 'notes', label: 'Notes', type: 'textarea', required: false },
  ],
}

function renderSuggestions(
  suggestion: AssistSuggestion,
  overrides: Partial<Parameters<typeof AssistSuggestions>[0]> = {},
) {
  const props = {
    descriptor,
    values: {} as FormValues,
    suggestion,
    appliedRefs: [] as ContextRefRef[],
    onApply: vi.fn(),
    onApplyAll: vi.fn(),
    onIgnore: vi.fn(),
    onApplyRef: vi.fn(),
    onIgnoreRef: vi.fn(),
    onDismiss: vi.fn(),
    ...overrides,
  }
  return render(<AssistSuggestions {...props} />)
}

describe('AssistSuggestions', () => {
  it('shows the empty message when there are no field suggestions', () => {
    renderSuggestions({ suggestedValues: {}, suggestedContextRefs: [] })
    expect(screen.getByText('没有新的字段建议。')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '全部应用' })).not.toBeInTheDocument()
  })

  it('renders suggestion rows with current, suggested and reason values', () => {
    renderSuggestions({
      suggestedValues: { scope: 'full' },
      explanations: { scope: 'because scope' },
      suggestedContextRefs: [],
    })
    expect(screen.getByText('because scope')).toBeInTheDocument()
    expect(screen.getByText(/当前：/)).toHaveTextContent('（空）')
    expect(screen.getByText(/建议：/)).toHaveTextContent('full')
    expect(screen.getByRole('button', { name: '全部应用' })).toBeInTheDocument()
  })

  it('omits the reason paragraph when an explanation is absent', () => {
    renderSuggestions({ suggestedValues: { notes: 'done' }, suggestedContextRefs: [] })
    expect(screen.getByText(/建议：/)).toHaveTextContent('done')
    expect(screen.queryByText(/^because/)).not.toBeInTheDocument()
  })

  it('renders non-string suggested values through the display coercion', () => {
    renderSuggestions({
      suggestedValues: { notes: ['a', 'b'] as unknown as string },
      suggestedContextRefs: [],
    })
    // array -> joined display; number/boolean are covered by the shared display branch.
    expect(screen.getByText(/建议：/)).toHaveTextContent('a、b')
  })

  it('coerces number, boolean, blank and empty-array suggestions, skipping undeclared keys', () => {
    const blankDescriptor: FormDescriptor = {
      formRef: 'f',
      fields: [
        { key: 'num', label: 'Num', type: 'number', required: false },
        { key: 'yes', label: 'On', type: 'boolean', required: false },
        { key: 'no', label: 'Off', type: 'boolean', required: false },
        { key: 'blank', label: 'Blank', type: 'text', required: false },
        { key: 'emptyArr', label: 'Arr', type: 'multi_select', required: false },
        { key: 'skipped', label: 'Skip', type: 'text', required: false },
      ],
    }
    renderSuggestions(
      {
        suggestedValues: { num: 7, yes: true, no: false, blank: '', emptyArr: [] },
        suggestedContextRefs: [],
      },
      { descriptor: blankDescriptor },
    )

    const suggested = screen.getAllByText(/建议：/).map((node) => node.textContent ?? '')
    expect(suggested.some((text) => text.includes('7'))).toBe(true)
    expect(suggested.some((text) => text.includes('是'))).toBe(true)
    expect(suggested.some((text) => text.includes('否'))).toBe(true)
    expect(suggested.filter((text) => text.includes('（空）'))).toHaveLength(2)
    // The undeclared 'skipped' key has no row.
    expect(screen.queryByText('Skip')).not.toBeInTheDocument()
  })

  it('renders context refs and distinguishes applied from pending', async () => {
    const onApplyRef = vi.fn()
    const refs: ContextRefRef[] = [{ refType: 'project', refId: 'p1' }]
    renderSuggestions(
      { suggestedValues: {}, suggestedContextRefs: refs },
      { onApplyRef },
    )
    expect(screen.getByText('project')).toBeInTheDocument()

    await userEvent.setup().click(screen.getByRole('button', { name: '应用' }))
    expect(onApplyRef).toHaveBeenCalledWith(refs[0])

    renderSuggestions(
      { suggestedValues: {}, suggestedContextRefs: refs },
      { appliedRefs: refs },
    )
    expect(screen.getByText('已应用')).toBeInTheDocument()
  })
})
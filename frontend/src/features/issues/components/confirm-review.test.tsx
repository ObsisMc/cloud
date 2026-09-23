import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type { FormDescriptor, FormValues } from '@/features/issues/types'
import { ConfirmReview } from './confirm-review'

const EMPTY: FormDescriptor = { formRef: 'f', fields: [] }

const CATALOG: FormDescriptor = {
  formRef: 'f',
  fields: [
    {
      key: 'choice',
      label: 'Choice',
      type: 'select',
      required: false,
      options: [{ value: 'a', label: 'Option A' }],
    },
    { key: 'free', label: 'Free', type: 'text', required: false },
  ],
}

function renderReview(
  descriptor: FormDescriptor,
  values: FormValues,
  overrides: Partial<Parameters<typeof ConfirmReview>[0]> = {},
) {
  return render(
    <ConfirmReview
      targetName="Security Review"
      descriptor={descriptor}
      values={values}
      contextRefs={[]}
      appliedKeys={[]}
      onBack={() => {}}
      onConfirm={() => {}}
      pending={false}
      {...overrides}
    />,
  )
}

describe('ConfirmReview', () => {
  it('renders effective values and option labels', () => {
    renderReview(CATALOG, { free: 'hello', choice: 'a' })

    expect(screen.getByText('Security Review')).toBeInTheDocument()
    expect(screen.getByText('hello')).toBeInTheDocument()
    expect(screen.getByText('Option A')).toBeInTheDocument()
    // Empty context refs render exactly one em dash.
    expect(screen.getAllByText('—')).toHaveLength(1)
  })

  it('coerces empty, numeric, boolean and array values for display', () => {
    const descriptor: FormDescriptor = {
      formRef: 'f',
      fields: [
        { key: 'str', label: 'S', type: 'text', required: false },
        { key: 'num', label: 'N', type: 'number', required: false },
        { key: 'yes', label: 'Y', type: 'boolean', required: false },
        { key: 'no', label: 'F', type: 'boolean', required: false },
        { key: 'arr', label: 'A', type: 'multi_select', required: false },
        { key: 'emptyStr', label: 'ES', type: 'text', required: false },
        { key: 'emptyArr', label: 'EA', type: 'multi_select', required: false },
      ],
    }
    renderReview(
      descriptor,
      { str: 'x', num: 5, yes: true, no: false, arr: ['a', 'b'], emptyStr: '', emptyArr: [] },
      { contextRefs: [{ refType: 'project', refId: '12345678-90ab' }] },
    )

    expect(screen.getByText('x')).toBeInTheDocument()
    expect(screen.getByText('5')).toBeInTheDocument()
    expect(screen.getByText('是')).toBeInTheDocument()
    expect(screen.getByText('否')).toBeInTheDocument()
    expect(screen.getByText('a、b')).toBeInTheDocument()
    expect(screen.getByText('12345678')).toBeInTheDocument()
    // Empty string + empty array are the only two em dashes (context refs are non-empty).
    expect(screen.getAllByText('—')).toHaveLength(2)
  })

  it('marks applied AI suggestions', () => {
    renderReview(CATALOG, { free: 'hello' }, { appliedKeys: ['free'] })
    expect(screen.getByText('（来自 AI 建议）')).toBeInTheDocument()
  })

  it('switches to the pending state', () => {
    const onConfirm = vi.fn<() => void>()
    renderReview(EMPTY, {}, { pending: true, onConfirm })

    expect(screen.getByText('确认中…')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '返回修改' })).toBeDisabled()
  })
})

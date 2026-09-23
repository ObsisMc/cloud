import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type { FormField } from '@/features/issues/types'
import { FormFieldRenderer } from './form-field-renderer'

function field(overrides: Partial<FormField> = {}): FormField {
  return { key: 'k', label: 'Field', type: 'text', required: false, ...overrides }
}

describe('FormFieldRenderer', () => {
  it('renders a text input as the default control', () => {
    render(<FormFieldRenderer field={field({ type: 'text' })} value="hello" onChange={() => {}} />)
    expect(screen.getByRole('textbox')).toHaveValue('hello')
  })

  it('renders a textarea', () => {
    render(<FormFieldRenderer field={field({ type: 'textarea' })} value="hi" onChange={() => {}} />)
    expect(screen.getByRole('textbox').tagName).toBe('TEXTAREA')
  })

  it('renders a number input and coerces its change to a number or null', () => {
    const onChange = vi.fn()
    render(<FormFieldRenderer field={field({ type: 'number' })} value={7} onChange={onChange} />)
    const input = screen.getByRole('spinbutton')

    fireEvent.change(input, { target: { value: '42' } })
    expect(onChange).toHaveBeenCalledWith(42)

    fireEvent.change(input, { target: { value: '' } })
    expect(onChange).toHaveBeenCalledWith(null)
  })

  it('renders a boolean checkbox', () => {
    render(
      <FormFieldRenderer field={field({ type: 'boolean' })} value={true} onChange={() => {}} />,
    )
    expect(screen.getByRole('checkbox')).toBeChecked()
  })

  it('renders an unchecked boolean checkbox for a falsy value', () => {
    render(
      <FormFieldRenderer field={field({ type: 'boolean' })} value={false} onChange={() => {}} />,
    )
    expect(screen.getByRole('checkbox')).not.toBeChecked()
  })

  it('renders an unknown type as a plain text input rather than guessing', () => {
    render(
      <FormFieldRenderer
        field={field({ type: 'unknown' as FormField['type'] })}
        value={{}}
        onChange={() => {}}
      />,
    )
    // Non-string, non-number values are coerced to an empty string.
    expect(screen.getByRole('textbox')).toHaveValue('')
  })

  it('renders a select with its options', () => {
    render(
      <FormFieldRenderer
        field={field({
          type: 'select',
          options: [
            { value: 'a', label: 'Option A' },
            { value: 'b', label: 'Option B' },
          ],
        })}
        value="a"
        onChange={() => {}}
      />,
    )
    expect(screen.getByRole('combobox')).toBeInTheDocument()
  })

  it('renders multi_select checkboxes and toggles membership', () => {
    const onChange = vi.fn()
    render(
      <FormFieldRenderer
        field={field({
          type: 'multi_select',
          options: [
            { value: 'a', label: 'A' },
            { value: 'b', label: 'B' },
          ],
        })}
        value={['a']}
        onChange={onChange}
      />,
    )
    const boxes = screen.getAllByRole('checkbox')
    expect(boxes[0]).toBeChecked()
    expect(boxes[1]).not.toBeChecked()

    fireEvent.click(boxes[1])
    expect(onChange).toHaveBeenCalledWith(['a', 'b'])
  })

  it('treats a non-array multi_select value as empty', () => {
    render(
      <FormFieldRenderer
        field={field({
          type: 'multi_select',
          options: [{ value: 'a', label: 'A' }],
        })}
        value="not-an-array"
        onChange={() => {}}
      />,
    )
    expect(screen.getByRole('checkbox')).not.toBeChecked()
  })

  it('ignores non-string entries in a multi_select value', () => {
    render(
      <FormFieldRenderer
        field={field({
          type: 'multi_select',
          options: [
            { value: 'a', label: 'A' },
            { value: 'b', label: 'B' },
          ],
        })}
        value={['a', 42, null]}
        onChange={() => {}}
      />,
    )
    const boxes = screen.getAllByRole('checkbox')
    expect(boxes[0]).toBeChecked()
    expect(boxes[1]).not.toBeChecked()
  })

  it('removes an option when its checkbox is unchecked', () => {
    const onChange = vi.fn()
    render(
      <FormFieldRenderer
        field={field({
          type: 'multi_select',
          options: [
            { value: 'a', label: 'A' },
            { value: 'b', label: 'B' },
          ],
        })}
        value={['a', 'b']}
        onChange={onChange}
      />,
    )
    fireEvent.click(screen.getAllByRole('checkbox')[0])
    expect(onChange).toHaveBeenCalledWith(['b'])
  })

  it('marks required fields and renders the description', () => {
    render(
      <FormFieldRenderer
        field={field({ required: true, description: 'Helpful hint' })}
        value=""
        onChange={() => {}}
      />,
    )
    expect(screen.getByText('Field')).toBeInTheDocument()
    expect(screen.getByText('*')).toBeInTheDocument()
    expect(screen.getByText('Helpful hint')).toBeInTheDocument()
  })
})

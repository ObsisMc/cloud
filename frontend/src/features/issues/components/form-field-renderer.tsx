import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import type { FormField, FormOption } from '@/features/issues/types'

/** Text-like controls read their value as a string; anything else renders empty. */
function asText(value: unknown): string {
  if (typeof value === 'string') return value
  if (typeof value === 'number') return String(value)
  return ''
}

function asList(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === 'string')
    : []
}

function optionLabel(options: FormOption[], value: string): string {
  return options.find((option) => option.value === value)?.label ?? value
}

function toggle(list: string[], value: string, on: boolean): string[] {
  if (on) return list.includes(value) ? list : [...list, value]
  return list.filter((item) => item !== value)
}

/** Renders the control for one declared field type. The type is never guessed at. */
function control(
  field: FormField,
  value: unknown,
  onChange: (next: unknown) => void,
  options: FormOption[],
) {
  const id = `workflow-field-${field.key}`
  switch (field.type) {
    case 'textarea':
      return (
        <Textarea
          id={id}
          rows={3}
          value={asText(value)}
          placeholder={field.placeholder ?? ''}
          onChange={(event) => onChange(event.target.value)}
        />
      )
    case 'number':
      return (
        <Input
          id={id}
          type="number"
          value={asText(value)}
          placeholder={field.placeholder ?? ''}
          onChange={(event) =>
            onChange(event.target.value === '' ? null : Number(event.target.value))
          }
        />
      )
    case 'boolean':
      return (
        <Checkbox
          id={id}
          checked={value === true}
          onCheckedChange={(checked) => onChange(checked)}
        />
      )
    case 'select':
      return (
        <Select
          value={typeof value === 'string' ? value : ''}
          onValueChange={(next) => onChange(next ?? '')}
        >
          <SelectTrigger id={id} className="w-full">
            <SelectValue>
              {(current: unknown) =>
                optionLabel(options, typeof current === 'string' ? current : '')
              }
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            {options.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )
    case 'multi_select':
      return (
        <div className="space-y-1.5">
          {options.map((option) => (
            <Label key={option.value} className="font-normal">
              <Checkbox
                checked={asList(value).includes(option.value)}
                onCheckedChange={(checked) =>
                  onChange(toggle(asList(value), option.value, checked))
                }
              />
              {option.label}
            </Label>
          ))}
        </div>
      )
    default:
      return (
        <Input
          id={id}
          value={asText(value)}
          placeholder={field.placeholder ?? ''}
          onChange={(event) => onChange(event.target.value)}
        />
      )
  }
}

/** One rendered form control, with its label, required marker and help text. */
export function FormFieldRenderer({
  field,
  value,
  onChange,
}: {
  field: FormField
  value: unknown
  onChange: (next: unknown) => void
}) {
  return (
    <div className="space-y-1.5">
      <Label htmlFor={`workflow-field-${field.key}`}>
        {field.label}
        {field.required ? <span className="text-destructive">*</span> : null}
      </Label>
      {control(field, value, onChange, field.options ?? [])}
      {field.description ? (
        <p className="text-xs text-muted-foreground">{field.description}</p>
      ) : null}
    </div>
  )
}

import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

/**
 * Labeled input row for dialog forms. `hint` renders as destructive microcopy
 * (validation hints and fault codes share the slot).
 */
export function DialogFormField({
  id,
  label,
  value,
  onChange,
  placeholder,
  hint,
  required = false,
}: {
  id: string
  label: string
  value: string
  onChange: (value: string) => void
  placeholder?: string
  hint?: string | undefined
  required?: boolean
}) {
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        required={required}
      />
      {hint && <p className="text-xs text-destructive">{hint}</p>}
    </div>
  )
}

import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { cn } from '@/lib/utils'
import { Button, buttonVariants } from './button'

describe('Button', () => {
  it('renders the default variant and size', () => {
    render(<Button>Go</Button>)

    const button = screen.getByRole('button', { name: 'Go' })
    expect(button.getAttribute('data-slot')).toBe('button')
    expect(button.className).toBe(cn(buttonVariants({ variant: 'default', size: 'default' })))
  })

  it('applies variant, size and extra classes', () => {
    render(
      <Button variant="outline" size="sm" className="extra">
        Go
      </Button>,
    )

    const button = screen.getByRole('button', { name: 'Go' })
    expect(button.className).toBe(
      cn(buttonVariants({ variant: 'outline', size: 'sm', className: 'extra' })),
    )
  })

  it('forwards native props', () => {
    render(<Button disabled>Go</Button>)

    expect(screen.getByRole('button', { name: 'Go' }).hasAttribute('disabled')).toBe(true)
  })
})

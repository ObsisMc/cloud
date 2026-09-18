import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { SidebarProvider } from '@/components/ui/sidebar'
import { PageHeader } from './page-header'

function renderHeader(props: Parameters<typeof PageHeader>[0]) {
  return render(
    <MemoryRouter>
      <SidebarProvider>
        <PageHeader {...props} />
      </SidebarProvider>
    </MemoryRouter>,
  )
}

describe('PageHeader', () => {
  it('renders just the title when there is no breadcrumb', () => {
    renderHeader({ title: 'Issues' })
    expect(screen.getByRole('heading', { name: 'Issues' })).toBeInTheDocument()
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('renders a clickable parent link before the title when given a breadcrumb', () => {
    renderHeader({ title: 'ORA-1', breadcrumb: { label: 'Issues', to: '/ora-demo/issues' } })
    expect(screen.getByRole('heading', { name: 'ORA-1' })).toBeInTheDocument()
    const link = screen.getByRole('link', { name: 'Issues' })
    expect(link).toHaveAttribute('href', '/ora-demo/issues')
  })
})

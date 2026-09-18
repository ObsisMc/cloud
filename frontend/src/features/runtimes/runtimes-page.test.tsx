import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '@/test/render'
import { db } from '@/mocks/data/store'
import { RuntimesPage } from './runtimes-page'

describe('RuntimesPage', () => {
  it('renders every seeded runtime', async () => {
    renderWithProviders(<RuntimesPage slug={db.workspace.slug} />)
    await Promise.all(
      db.runtimes.map(async (runtime) => {
        expect(await screen.findByText(runtime.name)).toBeInTheDocument()
      }),
    )
  })

  it('stops a running runtime', async () => {
    const runtime = db.runtimes.find((r) => r.status === 'running')
    if (!runtime) throw new Error('runtime seed data must contain a running runtime')
    const user = userEvent.setup()
    renderWithProviders(<RuntimesPage slug={db.workspace.slug} />)
    const runtimeName = await screen.findByText(runtime.name)
    const rowContainer = runtimeName.closest('div')
    const row = rowContainer?.parentElement
    if (!row) throw new Error('runtime row must have a parent container')

    await user.click(within(row).getByRole('button', { name: '停止' }))

    await waitFor(() => {
      expect(db.runtimes.find((r) => r.id === runtime.id)?.status).toBe('stopped')
    })
  })
})

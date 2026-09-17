import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '@/test/render'
import { db } from '@/mocks/data/store'
import { RuntimesPage } from './runtimes-page'

describe('RuntimesPage', () => {
  it('renders every seeded runtime', async () => {
    renderWithProviders(<RuntimesPage slug={db.workspace.slug} />)
    for (const runtime of db.runtimes) {
      expect(await screen.findByText(runtime.name)).toBeInTheDocument()
    }
  })

  it('stops a running runtime', async () => {
    const runtime = db.runtimes.find((r) => r.status === 'running')!
    const user = userEvent.setup()
    renderWithProviders(<RuntimesPage slug={db.workspace.slug} />)
    const row = (await screen.findByText(runtime.name)).closest('div')!.parentElement!

    await user.click(within(row).getByRole('button', { name: /stop/i }))

    await waitFor(() => {
      expect(db.runtimes.find((r) => r.id === runtime.id)?.status).toBe('stopped')
    })
  })
})

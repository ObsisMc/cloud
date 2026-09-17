import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '@/test/render'
import { db } from '@/mocks/data/store'
import { AutopilotsPage } from './autopilots-page'

describe('AutopilotsPage', () => {
  it('renders every seeded autopilot', async () => {
    renderWithProviders(<AutopilotsPage slug={db.workspace.slug} />)
    for (const autopilot of db.autopilots) {
      expect(await screen.findByText(autopilot.name)).toBeInTheDocument()
    }
  })

  it('toggles an active autopilot to paused', async () => {
    const index = db.autopilots.findIndex((a) => a.status === 'active')
    const autopilot = db.autopilots[index]
    const user = userEvent.setup()
    renderWithProviders(<AutopilotsPage slug={db.workspace.slug} />)
    await screen.findByText(autopilot.name)

    const toggle = screen.getAllByRole('switch')[index]
    expect(toggle).toHaveAttribute('aria-checked', 'true')

    await user.click(toggle)

    await waitFor(() => {
      expect(db.autopilots.find((a) => a.id === autopilot.id)?.status).toBe('paused')
    })
  })
})

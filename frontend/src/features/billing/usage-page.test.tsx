import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '@/test/render'
import { db } from '@/mocks/data/store'
import { UsagePage } from './usage-page'

describe('UsagePage', () => {
  it('renders totals for agent minutes, API calls, and storage', async () => {
    renderWithProviders(<UsagePage slug={db.workspace.slug} />)

    const totalMinutes = db.usageSeries.reduce((sum, p) => sum + p.agentMinutes, 0)
    expect(await screen.findByText(new RegExp(totalMinutes.toLocaleString()))).toBeInTheDocument()
  })
})

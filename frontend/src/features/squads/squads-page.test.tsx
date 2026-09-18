import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '@/test/render'
import { db } from '@/mocks/data/store'
import { SquadsPage } from './squads-page'

describe('SquadsPage', () => {
  it('renders every seeded squad', async () => {
    renderWithProviders(<SquadsPage slug={db.workspace.slug} />)
    await Promise.all(
      db.squads.map(async (squad) => {
        expect(await screen.findByText(squad.name)).toBeInTheDocument()
      }),
    )
  })
})

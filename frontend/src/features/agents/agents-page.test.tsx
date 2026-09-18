import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '@/test/render'
import { db } from '@/mocks/data/store'
import { AgentsPage } from './agents-page'

describe('AgentsPage', () => {
  it('renders every seeded agent with its role', async () => {
    renderWithProviders(<AgentsPage slug={db.workspace.slug} />)
    await Promise.all(
      db.agents.map(async (agent) => {
        expect(await screen.findByText(agent.name)).toBeInTheDocument()
      }),
    )
  })
})

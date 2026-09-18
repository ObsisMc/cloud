import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '@/test/render'
import { db } from '@/mocks/data/store'
import { AgentsPage } from './agents-page'

describe('AgentsPage', () => {
  it('renders every seeded agent with its role', async () => {
    renderWithProviders(<AgentsPage slug={db.workspace.slug} />)
    for (const agent of db.agents) {
      expect(await screen.findByText(agent.name)).toBeInTheDocument()
    }
  })
})

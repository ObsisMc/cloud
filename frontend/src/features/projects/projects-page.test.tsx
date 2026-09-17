import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '@/test/render'
import { db } from '@/mocks/data/store'
import { ProjectsPage } from './projects-page'

describe('ProjectsPage', () => {
  it('renders every seeded project as a card', async () => {
    renderWithProviders(<ProjectsPage slug={db.workspace.slug} />)

    for (const project of db.projects) {
      expect(await screen.findByText(project.title)).toBeInTheDocument()
    }
  })
})

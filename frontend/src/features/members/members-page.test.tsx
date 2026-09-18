import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '@/test/render'
import { db } from '@/mocks/data/store'
import { MembersPage } from './members-page'

describe('MembersPage', () => {
  it('renders every seeded member with their role', async () => {
    renderWithProviders(<MembersPage slug={db.workspace.slug} />)
    await Promise.all(
      db.users.map(async (user) => {
        expect(await screen.findByText(user.name)).toBeInTheDocument()
      }),
    )
  })
})

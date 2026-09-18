import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '@/test/render'
import { db } from '@/mocks/data/store'
import { SkillsPage } from './skills-page'

describe('SkillsPage', () => {
  it('renders every seeded skill', async () => {
    renderWithProviders(<SkillsPage slug={db.workspace.slug} />)
    await Promise.all(
      db.skills.map(async (skill) => {
        expect(await screen.findByText(skill.name)).toBeInTheDocument()
      }),
    )
  })

  it('flips a skill from disabled to enabled', async () => {
    const index = db.skills.findIndex((s) => !s.enabled)
    const skill = db.skills[index]
    if (!skill) throw new Error('skill seed data must contain a disabled skill')
    const user = userEvent.setup()
    renderWithProviders(<SkillsPage slug={db.workspace.slug} />)
    await screen.findByText(skill.name)

    const toggle = screen.getAllByRole('switch')[index]
    if (!toggle) throw new Error('each skill must render a toggle')
    expect(toggle).toHaveAttribute('aria-checked', 'false')

    await user.click(toggle)

    await waitFor(() => {
      expect(db.skills.find((s) => s.id === skill.id)?.enabled).toBe(true)
    })
  })
})

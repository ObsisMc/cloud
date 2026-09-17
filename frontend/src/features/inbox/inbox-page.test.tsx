import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '@/test/render'
import { db } from '@/mocks/data/store'
import { InboxPage } from './inbox-page'

describe('InboxPage', () => {
  it('renders every seeded inbox item', async () => {
    renderWithProviders(<InboxPage slug={db.workspace.slug} />)

    const titles = [...new Set(db.inboxItems.map((i) => i.title))]
    for (const title of titles) {
      expect((await screen.findAllByText(title)).length).toBeGreaterThan(0)
    }
    const byTitle = new Map<string, number>()
    for (const item of db.inboxItems) byTitle.set(item.title, (byTitle.get(item.title) ?? 0) + 1)
    for (const [title, count] of byTitle) {
      expect(screen.getAllByText(title)).toHaveLength(count)
    }
  })

  it('marks every item read via "Mark all read"', async () => {
    const user = userEvent.setup()
    renderWithProviders(<InboxPage slug={db.workspace.slug} />)
    await screen.findByText(db.inboxItems[0].title)

    await user.click(screen.getByRole('button', { name: /mark all read/i }))

    await waitFor(() => {
      expect(db.inboxItems.every((i) => i.read)).toBe(true)
    })
  })
})

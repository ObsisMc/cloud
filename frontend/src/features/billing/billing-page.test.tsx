import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '@/test/render'
import { db } from '@/mocks/data/store'
import { BillingPage } from './billing-page'

describe('BillingPage', () => {
  it('renders the plan summary and every seeded invoice', async () => {
    renderWithProviders(<BillingPage slug={db.workspace.slug} />)

    expect(await screen.findByText(db.workspace.plan, { exact: false })).toBeInTheDocument()
    for (const invoice of db.invoices) {
      expect(await screen.findByText(`$${invoice.amount}`)).toBeInTheDocument()
    }
  })
})

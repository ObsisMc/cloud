import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { renderWithProviders } from '@/test/render'
import { db } from '@/mocks/data/store'
import { BillingPage } from './billing-page'

const PLAN_LABELS: Record<string, string> = { free: '免费版', pro: '专业版', business: '企业版' }

describe('BillingPage', () => {
  it('renders the plan summary and every seeded invoice', async () => {
    renderWithProviders(<BillingPage slug={db.workspace.slug} />)

    expect(await screen.findByText(PLAN_LABELS[db.workspace.plan])).toBeInTheDocument()
    for (const invoice of db.invoices) {
      expect(await screen.findByText(`¥${invoice.amount}`)).toBeInTheDocument()
    }
  })
})

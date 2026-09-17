import { useQuery } from '@tanstack/react-query'
import { mockApi } from '@/lib/mock-api-client'
import type { Invoice, UsagePoint } from '@/mocks/data/types'

interface BillingSummary {
  plan: 'free' | 'pro' | 'business'
  seats: number
  renewalDate: string
  amount: number
}

export function useBillingSummary(slug: string) {
  return useQuery({
    queryKey: ['billing', slug],
    queryFn: async () => {
      const { data } = await mockApi.get<BillingSummary>(`/workspaces/${slug}/billing`)
      return data
    },
  })
}

export function useInvoices(slug: string) {
  return useQuery({
    queryKey: ['invoices', slug],
    queryFn: async () => {
      const { data } = await mockApi.get<Invoice[]>(`/workspaces/${slug}/invoices`)
      return data
    },
  })
}

export function useUsageSeries(slug: string) {
  return useQuery({
    queryKey: ['usage', slug],
    queryFn: async () => {
      const { data } = await mockApi.get<UsagePoint[]>(`/workspaces/${slug}/usage`)
      return data
    },
  })
}

import { afterEach, expect, it } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { http } from '@/lib/api-client'
import { fakeBackend } from '@/test/backend'
import type { SpacePlugin } from '@/api/generated.schemas'
import { marketplaceEntry, marketplaceSelection } from '../test-utils'
import { useSkillDeployments } from './skill-deployments'

const original = http.defaults.adapter
afterEach(() => {
  http.defaults.adapter = original
})

const wrap = ({ children }: { children: ReactNode }) => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

const skillSelection = (): SpacePlugin =>
  marketplaceSelection(marketplaceEntry('skills', { kind: 'skill' }))

it('aggregates project receipts into a single delivery state', async () => {
  const selection = skillSelection()
  fakeBackend((config) =>
    (config.url ?? '').includes('/plugins/results/')
      ? {
          status: 200,
          data: {
            items: [
              { projectId: 'a', name: 'A', state: 'installed', installedVersion: '1.0.0', failure: '' },
              { projectId: 'b', name: 'B', state: 'applying', installedVersion: '', failure: '' },
            ],
          },
        }
      : { status: 404 },
  )
  const { result } = renderHook(
    () => useSkillDeployments('tenant', 'space', [selection]),
    { wrapper: wrap },
  )
  await waitFor(() => {
    const summary = result.current.summaryOf(selection)
    expect(summary?.aggregate).toBe('applying')
    expect(summary?.states).toHaveLength(2)
  })
})

it('marks a failed receipt and keeps the reason', async () => {
  const selection = skillSelection()
  fakeBackend((config) =>
    (config.url ?? '').includes('/plugins/results/')
      ? {
          status: 200,
          data: {
            items: [
              {
                projectId: 'a',
                name: 'A',
                state: 'failed',
                installedVersion: '',
                failure: '技能包下载失败。',
              },
            ],
          },
        }
      : { status: 404 },
  )
  const { result } = renderHook(
    () => useSkillDeployments('tenant', 'space', [selection]),
    { wrapper: wrap },
  )
  await waitFor(() => {
    expect(result.current.summaryOf(selection)?.aggregate).toBe('failed')
    expect(result.current.summaryOf(selection)?.failure).toBe('技能包下载失败。')
  })
})

it('reports none when a selected Skill has no receipts', async () => {
  const selection = skillSelection()
  fakeBackend((config) =>
    (config.url ?? '').includes('/plugins/results/')
      ? { status: 200, data: { items: [] } }
      : { status: 404 },
  )
  const { result } = renderHook(
    () => useSkillDeployments('tenant', 'space', [selection]),
    { wrapper: wrap },
  )
  await waitFor(() => {
    expect(result.current.summaryOf(selection)?.aggregate).toBe('none')
  })
})

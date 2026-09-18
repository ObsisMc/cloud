import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { delay, http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'
import { db } from '@/mocks/data/store'
import type { Issue } from '@/mocks/data/types'
import { server } from '@/test/msw-server'
import { useUpdateIssue } from './api'

function wrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

describe('useUpdateIssue', () => {
  it('updates every cached issues list optimistically, ahead of the request settling', async () => {
    const issue = { ...db.issues[0], status: 'backlog' } as Issue
    // Hold the response back so the assertion below can only pass if the
    // cache was written before the request settled, not after.
    server.use(
      http.patch(`/mock-api/workspaces/${db.workspace.slug}/issues/${issue.id}`, async () => {
        await delay(50)
        return HttpResponse.json({ ...issue, status: 'in_progress' })
      }),
    )
    const queryClient = new QueryClient()
    // Seed two differently-filtered list caches, mirroring how the board and
    // "My Issues" can both hold the same issue at once.
    queryClient.setQueryData(['issues', db.workspace.slug, {}], [issue])
    queryClient.setQueryData(['issues', db.workspace.slug, { assigneeId: issue.assigneeId }], [issue])

    const { result } = renderHook(() => useUpdateIssue(db.workspace.slug), { wrapper: wrapper(queryClient) })
    result.current.mutate({ id: issue.id, patch: { status: 'in_progress' } })

    await waitFor(() => {
      const list = queryClient.getQueryData<Issue[]>(['issues', db.workspace.slug, {}])
      expect(list?.[0].status).toBe('in_progress')
    })
    expect(result.current.isSuccess).toBe(false)
    const filtered = queryClient.getQueryData<Issue[]>(['issues', db.workspace.slug, { assigneeId: issue.assigneeId }])
    expect(filtered?.[0].status).toBe('in_progress')

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
  })

  it('rolls back the cached lists if the request fails', async () => {
    const issue = { ...db.issues[0], status: 'backlog' } as Issue
    server.use(
      http.patch(`/mock-api/workspaces/${db.workspace.slug}/issues/${issue.id}`, () =>
        HttpResponse.json({ message: 'nope' }, { status: 500 }),
      ),
    )
    const queryClient = new QueryClient()
    queryClient.setQueryData(['issues', db.workspace.slug, {}], [issue])

    const { result } = renderHook(() => useUpdateIssue(db.workspace.slug), { wrapper: wrapper(queryClient) })
    result.current.mutate({ id: issue.id, patch: { status: 'done' } })

    await waitFor(() => expect(result.current.isError).toBe(true))
    const list = queryClient.getQueryData<Issue[]>(['issues', db.workspace.slug, {}])
    expect(list?.[0].status).toBe('backlog')
  })
})

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'
import { server } from '@/test/msw-server'
import type { Issue } from './types'
import { useCreateIssue, useIssues, useMoveIssue, useUpdateIssue } from './api'

function wrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

const issueFixture: Issue = {
  id: 'i1',
  tenantId: 't1',
  creatorUserId: 'u1',
  assigneeType: 'user',
  assigneeId: null,
  assigneeUserId: null,
  parentIssueId: null,
  projectRef: null,
  title: 'Fix the login',
  description: '',
  status: 'backlog',
  priority: 'none',
  position: 0,
  number: 1,
  properties: {},
  version: 3,
  createdAt: '',
  updatedAt: '',
  labels: [],
}

describe('useIssues', () => {
  it('unwraps the items envelope into the issue list', async () => {
    server.use(
      http.get('/api/v1/tenants/t1/issues', () =>
        HttpResponse.json({ items: [issueFixture], nextCursor: '' }),
      ),
    )
    const { result } = renderHook(() => useIssues('t1'), { wrapper: wrapper(new QueryClient()) })

    await waitFor(() => expect(result.current.data).toEqual([issueFixture]))
  })
})

describe('useUpdateIssue', () => {
  it('sends the version alongside the patch and caches the returned issue', async () => {
    let body: unknown
    const updated = { ...issueFixture, status: 'done', version: 4 }
    server.use(
      http.put('/api/v1/tenants/t1/issues/i1', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json(updated)
      }),
    )
    const queryClient = new QueryClient()
    const { result } = renderHook(() => useUpdateIssue('t1'), {
      wrapper: wrapper(queryClient),
    })

    result.current.mutate({ id: 'i1', version: 3, patch: { status: 'done' } })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(body).toEqual({ status: 'done', version: 3 })
    expect(queryClient.getQueryData(['issue', 't1', 'i1'])).toEqual(updated)
  })
})

describe('useCreateIssue', () => {
  it('sends an idempotency key and unwraps the created resource', async () => {
    let idempotencyKey: string | null = null
    server.use(
      http.post('/api/v1/tenants/t1/issues', async ({ request }) => {
        idempotencyKey = request.headers.get('Idempotency-Key')
        return HttpResponse.json({ resource: issueFixture })
      }),
    )
    const { result } = renderHook(() => useCreateIssue('t1'), {
      wrapper: wrapper(new QueryClient()),
    })

    result.current.mutate({ title: 'Fix the login' })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(idempotencyKey).toBeTruthy()
    expect(result.current.data).toEqual(issueFixture)
  })

  it('sends the parentIssueId when creating a sub-issue', async () => {
    let body: unknown
    server.use(
      http.post('/api/v1/tenants/t1/issues', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json({ resource: issueFixture })
      }),
    )
    const { result } = renderHook(() => useCreateIssue('t1'), {
      wrapper: wrapper(new QueryClient()),
    })

    result.current.mutate({ title: 'Child task', parentIssueId: 'p1' })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(body).toEqual({ title: 'Child task', parentIssueId: 'p1' })
  })
})

describe('useMoveIssue', () => {
  it('POSTs status, anchors and version to the move endpoint and caches the result', async () => {
    let body: unknown
    let idempotencyKey: string | null = null
    const moved = { ...issueFixture, status: 'done', version: 4 }
    server.use(
      http.post('/api/v1/tenants/t1/issues/i1/move', async ({ request }) => {
        idempotencyKey = request.headers.get('Idempotency-Key')
        body = await request.json()
        return HttpResponse.json(moved)
      }),
    )
    const queryClient = new QueryClient()
    const { result } = renderHook(() => useMoveIssue('t1'), { wrapper: wrapper(queryClient) })

    result.current.mutate({ id: 'i1', version: 3, move: { status: 'done', beforeId: 'b1' } })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(body).toEqual({ status: 'done', beforeId: 'b1', version: 3 })
    expect(idempotencyKey).toBeTruthy()
    expect(queryClient.getQueryData(['issue', 't1', 'i1'])).toEqual(moved)
  })
})

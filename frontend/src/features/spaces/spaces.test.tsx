import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import type { ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { SessionProvider } from '@/features/auth/session'
import { useJoinedSpaces } from '@/features/spaces/api'
import { CurrentSpaceProvider, useCurrentSpace } from '@/features/spaces/current-space'
import { isValidSlug, slugFromName } from '@/features/spaces/slug'
import { parseSSEFrames, reconnectDelay } from '@/features/spaces/use-space-events'
import {
  installCloudSpaceHandlers,
  installSignedInSession,
  TEST_TENANT_ID,
} from '@/test/cloud-handlers'
import { server } from '@/test/msw-server'

function wrapperFor(slug: string) {
  return function Wrapper({ children }: { children: ReactNode }) {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    return (
      <QueryClientProvider client={queryClient}>
        <SessionProvider>
          <CurrentSpaceProvider slug={slug}>{children}</CurrentSpaceProvider>
        </SessionProvider>
      </QueryClientProvider>
    )
  }
}
const wrapper = wrapperFor('cloud-dev')

describe('parseSSEFrames', () => {
  it('splits complete frames into typed events and keeps partial tails', () => {
    const stream =
      'data: {"type":"project.updated","spaceId":"s","projectId":"p","version":3}\n\n' +
      'data: {"type":"space.updated","spaceId":"s"}\n\n' +
      'data: {"type":"project.cr'
    const { events, rest } = parseSSEFrames(stream)
    expect(events).toEqual([
      { type: 'project.updated', spaceId: 's', projectId: 'p', version: 3 },
      { type: 'space.updated', spaceId: 's' },
    ])
    expect(rest).toBe('data: {"type":"project.cr')
  })

  it('ignores malformed data payloads and non-data lines', () => {
    const stream =
      'retry: 1000\n\ndata: not-json\n\ndata: {"type":"space.updated","spaceId":"s"}\n\n'
    const { events } = parseSSEFrames(stream)
    expect(events).toEqual([{ type: 'space.updated', spaceId: 's' }])
  })
})

describe('CurrentSpaceProvider', () => {
  it('fetches nothing and stays pending while signed out', async () => {
    const { result } = renderHook(() => useCurrentSpace(), { wrapper })
    // Give the session probe time to settle at 401; no tenant request follows.
    await waitFor(() => expect(result.current.isPending).toBe(true))
    expect(result.current.space).toBeUndefined()
    expect(result.current.tenantId).toBeUndefined()
    expect(result.current.spaces).toBeUndefined()
  })

  it('resolves the route slug against the real spaces list once signed in', async () => {
    installCloudSpaceHandlers('owner')
    const { result } = renderHook(() => useCurrentSpace(), { wrapper })
    await waitFor(() => {
      expect(result.current.tenantId).toBe(TEST_TENANT_ID)
      expect(result.current.space?.slug).toBe('cloud-dev')
    })
    expect(result.current.isPending).toBe(false)
  })

  it('leaves the space unresolved for a slug the member did not join', async () => {
    installCloudSpaceHandlers('owner')
    const { result } = renderHook(() => useCurrentSpace(), { wrapper: wrapperFor('not-joined') })
    await waitFor(() => {
      expect(result.current.tenantId).toBe(TEST_TENANT_ID)
      expect(result.current.isPending).toBe(false)
    })
    expect(result.current.space).toBeUndefined()
    expect(result.current.spaces).toHaveLength(1)
  })
})

describe('useJoinedSpaces', () => {
  it('resolves to an empty list, not pending, for a member with no tenant', async () => {
    installSignedInSession()
    server.use(
      http.get('/api/v1/me/tenants', () => HttpResponse.json({ items: [], nextCursor: '' })),
    )
    const { result } = renderHook(() => useJoinedSpaces(), { wrapper })
    await waitFor(() => expect(result.current.isPending).toBe(false))
    expect(result.current).toEqual({
      tenantId: undefined,
      spaces: [],
      isPending: false,
      isError: false,
    })
  })

  it('reports an error when the tenant list fails for a non-auth reason', async () => {
    installSignedInSession()
    server.use(
      http.get('/api/v1/me/tenants', () =>
        HttpResponse.json({ code: 'internal_error', params: {}, requestId: 'r' }, { status: 500 }),
      ),
    )
    const { result } = renderHook(() => useJoinedSpaces(), { wrapper })
    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.isPending).toBe(false)
    expect(result.current.spaces).toBeUndefined()
  })
})

describe('slug', () => {
  it('derives a backend-valid slug from a display name', () => {
    expect(slugFromName('Acme Inc')).toBe('acme-inc')
    expect(slugFromName('  --Hello, World!--  ')).toBe('hello-world')
    expect(slugFromName('研发组织')).toBe('')
    expect(slugFromName('a'.repeat(80))).toHaveLength(64)
    expect(isValidSlug(slugFromName('Acme Inc'))).toBe(true)
  })

  it('accepts exactly what the backend pattern accepts', () => {
    expect(isValidSlug('acme')).toBe(true)
    expect(isValidSlug('a1-b2')).toBe(true)
    expect(isValidSlug('-acme')).toBe(false)
    expect(isValidSlug('Acme')).toBe(false)
    expect(isValidSlug('')).toBe(false)
    expect(isValidSlug('a'.repeat(65))).toBe(false)
  })
})

describe('reconnectDelay', () => {
  it('doubles from one second and caps at thirty', () => {
    expect([0, 1, 2, 3, 4, 5, 10].map(reconnectDelay)).toEqual([
      1000, 2000, 4000, 8000, 16000, 30000, 30000,
    ])
  })
})

import { screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import type { SpaceListItem } from '@/api/generated.schemas'
import { SpacesPage } from '@/features/spaces/spaces-page'
import { useAuthStore } from '@/state/auth-store'
import { server } from '@/test/msw-server'
import { renderWithProviders } from '@/test/render'

const TENANT_ID = '11111111-1111-1111-1111-111111111111'

function spaceItem(role: string): SpaceListItem {
  return {
    id: '22222222-2222-2222-2222-222222222222',
    tenantId: TENANT_ID,
    name: 'Team Space',
    slug: 'team',
    description: '',
    role,
    createdBy: 'u1',
    version: 1,
    createdAt: '2026-09-21T10:00:00+08:00',
    updatedAt: '2026-09-21T10:00:00+08:00',
    archivedAt: null,
  }
}

/** Mounts the page with one space in which the signed-in member holds `role`. */
async function renderPageWithRole(role: string) {
  server.use(
    http.get(`/api/v1/tenants/${TENANT_ID}/spaces`, () =>
      HttpResponse.json({ items: [spaceItem(role)], nextCursor: '' }),
    ),
  )
  renderWithProviders(<SpacesPage />)
  await screen.findByText('Team Space')
}

/**
 * The backend's archiveSpace requires the owner role and rejects admins
 * (`requireSpaceRole(m, "owner")`), so the UI must only offer archive to an
 * owner. These assertions drive the real page rather than mocking a 403.
 */
describe('SpacesPage archive affordance', () => {
  beforeEach(() => {
    useAuthStore.getState().setSession({
      user: { id: 'u1', displayName: 'Alice', subject: 'alice' },
      tenantId: TENANT_ID,
      tenantName: 'Acme',
    })
  })

  afterEach(() => {
    useAuthStore.getState().clear()
  })

  it('offers archive to an owner', async () => {
    await renderPageWithRole('owner')
    expect(screen.getByRole('button', { name: /归档/ })).toBeInTheDocument()
  })

  it('hides archive from an admin who is not the owner', async () => {
    await renderPageWithRole('admin')
    expect(screen.queryByRole('button', { name: /归档/ })).toBeNull()
  })

  it('hides archive from a regular member', async () => {
    await renderPageWithRole('member')
    expect(screen.queryByRole('button', { name: /归档/ })).toBeNull()
  })

  it('hides archive from an unrecognized role', async () => {
    await renderPageWithRole('superuser')
    expect(screen.queryByRole('button', { name: /归档/ })).toBeNull()
  })
})

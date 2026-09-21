import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import type { SpaceListItem } from '@/api/generated.schemas'
import { MembersPage } from '@/features/members/members-page'
import { db } from '@/mocks/data/store'
import { useAuthStore } from '@/state/auth-store'
import { setCloudSession, TEST_SPACE_ID, TEST_TENANT_ID } from '@/test/cloud-session'
import { server } from '@/test/msw-server'
import { renderWithProviders } from '@/test/render'

const ALICE_ID = '33333333-3333-3333-3333-333333333333'
const BOB_ID = '44444444-4444-4444-4444-444444444444'

function spaceItem(role: string): SpaceListItem {
  return {
    id: TEST_SPACE_ID,
    tenantId: TEST_TENANT_ID,
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

function memberRow(id: string, displayName: string, role: string) {
  return {
    id,
    workspaceId: TEST_SPACE_ID,
    userId: id,
    role,
    status: 'active',
    version: 1,
    displayName,
    joinedAt: '2026-09-20T10:00:00+08:00',
  }
}

/** Installs the spaces list (for the current-space provider) and the space members list. */
function installSpaceHandlers(role: string, members: unknown[]) {
  server.use(
    http.get(`/api/v1/tenants/${TEST_TENANT_ID}/spaces`, () =>
      HttpResponse.json({ items: [spaceItem(role)], nextCursor: '' }),
    ),
    http.get(`/api/v1/tenants/${TEST_TENANT_ID}/spaces/${TEST_SPACE_ID}/members`, () =>
      HttpResponse.json({ items: members, nextCursor: '' }),
    ),
  )
}

describe('MembersPage', () => {
  beforeEach(() => {
    setCloudSession()
  })

  afterEach(() => {
    useAuthStore.getState().clear()
  })

  it('renders real space members read-only for a plain member', async () => {
    installSpaceHandlers('member', [
      memberRow(ALICE_ID, 'Alice', 'owner'),
      memberRow(BOB_ID, 'Bob', 'member'),
    ])
    renderWithProviders(<MembersPage slug="team" />, { slug: 'team' })

    expect(await screen.findByText('Alice')).toBeInTheDocument()
    expect(await screen.findByText('Bob')).toBeInTheDocument()
    expect(screen.getByText('所有者')).toBeInTheDocument()
    // Members are read-only: no add form, no role selectors, no action column.
    expect(screen.queryByLabelText('新成员 userId')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Bob 的角色')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '禁用' })).not.toBeInTheDocument()
  })

  it('lets an owner add a member through the upsert API', async () => {
    installSpaceHandlers('owner', [memberRow(ALICE_ID, 'Alice', 'owner')])
    let putBody: unknown = null
    server.use(
      http.put(
        `/api/v1/tenants/${TEST_TENANT_ID}/spaces/${TEST_SPACE_ID}/members/:uid`,
        async ({ request }) => {
          putBody = await request.json()
          return HttpResponse.json(memberRow(BOB_ID, 'Bob', 'member'))
        },
      ),
    )
    renderWithProviders(<MembersPage slug="team" />, { slug: 'team' })
    const user = userEvent.setup()

    expect(await screen.findByText('Alice')).toBeInTheDocument()
    await user.type(screen.getByLabelText('新成员 userId'), BOB_ID)
    await user.click(screen.getByRole('button', { name: '添加' }))

    await waitFor(() => expect(putBody).not.toBeNull())
    expect(putBody).toEqual({ role: 'member', status: 'active', version: 0 })
  })

  it('keeps the demo store table for mock sessions', async () => {
    useAuthStore.getState().clear()
    renderWithProviders(<MembersPage slug={db.workspace.slug} />, { slug: db.workspace.slug })
    const first = db.users[0]
    if (!first) throw new Error('seed users must not be empty')
    expect(await screen.findByText(first.name)).toBeInTheDocument()
    expect(screen.queryByLabelText('新成员 userId')).not.toBeInTheDocument()
  })
})

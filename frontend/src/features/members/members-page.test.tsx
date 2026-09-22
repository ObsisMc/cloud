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

const MEMBERS_KEY = `/api/v1/tenants/${TEST_TENANT_ID}/spaces/${TEST_SPACE_ID}/members`

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

/**
 * Renders the members page for an owner session (Alice as owner), waits for the
 * list to load, and opens the add-member dialog. Returns the user-event instance
 * so the test can drive the dialog further.
 */
async function renderOwnerWithAddDialog() {
  installSpaceHandlers('owner', [memberRow(ALICE_ID, 'Alice', 'owner')])
  const user = userEvent.setup()
  renderWithProviders(<MembersPage slug="team" />, { slug: 'team' })
  await screen.findByText('Alice')
  await user.click(screen.getByRole('button', { name: '添加成员' }))
  return user
}

/** Types an email into the open add-member dialog and submits the add. */
async function typeAndSubmitEmail(
  user: ReturnType<typeof userEvent.setup>,
  email: string,
): Promise<void> {
  await user.type(screen.getByLabelText('成员邮箱'), email)
  await user.click(screen.getByRole('button', { name: '添加' }))
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
    // Members are read-only: no add-member trigger, no role selectors, no action column.
    expect(screen.queryByRole('button', { name: '添加成员' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Bob 的角色')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '禁用' })).not.toBeInTheDocument()
  })

  it('shows the add-member dialog for an owner and adds by email through the API', async () => {
    let postBody: unknown = null
    let idempotencyKey = ''
    server.use(
      http.post(MEMBERS_KEY, async ({ request }) => {
        postBody = await request.json()
        idempotencyKey = request.headers.get('Idempotency-Key') ?? ''
        return HttpResponse.json(memberRow(BOB_ID, 'Bob', 'member'))
      }),
    )
    const user = await renderOwnerWithAddDialog()
    expect(screen.getByLabelText('成员邮箱')).toBeInTheDocument()

    await typeAndSubmitEmail(user, 'bob@example.com')

    await waitFor(() => expect(postBody).not.toBeNull())
    expect(postBody).toEqual({ email: 'bob@example.com' })
    // POST must carry a non-empty idempotency key so retries dedupe.
    expect(idempotencyKey.length).toBeGreaterThan(0)
    // The dialog closes on success.
    await waitFor(() => expect(screen.queryByLabelText('成员邮箱')).not.toBeInTheDocument())
  })

  it('rejects an empty or malformed email with a local hint', async () => {
    let posted = false
    server.use(
      http.post(MEMBERS_KEY, async () => {
        posted = true
        return HttpResponse.json(memberRow(BOB_ID, 'Bob', 'member'))
      }),
    )
    const user = await renderOwnerWithAddDialog()

    await user.click(screen.getByRole('button', { name: '添加' }))
    expect(await screen.findByText('请输入邮箱地址。')).toBeInTheDocument()
    expect(posted).toBe(false)

    await typeAndSubmitEmail(user, 'not-an-email')
    expect(await screen.findByText('请输入有效的邮箱地址。')).toBeInTheDocument()
    expect(posted).toBe(false)
  })

  it('shows the not-registered hint when the backend rejects an unknown email', async () => {
    server.use(
      http.post(MEMBERS_KEY, () =>
        HttpResponse.json(
          { code: 'user_not_registered', params: {}, requestId: 'r' },
          { status: 404 },
        ),
      ),
    )
    const user = await renderOwnerWithAddDialog()

    await typeAndSubmitEmail(user, 'ghost@example.com')

    expect(await screen.findByText('该邮箱尚未注册，请先完成注册。')).toBeInTheDocument()
    // The dialog stays open so the actor can correct the address.
    expect(screen.getByLabelText('成员邮箱')).toBeInTheDocument()
  })

  it('closes the dialog when re-adding an existing member succeeds (idempotent)', async () => {
    server.use(http.post(MEMBERS_KEY, () => HttpResponse.json(memberRow(BOB_ID, 'Bob', 'member'))))
    const user = await renderOwnerWithAddDialog()

    await typeAndSubmitEmail(user, 'bob@example.com')

    await waitFor(() => expect(screen.queryByLabelText('成员邮箱')).not.toBeInTheDocument())
  })

  it('keeps the demo store table for mock sessions', async () => {
    useAuthStore.getState().clear()
    renderWithProviders(<MembersPage slug={db.workspace.slug} />, { slug: db.workspace.slug })
    const first = db.users[0]
    if (!first) throw new Error('seed users must not be empty')
    expect(await screen.findByText(first.name)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '添加成员' })).not.toBeInTheDocument()
  })

  it('shows role select and remove for members when the actor is the owner; owner rows are read-only', async () => {
    installSpaceHandlers('owner', [
      memberRow(ALICE_ID, 'Alice', 'owner'),
      memberRow(BOB_ID, 'Bob', 'member'),
    ])
    renderWithProviders(<MembersPage slug="team" />, { slug: 'team' })
    await screen.findByText('Alice')

    // The member row gets a role selector (owner-only) and a remove action.
    expect(screen.getByLabelText('Bob 的角色')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '移除' })).toBeInTheDocument()
    // The owner row (Alice) renders a plain Owner label with no management
    // actions, so exactly one row carries remove/disable.
    expect(screen.getByText('所有者')).toBeInTheDocument()
    expect(screen.queryByLabelText('Alice 的角色')).not.toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: '移除' })).toHaveLength(1)
    expect(screen.getAllByRole('button', { name: '禁用' })).toHaveLength(1)
  })

  it('keeps admin to add-only: no role select, no remove, add trigger preserved', async () => {
    installSpaceHandlers('admin', [
      memberRow(ALICE_ID, 'Alice', 'owner'),
      memberRow(BOB_ID, 'Bob', 'member'),
    ])
    renderWithProviders(<MembersPage slug="team" />, { slug: 'team' })
    await screen.findByText('Alice')

    expect(screen.getByRole('button', { name: '添加成员' })).toBeInTheDocument()
    expect(screen.queryByLabelText('Bob 的角色')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '移除' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '禁用' })).not.toBeInTheDocument()
  })

  it('does not offer the owner role in the role selector', async () => {
    installSpaceHandlers('owner', [memberRow(BOB_ID, 'Bob', 'member')])
    const user = userEvent.setup()
    renderWithProviders(<MembersPage slug="team" />, { slug: 'team' })
    await screen.findByText('Bob')

    await user.click(screen.getByLabelText('Bob 的角色'))
    expect(await screen.findByText('管理员')).toBeInTheDocument()
    // '成员' renders in both the select trigger and the open item, so assert on
    // the multiple match.
    expect(screen.getAllByText('成员')).not.toHaveLength(0)
    // Ownership is immutable through the member API: never offered.
    expect(screen.queryAllByText('所有者')).toHaveLength(0)
  })

  it('confirms membership-only removal before calling the DELETE member endpoint', async () => {
    installSpaceHandlers('owner', [
      memberRow(ALICE_ID, 'Alice', 'owner'),
      memberRow(BOB_ID, 'Bob', 'member'),
    ])
    let deleted = false
    let idempotencyKey = ''
    server.use(
      http.delete(`${MEMBERS_KEY}/${BOB_ID}`, async ({ request }) => {
        deleted = true
        idempotencyKey = request.headers.get('Idempotency-Key') ?? ''
        return HttpResponse.json(memberRow(BOB_ID, 'Bob', 'member'))
      }),
    )
    const user = userEvent.setup()
    renderWithProviders(<MembersPage slug="team" />, { slug: 'team' })
    await screen.findByText('Alice')

    await user.click(screen.getByRole('button', { name: '移除' }))
    expect(await screen.findByText('从工作区移除「Bob」？')).toBeInTheDocument()
    // The confirm copy explains workspace-membership-only removal, not account
    // or tenant-membership deletion, and that created resources remain.
    expect(screen.getByText(/账号与租户成员关系不受影响/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '确认移除' }))
    await waitFor(() => expect(deleted).toBe(true))
    // DELETE must carry a non-empty idempotency key so retries dedupe.
    expect(idempotencyKey.length).toBeGreaterThan(0)
  })
})

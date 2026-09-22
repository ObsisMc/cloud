import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'
import { MembersPage } from '@/features/members/members-page'
import { installCloudSpaceHandlers, TEST_SPACE_ID, TEST_TENANT_ID } from '@/test/cloud-handlers'
import { renderWithProviders } from '@/test/render'
import { server } from '@/test/msw-server'

const ALICE_ID = '33333333-3333-3333-3333-333333333333'
const BOB_ID = '44444444-4444-4444-4444-444444444444'

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

function installMembersHandler(members: unknown[]) {
  server.use(
    http.get(`/api/v1/tenants/${TEST_TENANT_ID}/spaces/${TEST_SPACE_ID}/members`, () =>
      HttpResponse.json({ items: members, nextCursor: '' }),
    ),
  )
}

describe('MembersPage', () => {
  it('renders real members and hides management controls from members', async () => {
    installCloudSpaceHandlers('member')
    installMembersHandler([
      memberRow(ALICE_ID, 'Alice', 'owner'),
      memberRow(BOB_ID, 'Bob', 'member'),
    ])
    renderWithProviders(<MembersPage slug="cloud-dev" />, { slug: 'cloud-dev' })

    expect(await screen.findByText('Alice')).toBeInTheDocument()
    expect(await screen.findByText('Bob')).toBeInTheDocument()
    expect(screen.getByText('所有者')).toBeInTheDocument()
    // Members are read-only: no add form, no role selectors, no action column.
    expect(screen.queryByLabelText('新成员 userId')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Bob 的角色')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '禁用' })).not.toBeInTheDocument()
  })

  it('lets an owner add a member through the upsert API', async () => {
    installCloudSpaceHandlers('owner')
    installMembersHandler([memberRow(ALICE_ID, 'Alice', 'owner')])
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
    renderWithProviders(<MembersPage slug="cloud-dev" />, { slug: 'cloud-dev' })
    const user = userEvent.setup()

    expect(await screen.findByText('Alice')).toBeInTheDocument()
    await user.type(screen.getByLabelText('新成员 userId'), BOB_ID)
    await user.click(screen.getByRole('button', { name: '添加' }))

    await waitFor(() => expect(putBody).not.toBeNull())
    expect(putBody).toEqual({ role: 'member', status: 'active', version: 0 })
  })

  it('lets an admin disable and re-enable a member and shows the backend fault code', async () => {
    installCloudSpaceHandlers('admin')
    installMembersHandler([
      memberRow(ALICE_ID, 'Alice', 'owner'),
      memberRow(BOB_ID, 'Bob', 'member'),
      {
        ...memberRow('55555555-5555-5555-5555-555555555555', 'Carol', 'member'),
        status: 'disabled',
      },
    ])
    const bodies: unknown[] = []
    server.use(
      http.put(
        `/api/v1/tenants/${TEST_TENANT_ID}/spaces/${TEST_SPACE_ID}/members/:uid`,
        async ({ request, params }) => {
          const body = await request.json()
          bodies.push({ uid: params['uid'], body })
          if (params['uid'] === BOB_ID) {
            return HttpResponse.json(
              { code: 'space_last_owner', params: {}, requestId: 'r' },
              { status: 409 },
            )
          }
          return HttpResponse.json(memberRow(String(params['uid']), 'Carol', 'member'))
        },
      ),
    )
    renderWithProviders(<MembersPage slug="cloud-dev" />, { slug: 'cloud-dev' })
    const user = userEvent.setup()

    expect(await screen.findByText('Bob')).toBeInTheDocument()
    // The owner row cannot be disabled by anyone; a member row can.
    const buttons = screen.getAllByRole('button', { name: /禁用|启用/ })
    const [ownerButton, memberButton, disabledButton] = buttons
    if (!ownerButton || !memberButton || !disabledButton) throw new Error('expected three rows')
    expect(ownerButton).toBeDisabled()
    expect(memberButton).toBeEnabled()
    expect(disabledButton).toHaveTextContent('启用')

    await user.click(memberButton)
    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).toEqual({
      uid: BOB_ID,
      body: { role: 'member', status: 'disabled', version: 1 },
    })
    expect(await screen.findByText(/操作失败：space_last_owner/)).toBeInTheDocument()

    const enable = screen.getAllByRole('button', { name: /禁用|启用/ })[2]
    if (!enable) throw new Error('expected a third member row')
    await user.click(enable)
    await waitFor(() => expect(bodies).toHaveLength(2))
    expect(bodies[1]).toMatchObject({ body: { role: 'member', status: 'active', version: 1 } })
    // Admins see role selectors but cannot grant owner.
    expect(screen.getByLabelText('Bob 的角色')).toBeInTheDocument()
  })
})

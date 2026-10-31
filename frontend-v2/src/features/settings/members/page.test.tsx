import { describe, expect, it } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { demoUser, fakeBackend } from '@/test/backend'
import { renderSettingsPage } from '../test-utils'
import MembersPage from './page'

const tenantId = '00000000-0000-4000-8000-0000000000aa'
const owner = {
  id: demoUser.id,
  userId: demoUser.id,
  tenantId,
  displayName: demoUser.displayName,
  role: 'owner',
  status: 'active',
  version: 1,
}
const invited = {
  ...owner,
  id: '00000000-0000-4000-8000-0000000000ab',
  userId: '00000000-0000-4000-8000-0000000000ab',
  displayName: 'alice',
  role: 'member',
}

/** The roster is server state: the fake keeps it so a successful invite is visible on refetch. */
function backend(role: 'owner' | 'member', roster = [owner]) {
  let members = [...roster]
  return fakeBackend((c) => {
    if (c.url === '/api/v1/me/tenants')
      return {
        status: 200,
        data: { items: [{ id: tenantId, name: '演示团队', role, status: 'active', version: 1 }] },
      }
    if (c.url === `/api/v1/tenants/${tenantId}/members`)
      return { status: 200, data: { items: members, nextCursor: '' } }
    if (c.url === `/api/v2/tenants/${tenantId}/members` && c.method === 'post') {
      // A new array per response: React Query's structural sharing would otherwise see the same
      // mutated reference and skip the re-render.
      members = [...members, invited]
      return { status: 201, data: invited }
    }
    return undefined
  })
}

describe('workspace members', () => {
  it('shows the server roster with roles and lets the owner invite by email', async () => {
    const user = userEvent.setup()
    const seen = backend('owner')
    renderSettingsPage(<MembersPage />, '/settings/members')
    const row = await screen.findByRole('row', { name: /演示用户/ })
    expect(within(row).getByText('owner')).toBeInTheDocument()

    await user.type(screen.getByLabelText('邮箱'), 'alice@demo.ora-space.dev')
    await user.click(screen.getByRole('button', { name: '邀请' }))
    expect(await screen.findByRole('status')).toHaveTextContent('已邀请 alice')
    expect(seen).toContain(`POST /api/v2/tenants/${tenantId}/members`)
    // The invite refetches the roster, so the new member appears without a reload.
    const added = await screen.findByRole('row', { name: /alice/ })
    expect(within(added).getByText('成员')).toBeInTheDocument()
    expect((screen.getByLabelText('邮箱') as HTMLInputElement).value).toBe('')
  })

  it('explains a rejected invitation instead of failing silently', async () => {
    const user = userEvent.setup()
    fakeBackend((c) => {
      if (c.url === '/api/v1/me/tenants')
        return { status: 200, data: { items: [{ id: tenantId, role: 'owner' }] } }
      if (c.url === `/api/v1/tenants/${tenantId}/members`)
        return { status: 200, data: { items: [owner], nextCursor: '' } }
      if (c.url === `/api/v2/tenants/${tenantId}/members`)
        return { status: 409, data: { code: 'member_exists', params: {}, requestId: 'r' } }
      return undefined
    })
    renderSettingsPage(<MembersPage />, '/settings/members')
    await user.type(await screen.findByLabelText('邮箱'), 'alice@demo.ora-space.dev')
    await user.click(screen.getByRole('button', { name: '邀请' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      '这个邮箱已经是团队成员。',
    )
  })

  it('offers a member no invitation entry', async () => {
    backend('member', [{ ...owner, role: 'member' }])
    renderSettingsPage(<MembersPage />, '/settings/members')
    expect(
      await screen.findByText(/邀请入口只向 owner 开放/),
    ).toBeInTheDocument()
    await waitFor(() =>
      expect(screen.queryByRole('button', { name: '邀请' })).not.toBeInTheDocument(),
    )
    expect(await screen.findByRole('row', { name: /演示用户/ })).toBeInTheDocument()
  })
})

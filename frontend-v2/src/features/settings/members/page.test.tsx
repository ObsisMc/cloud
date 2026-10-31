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

const squad = (overrides: Record<string, unknown> = {}) => ({
  id: 'squad-live',
  name: '产品小队',
  description: '',
  charter: '',
  leaderAgentId: null,
  archivedAt: null,
  version: 1,
  createdBy: owner.userId,
  createdAt: '2026-10-01T00:00:00Z',
  updatedAt: '2026-10-01T00:00:00Z',
  humanCount: 1,
  agentCount: 0,
  maintainerCount: 1,
  ...overrides,
})

/** The roster is server state: the fake keeps it so a successful invite is visible on refetch. */
function backend(role: 'owner' | 'member', roster = [owner], squads: unknown[] = []) {
  let members = [...roster]
  return fakeBackend((c) => {
    if (c.url === '/api/v1/me/tenants')
      return {
        status: 200,
        data: { items: [{ id: tenantId, name: '演示团队', role, status: 'active', version: 1 }] },
      }
    if (c.url === `/api/v1/tenants/${tenantId}/members`)
      return { status: 200, data: { items: members, nextCursor: '' } }
    if (c.url === `/api/v2/tenants/${tenantId}/squads`)
      return { status: 200, data: { items: squads, nextCursor: '' } }
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

  it('offers only live squads and sends the chosen one with the invitation', async () => {
    const user = userEvent.setup()
    const payloads: string[] = []
    const live = squad()
    const archived = squad({ id: 'squad-archived', name: '已归档小队', archivedAt: '2026-10-02T00:00:00Z' })
    fakeBackend((c) => {
      if (c.url === '/api/v1/me/tenants')
        return { status: 200, data: { items: [{ id: tenantId, name: '演示团队', role: 'owner' }] } }
      if (c.url === `/api/v1/tenants/${tenantId}/members`)
        return { status: 200, data: { items: [owner], nextCursor: '' } }
      if (c.url === `/api/v2/tenants/${tenantId}/squads`)
        return { status: 200, data: { items: [live, archived], nextCursor: '' } }
      if (c.url === `/api/v2/tenants/${tenantId}/members` && c.method === 'post') {
        payloads.push(String(c.data))
        return { status: 201, data: invited }
      }
      return undefined
    })
    renderSettingsPage(<MembersPage />, '/settings/members')
    const picker = await screen.findByLabelText('小队（选填）')
    // An archived squad cannot take new members, so it never appears as a choice.
    expect(within(picker).queryByText('已归档小队')).not.toBeInTheDocument()
    await user.selectOptions(picker, 'squad-live')
    await user.type(screen.getByLabelText('邮箱'), 'alice@demo.ora-space.dev')
    await user.click(screen.getByRole('button', { name: '邀请' }))
    await waitFor(() => expect(payloads).toHaveLength(1))
    expect(JSON.parse(payloads[0])).toEqual({
      email: 'alice@demo.ora-space.dev',
      squadId: 'squad-live',
    })
    // The form resets, so the next invitation does not silently join the same squad.
    await waitFor(() => expect((screen.getByLabelText('小队（选填）') as HTMLSelectElement).value).toBe(''))
  })

  it('explains a rejected invitation instead of failing silently', async () => {
    const user = userEvent.setup()
    fakeBackend((c) => {
      if (c.url === '/api/v1/me/tenants')
        return { status: 200, data: { items: [{ id: tenantId, role: 'owner' }] } }
      if (c.url === `/api/v1/tenants/${tenantId}/members`)
        return { status: 200, data: { items: [owner], nextCursor: '' } }
      if (c.url === `/api/v2/tenants/${tenantId}/squads`)
        return { status: 200, data: { items: [], nextCursor: '' } }
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

import { screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'
import { server } from '@/test/msw-server'
import { renderWithProviders } from '@/test/render'
import type { TenantMember } from '@/features/issues/types'
import { MembersPage } from './members-page'

const members: TenantMember[] = [
  { id: 'u1', userId: 'u1', role: 'admin', status: 'active', version: 1, displayName: 'Alice' },
  { id: 'u2', userId: 'u2', role: 'member', status: 'active', version: 1, displayName: 'Bob' },
]

describe('MembersPage', () => {
  it('renders every member with their role and status', async () => {
    server.use(
      http.get('/api/v1/tenants/t1/members', () =>
        HttpResponse.json({ items: members, nextCursor: '' }),
      ),
    )

    renderWithProviders(<MembersPage slug="t1" />)

    expect(await screen.findByText('Alice')).toBeInTheDocument()
    expect(screen.getByText('Bob')).toBeInTheDocument()
    expect(screen.getByText('管理员')).toBeInTheDocument()
    expect(screen.getAllByText('已加入')).toHaveLength(2)
  })
})

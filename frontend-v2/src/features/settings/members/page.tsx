import { useState } from 'react'
import { useGetApiV1MeTenants } from '@/api/me/me'
import { useRole, useUser } from '@/app/auth/user'
import {
  DataState,
  NoResults,
  PageHeader,
  SearchInput,
} from '../components/page-parts'
import { InviteForm } from './invite-form'
import { MemberRoster } from './member-roster'
import { useMembers } from './query'

/**
 * The workspace roster from `GET /tenants/{tid}/members`, plus the owner's invitation entry.
 * v2 cannot change a role, disable or remove a member, so the page offers none of those actions.
 */
export default function MembersPage() {
  const me = useUser()
  const role = useRole()
  const tenants = useGetApiV1MeTenants()
  const tenantId = tenants.data?.items[0]?.id ?? ''
  const roster = useMembers(tenantId)
  const [search, setSearch] = useState('')
  const members = roster.data?.items ?? []
  const filtered = members.filter((member) =>
    member.displayName.toLowerCase().includes(search.trim().toLowerCase()),
  )

  return (
    <section>
      <PageHeader
        title="成员"
        description="工作空间成员与角色。owner 邀请时填邮箱即可，被邀请人直接用这个邮箱登录。"
        count={roster.data ? members.length : undefined}
      />
      <DataState
        pending={tenants.isPending || roster.isPending}
        error={tenants.error ?? roster.error}
        retry={() => {
          void tenants.refetch()
          void roster.refetch()
        }}
      >
        {role === 'owner' && <InviteForm tenantId={tenantId} />}
        {role === 'member' && (
          <p className="mb-5 text-sm text-muted-foreground">
            你在这个工作空间是成员，邀请入口只向 owner 开放。
          </p>
        )}
        <div className="settings-toolbar">
          <SearchInput label="搜索成员" value={search} onChange={setSearch} />
        </div>
        {filtered.length ? (
          <MemberRoster members={filtered} currentUserId={me.id} />
        ) : (
          <NoResults filtered={members.length > 0} onClear={() => setSearch('')} />
        )}
        <div className="settings-list-footer">
          <span>{filtered.length} 名成员</span>
          <span>角色由启动配置或邀请决定</span>
        </div>
      </DataState>
    </section>
  )
}

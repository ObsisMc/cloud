import type { ComponentType } from 'react'
import { createBrowserRouter, Navigate, useParams } from 'react-router-dom'
import { DashboardLayout } from '@/components/layout/dashboard-layout'
import { AgentDetailPage } from '@/features/agents/agent-detail-page'
import { AgentsPage } from '@/features/agents/agents-page'
import { LoginPage } from '@/features/auth/login-page'
import { BillingPage } from '@/features/billing/billing-page'
import { ChatPage } from '@/features/chat/chat-page'
import { InboxPage } from '@/features/inbox/inbox-page'
import { IssueDetailPage } from '@/features/issues/issue-detail-page'
import { IssuesPage } from '@/features/issues/issues-page'
import { MembersPage } from '@/features/members/members-page'
import { MyIssuesPage } from '@/features/my-issues/my-issues-page'
import { ProjectDetailPage } from '@/features/projects/project-detail-page'
import { ProjectsPage } from '@/features/projects/projects-page'
import { RuntimesPage } from '@/features/runtimes/runtimes-page'
import { GeneralSettingsPage } from '@/features/settings/general-settings-page'
import { SettingsLayout } from '@/features/settings/settings-layout'
import { SkillsPage } from '@/features/skills/skills-page'
import { SpacesPage } from '@/features/spaces/spaces-page'
import { SquadDetailPage } from '@/features/squads/squad-detail-page'
import { SquadsPage } from '@/features/squads/squads-page'
import { useAuthStore } from '@/state/auth-store'

/** Routes `/` to the signed-in tenant's board, or to login when there is no session. */
function RootRedirect() {
  const tenantId = useAuthStore((s) => s.tenantId)
  if (tenantId) return <Navigate to={`/${tenantId}/issues`} replace />
  return <Navigate to="/login" replace />
}

/**
 * DashboardLayout only renders its children once the session is present, so every
 * page under it can trust the `:workspaceSlug` param (a tenant id) and doesn't
 * need to re-validate it — this just forwards it as the `slug` prop each page expects.
 */
function WithSlug({ component: Component }: { component: ComponentType<{ slug: string }> }) {
  const { workspaceSlug } = useParams<{ workspaceSlug: string }>()
  if (!workspaceSlug) throw new Error('workspace route must provide a slug')
  return <Component slug={workspaceSlug} />
}

export const router = createBrowserRouter([
  { path: '/', element: <RootRedirect /> },
  { path: '/login', element: <LoginPage /> },
  {
    path: '/:workspaceSlug',
    element: <DashboardLayout />,
    children: [
      { index: true, element: <Navigate to="issues" replace /> },
      { path: 'issues', element: <WithSlug component={IssuesPage} /> },
      { path: 'issues/:issueId', element: <WithSlug component={IssueDetailPage} /> },
      { path: 'my-issues', element: <WithSlug component={MyIssuesPage} /> },
      { path: 'projects', element: <WithSlug component={ProjectsPage} /> },
      { path: 'projects/:projectId', element: <WithSlug component={ProjectDetailPage} /> },
      { path: 'spaces', element: <SpacesPage /> },
      { path: 'squads', element: <WithSlug component={SquadsPage} /> },
      { path: 'squads/:squadId', element: <WithSlug component={SquadDetailPage} /> },
      { path: 'agents', element: <WithSlug component={AgentsPage} /> },
      { path: 'agents/:agentId', element: <WithSlug component={AgentDetailPage} /> },
      { path: 'skills', element: <WithSlug component={SkillsPage} /> },
      { path: 'runtimes', element: <WithSlug component={RuntimesPage} /> },
      { path: 'chat', element: <WithSlug component={ChatPage} /> },
      { path: 'chat/:sessionId', element: <WithSlug component={ChatPage} /> },
      { path: 'inbox', element: <WithSlug component={InboxPage} /> },
      {
        path: 'settings',
        element: <WithSlug component={SettingsLayout} />,
        children: [
          { index: true, element: <GeneralSettingsPage /> },
          { path: 'members', element: <WithSlug component={MembersPage} /> },
          { path: 'billing', element: <WithSlug component={BillingPage} /> },
        ],
      },
    ],
  },
  { path: '*', element: <Navigate to="/" replace /> },
])

import type { ComponentType } from 'react'
import { createBrowserRouter, Navigate, useParams } from 'react-router-dom'
import { DashboardLayout } from '@/components/layout/dashboard-layout'
import { AgentDetailPage } from '@/features/agents/agent-detail-page'
import { AgentsPage } from '@/features/agents/agents-page'
import { LoginPage } from '@/features/auth/login-page'
import { RequireSession } from '@/features/auth/require-session'
import { BillingPage } from '@/features/billing/billing-page'
import { ChatPage } from '@/features/chat/chat-page'
import { InboxPage } from '@/features/inbox/inbox-page'
import { IssueDetailPage } from '@/features/issues/issue-detail-page'
import { IssuesPage } from '@/features/issues/issues-page'
import { MembersPage } from '@/features/members/members-page'
import { MyIssuesPage } from '@/features/my-issues/my-issues-page'
import { OnboardingPage } from '@/features/onboarding/onboarding-page'
import { ProjectDetailPage } from '@/features/projects/project-detail-page'
import { ProjectsPage } from '@/features/projects/projects-page'
import { RuntimesPage } from '@/features/runtimes/runtimes-page'
import { GeneralSettingsPage } from '@/features/settings/general-settings-page'
import { SettingsLayout } from '@/features/settings/settings-layout'
import { SkillsPage } from '@/features/skills/skills-page'
import { SquadDetailPage } from '@/features/squads/squad-detail-page'
import { SquadsPage } from '@/features/squads/squads-page'

/**
 * DashboardLayout only renders its children once :workspaceSlug matches a
 * real workspace, so every page under it can trust the param and doesn't
 * need to re-validate it — this just forwards it as the `slug` prop each
 * page already expects.
 */
function WithSlug({ component: Component }: { component: ComponentType<{ slug: string }> }) {
  const { workspaceSlug } = useParams<{ workspaceSlug: string }>()
  if (!workspaceSlug) throw new Error('workspace route must provide a slug')
  return <Component slug={workspaceSlug} />
}

/**
 * `/` and `/onboarding` both resolve to "the member's first workspace, or the
 * screen that creates one": the onboarding page redirects members who already
 * have a workspace, so it doubles as the signed-in landing route.
 */
export const router = createBrowserRouter([
  { path: '/', element: <Navigate to="/onboarding" replace /> },
  { path: '/login', element: <LoginPage /> },
  {
    path: '/onboarding',
    element: (
      <RequireSession>
        <OnboardingPage />
      </RequireSession>
    ),
  },
  {
    path: '/:workspaceSlug',
    element: (
      <RequireSession>
        <DashboardLayout />
      </RequireSession>
    ),
    children: [
      { index: true, element: <Navigate to="issues" replace /> },
      { path: 'issues', element: <WithSlug component={IssuesPage} /> },
      { path: 'issues/:issueId', element: <WithSlug component={IssueDetailPage} /> },
      { path: 'my-issues', element: <WithSlug component={MyIssuesPage} /> },
      { path: 'projects', element: <WithSlug component={ProjectsPage} /> },
      { path: 'projects/:projectId', element: <WithSlug component={ProjectDetailPage} /> },
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

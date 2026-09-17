import { createBrowserRouter, Navigate } from 'react-router-dom'
import { DashboardLayout } from '@/components/layout/dashboard-layout'
import { AgentDetailPage } from '@/features/agents/agent-detail-page'
import { AgentsPage } from '@/features/agents/agents-page'
import { AutopilotsPage } from '@/features/autopilots/autopilots-page'
import { LoginPage } from '@/features/auth/login-page'
import { BillingPage } from '@/features/billing/billing-page'
import { UsagePage } from '@/features/billing/usage-page'
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
import { SquadDetailPage } from '@/features/squads/squad-detail-page'
import { SquadsPage } from '@/features/squads/squads-page'
import { db } from '@/mocks/data/store'

const slug = db.workspace.slug

export const router = createBrowserRouter([
  { path: '/', element: <Navigate to={`/${slug}/issues`} replace /> },
  { path: '/login', element: <LoginPage /> },
  {
    path: '/:workspaceSlug',
    element: <DashboardLayout />,
    children: [
      { index: true, element: <Navigate to="issues" replace /> },
      { path: 'issues', element: <IssuesPage slug={slug} /> },
      { path: 'issues/:issueId', element: <IssueDetailPage slug={slug} /> },
      { path: 'my-issues', element: <MyIssuesPage slug={slug} /> },
      { path: 'projects', element: <ProjectsPage slug={slug} /> },
      { path: 'projects/:projectId', element: <ProjectDetailPage slug={slug} /> },
      { path: 'squads', element: <SquadsPage slug={slug} /> },
      { path: 'squads/:squadId', element: <SquadDetailPage slug={slug} /> },
      { path: 'agents', element: <AgentsPage slug={slug} /> },
      { path: 'agents/:agentId', element: <AgentDetailPage slug={slug} /> },
      { path: 'autopilots', element: <AutopilotsPage slug={slug} /> },
      { path: 'skills', element: <SkillsPage slug={slug} /> },
      { path: 'runtimes', element: <RuntimesPage slug={slug} /> },
      { path: 'chat', element: <ChatPage slug={slug} /> },
      { path: 'chat/:sessionId', element: <ChatPage slug={slug} /> },
      { path: 'inbox', element: <InboxPage slug={slug} /> },
      { path: 'usage', element: <UsagePage slug={slug} /> },
      {
        path: 'settings',
        element: <SettingsLayout slug={slug} />,
        children: [
          { index: true, element: <GeneralSettingsPage /> },
          { path: 'members', element: <MembersPage slug={slug} /> },
          { path: 'billing', element: <BillingPage slug={slug} /> },
        ],
      },
    ],
  },
  { path: '*', element: <Navigate to="/" replace /> },
])

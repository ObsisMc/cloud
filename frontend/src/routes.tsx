import { createBrowserRouter, Navigate } from 'react-router-dom'
import { DashboardLayout } from '@/components/layout/dashboard-layout'
import { AgentDetailPage } from '@/features/agents/agent-detail-page'
import { AgentsPage } from '@/features/agents/agents-page'
import { AutopilotsPage } from '@/features/autopilots/autopilots-page'
import { LoginPage } from '@/features/auth/login-page'
import { ChatPage } from '@/features/chat/chat-page'
import { IssueDetailPage } from '@/features/issues/issue-detail-page'
import { IssuesPage } from '@/features/issues/issues-page'
import { MyIssuesPage } from '@/features/my-issues/my-issues-page'
import { ProjectDetailPage } from '@/features/projects/project-detail-page'
import { ProjectsPage } from '@/features/projects/projects-page'
import { RuntimesPage } from '@/features/runtimes/runtimes-page'
import { SkillsPage } from '@/features/skills/skills-page'
import { SquadDetailPage } from '@/features/squads/squad-detail-page'
import { SquadsPage } from '@/features/squads/squads-page'
import { db } from '@/mocks/data/store'

function WorkspaceIssues() {
  return <IssuesPage slug={db.workspace.slug} />
}

function WorkspaceIssueDetail() {
  return <IssueDetailPage slug={db.workspace.slug} />
}

function WorkspaceMyIssues() {
  return <MyIssuesPage slug={db.workspace.slug} />
}

function WorkspaceProjects() {
  return <ProjectsPage slug={db.workspace.slug} />
}

function WorkspaceProjectDetail() {
  return <ProjectDetailPage slug={db.workspace.slug} />
}

function WorkspaceSquads() {
  return <SquadsPage slug={db.workspace.slug} />
}

function WorkspaceSquadDetail() {
  return <SquadDetailPage slug={db.workspace.slug} />
}

function WorkspaceAgents() {
  return <AgentsPage slug={db.workspace.slug} />
}

function WorkspaceAgentDetail() {
  return <AgentDetailPage slug={db.workspace.slug} />
}

function WorkspaceAutopilots() {
  return <AutopilotsPage slug={db.workspace.slug} />
}

function WorkspaceSkills() {
  return <SkillsPage slug={db.workspace.slug} />
}

function WorkspaceRuntimes() {
  return <RuntimesPage slug={db.workspace.slug} />
}

function WorkspaceChat() {
  return <ChatPage slug={db.workspace.slug} />
}

export const router = createBrowserRouter([
  { path: '/', element: <Navigate to={`/${db.workspace.slug}/issues`} replace /> },
  { path: '/login', element: <LoginPage /> },
  {
    path: '/:workspaceSlug',
    element: <DashboardLayout />,
    children: [
      { index: true, element: <Navigate to="issues" replace /> },
      { path: 'issues', element: <WorkspaceIssues /> },
      { path: 'issues/:issueId', element: <WorkspaceIssueDetail /> },
      { path: 'my-issues', element: <WorkspaceMyIssues /> },
      { path: 'projects', element: <WorkspaceProjects /> },
      { path: 'projects/:projectId', element: <WorkspaceProjectDetail /> },
      { path: 'squads', element: <WorkspaceSquads /> },
      { path: 'squads/:squadId', element: <WorkspaceSquadDetail /> },
      { path: 'agents', element: <WorkspaceAgents /> },
      { path: 'agents/:agentId', element: <WorkspaceAgentDetail /> },
      { path: 'autopilots', element: <WorkspaceAutopilots /> },
      { path: 'skills', element: <WorkspaceSkills /> },
      { path: 'runtimes', element: <WorkspaceRuntimes /> },
      { path: 'chat', element: <WorkspaceChat /> },
      { path: 'chat/:sessionId', element: <WorkspaceChat /> },
    ],
  },
  { path: '*', element: <Navigate to="/" replace /> },
])

import { createBrowserRouter, Navigate } from 'react-router-dom'
import { DashboardLayout } from '@/components/layout/dashboard-layout'
import { LoginPage } from '@/features/auth/login-page'
import { IssueDetailPage } from '@/features/issues/issue-detail-page'
import { IssuesPage } from '@/features/issues/issues-page'
import { MyIssuesPage } from '@/features/my-issues/my-issues-page'
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
    ],
  },
  { path: '*', element: <Navigate to="/" replace /> },
])

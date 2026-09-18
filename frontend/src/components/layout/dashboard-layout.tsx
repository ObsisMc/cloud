import { Navigate, Outlet, useParams } from 'react-router-dom'
import { AppSidebar } from '@/components/layout/app-sidebar'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { db, workspaceBySlug } from '@/mocks/data/store'
import { useAuthStore } from '@/state/auth-store'

export function DashboardLayout() {
  const { workspaceSlug } = useParams<{ workspaceSlug: string }>()
  const token = useAuthStore((s) => s.token)

  if (!token) return <Navigate to="/login" replace />
  const workspace = workspaceBySlug(workspaceSlug)
  if (!workspace) {
    return <Navigate to={`/${db.workspace.slug}/issues`} replace />
  }

  return (
    <SidebarProvider className="h-svh">
      <AppSidebar slug={workspace.slug} />
      <SidebarInset className="min-h-0 overflow-hidden">
        <Outlet />
      </SidebarInset>
    </SidebarProvider>
  )
}

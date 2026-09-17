import { Navigate, Outlet, useParams } from 'react-router-dom'
import { AppSidebar } from '@/components/layout/app-sidebar'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { db } from '@/mocks/data/store'
import { useAuthStore } from '@/state/auth-store'

export function DashboardLayout() {
  const { workspaceSlug } = useParams<{ workspaceSlug: string }>()
  const token = useAuthStore((s) => s.token)

  if (!token) return <Navigate to="/login" replace />
  if (!workspaceSlug || workspaceSlug !== db.workspace.slug) {
    return <Navigate to={`/${db.workspace.slug}/issues`} replace />
  }

  return (
    <SidebarProvider className="h-svh">
      <AppSidebar slug={workspaceSlug} />
      <SidebarInset className="min-h-0 overflow-hidden">
        <Outlet />
      </SidebarInset>
    </SidebarProvider>
  )
}

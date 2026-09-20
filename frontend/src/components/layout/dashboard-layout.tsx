import { Navigate, Outlet, useParams } from 'react-router-dom'
import { AppSidebar } from '@/components/layout/app-sidebar'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { useAuthStore } from '@/state/auth-store'

/** Authenticated shell. `:workspaceSlug` carries the tenant id; the session gates access. */
export function DashboardLayout() {
  const { workspaceSlug } = useParams<{ workspaceSlug: string }>()
  const user = useAuthStore((s) => s.user)

  if (!user) return <Navigate to="/login" replace />
  if (!workspaceSlug) throw new Error('workspace route must provide a slug')

  return (
    <SidebarProvider className="h-svh">
      <AppSidebar slug={workspaceSlug} />
      <SidebarInset className="min-h-0 overflow-hidden">
        <Outlet />
      </SidebarInset>
    </SidebarProvider>
  )
}

import { Navigate, Outlet, useParams } from 'react-router-dom'
import { AppSidebar } from '@/components/layout/app-sidebar'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { CurrentSpaceProvider, useCurrentSpace } from '@/features/spaces/current-space'
import { useSpaceEvents } from '@/features/spaces/use-space-events'
import { workspacePaths } from '@/lib/paths'

/**
 * Dashboard gate for a signed-in member (the route wraps it in
 * `RequireSession`). It resolves `:workspaceSlug` against the member's real
 * spaces and subscribes the tab to that space's event stream. Everything
 * under the outlet can trust the resolved space.
 */
export function DashboardLayout() {
  const { workspaceSlug } = useParams<{ workspaceSlug: string }>()
  if (!workspaceSlug) throw new Error('dashboard route must provide a slug')
  return (
    <CurrentSpaceProvider slug={workspaceSlug}>
      <DashboardShell slug={workspaceSlug} />
    </CurrentSpaceProvider>
  )
}

function DashboardShell({ slug }: { slug: string }) {
  const { tenantId, space, spaces, isPending, isError } = useCurrentSpace()
  useSpaceEvents(tenantId, space?.id)
  if (isPending) return null
  if (isError) {
    return (
      <div className="flex h-svh items-center justify-center bg-muted/30">
        <p className="text-sm text-muted-foreground">无法加载工作区，请刷新重试</p>
      </div>
    )
  }
  // A member with no live workspace has nothing to render here; the
  // onboarding screen creates one instead of showing demo data.
  const first = spaces?.[0]
  if (!first) return <Navigate to="/onboarding" replace />
  // An unknown or archived slug falls back to the first joined space so the
  // route never points at a space the member cannot see.
  if (!space) return <Navigate to={workspacePaths(first.slug).issues} replace />
  return (
    <SidebarProvider className="h-svh">
      <AppSidebar slug={slug} />
      <SidebarInset className="min-h-0 overflow-hidden">
        <Outlet />
      </SidebarInset>
    </SidebarProvider>
  )
}

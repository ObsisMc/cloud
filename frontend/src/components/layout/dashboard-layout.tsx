import { Navigate, Outlet, useNavigate, useParams } from 'react-router-dom'
import { AppSidebar } from '@/components/layout/app-sidebar'
import { Button } from '@/components/ui/button'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { CurrentSpaceProvider, useCurrentSpace } from '@/features/spaces/current-space'
import { useSpaceEvents } from '@/features/spaces/use-space-events'
import { db, workspaceBySlug } from '@/mocks/data/store'
import { useAuthStore } from '@/state/auth-store'
import { useDemoAuthStore } from '@/state/demo-auth-store'

/**
 * Dashboard gate: a demo session requires the mock token and resolves the slug
 * against the demo store; a cloud session (ora-web cookie) resolves it against
 * the real spaces list and subscribes the tab to the space event stream.
 * Everything under the outlet can trust the resolved space.
 */
export function DashboardLayout() {
  const { workspaceSlug } = useParams<{ workspaceSlug: string }>()
  const tenantId = useAuthStore((s) => s.tenantId)
  const demoToken = useDemoAuthStore((s) => s.token)

  if (!tenantId && !demoToken) return <Navigate to="/login" replace />
  const workspace = workspaceBySlug(workspaceSlug)
  if (!tenantId && !workspace) {
    return <Navigate to={`/${db.workspace.slug}/issues`} replace />
  }

  return (
    <CurrentSpaceProvider slug={workspaceSlug ?? ''}>
      <DashboardShell slug={workspaceSlug ?? ''} />
    </CurrentSpaceProvider>
  )
}

function DashboardShell({ slug }: { slug: string }) {
  const { cloudMode, tenantId, space, spaces } = useCurrentSpace()
  useSpaceEvents(tenantId, space?.id)
  // A cloud session with no joined space has nothing to render and must not
  // fall back to demo data: show an explicit empty state instead.
  if (cloudMode && spaces && spaces.length === 0) {
    return <EmptyWorkspaceState />
  }
  // In cloud mode an unknown or archived slug falls back to the first joined
  // space so the route param never points at a space the member cannot see.
  if (space === undefined && spaces && spaces.length > 0) {
    const first = spaces[0]
    if (first) return <Navigate to={`/${first.slug}/projects`} replace />
  }
  return (
    <SidebarProvider className="h-svh">
      <AppSidebar slug={slug} />
      <SidebarInset className="min-h-0 overflow-hidden">
        <Outlet />
      </SidebarInset>
    </SidebarProvider>
  )
}

/** A cloud session with no joined space: nothing to leak, nothing to mock. */
function EmptyWorkspaceState() {
  const navigate = useNavigate()
  const clear = useAuthStore((s) => s.clear)
  const clearDemo = useDemoAuthStore((s) => s.clear)

  function handleLogout() {
    clear()
    clearDemo()
    void navigate('/login')
  }

  return (
    <div className="flex h-svh items-center justify-center bg-muted/30">
      <div className="space-y-3 text-center">
        <p className="text-sm text-muted-foreground">你尚未加入任何工作区，请联系管理员添加</p>
        <Button variant="outline" onClick={handleLogout}>
          退出登录
        </Button>
      </div>
    </div>
  )
}

import {
  Bot,
  Boxes,
  Check,
  ChevronDown,
  CircuitBoard,
  Cog,
  Inbox,
  Layers,
  ListTodo,
  LogOut,
  MessageCircle,
  Plus,
  Server,
  Sparkles,
  Users,
  type LucideIcon,
} from 'lucide-react'
import { useState } from 'react'
import { NavLink, useLocation, useNavigate } from 'react-router-dom'
import type { SpaceListItem } from '@/api/generated.schemas'
import { ActorAvatar } from '@/components/common/actor-avatar'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
} from '@/components/ui/sidebar'
import { useLogout } from '@/features/auth/api'
import { useInboxItems } from '@/features/inbox/api'
import { CreateSpaceDialog } from '@/features/spaces/create-space-dialog'
import { useCurrentSpace } from '@/features/spaces/current-space'
import { workspacePaths } from '@/lib/paths'
import { db, workspaceBySlug } from '@/mocks/data/store'
import { useAuthStore } from '@/state/auth-store'
import { useDemoAuthStore } from '@/state/demo-auth-store'

type NavItem = {
  to: (p: ReturnType<typeof workspacePaths>) => string
  label: string
  icon: LucideIcon
  /** Real-backend surfaces: shown only in cloud mode (demo mode hides them). */
  cloudOnly?: boolean
}

const workNav: NavItem[] = [
  { to: (p) => p.issues, label: '任务', icon: Layers, cloudOnly: true },
  { to: (p) => p.projects, label: '项目', icon: CircuitBoard },
  { to: (p) => p.spaces, label: '空间', icon: Boxes, cloudOnly: true },
]

const aiTeamNav: NavItem[] = [
  { to: (p) => p.agents, label: '智能体', icon: Bot },
  { to: (p) => p.squads, label: '小队', icon: Users },
  { to: (p) => p.skills, label: '技能', icon: Sparkles },
  { to: (p) => p.runtimes, label: '运行时', icon: Server },
]

const utilityNav: NavItem[] = [{ to: (p) => p.settings, label: '设置', icon: Cog }]

/** Workspaces the switcher lists: cloud lists only joined real spaces. */
function switchableSpaces(cloudMode: boolean, spaces: SpaceListItem[] | undefined) {
  if (cloudMode) return spaces ?? []
  return db.workspaces
}

/** One sidebar nav group: active state follows the current path prefix. */
function NavGroup({
  items,
  p,
  pathname,
}: {
  items: NavItem[]
  p: ReturnType<typeof workspacePaths>
  pathname: string
}) {
  return (
    <SidebarMenu className="gap-0.5">
      {items.map((item) => {
        const href = item.to(p)
        const Icon = item.icon
        return (
          <SidebarMenuItem key={item.label}>
            <SidebarMenuButton isActive={pathname.startsWith(href)} render={<NavLink to={href} />}>
              <Icon />
              <span>{item.label}</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        )
      })}
    </SidebarMenu>
  )
}

/** A workspace entry the switcher can list: real space or demo store workspace. */
interface SwitchableWorkspace {
  id: string
  name: string
  slug: string
  avatarColor?: string
}

/**
 * Top-left workspace switcher: active workspace label + dropdown with the
 * switchable workspace list, a create entry (cloud only) and sign-out. Cloud
 * workspaces have no mock issue boards, so a switch lands on projects there.
 */
function WorkspaceSwitcher({
  cloudMode,
  activeWorkspace,
  switchable,
  slug,
  userName,
  userHandle,
  onSwitch,
  onCreate,
  onLogout,
}: {
  cloudMode: boolean
  activeWorkspace: { name: string; avatarColor?: string }
  switchable: readonly SwitchableWorkspace[]
  slug: string
  userName: string | undefined
  userHandle: string | undefined
  onSwitch: (path: string) => void
  onCreate: () => void
  onLogout: () => void
}) {
  return (
    <SidebarHeader className="py-3">
      <SidebarMenu>
        <SidebarMenuItem>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <SidebarMenuButton>
                  <span
                    className="flex size-5 items-center justify-center rounded-sm text-[11px] font-semibold text-white"
                    style={{ backgroundColor: activeWorkspace.avatarColor ?? '#3b82f6' }}
                  >
                    {activeWorkspace.name.charAt(0)}
                  </span>
                  <span className="flex-1 truncate font-medium">{activeWorkspace.name}</span>
                  <ChevronDown className="size-3 text-muted-foreground" />
                </SidebarMenuButton>
              }
            />
            <DropdownMenuContent className="w-56" align="start" side="bottom" sideOffset={4}>
              <div className="flex items-center gap-2.5 px-2 py-1.5">
                <ActorAvatar actor={{ name: userName ?? '用户', type: 'user' }} size="lg" />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium leading-tight">{userName ?? '用户'}</p>
                  <p className="truncate text-xs text-muted-foreground leading-tight">
                    {userHandle}
                  </p>
                </div>
              </div>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuLabel className="text-xs text-muted-foreground">
                  工作区
                </DropdownMenuLabel>
                {switchable.map((ws) => (
                  <DropdownMenuItem
                    key={ws.id}
                    onClick={() => {
                      const target = cloudMode ? 'projects' : 'issues'
                      if (ws.slug !== slug) onSwitch(`/${ws.slug}/${target}`)
                    }}
                  >
                    <span
                      className="flex size-5 items-center justify-center rounded-sm text-[10px] font-semibold text-white"
                      style={{ backgroundColor: ws.avatarColor ?? '#3b82f6' }}
                    >
                      {ws.name.charAt(0)}
                    </span>
                    <span className="flex-1 truncate">{ws.name}</span>
                    {ws.slug === slug && <Check className="size-3.5" />}
                  </DropdownMenuItem>
                ))}
                {cloudMode && (
                  <DropdownMenuItem onClick={onCreate}>
                    <Plus className="size-3.5" />
                    新建工作区
                  </DropdownMenuItem>
                )}
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuItem variant="destructive" onClick={onLogout}>
                <LogOut className="size-3.5" />
                退出登录
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </SidebarMenuItem>
      </SidebarMenu>
    </SidebarHeader>
  )
}

// oxlint-disable-next-line max-lines-per-function -- this composition root owns the complete sidebar navigation tree.
export function AppSidebar({ slug }: { slug: string }) {
  const p = workspacePaths(slug)
  const { pathname } = useLocation()
  const navigate = useNavigate()
  const cloudUser = useAuthStore((s) => s.user)
  const demoUser = useDemoAuthStore((s) => s.user)
  const logout = useLogout()
  const { cloudMode, spaces, space, tenantId } = useCurrentSpace()
  const { data: inboxItems = [] } = useInboxItems(slug)
  const unreadCount = inboxItems.filter((i) => !i.read).length
  // Cloud sessions switch between real spaces; mock sessions between the demo
  // store workspaces. The active label resolves the real space in cloud mode.
  const activeWorkspace = space ?? workspaceBySlug(slug) ?? db.workspace
  const switchable = switchableSpaces(cloudMode, spaces)
  const [createSpaceOpen, setCreateSpaceOpen] = useState(false)
  const userName = cloudMode ? cloudUser?.displayName : demoUser?.name
  const userHandle = cloudMode ? cloudUser?.subject : demoUser?.email

  function handleLogout() {
    // Cloud logout clears the server cookie through the edge; demo logout only
    // clears the local mock session. Both clear the local stores before leaving.
    if (cloudMode) {
      logout.mutate(undefined, {
        onSettled: () => {
          useDemoAuthStore.getState().clear()
          void navigate('/login')
        },
      })
    } else {
      useDemoAuthStore.getState().clear()
      void navigate('/login')
    }
  }

  return (
    <Sidebar variant="inset">
      <CreateSpaceDialog
        open={createSpaceOpen}
        onOpenChange={setCreateSpaceOpen}
        tenantId={tenantId}
        onCreated={(newSlug) => void navigate(`/${newSlug}/projects`)}
      />
      <WorkspaceSwitcher
        cloudMode={cloudMode}
        activeWorkspace={activeWorkspace}
        switchable={switchable}
        slug={slug}
        userName={userName}
        userHandle={userHandle}
        onSwitch={(path) => void navigate(path)}
        onCreate={() => setCreateSpaceOpen(true)}
        onLogout={handleLogout}
      />

      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu className="gap-0.5">
              <SidebarMenuItem>
                <SidebarMenuButton
                  isActive={pathname === p.inbox}
                  render={<NavLink to={p.inbox} />}
                >
                  <Inbox />
                  <span>收件箱</span>
                  {unreadCount > 0 && (
                    <span className="ml-auto rounded-full bg-primary px-1.5 text-[10px] text-primary-foreground">
                      {unreadCount}
                    </span>
                  )}
                </SidebarMenuButton>
              </SidebarMenuItem>
              {cloudMode && (
                <SidebarMenuItem>
                  <SidebarMenuButton
                    isActive={pathname === p.myIssues}
                    render={<NavLink to={p.myIssues} />}
                  >
                    <ListTodo />
                    <span>我的任务</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              )}
              <SidebarMenuItem>
                <SidebarMenuButton
                  isActive={pathname.startsWith(p.chat)}
                  render={<NavLink to={p.chat} />}
                >
                  <MessageCircle />
                  <span>聊天</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>

        <SidebarGroup>
          <SidebarGroupLabel>工作</SidebarGroupLabel>
          <SidebarGroupContent>
            <NavGroup
              items={workNav.filter((item) => !item.cloudOnly || cloudMode)}
              p={p}
              pathname={pathname}
            />
          </SidebarGroupContent>
        </SidebarGroup>

        {!cloudMode && (
          <SidebarGroup>
            <SidebarGroupLabel>AI 团队</SidebarGroupLabel>
            <SidebarGroupContent>
              <NavGroup items={aiTeamNav} p={p} pathname={pathname} />
            </SidebarGroupContent>
          </SidebarGroup>
        )}
      </SidebarContent>

      <SidebarFooter className="p-2">
        <NavGroup items={utilityNav} p={p} pathname={pathname} />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}

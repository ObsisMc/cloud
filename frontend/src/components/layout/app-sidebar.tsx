import {
  Bot,
  Check,
  ChevronDown,
  CircuitBoard,
  Cog,
  Gauge,
  Inbox,
  Layers,
  ListTodo,
  LogOut,
  MessageCircle,
  Server,
  Sparkles,
  Users,
} from 'lucide-react'
import { NavLink, useLocation, useNavigate } from 'react-router-dom'
import { ActorAvatar } from '@/components/common/actor-avatar'
import {
  DropdownMenu,
  DropdownMenuContent,
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
import { useInboxItems } from '@/features/inbox/api'
import { workspacePaths } from '@/lib/paths'
import { db } from '@/mocks/data/store'
import { useAuthStore } from '@/state/auth-store'

const workNav = [
  { to: (p: ReturnType<typeof workspacePaths>) => p.issues, label: 'Issues', icon: Layers },
  { to: (p: ReturnType<typeof workspacePaths>) => p.projects, label: 'Projects', icon: CircuitBoard },
  { to: (p: ReturnType<typeof workspacePaths>) => p.autopilots, label: 'Autopilots', icon: Sparkles },
]

const aiTeamNav = [
  { to: (p: ReturnType<typeof workspacePaths>) => p.agents, label: 'Agents', icon: Bot },
  { to: (p: ReturnType<typeof workspacePaths>) => p.squads, label: 'Squads', icon: Users },
  { to: (p: ReturnType<typeof workspacePaths>) => p.skills, label: 'Skills', icon: Sparkles },
  { to: (p: ReturnType<typeof workspacePaths>) => p.runtimes, label: 'Runtimes', icon: Server },
]

const utilityNav = [
  { to: (p: ReturnType<typeof workspacePaths>) => p.usage, label: 'Usage', icon: Gauge },
  { to: (p: ReturnType<typeof workspacePaths>) => p.settings, label: 'Settings', icon: Cog },
]

export function AppSidebar({ slug }: { slug: string }) {
  const p = workspacePaths(slug)
  const { pathname } = useLocation()
  const navigate = useNavigate()
  const user = useAuthStore((s) => s.user)
  const clear = useAuthStore((s) => s.clear)
  const { data: inboxItems = [] } = useInboxItems(slug)
  const unreadCount = inboxItems.filter((i) => !i.read).length

  function handleLogout() {
    clear()
    navigate('/login')
  }

  return (
    <Sidebar variant="inset">
      <SidebarHeader className="py-3">
        <SidebarMenu>
          <SidebarMenuItem>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <SidebarMenuButton>
                    <span
                      className="flex size-5 items-center justify-center rounded-sm text-[11px] font-semibold text-white"
                      style={{ backgroundColor: db.workspace.avatarColor }}
                    >
                      {db.workspace.name.charAt(0)}
                    </span>
                    <span className="flex-1 truncate font-medium">{db.workspace.name}</span>
                    <ChevronDown className="size-3 text-muted-foreground" />
                  </SidebarMenuButton>
                }
              />
              <DropdownMenuContent className="w-56" align="start" side="bottom" sideOffset={4}>
                <div className="flex items-center gap-2.5 px-2 py-1.5">
                  <ActorAvatar actor={user ?? undefined} size="lg" />
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium leading-tight">{user?.name}</p>
                    <p className="truncate text-xs text-muted-foreground leading-tight">{user?.email}</p>
                  </div>
                </div>
                <DropdownMenuSeparator />
                <DropdownMenuLabel className="text-xs text-muted-foreground">Workspaces</DropdownMenuLabel>
                <DropdownMenuItem>
                  <span
                    className="flex size-5 items-center justify-center rounded-sm text-[10px] font-semibold text-white"
                    style={{ backgroundColor: db.workspace.avatarColor }}
                  >
                    {db.workspace.name.charAt(0)}
                  </span>
                  <span className="flex-1 truncate">{db.workspace.name}</span>
                  <Check className="size-3.5" />
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem variant="destructive" onClick={handleLogout}>
                  <LogOut className="size-3.5" />
                  Log out
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu className="gap-0.5">
              <SidebarMenuItem>
                <SidebarMenuButton isActive={pathname === p.inbox} render={<NavLink to={p.inbox} />}>
                  <Inbox />
                  <span>Inbox</span>
                  {unreadCount > 0 && (
                    <span className="ml-auto rounded-full bg-primary px-1.5 text-[10px] text-primary-foreground">
                      {unreadCount}
                    </span>
                  )}
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton isActive={pathname === p.myIssues} render={<NavLink to={p.myIssues} />}>
                  <ListTodo />
                  <span>My Issues</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton isActive={pathname.startsWith(p.chat)} render={<NavLink to={p.chat} />}>
                  <MessageCircle />
                  <span>Chat</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>

        <SidebarGroup>
          <SidebarGroupLabel>Work</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu className="gap-0.5">
              {workNav.map((item) => {
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
          </SidebarGroupContent>
        </SidebarGroup>

        <SidebarGroup>
          <SidebarGroupLabel>AI Team</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu className="gap-0.5">
              {aiTeamNav.map((item) => {
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
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter className="p-2">
        <SidebarMenu className="gap-0.5">
          {utilityNav.map((item) => {
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
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}

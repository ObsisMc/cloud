import { act, render, renderHook, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupAction,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInput,
  SidebarInset,
  SidebarMenu,
  SidebarMenuAction,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSkeleton,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  SidebarProvider,
  SidebarRail,
  SidebarSeparator,
  SidebarTrigger,
  useSidebar,
} from './sidebar'

function Chrome({ collapsible = 'offcanvas' }: { collapsible?: 'offcanvas' | 'icon' | 'none' }) {
  return (
    <Sidebar collapsible={collapsible}>
      <SidebarHeader>
        <SidebarInput placeholder="Search" />
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>Group</SidebarGroupLabel>
          <SidebarGroupAction aria-label="group action" />
          <SidebarGroupContent>
            <SidebarMenu>
              <SidebarMenuItem>
                <SidebarMenuButton isActive tooltip="Home">
                  Home
                </SidebarMenuButton>
                <SidebarMenuAction showOnHover aria-label="more" />
                <SidebarMenuBadge>3</SidebarMenuBadge>
                <SidebarMenuSub>
                  <SidebarMenuSubItem>
                    <SidebarMenuSubButton size="sm" isActive href="#child">
                      Child
                    </SidebarMenuSubButton>
                  </SidebarMenuSubItem>
                </SidebarMenuSub>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton tooltip={{ children: 'Rich tooltip' }}>Rich</SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton>Plain</SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuSkeleton showIcon />
              <SidebarMenuSkeleton />
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
        <SidebarSeparator />
      </SidebarContent>
      <SidebarFooter>footer</SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}

function clickNth(name: string, index: number) {
  const target = screen.getAllByRole('button', { name })[index]
  if (!target) throw new Error(`no button "${name}" at index ${index}`)
  return target
}

const collapsedWrapper = ({ children }: { children: ReactNode }) => (
  <SidebarProvider defaultOpen={false}>{children}</SidebarProvider>
)

function setViewportWidth(width: number) {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: width })
}

describe('Sidebar', () => {
  afterEach(() => {
    setViewportWidth(1280)
    vi.restoreAllMocks()
  })

  it('renders every primitive on desktop and toggles collapsed state through the trigger, rail and shortcut', async () => {
    const user = userEvent.setup()
    render(
      <SidebarProvider>
        <Chrome />
        <SidebarInset>
          <SidebarTrigger />
          main
        </SidebarInset>
      </SidebarProvider>,
    )

    expect(screen.getByRole('button', { name: 'Home' })).toHaveAttribute('data-active')
    expect(screen.getByText('3')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Child' })).toHaveAttribute('data-size', 'sm')
    expect(screen.getByPlaceholderText('Search')).toBeInTheDocument()
    const sidebar = document.querySelector('[data-slot="sidebar"]')
    expect(sidebar).toHaveAttribute('data-state', 'expanded')

    await user.click(clickNth('展开/收起侧边栏', 0))
    expect(sidebar).toHaveAttribute('data-state', 'collapsed')
    expect(document.cookie).toContain('sidebar_state=false')

    await user.click(clickNth('展开/收起侧边栏', 1))
    expect(sidebar).toHaveAttribute('data-state', 'expanded')

    await user.keyboard('{Control>}b{/Control}')
    expect(sidebar).toHaveAttribute('data-state', 'collapsed')
  })

  it('renders a static container when not collapsible and honors a controlled open state', () => {
    const onOpenChange = vi.fn<(open: boolean) => void>()
    render(
      <SidebarProvider open={false} onOpenChange={onOpenChange}>
        <Chrome collapsible="none" />
        <SidebarTrigger onClick={() => {}} />
      </SidebarProvider>,
    )
    expect(document.querySelector('[data-slot="sidebar"]')).not.toHaveAttribute('data-state')
    act(() => {
      clickNth('展开/收起侧边栏', 1).click()
    })
    expect(onOpenChange).toHaveBeenCalledWith(true)
  })

  it('uses a sheet on mobile and opens it through the trigger', async () => {
    setViewportWidth(375)
    const user = userEvent.setup()
    render(
      <SidebarProvider>
        <Chrome />
        <SidebarTrigger />
      </SidebarProvider>,
    )
    expect(screen.queryByRole('button', { name: 'Home' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '展开/收起侧边栏' }))
    expect(await screen.findByRole('button', { name: 'Home' })).toBeInTheDocument()
    expect(screen.getByText('Displays the mobile sidebar.')).toBeInTheDocument()
  })

  it('exposes its context and refuses to work outside the provider', () => {
    const { result } = renderHook(() => useSidebar(), { wrapper: collapsedWrapper })
    expect(result.current.state).toBe('collapsed')
    act(() => result.current.setOpen(true))
    expect(result.current.state).toBe('expanded')
    act(() => result.current.setOpenMobile(true))
    expect(result.current.openMobile).toBe(true)

    expect(() => renderHook(() => useSidebar())).toThrow(/SidebarProvider/)
  })
})

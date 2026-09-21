import { useState, type ReactNode } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useSession } from '@/features/auth/session'
import { useCreateSpace, useCreateTenant, useJoinedSpaces } from '@/features/spaces/api'
import { isValidSlug, slugFromName } from '@/features/spaces/slug'
import { workspacePaths, workspaceUrlPrefix } from '@/lib/paths'

/**
 * First-run screen: a signed-in member who joined no workspace names their
 * first one here. Creating it provisions the member's tenant behind the
 * scenes (the product never shows tenants); a member whose tenant exists but
 * has no live workspace creates one in that tenant instead. Members who
 * already have a workspace are sent to it — later workspaces are created
 * from the sidebar, not here.
 */
export function OnboardingPage() {
  const { tenantId, spaces, isPending, isError } = useJoinedSpaces()

  if (isPending) return null
  if (isError) {
    return (
      <Shell>
        <p className="text-sm text-destructive">无法加载你的工作区，请刷新重试</p>
      </Shell>
    )
  }
  const existing = spaces?.[0]
  if (existing) return <Navigate to={workspacePaths(existing.slug).issues} replace />
  return (
    <Shell>
      <CreateFirstWorkspace tenantId={tenantId} />
    </Shell>
  )
}

function Shell({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-svh items-center justify-center bg-muted/30 px-4">
      <div className="w-full max-w-md space-y-6">{children}</div>
    </div>
  )
}

/**
 * Picks the creation API by the member's state: without a tenant the
 * workspace comes with a new tenant; with one it is a space in that tenant.
 * Both resolve to the slug the caller should navigate to.
 */
function useCreateFirstWorkspace(tenantId: string | undefined) {
  const createTenant = useCreateTenant()
  const createSpace = useCreateSpace(tenantId)
  const errorCode =
    createTenant.error?.response?.data?.code ?? createSpace.error?.response?.data?.code

  function create(input: { name: string; slug: string }, onCreated: (slug: string) => void) {
    if (tenantId) {
      createSpace.mutate({ ...input, description: '' }, { onSuccess: (s) => onCreated(s.slug) })
      return
    }
    createTenant.mutate(input, { onSuccess: (created) => onCreated(created.space.slug) })
  }

  return { create, pending: createTenant.isPending || createSpace.isPending, errorCode }
}

/**
 * Name + slug form. The slug follows the name until the user edits it, and
 * the URL preview shows exactly where the workspace will live.
 */
function CreateFirstWorkspace({ tenantId }: { tenantId: string | undefined }) {
  const navigate = useNavigate()
  const { session, signOut } = useSession()
  const { create, pending, errorCode } = useCreateFirstWorkspace(tenantId)
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  const [slugTouched, setSlugTouched] = useState(false)
  const submittable = name.trim() !== '' && isValidSlug(slug) && !pending
  const displayName = session.status === 'signed-in' ? session.user.displayName : ''

  function updateName(value: string) {
    setName(value)
    if (!slugTouched) setSlug(slugFromName(value))
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        if (!submittable) return
        create(
          { name: name.trim(), slug },
          (created) => void navigate(workspacePaths(created).issues),
        )
      }}
      className="space-y-6"
    >
      <div className="space-y-1">
        <h1 className="text-lg font-semibold">创建你的第一个工作区</h1>
        <p className="text-sm text-muted-foreground">
          {displayName ? `${displayName}，` : ''}
          工作区是团队协作的地方：项目、成员和任务都属于某个工作区。你将成为它的所有者。
        </p>
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="onboarding-name">工作区名称</Label>
        <Input
          id="onboarding-name"
          value={name}
          onChange={(e) => updateName(e.target.value)}
          placeholder="Acme Inc"
          autoFocus
          required
        />
      </div>
      <SlugField
        slug={slug}
        onChange={(value) => {
          setSlugTouched(true)
          setSlug(value)
        }}
      />
      {errorCode && <p className="text-xs text-destructive">创建失败：{errorCode}</p>}
      <Button type="submit" className="w-full" disabled={!submittable}>
        {pending ? '创建中…' : '创建工作区'}
      </Button>
      <Button
        type="button"
        variant="ghost"
        className="w-full"
        disabled={pending}
        onClick={() => void signOut()}
      >
        退出登录
      </Button>
    </form>
  )
}

/**
 * URL field: the reserved `host/w/` prefix is rendered as a fixed adornment
 * and only the slug after it is editable, so what the member types is exactly
 * what the final address ends with.
 */
function SlugField({ slug, onChange }: { slug: string; onChange: (slug: string) => void }) {
  const invalid = slug !== '' && !isValidSlug(slug)
  return (
    <div className="space-y-1.5">
      <Label htmlFor="onboarding-slug">工作区地址（创建后不可修改）</Label>
      <div className="flex h-8 items-stretch rounded-lg border border-input bg-transparent focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50">
        <span
          aria-hidden="true"
          className="flex select-none items-center whitespace-nowrap rounded-l-lg border-r border-input bg-muted/60 px-2.5 font-mono text-sm text-muted-foreground"
        >
          {workspaceUrlPrefix()}
        </span>
        <Input
          id="onboarding-slug"
          value={slug}
          onChange={(e) => onChange(e.target.value.toLowerCase())}
          placeholder="acme"
          aria-invalid={invalid || undefined}
          aria-describedby="onboarding-slug-hint"
          className="h-auto rounded-l-none border-0 font-mono focus-visible:ring-0"
          required
        />
      </div>
      <p id="onboarding-slug-hint" className="text-xs text-muted-foreground">
        {invalid
          ? '小写字母、数字与连字符，以字母或数字开头，最多 64 个字符'
          : `完整地址：${workspaceUrlPrefix()}${slug || 'acme'}`}
      </p>
    </div>
  )
}

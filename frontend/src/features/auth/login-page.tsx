import { useState } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useDemoLogin, useLogin } from '@/features/auth/api'
import { db } from '@/mocks/data/store'
import { useAuthStore } from '@/state/auth-store'
import { useDemoAuthStore } from '@/state/demo-auth-store'

/**
 * Sign-in offers two flows: the demo flow keeps the mock store pages working
 * (any email), and the real-account flow signs in through the ora-web edge with
 * the user's email, which provisions the identity into the bootstrap tenant and
 * sets the `ora_subject` session cookie (HttpOnly). The real flow drives the
 * Collaboration Workspace shell; the demo flow drives the local mock store.
 */
export function LoginPage() {
  const tenantId = useAuthStore((s) => s.tenantId)
  const demoToken = useDemoAuthStore((s) => s.token)

  if (tenantId) return <Navigate to="/default/projects" replace />
  if (demoToken) return <Navigate to={`/${db.workspace.slug}/issues`} replace />

  return (
    <div className="flex min-h-svh items-center justify-center bg-muted/30 px-4">
      <div className="w-full max-w-sm space-y-6">
        <div className="space-y-1 text-center">
          <div className="mx-auto flex size-10 items-center justify-center rounded-lg bg-primary text-primary-foreground font-semibold">
            O
          </div>
          <h1 className="text-lg font-semibold">登录 Ora</h1>
          <p className="text-sm text-muted-foreground">
            真实账号连接后端协作空间；演示账号使用本地模拟数据。
          </p>
        </div>
        <Tabs defaultValue="cloud">
          <TabsList className="grid w-full grid-cols-2">
            <TabsTrigger value="cloud">真实账号</TabsTrigger>
            <TabsTrigger value="demo">演示账号</TabsTrigger>
          </TabsList>
          <TabsContent value="cloud">
            <CloudSignInForm />
          </TabsContent>
          <TabsContent value="demo">
            <DemoSignInForm />
          </TabsContent>
        </Tabs>
      </div>
    </div>
  )
}

/**
 * Real-account sign-in through the ora-web edge: the email is provisioned into
 * the bootstrap tenant and the session cookie is set server-side; the shell then
 * lands on the default space. Fields, button flow, loading and error states
 * follow the reference login, adapted from the devgateway source/subject/display
 * triple to the cookie session's email identity.
 */
function CloudSignInForm() {
  const navigate = useNavigate()
  const login = useLogin()
  const [email, setEmail] = useState('')

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        login.mutate(email, {
          onSuccess: () => navigate('/default/projects'),
        })
      }}
      className="space-y-4"
    >
      <div className="space-y-1.5">
        <Label htmlFor="cloud-email">邮箱</Label>
        <Input
          id="cloud-email"
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          placeholder="you@company.com"
          required
        />
      </div>
      {login.isError && (
        <p className="text-xs text-destructive">登录失败：请确认后端已启动后重试。</p>
      )}
      <Button type="submit" className="w-full" disabled={login.isPending}>
        {login.isPending ? '登录中…' : '连接后端登录'}
      </Button>
    </form>
  )
}

/** Demo sign-in against the mock store; any email works. */
function DemoSignInForm() {
  const navigate = useNavigate()
  const login = useDemoLogin()
  const [email, setEmail] = useState('ruihao053@gmail.com')

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        login.mutate(email, {
          onSuccess: () => navigate(`/${db.workspace.slug}/issues`),
        })
      }}
      className="space-y-4"
    >
      <div className="space-y-1.5">
        <Label htmlFor="email">邮箱</Label>
        <Input
          id="email"
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          placeholder="you@company.com"
          required
        />
      </div>
      <Button type="submit" className="w-full" disabled={login.isPending}>
        {login.isPending ? '登录中…' : '继续'}
      </Button>
    </form>
  )
}

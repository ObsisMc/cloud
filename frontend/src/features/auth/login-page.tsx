import { useState } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useLogin } from '@/features/auth/api'
import { hasCloudSession, loginCloud } from '@/lib/cloud-session'
import { db } from '@/mocks/data/store'
import { useAuthStore } from '@/state/auth-store'

/**
 * Sign-in offers two flows: the demo flow keeps the mock store pages working
 * (any email), and the cloud flow signs in through devgateway with a stable
 * identity (source/subject) so collaboration pages read the real backend.
 * Cloud credentials live per-tab, which is what multi-account verification
 * relies on.
 */
export function LoginPage() {
  const token = useAuthStore((s) => s.token)

  if (hasCloudSession()) return <Navigate to="/default/projects" replace />
  if (token) return <Navigate to={`/${db.workspace.slug}/issues`} replace />

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

/** Cloud sign-in: asks devgateway for dual JWTs and lands on the default space. */
function CloudSignInForm() {
  const navigate = useNavigate()
  const [source, setSource] = useState('huawei-corp')
  const [subject, setSubject] = useState('stable-account-id')
  const [display, setDisplay] = useState('Alice')
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)

  async function signIn() {
    setError('')
    setPending(true)
    try {
      await loginCloud(source, subject, display)
      void navigate('/default/projects')
    } catch {
      setError(
        '登录失败：devgateway（:8090）或后端（:8080）未启动；若刚刷新过页面，请按下方提示清理浏览器 Service Worker 后重试',
      )
    } finally {
      setPending(false)
    }
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        void signIn()
      }}
      className="space-y-4"
    >
      <div className="space-y-1.5">
        <Label htmlFor="cloud-source">身份源（source）</Label>
        <Input
          id="cloud-source"
          value={source}
          onChange={(e) => setSource(e.target.value)}
          required
        />
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="cloud-subject">账号标识（subject）</Label>
        <Input
          id="cloud-subject"
          value={subject}
          onChange={(e) => setSubject(e.target.value)}
          placeholder="stable-account-id / bob / carol"
          required
        />
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="cloud-display">显示名</Label>
        <Input
          id="cloud-display"
          value={display}
          onChange={(e) => setDisplay(e.target.value)}
          required
        />
      </div>
      {error && <p className="text-xs text-destructive">{error}</p>}
      <Button type="submit" className="w-full" disabled={pending}>
        {pending ? '登录中…' : '连接后端登录'}
      </Button>
    </form>
  )
}

/** Demo sign-in against the mock store; any email works. */
function DemoSignInForm() {
  const navigate = useNavigate()
  const login = useLogin()
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

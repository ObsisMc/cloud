import { useState } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useLogin } from '@/features/auth/api'
import { db } from '@/mocks/data/store'
import { useAuthStore } from '@/state/auth-store'

export function LoginPage() {
  const token = useAuthStore((s) => s.token)
  const navigate = useNavigate()
  const login = useLogin()
  const [email, setEmail] = useState('ruihao053@gmail.com')

  if (token) return <Navigate to={`/${db.workspace.slug}/issues`} replace />

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    login.mutate(email, {
      onSuccess: () => navigate(`/${db.workspace.slug}/issues`),
    })
  }

  return (
    <div className="flex min-h-svh items-center justify-center bg-muted/30 px-4">
      <div className="w-full max-w-sm space-y-6">
        <div className="space-y-1 text-center">
          <div className="mx-auto flex size-10 items-center justify-center rounded-lg bg-primary text-primary-foreground font-semibold">
            O
          </div>
          <h1 className="text-lg font-semibold">登录 Ora</h1>
          <p className="text-sm text-muted-foreground">
            模拟环境 — 输入任意邮箱即可以演示账号身份登录。
          </p>
        </div>
        <form onSubmit={handleSubmit} className="space-y-4">
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
      </div>
    </div>
  )
}

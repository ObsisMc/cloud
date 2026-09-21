import { useState } from 'react'
import { Navigate, useSearchParams } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { startLogin } from '@/features/auth/api'
import { useSession } from '@/features/auth/session'
import { safeReturnTo } from '@/lib/paths'

/**
 * Sign-in screen. The only credential is the gateway session cookie, so the
 * page has nothing to collect: it starts the GitHub login and hands the
 * browser to the provider. A signed-in tab is sent straight to `returnTo`.
 */
export function LoginPage() {
  const { session } = useSession()
  const [params] = useSearchParams()
  const returnTo = safeReturnTo(params.get('returnTo'))
  const [pending, setPending] = useState(false)
  const [failed, setFailed] = useState(false)

  if (session.status === 'signed-in') return <Navigate to={returnTo} replace />

  async function signIn() {
    setFailed(false)
    setPending(true)
    try {
      await startLogin('github', returnTo)
    } catch {
      setFailed(true)
      setPending(false)
    }
  }

  return (
    <div className="flex min-h-svh items-center justify-center bg-muted/30 px-4">
      <div className="w-full max-w-sm space-y-6">
        <div className="space-y-1 text-center">
          <div className="mx-auto flex size-10 items-center justify-center rounded-lg bg-primary text-primary-foreground font-semibold">
            O
          </div>
          <h1 className="text-lg font-semibold">登录 Ora</h1>
          <p className="text-sm text-muted-foreground">使用 GitHub 账号继续。</p>
        </div>
        <Button
          type="button"
          className="w-full"
          disabled={pending || session.status === 'loading'}
          onClick={() => void signIn()}
        >
          {pending ? '正在跳转到 GitHub…' : '使用 GitHub 登录'}
        </Button>
        {failed && (
          <p className="text-center text-xs text-destructive">
            无法开始登录：认证网关不可用，请稍后重试
          </p>
        )}
      </div>
    </div>
  )
}

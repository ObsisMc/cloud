import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Navigate, useSearchParams } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { fetchLoginProviders, startLogin, type LoginProvider } from '@/features/auth/api'
import { useSession } from '@/features/auth/session'
import { safeReturnTo } from '@/lib/paths'

/** Query key of the provider list; it only changes with the gateway configuration. */
const PROVIDERS_QUERY_KEY = ['login-providers'] as const

const PROVIDER_LABELS: Record<LoginProvider, { idle: string; pending: string }> = {
  github: { idle: '使用 GitHub 登录', pending: '正在跳转到 GitHub…' },
  dev: { idle: '开发者登录（仅本地）', pending: '正在打开开发者登录…' },
}

/**
 * Sign-in screen. The only credential is the gateway session cookie, so the
 * page has nothing to collect: it asks the gateway which providers exist,
 * offers one button per provider, and hands the browser to the chosen one. A
 * signed-in tab is sent straight to `returnTo`.
 */
export function LoginPage() {
  const { session } = useSession()
  const [params] = useSearchParams()
  const returnTo = safeReturnTo(params.get('returnTo'))
  const providers = useQuery({
    queryKey: PROVIDERS_QUERY_KEY,
    queryFn: ({ signal }) => fetchLoginProviders(signal),
    retry: false,
    staleTime: Infinity,
  })
  const [pending, setPending] = useState<LoginProvider | null>(null)
  const [failed, setFailed] = useState(false)

  if (session.status === 'signed-in') return <Navigate to={returnTo} replace />

  async function signIn(provider: LoginProvider) {
    setFailed(false)
    setPending(provider)
    try {
      await startLogin(provider, returnTo)
    } catch {
      setFailed(true)
      setPending(null)
    }
  }

  const busy = pending !== null || session.status === 'loading'
  return (
    <div className="flex min-h-svh items-center justify-center bg-muted/30 px-4">
      <div className="w-full max-w-sm space-y-6">
        <div className="space-y-1 text-center">
          <div className="mx-auto flex size-10 items-center justify-center rounded-lg bg-primary text-primary-foreground font-semibold">
            O
          </div>
          <h1 className="text-lg font-semibold">登录 Ora</h1>
          <p className="text-sm text-muted-foreground">选择一种方式继续。</p>
        </div>
        <div className="space-y-2">
          {providers.data?.map((provider) => (
            <Button
              key={provider}
              type="button"
              variant={provider === 'github' ? 'default' : 'outline'}
              className="w-full"
              disabled={busy}
              onClick={() => void signIn(provider)}
            >
              {pending === provider
                ? PROVIDER_LABELS[provider].pending
                : PROVIDER_LABELS[provider].idle}
            </Button>
          ))}
        </div>
        {providers.data?.length === 0 && (
          <p className="text-center text-xs text-muted-foreground">认证网关未配置任何登录方式</p>
        )}
        {(failed || providers.isError) && (
          <p className="text-center text-xs text-destructive">
            无法开始登录：认证网关不可用，请稍后重试
          </p>
        )}
      </div>
    </div>
  )
}

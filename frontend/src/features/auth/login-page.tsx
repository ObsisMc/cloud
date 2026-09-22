import { useCallback, useEffect, useRef, useState } from 'react'
import { Navigate, useSearchParams } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { isExternalProvider, startLogin, type LoginProvider } from '@/features/auth/api'
import { useLoginProviders } from '@/features/auth/providers'
import { useSession } from '@/features/auth/session'
import { safeReturnTo } from '@/lib/paths'

const PROVIDER_LABELS: Record<LoginProvider, { idle: string; pending: string }> = {
  'huawei-idaas': { idle: '使用华为统一登录', pending: '正在跳转华为统一登录…' },
  github: { idle: '使用 GitHub 登录', pending: '正在跳转到 GitHub…' },
  dev: { idle: '开发者登录（仅本地）', pending: '正在打开开发者登录…' },
}

/**
 * The provider a deployment signs everyone in with, when there is nothing to
 * choose: exactly one provider and it is external. A gateway that also offers
 * the development form leaves the choice to the developer.
 */
function soleExternalProvider(providers: LoginProvider[] | undefined): LoginProvider | undefined {
  const [only] = providers ?? []
  return providers?.length === 1 && only && isExternalProvider(only) ? only : undefined
}

/**
 * Sign-in screen. The only credential is the gateway session cookie, so the
 * page has nothing to collect: it asks the gateway which providers exist and
 * hands the browser to one of them. With a single external provider (the
 * production shape) it starts that login by itself, once; otherwise it offers
 * one button per provider. A signed-in tab is sent straight to `returnTo`;
 * a disabled account is told so instead of being sent to log in again.
 */
export function LoginPage() {
  const { session, signOutOfGitHub } = useSession()
  const [params] = useSearchParams()
  const returnTo = safeReturnTo(params.get('returnTo'))
  const providers = useLoginProviders()
  const [pending, setPending] = useState<LoginProvider | null>(null)
  const [failed, setFailed] = useState(false)
  const autoStarted = useRef(false)

  const signIn = useCallback(
    async (provider: LoginProvider) => {
      setFailed(false)
      setPending(provider)
      try {
        await startLogin(provider, returnTo)
      } catch {
        setFailed(true)
        setPending(null)
      }
    },
    [returnTo],
  )

  const automatic = soleExternalProvider(providers.data)
  const canStart = session.status === 'signed-out'
  useEffect(() => {
    // Exactly one attempt: a failed start must show the retry button, never loop.
    if (!automatic || !canStart || autoStarted.current) return
    autoStarted.current = true
    void signIn(automatic)
  }, [automatic, canStart, signIn])

  if (session.status === 'signed-in') return <Navigate to={returnTo} replace />

  const disabled = session.status === 'disabled'
  const busy = pending !== null || session.status === 'loading'
  // While the automatic start is in flight nothing is offered; after it failed, the same
  // provider comes back as an explicit retry.
  const offered = disabled || (automatic && !failed) ? [] : (providers.data ?? [])
  return (
    <div className="flex min-h-svh items-center justify-center bg-muted/30 px-4">
      <div className="w-full max-w-sm space-y-6">
        <Heading disabled={disabled} automatic={automatic} pending={pending} />
        <ProviderButtons
          providers={offered}
          pending={pending}
          busy={busy}
          retry={automatic !== undefined}
          onSignIn={(provider) => void signIn(provider)}
        />
        {!disabled && providers.data?.includes('github') && (
          <p className="text-center text-xs text-muted-foreground">
            想换一个 GitHub 账号？
            <button
              type="button"
              className="ml-1 underline underline-offset-2 hover:text-foreground"
              disabled={busy}
              onClick={() => void signOutOfGitHub()}
            >
              先退出 GitHub
            </button>
          </p>
        )}
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

function Heading({
  disabled,
  automatic,
  pending,
}: {
  disabled: boolean
  automatic: LoginProvider | undefined
  pending: LoginProvider | null
}) {
  let title = '登录 Ora'
  let description = '选择一种方式继续。'
  if (disabled) {
    title = '账号已被停用'
    description = 'Cloud 已拒绝当前账号，请联系管理员恢复访问。'
  } else if (automatic) {
    if (pending) title = PROVIDER_LABELS[automatic].pending
    description = '登录成功后将自动返回。'
  }
  return (
    <div className="space-y-1 text-center">
      <div className="mx-auto flex size-10 items-center justify-center rounded-lg bg-primary text-primary-foreground font-semibold">
        O
      </div>
      <h1 className="text-lg font-semibold">{title}</h1>
      <p className="text-sm text-muted-foreground">{description}</p>
    </div>
  )
}

/** One button per offered provider; in retry mode the label says so instead of naming it. */
function ProviderButtons({
  providers,
  pending,
  busy,
  retry,
  onSignIn,
}: {
  providers: LoginProvider[]
  pending: LoginProvider | null
  busy: boolean
  retry: boolean
  onSignIn: (provider: LoginProvider) => void
}) {
  if (providers.length === 0) return null
  return (
    <div className="space-y-2">
      {providers.map((provider) => {
        let label = retry ? '重新登录' : PROVIDER_LABELS[provider].idle
        if (pending === provider) label = PROVIDER_LABELS[provider].pending
        return (
          <Button
            key={provider}
            type="button"
            variant={isExternalProvider(provider) ? 'default' : 'outline'}
            className="w-full"
            disabled={busy}
            onClick={() => onSignIn(provider)}
          >
            {label}
          </Button>
        )
      })}
    </div>
  )
}

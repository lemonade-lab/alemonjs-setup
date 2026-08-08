import { useStoreState } from '../store/guideStore'
import { useCallback, useEffect, type ReactNode } from 'react'
import { LockKeyhole, LogOut, UserRound, X } from 'lucide-react'
import { Button } from './Button'

type AuthStatus = { enabled: boolean; authenticated: boolean; account?: string }

async function authRequest(path: string, init?: RequestInit) {
  const controller = new AbortController()
  const timer = window.setTimeout(() => controller.abort(), 8000)
  try {
    const response = await fetch(`/api/v1/auth/${path}`, {
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) },
      signal: controller.signal,
      ...init
    })
    const data = (await response.json()) as AuthStatus & { error?: string }
    if (!response.ok) throw new Error(data.error || '身份认证操作未完成。')
    return data
  } finally {
    window.clearTimeout(timer)
  }
}

function notifyAuthChanged() {
  window.dispatchEvent(new Event('alx:auth-changed'))
}

async function readStatus() {
  return authRequest('status')
}

export function AuthGate({ children }: { children: ReactNode }) {
  const [status, setStatus] = useStoreState<AuthStatus | null>(null)
  const [loading, setLoading] = useStoreState(true)
  const [loadError, setLoadError] = useStoreState('')
  const [account, setAccount] = useStoreState('')
  const [password, setPassword] = useStoreState('')
  const [error, setError] = useStoreState('')
  const [busy, setBusy] = useStoreState(false)
  const refresh = useCallback(() => {
    setLoading(true)
    setLoadError('')
    void readStatus()
      .then(setStatus)
      .catch(reason => {
        setStatus(null)
        setLoadError(
          reason instanceof Error ? reason.message : '无法读取身份认证状态。'
        )
      })
      .finally(() => setLoading(false))
  }, [setLoadError, setLoading, setStatus])
  useEffect(() => {
    refresh()
    window.addEventListener('alx:auth-changed', refresh)
    return () => window.removeEventListener('alx:auth-changed', refresh)
  }, [refresh])
  const login = async () => {
    setBusy(true)
    setError('')
    try {
      await authRequest('login', {
        method: 'POST',
        body: JSON.stringify({ account, password })
      })
      setPassword('')
      notifyAuthChanged()
      refresh()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '登录未完成。')
    } finally {
      setBusy(false)
    }
  }
  if (loadError)
    return (
      <main className="auth-gate flex min-h-screen items-center justify-center p-5">
        <section className="grid w-full max-w-90 gap-3 rounded-xl border border-slate-200 bg-white p-6 text-center shadow-[0_18px_52px_rgb(28_26_23/0.12)] dark:border-slate-700 dark:bg-slate-900">
          <div className="grid gap-1">
            <strong className="text-sm text-slate-800 dark:text-slate-100">
              无法连接到后台服务
            </strong>
            <p className="mt-1 text-xs leading-5 text-slate-500 dark:text-slate-400">
              {loadError}。请确认 alx 后台仍在运行，然后重试。
            </p>
          </div>
          <button
            className="primary-button"
            onClick={refresh}
            disabled={loading}
          >
            {loading ? '重试中…' : '重试'}
          </button>
        </section>
      </main>
    )
  if (!status)
    return (
      <main className="auth-gate flex min-h-screen items-center justify-center p-5 text-sm text-slate-500">
        <span>正在读取身份认证状态…</span>
      </main>
    )
  if (!status.enabled || status.authenticated) return <>{children}</>
  return (
    <main className="auth-gate flex min-h-screen items-center justify-center p-5">
      <section className="grid w-full max-w-90 gap-3 rounded-xl border border-slate-200 bg-white p-6 shadow-[0_18px_52px_rgb(28_26_23/0.12)]">
        <LockKeyhole className="size-6 text-brand-600" />
        <div>
          <strong className="text-sm text-slate-800">身份认证</strong>
          <p className="mt-1 text-xs leading-5 text-slate-500">
            此 alx 服务已开启账户保护，请登录后继续。
          </p>
        </div>
        <label className="grid gap-1 text-xs font-semibold text-slate-600">
          账户
          <input
            className="min-h-9 rounded-md border border-slate-300 px-2.5 font-normal"
            autoComplete="username"
            value={account}
            onChange={event => setAccount(event.target.value)}
          />
        </label>
        <label className="grid gap-1 text-xs font-semibold text-slate-600">
          密码
          <input
            className="min-h-9 rounded-md border border-slate-300 px-2.5 font-normal"
            autoComplete="current-password"
            type="password"
            value={password}
            onChange={event => setPassword(event.target.value)}
            onKeyDown={event => {
              if (event.key === 'Enter') void login()
            }}
          />
        </label>
        {error && <small className="text-xs text-red-700">{error}</small>}
        <Button
          variant="primary"
          loading={busy}
          loadingLabel="正在登录…"
          disabled={!account || !password}
          onClick={() => void login()}
        >
          登录
        </Button>
      </section>
    </main>
  )
}

export function AuthControl() {
  const [status, setStatus] = useStoreState<AuthStatus | null>(null)
  const [open, setOpen] = useStoreState(false)
  const [account, setAccount] = useStoreState('')
  const [password, setPassword] = useStoreState('')
  const [confirmation, setConfirmation] = useStoreState('')
  const [error, setError] = useStoreState('')
  const [busy, setBusy] = useStoreState(false)
  const refresh = useCallback(() => {
    void readStatus()
      .then(setStatus)
      .catch(() => setStatus(null))
  }, [setStatus])
  useEffect(() => {
    refresh()
    window.addEventListener('alx:auth-changed', refresh)
    return () => window.removeEventListener('alx:auth-changed', refresh)
  }, [refresh])
  useEffect(() => {
    const closeWhenAnotherToolOpens = (event: Event) => {
      if ((event as CustomEvent<string>).detail !== 'auth') setOpen(false)
    }
    window.addEventListener('alx:top-tool-open', closeWhenAnotherToolOpens)
    return () =>
      window.removeEventListener('alx:top-tool-open', closeWhenAnotherToolOpens)
  }, [setOpen])
  const enable = async () => {
    setBusy(true)
    setError('')
    try {
      await authRequest('setup', {
        method: 'POST',
        body: JSON.stringify({ account, password, confirmation })
      })
      setPassword('')
      setConfirmation('')
      setOpen(false)
      notifyAuthChanged()
      refresh()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '身份认证未开启。')
    } finally {
      setBusy(false)
    }
  }
  const logout = async () => {
    await authRequest('logout', { method: 'POST' })
    notifyAuthChanged()
    refresh()
  }
  return (
    <div className="relative">
      <Button
        variant="icon"
        className={
          status?.enabled ? 'border-brand-100 bg-brand-50 text-brand-600' : ''
        }
        onClick={() =>
          setOpen(value => {
            const next = !value
            if (next)
              window.dispatchEvent(
                new CustomEvent('alx:top-tool-open', { detail: 'auth' })
              )
            return next
          })
        }
        aria-label="身份认证"
        aria-expanded={open}
        title="身份认证"
      >
        <LockKeyhole className="size-4" />
      </Button>
      {open && (
        <section
          className="topbar-popover absolute right-0 top-[calc(100%+8px)] z-50 grid w-[min(22.5rem,calc(100vw-2rem))] max-h-[calc(100vh-5rem)] gap-2.5 overflow-y-auto rounded-xl border border-slate-200 bg-white p-3 shadow-[0_18px_42px_rgb(28_26_23/0.13)]"
          role="dialog"
          aria-label="身份认证"
          onKeyDown={event => {
            if (event.key === 'Escape') setOpen(false)
          }}
        >
          <header className="flex items-center justify-between">
            <strong className="text-xs text-slate-800">
              {status?.enabled ? '身份认证已开启' : '开启身份认证'}
            </strong>
            <Button
              variant="icon"
              className="topbar-popover-close size-6"
              onClick={() => setOpen(false)}
              aria-label="关闭身份认证"
            >
              <X className="size-4" />
            </Button>
          </header>
          {status?.enabled ? (
            <>
              <p className="m-0 text-xs leading-5 text-slate-500">
                当前账户：
                <b className="text-slate-700">{status.account || '已登录'}</b>
              </p>
              <Button
                variant="secondary"
                className="gap-1.5 justify-self-start"
                onClick={() => void logout()}
              >
                <LogOut className="size-3.5" />
                退出登录
              </Button>
            </>
          ) : (
            <>
              <p className="m-0 text-xs leading-5 text-slate-500">
                开启后，访问本机管理 API 前必须登录。
              </p>
              <label className="grid gap-1 text-[11px] font-semibold text-slate-600">
                账户
                <input
                  className="min-h-8 w-full rounded-md border border-slate-300 px-2 font-normal"
                  autoComplete="username"
                  value={account}
                  onChange={event => setAccount(event.target.value)}
                />
              </label>
              <label className="grid gap-1 text-[11px] font-semibold text-slate-600">
                密码
                <input
                  className="min-h-8 w-full rounded-md border border-slate-300 px-2 font-normal"
                  autoComplete="new-password"
                  type="password"
                  value={password}
                  onChange={event => setPassword(event.target.value)}
                />
              </label>
              <label className="grid gap-1 text-[11px] font-semibold text-slate-600">
                确认密码
                <input
                  className="min-h-8 w-full rounded-md border border-slate-300 px-2 font-normal"
                  autoComplete="new-password"
                  type="password"
                  value={confirmation}
                  onChange={event => setConfirmation(event.target.value)}
                />
              </label>
              {error && (
                <small className="text-[11px] text-amber-700">{error}</small>
              )}
              <Button
                variant="primary"
                className="gap-1.5"
                loading={busy}
                loadingLabel="正在开启…"
                disabled={!account || !password || !confirmation}
                onClick={() => void enable()}
              >
                <UserRound className="size-3.5" />
                开启身份认证
              </Button>
            </>
          )}
        </section>
      )}
    </div>
  )
}

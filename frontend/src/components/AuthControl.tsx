import { useEffect, useState, type ReactNode } from 'react'
import { LockKeyhole, LogOut, UserRound, X } from 'lucide-react'
import { Button } from './Button'

type AuthStatus = { enabled: boolean; authenticated: boolean; account?: string }

async function authRequest(path: string, init?: RequestInit) {
  const response = await fetch(`/api/v1/auth/${path}`, {
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) },
    ...init
  })
  const data = (await response.json()) as AuthStatus & { error?: string }
  if (!response.ok) throw new Error(data.error || '身份认证操作未完成。')
  return data
}

async function readStatus() {
  return authRequest('status')
}

function notifyAuthChanged() {
  window.dispatchEvent(new Event('albs:auth-changed'))
}

export function AuthGate({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AuthStatus | null>(null)
  const [account, setAccount] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const refresh = () => {
    void readStatus()
      .then(setStatus)
      .catch(reason =>
        setError(
          reason instanceof Error ? reason.message : '无法读取身份认证状态。'
        )
      )
  }
  useEffect(() => {
    refresh()
    window.addEventListener('albs:auth-changed', refresh)
    return () => window.removeEventListener('albs:auth-changed', refresh)
  }, [])
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
  if (!status)
    return (
      <main className="flex min-h-screen items-center justify-center bg-[radial-gradient(800px_360px_at_50%_0,#e8faf7,#f7f8fa_68%)] p-5 text-sm text-slate-500">
        <span>正在读取身份认证状态…</span>
      </main>
    )
  if (!status.enabled || status.authenticated) return <>{children}</>
  return (
    <main className="flex min-h-screen items-center justify-center bg-[radial-gradient(800px_360px_at_50%_0,#e8faf7,#f7f8fa_68%)] p-5">
      <section className="grid w-full max-w-[360px] gap-3 rounded-xl border border-slate-200 bg-white p-6 shadow-[0_18px_52px_rgb(15_23_42/0.12)]">
        <LockKeyhole className="size-6 text-brand-600" />
        <div>
          <strong className="text-sm text-slate-800">身份认证</strong>
          <p className="mt-1 text-xs leading-5 text-slate-500">
            此 albs 服务已开启账户保护，请登录后继续。
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
  const [status, setStatus] = useState<AuthStatus | null>(null)
  const [open, setOpen] = useState(false)
  const [account, setAccount] = useState('')
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const refresh = () => {
    void readStatus()
      .then(setStatus)
      .catch(() => setStatus(null))
  }
  useEffect(() => {
    refresh()
    window.addEventListener('albs:auth-changed', refresh)
    return () => window.removeEventListener('albs:auth-changed', refresh)
  }, [])
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
        className={status?.enabled ? 'border-brand-100 bg-brand-50 text-brand-600' : ''}
        onClick={() => setOpen(value => !value)}
        aria-label="身份认证"
        title="身份认证"
      >
        <LockKeyhole className="size-4" />
      </Button>
      {open && (
        <section className="absolute left-0 top-10 z-30 grid min-w-[260px] gap-2.5 rounded-xl border border-slate-200 bg-white p-3 shadow-[0_18px_42px_rgb(15_23_42/0.13)]">
          <header className="flex items-center justify-between">
            <strong className="text-xs text-slate-800">
              {status?.enabled ? '身份认证已开启' : '开启身份认证'}
            </strong>
            <Button
              variant="icon"
              className="size-6 border-transparent bg-transparent text-slate-400 hover:bg-slate-100"
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
                  className="min-h-8 rounded-md border border-slate-300 px-2 font-normal"
                  autoComplete="username"
                  value={account}
                  onChange={event => setAccount(event.target.value)}
                />
              </label>
              <label className="grid gap-1 text-[11px] font-semibold text-slate-600">
                密码
                <input
                  className="min-h-8 rounded-md border border-slate-300 px-2 font-normal"
                  autoComplete="new-password"
                  type="password"
                  value={password}
                  onChange={event => setPassword(event.target.value)}
                />
              </label>
              <label className="grid gap-1 text-[11px] font-semibold text-slate-600">
                确认密码
                <input
                  className="min-h-8 rounded-md border border-slate-300 px-2 font-normal"
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

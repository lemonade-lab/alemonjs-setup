import { useEffect, useState, type ReactNode } from 'react'
import { LockKeyhole, LogOut, UserRound, X } from 'lucide-react'

type AuthStatus = { enabled: boolean; authenticated: boolean; account?: string }

async function authRequest(path: string, init?: RequestInit) {
  const response = await fetch(`/api/v1/auth/${path}`, {
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) },
    ...init
  })
  const data = await response.json() as AuthStatus & { error?: string }
  if (!response.ok) throw new Error(data.error || '身份认证操作未完成。')
  return data
}

async function readStatus() { return authRequest('status') }

function notifyAuthChanged() { window.dispatchEvent(new Event('albs:auth-changed')) }

export function AuthGate({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AuthStatus | null>(null)
  const [account, setAccount] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const refresh = () => { void readStatus().then(setStatus).catch(reason => setError(reason instanceof Error ? reason.message : '无法读取身份认证状态。')) }
  useEffect(() => {
    refresh()
    window.addEventListener('albs:auth-changed', refresh)
    return () => window.removeEventListener('albs:auth-changed', refresh)
  }, [])
  const login = async () => {
    setBusy(true); setError('')
    try {
      await authRequest('login', { method: 'POST', body: JSON.stringify({ account, password }) })
      setPassword(''); notifyAuthChanged(); refresh()
    } catch (reason) { setError(reason instanceof Error ? reason.message : '登录未完成。') } finally { setBusy(false) }
  }
  if (!status) return <main className="auth-gate"><span>正在读取身份认证状态…</span></main>
  if (!status.enabled || status.authenticated) return <>{children}</>
  return <main className="auth-gate"><section className="auth-login-card"><LockKeyhole /><div><strong>身份认证</strong><p>此 albs 服务已开启账户保护，请登录后继续。</p></div><label>账户<input autoComplete="username" value={account} onChange={event => setAccount(event.target.value)} /></label><label>密码<input autoComplete="current-password" type="password" value={password} onChange={event => setPassword(event.target.value)} onKeyDown={event => { if (event.key === 'Enter') void login() }} /></label>{error && <small>{error}</small>}<button className="primary-button" disabled={busy || !account || !password} onClick={() => void login()}>{busy ? '正在登录…' : '登录'}</button></section></main>
}

export function AuthControl() {
  const [status, setStatus] = useState<AuthStatus | null>(null)
  const [open, setOpen] = useState(false)
  const [account, setAccount] = useState('')
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const refresh = () => { void readStatus().then(setStatus).catch(() => setStatus(null)) }
  useEffect(() => { refresh(); window.addEventListener('albs:auth-changed', refresh); return () => window.removeEventListener('albs:auth-changed', refresh) }, [])
  const enable = async () => {
    setBusy(true); setError('')
    try {
      await authRequest('setup', { method: 'POST', body: JSON.stringify({ account, password, confirmation }) })
      setPassword(''); setConfirmation(''); setOpen(false); notifyAuthChanged(); refresh()
    } catch (reason) { setError(reason instanceof Error ? reason.message : '身份认证未开启。') } finally { setBusy(false) }
  }
  const logout = async () => {
    await authRequest('logout', { method: 'POST' })
    notifyAuthChanged(); refresh()
  }
  return <div className="auth-control"><button className={`auth-control-button ${status?.enabled ? 'enabled' : ''}`} onClick={() => setOpen(value => !value)} aria-label="身份认证" title="身份认证"><LockKeyhole /></button>{open && <section className="auth-popover"><header><strong>{status?.enabled ? '身份认证已开启' : '开启身份认证'}</strong><button onClick={() => setOpen(false)} aria-label="关闭身份认证"><X /></button></header>{status?.enabled ? <><p>当前账户：<b>{status.account || '已登录'}</b></p><button className="secondary-button" onClick={() => void logout()}><LogOut /> 退出登录</button></> : <><p>开启后，访问本机管理 API 前必须登录。</p><label>账户<input autoComplete="username" value={account} onChange={event => setAccount(event.target.value)} /></label><label>密码<input autoComplete="new-password" type="password" value={password} onChange={event => setPassword(event.target.value)} /></label><label>确认密码<input autoComplete="new-password" type="password" value={confirmation} onChange={event => setConfirmation(event.target.value)} /></label>{error && <small>{error}</small>}<button className="primary-button" disabled={busy || !account || !password || !confirmation} onClick={() => void enable()}><UserRound /> {busy ? '正在开启…' : '开启身份认证'}</button></>}</section>}</div>
}

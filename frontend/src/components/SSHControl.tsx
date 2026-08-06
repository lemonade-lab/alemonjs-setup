import { Copy, KeyRound, Plus, RefreshCw, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { ConfirmDialog } from './ConfirmDialog'

type SSHKey = { name: string; value: string }

export function SSHControl() {
  const [open, setOpen] = useState(false)
  const [keys, setKeys] = useState<SSHKey[]>([])
  const [loading, setLoading] = useState(false)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const [confirmGenerate, setConfirmGenerate] = useState(false)
  const load = async () => {
    setLoading(true)
    try {
      const response = await fetch('/api/v1/system/ssh')
      const data = await response.json() as { keys?: SSHKey[]; error?: string }
      if (!response.ok) throw new Error(data.error || '无法读取 SSH 公钥。')
      setKeys(data.keys ?? [])
      setMessage('')
    } catch (reason) { setMessage(reason instanceof Error ? reason.message : '无法读取 SSH 公钥。') } finally { setLoading(false) }
  }
  useEffect(() => { if (open) void load() }, [open])
  const generate = async () => {
    setBusy(true)
    try {
      const response = await fetch('/api/v1/system/ssh', { method: 'POST' })
      const data = await response.json() as SSHKey & { error?: string }
      if (!response.ok) throw new Error(data.error || '生成 SSH 密钥失败。')
      setKeys([data])
      setMessage('已生成 Ed25519 SSH 密钥。')
    } catch (reason) { setMessage(reason instanceof Error ? reason.message : '生成 SSH 密钥失败。') } finally { setBusy(false) }
  }
  const copy = async (value: string) => { try { await navigator.clipboard.writeText(value); setMessage('公钥已复制。') } catch { setMessage('复制失败，请手动复制公钥。') } }
  return <div className="ssh-control"><button className="ssh-control-button" onClick={() => setOpen(value => !value)} aria-label="SSH 管理" title="SSH 管理"><KeyRound /></button>{open && <section className="ssh-popover"><header><div><strong>SSH 管理</strong><span>仅展示公钥，不读取私钥。</span></div><button onClick={() => setOpen(false)} aria-label="关闭"><X /></button></header>{loading ? <p>正在读取 SSH 公钥…</p> : keys.length ? <div className="ssh-keys">{keys.map(key => <article key={key.name}><strong>{key.name}</strong><code>{key.value}</code><button className="secondary-button" onClick={() => void copy(key.value)}><Copy /> 复制公钥</button></article>)}</div> : <section className="ssh-empty"><strong>还没有 SSH 公钥</strong><span>生成后可添加到 GitHub、Gitee 等代码托管平台。</span><button className="primary-button" disabled={busy} onClick={() => setConfirmGenerate(true)}><Plus /> 生成 Ed25519 密钥</button></section>}{message && <small className={message.includes('失败') || message.includes('无法') ? 'ssh-error' : ''}>{message}</small>}<footer><button className="icon-button" onClick={() => void load()} disabled={loading} aria-label="刷新 SSH 公钥" title="刷新"><RefreshCw /></button></footer></section>}<ConfirmDialog open={confirmGenerate} title="生成 SSH 密钥" subtitle="将只在本机生成一对 Ed25519 密钥。私钥不会上传、展示或写入项目。" message="生成后请复制公钥并添加到 GitHub、Gitee 等代码托管平台，才能使用 SSH 地址拉取或推送仓库。" confirmLabel="生成密钥" busy={busy} onCancel={() => setConfirmGenerate(false)} onConfirm={() => { setConfirmGenerate(false); void generate() }} /></div>
}

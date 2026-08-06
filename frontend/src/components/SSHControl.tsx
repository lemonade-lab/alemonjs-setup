import { Copy, ExternalLink, KeyRound, Plus, RefreshCw, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { ConfirmDialog } from './ConfirmDialog'
import { Button } from './Button'

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
      const data = (await response.json()) as {
        keys?: SSHKey[]
        error?: string
      }
      if (!response.ok) throw new Error(data.error || '无法读取 SSH 公钥。')
      setKeys(data.keys ?? [])
      setMessage('')
    } catch (reason) {
      setMessage(
        reason instanceof Error ? reason.message : '无法读取 SSH 公钥。'
      )
    } finally {
      setLoading(false)
    }
  }
  useEffect(() => {
    if (open) void load()
  }, [open])
  useEffect(() => {
    const closeWhenAnotherToolOpens = (event: Event) => {
      if ((event as CustomEvent<string>).detail !== 'ssh') setOpen(false)
    }
    window.addEventListener('alx:top-tool-open', closeWhenAnotherToolOpens)
    return () =>
      window.removeEventListener('alx:top-tool-open', closeWhenAnotherToolOpens)
  }, [])
  const generate = async () => {
    setBusy(true)
    try {
      const response = await fetch('/api/v1/system/ssh', { method: 'POST' })
      const data = (await response.json()) as SSHKey & { error?: string }
      if (!response.ok) throw new Error(data.error || '生成 SSH 密钥失败。')
      setKeys([data])
      setMessage('已生成 Ed25519 SSH 密钥。')
    } catch (reason) {
      setMessage(
        reason instanceof Error ? reason.message : '生成 SSH 密钥失败。'
      )
    } finally {
      setBusy(false)
    }
  }
  const copy = async (value: string) => {
    try {
      await navigator.clipboard.writeText(value)
      setMessage('公钥已复制。')
    } catch {
      setMessage('复制失败，请手动复制公钥。')
    }
  }
  return (
    <div className="relative">
      <Button
        variant="icon"
        onClick={() =>
          setOpen(value => {
            const next = !value
            if (next)
              window.dispatchEvent(
                new CustomEvent('alx:top-tool-open', { detail: 'ssh' })
              )
            return next
          })
        }
        aria-label="SSH 管理"
        aria-expanded={open}
        title="SSH 管理"
      >
        <KeyRound className="size-4" />
      </Button>
      {open && (
        <section
          className="absolute right-0 top-[calc(100%+8px)] z-50 grid w-[min(24rem,calc(100vw-2rem))] max-h-[calc(100vh-5rem)] gap-2.5 overflow-y-auto rounded-xl border border-slate-200 bg-white p-3 shadow-[0_18px_42px_rgb(28_26_23/0.13)]"
          onKeyDown={event => {
            if (event.key === 'Escape') setOpen(false)
          }}
        >
          <header className="flex items-start justify-between gap-3">
            <div className="grid gap-0.5">
              <strong className="text-xs text-slate-800">SSH 管理</strong>
            </div>
            <Button
              variant="icon"
              className="size-6 border-transparent bg-transparent text-slate-400 hover:bg-slate-100"
              onClick={() => setOpen(false)}
              aria-label="关闭"
            >
              <X className="size-4" />
            </Button>
          </header>
          {loading ? (
            <p className="m-0 text-xs text-slate-500">正在读取 SSH 公钥…</p>
          ) : keys.length ? (
            <div className="grid gap-2">
              {keys.map(key => (
                <article
                  className="flex items-center justify-between rounded-lg border border-slate-200 bg-slate-50 p-2.5"
                  key={key.name}
                >
                  <strong className="text-xs text-slate-700">{key.name}</strong>
                  <Button
                    variant="secondary"
                    className="gap-1.5 justify-self-end"
                    onClick={() => void copy(key.value)}
                  >
                    <Copy className="size-3.5" />
                    复制公钥
                  </Button>
                </article>
              ))}
            </div>
          ) : (
            <section className="grid gap-1.5 rounded-lg border border-dashed border-slate-300 bg-slate-50 p-3.5 text-center">
              <strong className="text-xs text-slate-700">
                还没有 SSH 公钥
              </strong>
              <span className="text-[11px] leading-4 text-slate-500">
                生成后可添加到 GitHub、Gitee 等代码托管平台。
              </span>
              <Button
                variant="primary"
                className="gap-1.5 justify-self-end"
                disabled={busy}
                onClick={() => setConfirmGenerate(true)}
              >
                <Plus className="size-3.5" />
                生成 Ed25519 密钥
              </Button>
            </section>
          )}
          <section
            className="grid gap-1.5 border-t border-slate-100 pt-2.5"
            aria-label="添加 SSH 公钥教程"
          >
            <strong className="text-xs text-slate-700">
              下一步：添加公钥到代码平台
            </strong>
            <span className="text-[11px] leading-4 text-slate-500">
              粘贴公钥并保存后，即可使用 SSH 克隆和推送。
            </span>
            <div className="flex gap-2">
              <a
                className="inline-flex items-center gap-1 text-[11px] font-semibold text-brand-600 hover:underline"
                href="https://github.com/settings/keys"
                target="_blank"
                rel="noreferrer"
              >
                GitHub 添加公钥 <ExternalLink className="size-3" />
              </a>
              <a
                className="text-[11px] font-semibold text-brand-600 hover:underline"
                href="https://docs.github.com/en/authentication/connecting-to-github-with-ssh/adding-a-new-ssh-key-to-your-github-account"
                target="_blank"
                rel="noreferrer"
              >
                教程
              </a>
            </div>
            <div className="flex gap-2">
              <a
                className="inline-flex items-center gap-1 text-[11px] font-semibold text-brand-600 hover:underline"
                href="https://gitee.com/profile/sshkeys"
                target="_blank"
                rel="noreferrer"
              >
                Gitee 添加公钥 <ExternalLink className="size-3" />
              </a>
              <a
                className="text-[11px] font-semibold text-brand-600 hover:underline"
                href="https://gitee.com/help/articles/4181"
                target="_blank"
                rel="noreferrer"
              >
                教程
              </a>
            </div>
          </section>
          {message && (
            <small
              className={`text-[11px] ${message.includes('失败') || message.includes('无法') ? 'text-orange-700' : 'text-slate-500'}`}
            >
              {message}
            </small>
          )}
          <footer className="flex justify-end border-t border-slate-100 pt-2">
            <Button
              variant="icon"
              onClick={() => void load()}
              disabled={loading}
              aria-label="刷新 SSH 公钥"
              title="刷新"
            >
              <RefreshCw className="size-4" />
            </Button>
          </footer>
        </section>
      )}
      <ConfirmDialog
        open={confirmGenerate}
        title="生成 SSH 密钥"
        subtitle="将只在本机生成一对 Ed25519 密钥。私钥不会上传、展示或写入项目。"
        message="生成后请复制公钥并添加到 GitHub、Gitee 等代码托管平台，才能使用 SSH 地址拉取或推送仓库。"
        confirmLabel="生成密钥"
        busy={busy}
        onCancel={() => setConfirmGenerate(false)}
        onConfirm={() => {
          setConfirmGenerate(false)
          void generate()
        }}
      />
    </div>
  )
}

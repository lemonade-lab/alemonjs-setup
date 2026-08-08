import {
  CheckCircle2,
  Download,
  ExternalLink,
  FileArchive,
  RefreshCw,
  Upload,
  X
} from 'lucide-react'
import { useEffect, useId, useState } from 'react'
import { ConfirmDialog } from './ConfirmDialog'
import { Tabs } from './Tabs'
import {
  useLazySetupUpdateQuery,
  useReleasesQuery
} from '../store/workspaceApi'
import { Button } from './Button'

type Release = {
  tag: string
  name: string
  url: string
  assets: Array<{ name: string; url: string }>
}

export function SetupUpdateButton() {
  const [check, { data, isFetching, error }] = useLazySetupUpdateQuery()
  const [open, setOpen] = useState(false)
  const [mode, setMode] = useState<'now' | 'manual'>('now')
  const [releaseURL, setReleaseURL] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const [confirmRestart, setConfirmRestart] = useState(false)
  useEffect(() => {
    const closeWhenAnotherToolOpens = (event: Event) => {
      if ((event as CustomEvent<string>).detail !== 'update') setOpen(false)
    }
    window.addEventListener('alx:top-tool-open', closeWhenAnotherToolOpens)
    return () =>
      window.removeEventListener('alx:top-tool-open', closeWhenAnotherToolOpens)
  }, [])
  const uploadInputID = useId()
  const { data: releaseData = [] } = useReleasesQuery('alemonx', {
    skip: !open || mode !== 'manual'
  })
  const releases = releaseData as Release[]
  const selected = releases.find(item => item.url === releaseURL) ?? releases[0]

  // Only version discovery is automatic. Downloading and replacing the
  // executable still require an explicit user confirmation.
  useEffect(() => {
    void check()
    const interval = window.setInterval(() => void check(), 6 * 60 * 60 * 1000)
    return () => window.clearInterval(interval)
  }, [check])

  const api = async (path: string, options: RequestInit) => {
    const response = await fetch(path, options)
    const result = (await response.json()) as {
      output?: string
      error?: string
    }
    if (!response.ok) throw new Error(result.error || '操作未完成。')
    return result
  }

  const reconnectAfterRestart = () => {
    const deadline = Date.now() + 40_000
    const retry = () => {
      void fetch('/api/v1/auth/status', { cache: 'no-store' })
        .then(response => {
          if (response.ok) {
            window.location.reload()
            return
          }
          throw new Error('应用仍在重启')
        })
        .catch(() => {
          if (Date.now() < deadline) window.setTimeout(retry, 500)
          else window.location.reload()
        })
    }
    // Give the old server enough time to send the update response and exit;
    // Windows may then need several retries before its executable is unlocked.
    window.setTimeout(retry, 900)
  }

  const download = async () => {
    setBusy(true)
    setMessage('')
    try {
      const result = await api('/api/v1/update/download', { method: 'POST' })
      setMessage(result.output || '更新包已下载完成。')
      await check()
      setConfirmRestart(true)
    } catch (reason) {
      setMessage(reason instanceof Error ? reason.message : '下载更新失败。')
    } finally {
      setBusy(false)
    }
  }

  const applyAndRestart = async () => {
    setBusy(true)
    setMessage('')
    try {
      await api('/api/v1/update/apply', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirm: true })
      })
      setMessage('正在重启应用…')
      reconnectAfterRestart()
    } catch (reason) {
      setMessage(reason instanceof Error ? reason.message : '更新重启失败。')
      setBusy(false)
    }
  }

  const upload = async () => {
    if (!file) return
    setBusy(true)
    setMessage('')
    try {
      const form = new FormData()
      form.append('package', file)
      form.append('confirm', 'true')
      const result = await api('/api/v1/update/load', {
        method: 'POST',
        body: form
      })
      setMessage(result.output || '正在重启应用…')
      reconnectAfterRestart()
    } catch (reason) {
      setMessage(reason instanceof Error ? reason.message : '载入更新失败。')
    } finally {
      setBusy(false)
    }
  }

  const openPanel = () => {
    window.dispatchEvent(
      new CustomEvent('alx:top-tool-open', { detail: 'update' })
    )
    setOpen(true)
    setMode('now')
    setMessage('')
    setConfirmRestart(false)
    void check()
  }

  return (
    <div className="relative">
      <button
        className={`inline-flex size-8 items-center justify-center rounded-md border transition disabled:cursor-wait disabled:opacity-70 ${
          data?.available
            ? 'border-brand-100 bg-brand-50 text-brand-600 hover:bg-brand-100'
            : 'border-transparent text-slate-500 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800'
        }`}
        onClick={openPanel}
        disabled={isFetching}
        aria-label="检查应用更新"
        title={
          isFetching
            ? '正在检查更新'
            : data?.available
              ? '发现可用更新'
              : '检查更新'
        }
      >
        <RefreshCw className="size-4" />
      </button>
      {open && (
        <section
          className="topbar-popover absolute left-0 top-[calc(100%+8px)] z-50 grid w-80 gap-2.5 rounded-xl border border-slate-200 bg-white p-3 shadow-[0_18px_42px_rgb(28_26_23/0.13)]"
          role="dialog"
          aria-label="应用更新"
        >
          <header className="flex items-start justify-between gap-3">
            <div className="flex items-center gap-2.5">
              <i className="inline-flex size-8 items-center justify-center rounded-lg bg-brand-50 text-brand-600">
                <RefreshCw className="size-4" />
              </i>
              <span className="grid gap-0.5">
                <strong className="text-sm text-ink-950">应用更新</strong>
                <small className="text-[11px] text-slate-400">
                  保持 ALemonX 为最新版本
                </small>
              </span>
            </div>
            <button
              className="topbar-popover-close"
              onClick={() => setOpen(false)}
              aria-label="关闭更新提示"
            >
              <X className="size-4" />
            </button>
          </header>
          <Tabs
            ariaLabel="更新方式"
            value={mode}
            onChange={setMode}
            variant="segmented"
            className="grid grid-cols-2"
            items={[
              { id: 'now', label: '自动更新' },
              { id: 'manual', label: '手动安装' }
            ]}
          />
          {mode === 'now' && (
            <section className="grid gap-2.5">
              {isFetching ? (
                <p className="rounded-lg border border-slate-200 bg-slate-50 p-2.5 text-xs leading-5 text-slate-500">
                  正在检查更新…
                </p>
              ) : error ? (
                <p className="rounded-lg border border-slate-200 bg-slate-50 p-2.5 text-xs leading-5 text-slate-500">
                  暂时无法检查更新，请稍后重试。
                </p>
              ) : data?.available ? (
                <>
                  <div className="flex items-center gap-2.5 rounded-lg border border-brand-100 bg-brand-50 p-3">
                    <i className="inline-flex size-8 items-center justify-center rounded-md bg-white text-brand-600">
                      <Download className="size-4" />
                    </i>
                    <span className="grid gap-0.5">
                      <small className="text-[11px] text-brand-700/70">
                        发现新版本
                      </small>
                      <strong className="text-sm text-brand-700">
                        {data.latest}
                      </strong>
                    </span>
                  </div>
                  {data.platformMatched && data.integrityReady ? (
                    <Button
                      className="inline-flex min-h-9 justify-self-end rounded-md bg-brand-600 px-3 text-xs font-semibold text-white transition hover:bg-brand-700 disabled:opacity-60"
                      disabled={busy}
                      onClick={() => {
                        if (data.downloadReady) setConfirmRestart(true)
                        else void download()
                      }}
                    >
                      {busy
                        ? '正在下载…'
                        : data.downloadReady
                          ? '更新并重启'
                          : '下载更新'}
                    </Button>
                  ) : (
                    <p className="rounded-lg border border-amber-200 bg-amber-50 p-2.5 text-xs leading-5 text-amber-800">
                      {data.platformMatched
                        ? '该版本未提供校验文件，无法自动更新；请切换到「手动安装」。'
                        : '当前系统没有匹配的更新包，无法自动更新；请切换到「手动安装」。'}
                    </p>
                  )}
                </>
              ) : (
                <div className="flex items-center gap-2.5 rounded-lg border border-slate-200 bg-slate-50 p-3">
                  <i className="inline-flex size-8 items-center justify-center rounded-md bg-white text-slate-500">
                    <CheckCircle2 className="size-4" />
                  </i>
                  <span className="grid gap-0.5">
                    <small className="text-[11px] text-slate-500">
                      当前版本
                    </small>
                    <strong className="text-sm text-slate-700">
                      {data?.current || '已是最新'}
                    </strong>
                  </span>
                </div>
              )}
            </section>
          )}
          {mode === 'manual' && (
            <section className="grid gap-2.5">
              <header className="grid gap-0.5">
                <strong className="text-xs text-slate-700">选择更新包</strong>
                <small className="text-[11px] leading-4 text-slate-500">
                  下载后，导入文件即可完成安装。
                </small>
              </header>
              <label className="grid gap-1 text-[11px] font-semibold text-slate-500">
                版本
                <select
                  className="min-h-9 rounded-md border border-slate-200 bg-white px-2 text-xs font-normal text-slate-700"
                  value={releaseURL || selected?.url || ''}
                  onChange={event => setReleaseURL(event.target.value)}
                >
                  {releases.map(item => (
                    <option key={item.url} value={item.url}>
                      {item.tag} · {item.name}
                    </option>
                  ))}
                </select>
              </label>
              <div className="overflow-hidden rounded-lg border border-slate-200">
                {selected?.assets.map(asset => (
                  <a
                    className="flex min-h-8 items-center gap-2 border-b border-slate-100 px-2.5 text-xs text-brand-600 last:border-0 hover:bg-brand-50"
                    key={asset.url}
                    href={asset.url}
                    target="_blank"
                    rel="noreferrer"
                  >
                    <Download className="size-3.5 shrink-0" />
                    <span className="min-w-0 truncate">{asset.name}</span>
                    <ExternalLink className="ml-auto size-3.5 shrink-0 text-slate-400" />
                  </a>
                ))}
              </div>
              <section
                className="grid gap-2.5"
                onDragOver={event => event.preventDefault()}
                onDrop={event => {
                  event.preventDefault()
                  setFile(event.dataTransfer.files[0] ?? null)
                }}
              >
                <input
                  className="sr-only"
                  id={uploadInputID}
                  type="file"
                  accept=".zip,.tgz,.tar.gz"
                  onChange={event => setFile(event.target.files?.[0] ?? null)}
                />
                <label
                  htmlFor={uploadInputID}
                  className="flex cursor-pointer items-center gap-2.5 rounded-lg border border-dashed border-brand-200 bg-brand-50/40 p-3 text-left transition hover:border-brand-600 hover:bg-brand-50"
                >
                  <i className="inline-flex size-8 shrink-0 items-center justify-center rounded-md bg-brand-50 text-brand-600">
                    {file ? (
                      <FileArchive className="size-4" />
                    ) : (
                      <Upload className="size-4" />
                    )}
                  </i>
                  <span className="grid min-w-0 gap-0.5">
                    <strong className="truncate text-xs text-slate-700">
                      {file ? file.name : '选择更新包'}
                    </strong>
                    <small className="text-[11px] leading-4 text-slate-500">
                      {file
                        ? '已选择，可开始安装。'
                        : '也可将 .zip 或 .tgz 文件拖到这里。'}
                    </small>
                  </span>
                </label>
                <Button disabled={!file || busy} onClick={() => void upload()}>
                  {busy ? '正在安装…' : '安装更新'}
                </Button>
                {message && (
                  <small className="rounded-md bg-slate-50 p-2 text-[11px] leading-4 text-slate-500">
                    {message}
                  </small>
                )}
              </section>
            </section>
          )}
          {mode === 'now' && message && (
            <small className="rounded-md bg-slate-50 p-2 text-[11px] leading-4 text-slate-500">
              {message}
            </small>
          )}
          {data?.releaseUrl && (
            <a
              className="inline-flex items-center gap-1 text-[11px] text-slate-500 hover:text-brand-600"
              href={data.releaseUrl}
              target="_blank"
              rel="noreferrer"
            >
              查看发布说明 <ExternalLink className="size-3" />
            </a>
          )}
        </section>
      )}
      <ConfirmDialog
        open={confirmRestart}
        title="立即更新并重启"
        subtitle="已下载的更新包保存在本机应用存储目录中。"
        message="应用会替换为新版本并自动重启；浏览器会在重启后重新连接。"
        confirmLabel="立即更新并重启"
        busy={busy}
        onCancel={() => setConfirmRestart(false)}
        onConfirm={() => void applyAndRestart()}
      />
    </div>
  )
}

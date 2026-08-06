import { CheckCircle2, Download, ExternalLink, FileArchive, RefreshCw, Upload, X } from 'lucide-react'
import { useId, useState } from 'react'
import { ConfirmDialog } from './ConfirmDialog'
import { useLazySetupUpdateQuery, useReleasesQuery } from '../store/workspaceApi'

type Release = { tag: string; name: string; url: string; assets: Array<{ name: string; url: string }> }

export function SetupUpdateButton() {
  const [check, { data, isFetching, error }] = useLazySetupUpdateQuery()
  const [open, setOpen] = useState(false)
  const [mode, setMode] = useState<'now' | 'manual'>('now')
  const [releaseURL, setReleaseURL] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const [confirmRestart, setConfirmRestart] = useState(false)
  const uploadInputID = useId()
  const { data: releaseData = [] } = useReleasesQuery('alemonjs-setup', { skip: !open || mode !== 'manual' })
  const releases = releaseData as Release[]
  const selected = releases.find(item => item.url === releaseURL) ?? releases[0]

  const api = async (path: string, options: RequestInit) => {
    const response = await fetch(path, options)
    const result = await response.json() as { output?: string; error?: string }
    if (!response.ok) throw new Error(result.error || '操作未完成。')
    return result
  }

  const download = async () => {
    setBusy(true); setMessage('')
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
    setBusy(true); setMessage('')
    try {
      await api('/api/v1/update/apply', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ confirm: true }) })
      setMessage('正在重启应用…')
      window.setTimeout(() => window.location.reload(), 1600)
    } catch (reason) {
      setMessage(reason instanceof Error ? reason.message : '更新重启失败。')
      setBusy(false)
    }
  }

  const upload = async () => {
    if (!file) return
    setBusy(true); setMessage('')
    try {
      const form = new FormData()
      form.append('package', file)
      form.append('confirm', 'true')
      const result = await api('/api/v1/update/load', { method: 'POST', body: form })
      setMessage(result.output || '更新包已载入。请重新打开应用。')
    } catch (reason) {
      setMessage(reason instanceof Error ? reason.message : '载入更新失败。')
    } finally {
      setBusy(false)
    }
  }

  const openPanel = () => {
    setOpen(true); setMode('now'); setMessage(''); setConfirmRestart(false)
    void check()
  }

  return <div className="setup-update">
    <button className="setup-update-button" onClick={openPanel} disabled={isFetching} aria-label="检查应用更新" title={isFetching ? '正在检查更新' : '检查更新'}><RefreshCw /></button>
    {open && <section className="setup-update-popover">
      <header>
        <div><i><RefreshCw /></i><span><strong>应用更新</strong><small>保持 AlemonJS Setup 为最新版本</small></span></div>
        <button onClick={() => setOpen(false)} aria-label="关闭更新提示"><X /></button>
      </header>
      <div className="update-modes">
        <button className={mode === 'now' ? 'active' : ''} onClick={() => setMode('now')}>自动更新</button>
        <button className={mode === 'manual' ? 'active' : ''} onClick={() => setMode('manual')}>手动安装</button>
      </div>
      {mode === 'now' && <section className="update-automatic">
        {isFetching ? <p>正在检查更新…</p> : error ? <p>暂时无法检查更新，请稍后重试。</p> : data?.available ? <>
          <div className="update-version-status"><i><Download /></i><span><small>发现新版本</small><strong>{data.latest}</strong></span></div>
          {data.platformMatched ? <button className="primary-button" disabled={busy} onClick={() => { if (data.downloadReady) setConfirmRestart(true); else void download() }}>{busy ? '正在下载…' : data.downloadReady ? '更新并重启' : '下载更新'}</button> : <p>当前系统没有匹配的更新包，请使用手动安装。</p>}
        </> : <div className="update-version-status is-current"><i><CheckCircle2 /></i><span><small>当前版本</small><strong>{data?.current || '已是最新'}</strong></span></div>}
      </section>}
      {mode === 'manual' && <section className="update-manual">
        <header><div><strong>选择更新包</strong><small>下载后，导入文件即可完成安装。</small></div></header>
        <label>版本<select value={releaseURL || selected?.url || ''} onChange={event => setReleaseURL(event.target.value)}>{releases.map(item => <option key={item.url} value={item.url}>{item.tag} · {item.name}</option>)}</select></label>
        <div className="update-assets">
          {selected?.assets.map(asset => <a key={asset.url} href={asset.url} target="_blank" rel="noreferrer"><Download /><span>{asset.name}</span><ExternalLink /></a>)}
        </div>
        <section className="update-load" onDragOver={event => event.preventDefault()} onDrop={event => { event.preventDefault(); setFile(event.dataTransfer.files[0] ?? null) }}>
          <input id={uploadInputID} type="file" accept=".zip,.tgz,.gz" onChange={event => setFile(event.target.files?.[0] ?? null)} />
          <label htmlFor={uploadInputID} className="update-upload-trigger"><i>{file ? <FileArchive /> : <Upload />}</i><span><strong>{file ? file.name : '选择更新包'}</strong><small>{file ? '已选择，可开始安装。' : '也可将 .zip 或 .tgz 文件拖到这里。'}</small></span></label>
          <button className="primary-button" disabled={!file || busy} onClick={() => void upload()}>{busy ? '正在安装…' : '安装更新'}</button>
          {message && <small className="update-message">{message}</small>}
        </section>
      </section>}
      {mode === 'now' && message && <small className="update-message">{message}</small>}
      {data?.releaseUrl && <a className="update-release-link" href={data.releaseUrl} target="_blank" rel="noreferrer">查看发布说明 <ExternalLink /></a>}
    </section>}
    <ConfirmDialog open={confirmRestart} title="立即更新并重启" subtitle="已下载的更新包保存在本机应用存储目录中。" message="应用会替换为新版本并自动重启；浏览器会在重启后重新连接。" confirmLabel="立即更新并重启" busy={busy} onCancel={() => setConfirmRestart(false)} onConfirm={() => void applyAndRestart()} />
  </div>
}

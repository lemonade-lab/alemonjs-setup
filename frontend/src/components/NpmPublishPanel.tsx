import { useCallback, useEffect, useState } from 'react'

type NPMStatus = { name: string; localVersion: string; latestVersion?: string; published: boolean; private: boolean; loggedIn: boolean; username?: string; suggestedVersion?: string; scripts: string[]; issues: string[] }
type PackPreview = { name?: string; version?: string; filename?: string; fileCount: number; unpackedSize: number; files: string[] }
type Props = { root: string; busy: boolean; onRun: (action: 'npm-version' | 'npm-publish', values: Record<string, string>) => Promise<boolean> }

async function readStatus(root: string): Promise<NPMStatus> {
  const response = await fetch(`/api/v1/publish/npm/status?${new URLSearchParams({ root })}`)
  const body = await response.json() as NPMStatus & { error?: string }
  if (!response.ok) throw new Error(body.error ?? '无法读取 npm 发布状态。')
  return body
}
async function readPreview(root: string): Promise<PackPreview> {
  const response = await fetch(`/api/v1/publish/npm/pack?${new URLSearchParams({ root })}`)
  const body = await response.json() as PackPreview & { error?: string }
  if (!response.ok) throw new Error(body.error ?? '无法生成打包预览。')
  return body
}
const size = (value: number) => value < 1024 ? `${value} B` : `${(value / 1024).toFixed(1)} KB`

export function NpmPublishPanel({ root, busy, onRun }: Props) {
  const [status, setStatus] = useState<NPMStatus | null>(null); const [loading, setLoading] = useState(true); const [error, setError] = useState('')
  const [tag, setTag] = useState('latest'); const [confirming, setConfirming] = useState(false); const [tokenMode, setTokenMode] = useState(false); const [token, setToken] = useState('')
  const [preview, setPreview] = useState<PackPreview | null>(null); const [previewing, setPreviewing] = useState(false); const [previewError, setPreviewError] = useState('')
  const refresh = useCallback(async () => { setLoading(true); setError(''); try { setStatus(await readStatus(root)) } catch (reason) { setStatus(null); setError(reason instanceof Error ? reason.message : '无法读取 npm 发布状态。') } finally { setLoading(false) } }, [root])
  useEffect(() => { void refresh() }, [refresh])
  const applySuggestedVersion = async () => { if (status?.suggestedVersion && await onRun('npm-version', { version: status.suggestedVersion })) { setPreview(null); await refresh() } }
  const createPreview = async () => { setPreviewing(true); setPreviewError(''); try { setPreview(await readPreview(root)) } catch (reason) { setPreviewError(reason instanceof Error ? reason.message : '无法生成打包预览。') } finally { setPreviewing(false) } }
  const publish = async () => { if (!confirming) { setConfirming(true); return }; if (await onRun('npm-publish', { tag, token: tokenMode ? token : '' })) { setToken(''); setConfirming(false); setPreview(null); await refresh() } }
  if (loading) return <p className="publish-state">正在读取 npm 官方仓库与本机登录状态…</p>
  if (error) return <section className="publish-state"><p>{error}</p><button className="secondary-button" onClick={() => void refresh()}>重新检查</button></section>
  if (!status) return null
  const issues = status.issues ?? []; const scripts = status.scripts ?? []; const canPublish = preview !== null && issues.every((issue) => !issue.startsWith('尚未登录 npm') || (tokenMode && token.trim() !== ''))
  return <section className="npm-publish-panel">
    <header className="publish-heading"><strong>{status.name || '未命名包'}</strong><div className="publish-toolbar"><label>标签<select value={tag} onChange={(event) => { setTag(event.target.value); setConfirming(false) }}><option value="latest">latest</option><option value="beta">beta</option><option value="next">next</option></select></label><button className="primary-button" disabled={busy || !canPublish} onClick={() => void publish()}>{confirming ? '确认发布' : '准备发布'}</button><button className="secondary-button" disabled={busy} onClick={() => void refresh()}>刷新</button>{confirming && <button className="text-button" onClick={() => setConfirming(false)}>取消</button>}</div></header>
    <section className="publish-status-line" aria-label="发布状态"><span>本地 <b>{status.localVersion || '未设置'}</b></span><span>npm <b>{status.published ? status.latestVersion : '从未发布'}</b></span><span>账户 <b className={status.loggedIn ? 'ready' : 'warning'}>{status.loggedIn ? status.username : '未登录'}</b></span></section>
    {status.suggestedVersion && status.suggestedVersion !== status.localVersion && <section className="version-suggestion"><strong>建议 v{status.suggestedVersion}</strong><button className="secondary-button" disabled={busy} onClick={() => void applySuggestedVersion()}>采用</button></section>}
    {issues.length > 0 && <section className="publish-issues"><strong>需要先处理</strong><ul>{issues.map((issue) => <li key={issue}>{issue}</li>)}</ul>{!status.loggedIn && <div className="npm-auth-options"><a href="https://www.npmjs.com/login" target="_blank" rel="noreferrer">登录 npm</a><button className="text-button" onClick={() => setTokenMode((value) => !value)}>{tokenMode ? '改用网页登录' : '使用一次性令牌'}</button>{tokenMode && <label>发布令牌<input type="password" value={token} onChange={(event) => { setToken(event.target.value); setConfirming(false) }} autoComplete="off" placeholder="npm_…" /></label>}<a href="https://www.npmjs.com/settings/tokens" target="_blank" rel="noreferrer">创建令牌</a></div>}</section>}
    <section className="pack-preview"><div><strong>打包预览</strong>{preview && <span>{preview.filename} · {preview.fileCount} 个文件 · {size(preview.unpackedSize)}</span>}</div><button className="secondary-button" disabled={busy || previewing} onClick={() => void createPreview()}>{previewing ? '预览中…' : preview ? '重新预览' : '查看内容'}</button>{previewError && <p className="error">{previewError}</p>}{preview && <details className="publish-scripts"><summary>将上传的文件</summary>{preview.files.map((file) => <code key={file}>{file}</code>)}</details>}</section>
    {scripts.length > 0 && <details className="publish-scripts"><summary>发布时 npm 会运行的脚本</summary>{scripts.map((script) => <code key={script}>{script}</code>)}</details>}
  </section>
}

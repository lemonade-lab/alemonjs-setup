import { useState, type ReactNode } from 'react'
import { ChevronDown, File, Folder, RefreshCw, X } from 'lucide-react'
import { useNpmStatusQuery, useLazyNpmPackQuery } from '../store/workspaceApi'
import { ErrorNotice } from './ErrorNotice'

type NPMStatus = { name: string; localVersion: string; latestVersion?: string; published: boolean; private: boolean; loggedIn: boolean; username?: string; suggestedVersion?: string; scripts: string[]; issues: string[] }
type PackPreview = { name?: string; version?: string; filename?: string; fileCount: number; unpackedSize: number; files: string[] }
type Props = { root: string; busy: boolean; onRun: (action: 'npm-version' | 'npm-publish', values: Record<string, string>) => Promise<boolean> }
type FileTree = { files: string[]; directories: Map<string, FileTree> }

const size = (value: number) => value < 1024 ? `${value} B` : `${(value / 1024).toFixed(1)} KB`

function packTree(files: string[]) {
  const tree: FileTree = { files: [], directories: new Map() }
  for (const path of files) {
    const parts = path.split('/').filter(Boolean)
    const filename = parts.pop()
    if (!filename) continue
    let branch = tree
    for (const part of parts) {
      const next = branch.directories.get(part) ?? { files: [], directories: new Map<string, FileTree>() }
      branch.directories.set(part, next)
      branch = next
    }
    branch.files.push(filename)
  }
  return tree
}

function PackFileTree({ files }: { files: string[] }) {
  const render = (tree: FileTree, depth = 0): ReactNode => <ul className="pack-file-tree" data-depth={depth}>{[...tree.directories.entries()].sort(([left], [right]) => left.localeCompare(right)).map(([name, child]) => <li className="pack-folder" key={name}><details open={depth < 1}><summary><ChevronDown /> <Folder /> {name}</summary>{render(child, depth + 1)}</details></li>)}{[...tree.files].sort((left, right) => left.localeCompare(right)).map((name) => <li className="pack-file" key={name}><File /> {name}</li>)}</ul>
  return <div className="pack-tree-frame" aria-label="npm 将上传的文件结构">{render(packTree(files))}</div>
}

export function NpmPublishPanel({ root, busy, onRun }: Props) {
  const { data: rawStatus, isFetching: loading, error: statusError, refetch } = useNpmStatusQuery(root, { skip: !root })
  const status = rawStatus as NPMStatus | undefined
  const [getPreview] = useLazyNpmPackQuery()
  const [tag, setTag] = useState('latest'); const [confirming, setConfirming] = useState(false); const [tokenMode, setTokenMode] = useState(false); const [token, setToken] = useState('')
  const [preview, setPreview] = useState<PackPreview | null>(null); const [previewing, setPreviewing] = useState(false); const [previewError, setPreviewError] = useState('')
  const refresh = async () => { await refetch() }
  const applySuggestedVersion = async () => { if (status?.suggestedVersion && await onRun('npm-version', { version: status.suggestedVersion })) { setPreview(null); await refresh() } }
  const createPreview = async () => { setPreviewing(true); setPreviewError(''); try { setPreview(await getPreview(root, true).unwrap() as PackPreview) } catch { setPreviewError('无法生成打包预览。') } finally { setPreviewing(false) } }
  const publish = async () => { if (!confirming) { setConfirming(true); return }; if (await onRun('npm-publish', { tag, token: tokenMode ? token : '' })) { setToken(''); setConfirming(false); setPreview(null); await refresh() } }
  if (loading) return <p className="publish-state">正在读取 npm 官方仓库与本机登录状态…</p>
  if (statusError) return <section className="publish-state"><p>无法读取 npm 发布状态。</p><button className="secondary-button" onClick={() => void refresh()}>重新检查</button></section>
  if (!status) return null
  const issues = status.issues ?? []; const scripts = status.scripts ?? []
  const loginRequired = !status.loggedIn
  const otherIssues = issues.filter((issue) => !issue.startsWith('尚未登录 npm'))
  const canPublish = preview !== null && otherIssues.length === 0 && (!loginRequired || (tokenMode && token.trim() !== ''))
  return <section className="npm-publish-panel">
    <header className="publish-heading"><strong>{status.name || '未命名包'}</strong><div className="publish-toolbar"><label>标签<select value={tag} onChange={(event) => { setTag(event.target.value); setConfirming(false) }}><option value="latest">latest</option><option value="beta">beta</option><option value="next">next</option></select></label>{confirming && <button className="icon-button" onClick={() => setConfirming(false)} aria-label="取消发布" title="取消"><X /></button>}<button className="icon-button" disabled={busy} onClick={() => void refresh()} aria-label="刷新发布状态" title="刷新"><RefreshCw /></button><button className="primary-button" disabled={busy || !canPublish} onClick={() => void publish()}>{confirming ? '确认发布' : '发布到 npm'}</button></div></header>
    <section className="publish-status-line" aria-label="发布状态"><span>本地 <b>{status.localVersion || '未设置'}</b></span><span>npm <b>{status.published ? status.latestVersion : '从未发布'}</b></span><span>账户 <b className={status.loggedIn ? 'ready' : 'warning'}>{status.loggedIn ? status.username : '未登录'}</b></span></section>
    {status.suggestedVersion && status.suggestedVersion !== status.localVersion && <section className="version-suggestion"><strong>建议 v{status.suggestedVersion}</strong><button className="secondary-button" disabled={busy} onClick={() => void applySuggestedVersion()}>采用</button></section>}
    {loginRequired && <section className="npm-auth-card"><div className="npm-auth-copy"><i>!</i><div><strong>先登录 npm</strong><span>登录后刷新此页，即可继续发布。</span></div></div><div className="npm-auth-actions"><a className="secondary-button" href="https://www.npmjs.com/login" target="_blank" rel="noreferrer">打开登录页</a><button className="text-button" onClick={() => setTokenMode((value) => !value)}>{tokenMode ? '改用网页登录' : '使用发布令牌'}</button><a className="text-button" href="https://www.npmjs.com/settings/tokens" target="_blank" rel="noreferrer">创建令牌</a></div>{tokenMode && <label className="npm-token-field">发布令牌<input type="password" value={token} onChange={(event) => { setToken(event.target.value); setConfirming(false) }} autoComplete="off" placeholder="npm_…" /></label>}</section>}
    {otherIssues.length > 0 && <section className="publish-issues"><ul>{otherIssues.map((issue) => <li key={issue}>{issue}</li>)}</ul></section>}
    <section className="pack-preview"><div><strong>{preview ? '打包已就绪' : '确认打包内容'}</strong><span>{preview ? `${preview.filename} · ${preview.fileCount} 个文件 · ${size(preview.unpackedSize)}` : '先生成预览，确认实际会上传的文件。'}</span></div><button className="secondary-button" disabled={busy || previewing} onClick={() => void createPreview()}>{previewing ? '预览中…' : preview ? '重新预览' : '查看打包内容'}</button>{previewError && <ErrorNotice message={previewError} onClose={() => setPreviewError('')} />}{preview && <details className="publish-scripts"><summary>查看文件清单</summary><PackFileTree files={preview.files} /></details>}</section>
    {scripts.length > 0 && <details className="publish-scripts"><summary>发布时 npm 会运行的脚本</summary>{scripts.map((script) => <code key={script}>{script}</code>)}</details>}
  </section>
}

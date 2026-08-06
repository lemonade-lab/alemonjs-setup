import { useState } from 'react'
import { ChevronDown, CloudDownload, File, Folder, GitBranch, GitCommitHorizontal, History, Network, Plus, RefreshCw, Tags, Trash2, Upload, X } from 'lucide-react'
import { ConfirmDialog } from './ConfirmDialog'
import { useGitWorkspaceActionMutation, useGitWorkspaceQuery } from '../store/workspaceApi'

type Project = { name: string; path: string }
type Tab = 'commit' | 'history' | 'tag' | 'branch' | 'remote'
type Action = 'fetch' | 'pull' | 'push' | 'commit' | 'branch-create' | 'branch-switch' | 'branch-track' | 'branch-delete' | 'tag-create' | 'tag-push' | 'tag-delete' | 'remote-add' | 'remote-set-url' | 'remote-remove'
type Pending = { action: Action; value?: string; message?: string } | null
type Change = { status: string; path: string }
type ChangeTreeNode = { name: string; path: string; change?: Change; children: Map<string, ChangeTreeNode> }

const tabItems: Array<{ id: Tab; label: string; icon: typeof GitCommitHorizontal }> = [
  { id: 'commit', label: '提交', icon: GitCommitHorizontal },
  { id: 'history', label: '记录', icon: History },
  { id: 'tag', label: '标签', icon: Tags },
  { id: 'branch', label: '分支', icon: GitBranch },
  { id: 'remote', label: '远程', icon: Network }
]

const actionCopy: Record<Action, { title: string; confirm: string; destructive?: boolean }> = {
  fetch: { title: '检查远程更新', confirm: '确认检查' }, pull: { title: '同步远程更新', confirm: '确认同步' }, push: { title: '推送当前分支', confirm: '确认推送' }, commit: { title: '创建本地提交', confirm: '确认提交' },
  'branch-create': { title: '创建并切换分支', confirm: '确认创建' }, 'branch-switch': { title: '切换分支', confirm: '确认切换' }, 'branch-track': { title: '检出远程分支', confirm: '确认检出' }, 'branch-delete': { title: '删除本地分支', confirm: '确认删除', destructive: true },
  'tag-create': { title: '创建版本标签', confirm: '确认创建' }, 'tag-push': { title: '推送标签', confirm: '确认推送' }, 'tag-delete': { title: '删除本地标签', confirm: '确认删除', destructive: true },
  'remote-add': { title: '添加远程仓库', confirm: '确认添加' }, 'remote-set-url': { title: '修改远程地址', confirm: '确认修改' }, 'remote-remove': { title: '移除远程仓库', confirm: '确认移除', destructive: true }
}

function buildChangeTree(changes: Change[]) {
  const root: ChangeTreeNode = { name: '', path: '', children: new Map() }
  for (const change of changes) {
    const parts = change.path.replace(/\/$/, '').split('/').filter(Boolean)
    let parent = root
    parts.forEach((name, index) => {
      const path = parent.path ? `${parent.path}/${name}` : name
      const existing = parent.children.get(name) ?? { name, path, children: new Map<string, ChangeTreeNode>() }
      if (index === parts.length - 1) existing.change = change
      parent.children.set(name, existing)
      parent = existing
    })
  }
  return [...root.children.values()]
}

function ChangeTree({ nodes }: { nodes: ChangeTreeNode[] }) {
  return <ul className="grid gap-0.5 pl-4 first:pl-0">{nodes.map(node => {
    const folder = node.children.size > 0 || node.change?.path.endsWith('/')
    return <li key={node.path}>{node.children.size > 0 ? <details className="group" open><summary className="flex cursor-pointer list-none items-center gap-1.5 rounded px-1.5 py-1 text-xs text-slate-700 hover:bg-slate-100 [&::-webkit-details-marker]:hidden"><ChevronDown className="size-3.5 transition-transform group-not-open:-rotate-90" /><Folder className="size-3.5 text-slate-400" /><span className="min-w-0 flex-1 truncate">{node.name}</span>{node.change && <code className="text-[10px] text-slate-400">{node.change.status || '??'}</code>}</summary><ChangeTree nodes={[...node.children.values()]} /></details> : <div className="flex items-center gap-1.5 rounded px-1.5 py-1 text-xs text-slate-600 hover:bg-slate-50">{folder ? <Folder className="size-3.5 text-slate-400" /> : <File className="size-3.5 text-slate-400" />}<span className="min-w-0 flex-1 truncate">{node.name}</span><code className="text-[10px] text-slate-400">{node.change?.status || '??'}</code></div>}</li>
  })}</ul>
}

export function RobotGitControl({ project, onClose }: { project: Project | null; onClose: () => void }) {
  const root = project?.path ?? ''
	const [tab, setTab] = useState<Tab>('commit')
  const { data, isLoading: isInitialLoading, isFetching, error, refetch } = useGitWorkspaceQuery({ root, view: tab }, { skip: !root })
  const [run, { isLoading }] = useGitWorkspaceActionMutation()
  const [pending, setPending] = useState<Pending>(null)
  const [output, setOutput] = useState('')
  const [commitMessage, setCommitMessage] = useState('')
  const [branchName, setBranchName] = useState('')
  const [tagName, setTagName] = useState('')
  const [tagMessage, setTagMessage] = useState('')
  const [remoteName, setRemoteName] = useState('origin')
  const [remoteURL, setRemoteURL] = useState('')
  if (!project) return null

  const execute = async (request: NonNullable<Pending>) => {
    try {
      const result = await run({ root, ...request }).unwrap()
      setOutput(result.output || 'Git 操作已完成。')
      if (request.action === 'commit') setCommitMessage('')
      if (request.action === 'branch-create') setBranchName('')
      if (request.action === 'tag-create') { setTagName(''); setTagMessage('') }
      await refetch()
    } catch (reason) {
      setOutput(reason instanceof Error ? reason.message : 'Git 操作未完成。')
    } finally { setPending(null) }
  }
  const request = (action: Action, value?: string, message?: string) => setPending({ action, value, message })
  const changes = data?.changes ?? []
  const changeTree = buildChangeTree(changes)
  const syncText = !data?.upstream ? '当前分支尚未关联远程分支' : `领先 ${data.ahead} · 落后 ${data.behind}`
  const confirm = pending ? actionCopy[pending.action] : null

  const commitPanel = <section className="grid gap-3 rounded-lg border border-slate-200 bg-white p-4">
    <div className="flex flex-wrap items-end justify-between gap-3 border-b border-slate-200 pb-3"><div className="grid gap-1"><strong className="text-sm font-semibold text-slate-800">工作区变更</strong><span className="text-xs text-slate-500">确认文件后，在右侧填写说明并提交。</span></div><div className="flex flex-wrap items-end gap-2"><small className="text-xs text-slate-400">{changes.length} 项待提交</small><input className="h-9 min-w-52 rounded-md border border-slate-300 bg-white px-2.5 text-xs text-slate-800 outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100" value={commitMessage} onChange={event => setCommitMessage(event.target.value)} placeholder="提交说明，例如：修复登录配置" /><button className="primary-button" disabled={isLoading || !changes.length || !commitMessage.trim()} onClick={() => request('commit', undefined, commitMessage)}>提交全部变更</button></div></div>
    {changes.length ? <ChangeTree nodes={changeTree} /> : <p className="grid min-h-24 place-items-center text-xs text-slate-500">工作区干净，没有待提交文件。</p>}
  </section>

  const historyPanel = <section className="git-tab-panel"><div className="git-tab-heading"><div><strong>最近提交记录</strong><span>用于确认当前机器人目录的本地历史。</span></div><small>{data?.commits.length ?? 0} 条</small></div>{data?.commits.length ? <ul className="git-history-list">{data.commits.map(item => <li key={item.sha}><code title={item.sha}>{item.shortSha}</code><span><strong>{item.subject}</strong><small>{item.createdAt}</small></span></li>)}</ul> : <p className="git-tab-empty">尚无提交记录。</p>}</section>

  const tagPanel = <section className="git-tab-panel"><div className="git-tab-heading"><div><strong>版本标签</strong><span>创建的是带说明的本地标签；确认后可单独推送到 origin。</span></div><small>{data?.tags.length ?? 0} 个</small></div><div className="git-form-grid"><input value={tagName} onChange={event => setTagName(event.target.value)} placeholder="标签名，例如 v1.2.3" /><input value={tagMessage} onChange={event => setTagMessage(event.target.value)} placeholder="标签说明，例如 release: v1.2.3" /><button className="secondary-button" disabled={isLoading || !tagName.trim() || !tagMessage.trim()} onClick={() => request('tag-create', tagName, tagMessage)}>创建标签</button></div>{data?.tags.length ? <ul className="git-history-list">{data.tags.map(item => <li key={item.name}><code>{item.name}</code><span><strong>{item.subject || '无说明'}</strong><small>{item.createdAt || '本地标签'}</small></span><div className="git-row-actions"><button className="text-button" disabled={isLoading} onClick={() => request('tag-push', item.name)}>推送</button><button className="text-button danger" disabled={isLoading} onClick={() => request('tag-delete', item.name)}>删除</button></div></li>)}</ul> : <p className="git-tab-empty">尚无标签。</p>}</section>

  const branchPanel = <section className="git-tab-panel">
    <div className="git-tab-heading"><div><strong>分支管理</strong><span>先获取远程分支，再选择要在本机打开的工作分支。</span></div><small>本地 {data?.branches.length ?? 0} · 远程 {data?.remoteBranches.length ?? 0}</small></div>
    <div className="git-branch-guide"><div><strong>1. 获取远程</strong><span>读取 origin 的全部分支和标签，不会修改你的代码。</span></div><button className="secondary-button" disabled={isLoading || !data?.remotes.length} onClick={() => request('fetch')}><CloudDownload />拉取所有远程分支</button></div>
    <div className="git-branch-section"><header><strong>本地分支</strong><span>创建新分支后会立即切换。</span></header><div className="git-form-row"><input value={branchName} onChange={event => setBranchName(event.target.value)} placeholder="新分支名称，例如 feat/login" /><button className="secondary-button" disabled={isLoading || !branchName.trim()} onClick={() => request('branch-create', branchName)}>创建并切换</button></div><ul className="git-history-list">{data?.branches.map(item => <li key={item.name}><code className={item.current ? 'current' : ''}>{item.name}</code><span><strong>{item.current ? '当前正在使用' : item.upstream ? `跟踪 ${item.upstream}` : '仅在本机'}</strong><small>{item.current ? '可在“远程”中一键推送' : '可切换后继续开发'}</small></span><div className="git-row-actions">{!item.current && <button className="text-button" disabled={isLoading} onClick={() => request('branch-switch', item.name)}>切换</button>}{!item.current && <button className="text-button danger" disabled={isLoading} onClick={() => request('branch-delete', item.name)}>删除</button>}</div></li>)}</ul></div>
    <div className="git-branch-section"><header><strong>远程分支</strong><span>点击“在本机打开”会创建同名本地分支并自动跟踪远程。</span></header>{data?.remoteBranches.length ? <ul className="git-history-list">{data.remoteBranches.map(item => <li key={item.name}><code>{item.name}</code><span><strong>{item.remote} 上的 {item.branch}</strong><small>尚未在本机检出</small></span><div className="git-row-actions"><button className="secondary-button" disabled={isLoading} onClick={() => request('branch-track', item.name)}>在本机打开</button></div></li>)}</ul> : <p className="git-tab-empty">还没有远程分支信息。点击上方“拉取所有远程分支”即可读取。</p>}</div>
  </section>

  const remotePanel = <section className="git-tab-panel git-remote-panel">
    <header className="git-tab-heading git-remote-heading">
      <div><strong>远程仓库</strong><span>{syncText}。同步不会自动处理冲突。</span></div>
      <div className="git-sync-actions">
        <button className="secondary-button" disabled={isLoading} onClick={() => request('fetch')}><CloudDownload />拉取远程</button>
        <button className="secondary-button" disabled={isLoading || !data?.upstream || !data.behind} onClick={() => request('pull')}><RefreshCw />同步</button>
        <button className="primary-button" disabled={isLoading || !data?.remotes.length} onClick={() => request('push')}><Upload />推送当前分支</button>
      </div>
    </header>
    <div className="git-remote-content">
      <section className="git-remote-result">
        {data?.remotes.length ? <ul className="git-history-list">{data.remotes.map(item => <li key={item.name}><code>{item.name}</code><span><strong title={item.url}>{item.url}</strong><small>{item.name === 'origin' ? '默认远程仓库' : '已配置远程仓库'}</small></span><div className="git-row-actions"><button className="text-button danger" disabled={isLoading} onClick={() => request('remote-remove', item.name)}><Trash2 />移除</button></div></li>)}</ul> : <p className="git-tab-empty">尚未配置远程仓库。</p>}
      </section>
      <section className="git-remote-editor" aria-label="编辑远程仓库">
        <strong>添加或修改</strong>
        <input value={remoteName} onChange={event => setRemoteName(event.target.value)} placeholder="名称，如 origin" aria-label="远程仓库名称" />
        <input value={remoteURL} onChange={event => setRemoteURL(event.target.value)} placeholder="仓库地址，如 git@github.com:org/repo.git" aria-label="远程仓库地址" />
        <div><button className="primary-button" disabled={isLoading || !remoteName.trim() || !remoteURL.trim()} onClick={() => request('remote-add', remoteName, remoteURL)}><Plus />添加</button><button className="secondary-button" disabled={isLoading || !remoteName.trim() || !remoteURL.trim()} onClick={() => request('remote-set-url', remoteName, remoteURL)}>更新地址</button></div>
      </section>
    </div>
  </section>

  const panel = tab === 'commit' ? commitPanel : tab === 'history' ? historyPanel : tab === 'tag' ? tagPanel : tab === 'branch' ? branchPanel : remotePanel
  return <div className="git-workspace-backdrop fixed inset-0 z-[80] flex items-center justify-center bg-slate-950/30 p-4" role="presentation"><section className="git-workspace-dialog grid max-h-[min(760px,calc(100vh-32px))] w-full max-w-[920px] grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_24px_70px_rgb(15_23_42/0.26)]" role="dialog" aria-modal="true" aria-label={`${project.name} 的 Git 管理`} aria-busy={isInitialLoading}>
    <header className="flex min-h-14 items-center justify-between gap-4 border-b border-slate-200 px-4"><div className="flex min-w-0 items-center gap-2"><GitBranch className="size-5 shrink-0 text-brand-600" /><span className="grid min-w-0 gap-0.5"><strong className="truncate text-sm font-semibold text-ink-950">{project.name} · Git</strong><small className="truncate text-[11px] text-slate-500" title={data?.gitRoot || project.path}>{data?.gitRoot || project.path}</small></span></div><div className="flex items-center gap-2"><button className="icon-button size-8 p-0" disabled={isFetching || isLoading} onClick={() => void refetch()} aria-label="刷新 Git 状态"><RefreshCw className="size-4" /></button><button className="icon-button size-8 p-0" onClick={onClose} aria-label="关闭 Git 管理"><X className="size-4" /></button></div></header>
    {isInitialLoading ? <p className="grid min-h-40 place-items-center text-sm text-slate-500">正在读取 Git 状态…</p> : error ? <p className="grid min-h-40 place-items-center text-sm text-slate-500">无法读取 Git 状态，请确认目录可访问。</p> : !data?.repository ? <section className="grid min-h-56 place-items-center gap-2 p-6 text-center"><GitBranch className="size-8 text-slate-400" /><strong className="text-sm font-semibold text-slate-800">此机器人目录尚未初始化 Git</strong><span className="text-xs text-slate-500">初始化仓库后，即可在这里管理提交、分支、标签与远程仓库。</span></section> : <div className="grid min-h-0 gap-3 overflow-auto p-4"><nav className="flex flex-wrap gap-1 border-b border-slate-200 pb-2" aria-label="Git 功能">{tabItems.map(item => { const Icon = item.icon; return <button key={item.id} className={tab === item.id ? 'inline-flex items-center gap-1.5 rounded-md bg-brand-50 px-2.5 py-1.5 text-xs font-semibold text-brand-700' : 'inline-flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-semibold text-slate-500 transition hover:bg-slate-100 hover:text-slate-800'} onClick={() => setTab(item.id)}><Icon className="size-3.5" />{item.label}</button> })}</nav>{panel}{output && <pre className="overflow-auto rounded-lg bg-slate-950 p-3 text-xs leading-5 text-slate-200">{output}</pre>}</div>}
    <ConfirmDialog open={pending !== null} title={confirm?.title || '确认 Git 操作'} subtitle={confirm?.destructive ? '此操作会修改本地 Git 历史或配置，无法自动恢复。' : '操作将只在当前机器人目录执行。'} message={pending?.value ? `目标：${pending.value}${pending.message ? `\n说明：${pending.message}` : ''}` : pending?.message ? `说明：${pending.message}` : '请确认继续。'} confirmLabel={confirm?.confirm || '确认'} busy={isLoading} onCancel={() => setPending(null)} onConfirm={() => { if (pending) void execute(pending) }} />
  </section></div>
}

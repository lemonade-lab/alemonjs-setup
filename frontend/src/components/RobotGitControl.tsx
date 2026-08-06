import { useState } from 'react'
import { GitBranch, RefreshCw, X } from 'lucide-react'
import { ConfirmDialog } from './ConfirmDialog'
import { useGitWorkspaceActionMutation, useGitWorkspaceQuery } from '../store/workspaceApi'

type Project = { name: string; path: string }
type PendingAction = 'pull' | 'commit' | null

export function RobotGitControl({ project, onClose }: { project: Project | null; onClose: () => void }) {
  const root = project?.path ?? ''
  const { data, isLoading: isInitialLoading, isFetching, error, refetch } = useGitWorkspaceQuery(root, { skip: !root })
  const [run, { isLoading }] = useGitWorkspaceActionMutation()
  const [message, setMessage] = useState('')
  const [pending, setPending] = useState<PendingAction>(null)
  const [output, setOutput] = useState('')
  const [showAllTags, setShowAllTags] = useState(false)
  if (!project) return null

  const execute = async (action: 'fetch' | 'pull' | 'commit') => {
    try {
      const result = await run({ root, action, ...(action === 'commit' ? { message } : {}) }).unwrap()
      setOutput(result.output || 'Git 操作已完成。')
      if (action === 'commit') setMessage('')
      await refetch()
    } catch (reason) {
      setOutput(reason instanceof Error ? reason.message : 'Git 操作未完成。')
    } finally {
      setPending(null)
    }
  }

  const changes = data?.changes ?? []
  const visibleTags = showAllTags ? data?.tags ?? [] : (data?.tags ?? []).slice(0, 6)
  const syncLabel = !data?.upstream
    ? '未配置远程分支'
    : data.behind
      ? `远程有 ${data.behind} 个更新可同步`
      : '已与远程同步'

  return (
    <div className="git-workspace-backdrop" role="presentation">
      <section className="git-workspace-dialog" role="dialog" aria-modal="true" aria-label={`${project.name} 的 Git 管理`} aria-busy={isInitialLoading}>
        <header>
          <div>
            <GitBranch />
            <span>
              <strong>{project.name} · Git</strong>
              <small title={project.path}>{data?.gitRoot || project.path}</small>
            </span>
          </div>
          <div>
            <button className="icon-button" disabled={isFetching || isLoading} onClick={() => void refetch()} aria-label="刷新 Git 状态" title="刷新 Git 状态"><RefreshCw /></button>
            <button className="icon-button" onClick={onClose} aria-label="关闭 Git 管理" title="关闭"><X /></button>
          </div>
        </header>
        {isInitialLoading ? <p className="git-workspace-state">正在读取 Git 状态…</p> : error ? <p className="git-workspace-state">无法读取 Git 状态，请确认目录可访问。</p> : !data?.repository ? (
          <section className="git-workspace-empty"><GitBranch /><strong>此机器人目录尚未初始化 Git</strong><span>初始化仓库后，这里会展示分支、提交、变更和远程同步状态。</span></section>
        ) : (
          <div className="git-workspace-content">
            <section className="git-workspace-summary">
              <div><small>当前分支</small><strong>{data.branch || 'HEAD 分离'}</strong><span>{data.upstream ? `${data.upstream} · ↑ ${data.ahead} / ↓ ${data.behind}` : '尚未关联远程分支'}</span></div>
              <div><small>远程仓库</small><strong title={data.remote}>{data.remote || '未配置 origin'}</strong></div>
              <div className={changes.length ? 'has-changes' : ''}><small>当前任务</small><strong>{changes.length ? `整理 ${changes.length} 项变更` : syncLabel}</strong><span>{changes.length ? '确认内容后创建本地提交' : '工作区没有待提交内容'}</span></div>
            </section>

            <section className="git-sync-row">
              <div><strong>远程同步</strong><span>{syncLabel}。先检查远程，再决定是否安全同步。</span></div>
              <div>
                <button className="secondary-button" disabled={isLoading} onClick={() => void execute('fetch')}>检查远程</button>
                <button className="secondary-button" disabled={isLoading || !data.upstream || !data.behind} onClick={() => setPending('pull')}>安全同步更新</button>
              </div>
            </section>

            <section className="git-commit-panel">
              <header><div><strong>整理并提交变更</strong><span>{changes.length ? '以下内容将一并暂存为一次本地提交。' : '工作区干净，可以继续开发或同步远程更新。'}</span></div><small>{changes.length} 项</small></header>
              {changes.length ? <ul>{changes.map(item => <li key={`${item.status}:${item.path}`}><code>{item.status || '??'}</code><span>{item.path}</span></li>)}</ul> : <p>没有待提交的文件。</p>}
              <footer>
                <input value={message} onChange={event => setMessage(event.target.value)} placeholder="写下这次改动的原因，例如：修复登录配置" />
                <button className="primary-button" disabled={isLoading || !changes.length || !message.trim()} onClick={() => setPending('commit')}>提交这 {changes.length} 项变更</button>
              </footer>
            </section>

            {output && <pre className="git-workspace-output">{output}</pre>}
            <div className="git-workspace-grid git-workspace-secondary">
              <section><header><strong>最近提交</strong><small>{data.commits.length}</small></header>{data.commits.length ? <ul>{data.commits.map(item => <li key={item.sha}><code>{item.shortSha}</code><span>{item.subject}<small>{item.createdAt}</small></span></li>)}</ul> : <p>尚无提交。</p>}</section>
              <section><header><strong>分支</strong><small>{data.branches.length}</small></header><div className="git-workspace-tags">{data.branches.length ? data.branches.map(item => <code className={item === data.branch ? 'current' : ''} key={item}>{item}</code>) : <p>尚无分支。</p>}</div></section>
              <section className="git-tags-panel"><header><strong>版本标签</strong><span><small>{data.tags.length} 个</small>{data.tags.length > 6 && <button className="text-button" onClick={() => setShowAllTags(value => !value)}>{showAllTags ? '收起' : '查看全部'}</button>}</span></header><div className="git-workspace-tags">{visibleTags.length ? visibleTags.map(item => <code key={item}>{item}</code>) : <p>尚无标签。</p>}</div></section>
            </div>
          </div>
        )}
        <ConfirmDialog
          open={pending !== null}
          title={pending === 'pull' ? '安全同步远程更新' : `提交 ${changes.length} 项变更`}
          subtitle={pending === 'pull' ? '只允许 fast-forward，不会自动合并冲突。' : '会先暂存上方列出的全部文件，再创建一次本地提交。'}
          message={pending === 'commit' ? `提交说明：${message}` : `将从 ${data?.upstream || '远程分支'} 同步到当前分支。`}
          confirmLabel={pending === 'pull' ? '确认同步' : '确认提交'}
          busy={isLoading}
          onCancel={() => setPending(null)}
          onConfirm={() => { if (pending) void execute(pending) }}
        />
      </section>
    </div>
  )
}

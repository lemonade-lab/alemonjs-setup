import cn from 'classnames'
import { useEffect, useState, type ReactNode } from 'react'
import { ChevronDown, File, Folder, Package, RefreshCw, X } from 'lucide-react'
import { useNpmStatusQuery, useLazyNpmPackQuery } from '../store/workspaceApi'
import { ErrorNotice } from './ErrorNotice'

type SourceCommit = {
  sha: string
  shortSha: string
  subject: string
  createdAt: string
}
type NPMStatus = {
  name: string
  localVersion: string
  latestVersion?: string
  published: boolean
  private: boolean
  loggedIn: boolean
  username?: string
  suggestedVersion?: string
  scripts: string[]
  branch?: string
  sourceCommits?: SourceCommit[]
  issues: string[]
}
type PackPreview = {
  name?: string
  version?: string
  filename?: string
  fileCount: number
  unpackedSize: number
  files: string[]
}
type Props = {
  root: string
  busy: boolean
  onRun: (
    action: 'npm-version' | 'npm-publish',
    values: Record<string, string>
  ) => Promise<boolean>
}
type FileTree = { files: string[]; directories: Map<string, FileTree> }

const size = (value: number) =>
  value < 1024 ? `${value} B` : `${(value / 1024).toFixed(1)} KB`
const emptySourceCommits: SourceCommit[] = []

function packTree(files: string[]) {
  const tree: FileTree = { files: [], directories: new Map() }
  for (const path of files) {
    const parts = path.split('/').filter(Boolean)
    const filename = parts.pop()
    if (!filename) continue
    let branch = tree
    for (const part of parts) {
      const next = branch.directories.get(part) ?? {
        files: [],
        directories: new Map<string, FileTree>()
      }
      branch.directories.set(part, next)
      branch = next
    }
    branch.files.push(filename)
  }
  return tree
}

function PackFileTree({ files }: { files: string[] }) {
  const render = (tree: FileTree, depth = 0): ReactNode => (
    <ul className="grid gap-0.5 pl-4 first:pl-0" data-depth={depth}>
      {[...tree.directories.entries()]
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([name, child]) => (
          <li key={name}>
            <details className="group" open={depth < 1}>
              <summary className="flex cursor-pointer list-none items-center gap-1.5 rounded px-1 py-0.5 text-xs text-slate-700 hover:bg-slate-100 [&::-webkit-details-marker]:hidden">
                <ChevronDown className="size-3.5 transition-transform group-not-open:-rotate-90" />
                <Folder className="size-3.5 text-brand-600" /> {name}
              </summary>
              {render(child, depth + 1)}
            </details>
          </li>
        ))}
      {[...tree.files]
        .sort((left, right) => left.localeCompare(right))
        .map(name => (
          <li
            className="flex items-center gap-1.5 rounded px-1 py-0.5 text-xs text-slate-600"
            key={name}
          >
            <File className="size-3.5 text-slate-400" /> {name}
          </li>
        ))}
    </ul>
  )
  return (
    <div
      className="mt-3 max-h-64 overflow-auto rounded-lg border border-slate-200 bg-slate-50 p-3 dark:border-slate-700 dark:bg-slate-950"
      aria-label="npm 将上传的文件结构"
    >
      {render(packTree(files))}
    </div>
  )
}

export function NpmPublishPanel({ root, busy, onRun }: Props) {
  const {
    data: rawStatus,
    isFetching: loading,
    error: statusError,
    refetch
  } = useNpmStatusQuery(root, { skip: !root })
  const status = rawStatus as NPMStatus | undefined
  const [getPreview] = useLazyNpmPackQuery()
  const [tag, setTag] = useState('latest')
  const [confirming, setConfirming] = useState(false)
  const [tokenMode, setTokenMode] = useState(false)
  const [token, setToken] = useState('')
  const [sourceCommit, setSourceCommit] = useState('')
  const [preview, setPreview] = useState<PackPreview | null>(null)
  const [previewing, setPreviewing] = useState(false)
  const [previewError, setPreviewError] = useState('')
  const refresh = async () => {
    await refetch()
  }
  const applySuggestedVersion = async () => {
    if (
      status?.suggestedVersion &&
      (await onRun('npm-version', { version: status.suggestedVersion }))
    ) {
      setPreview(null)
      await refresh()
    }
  }
  const createPreview = async () => {
    setPreviewing(true)
    setPreviewError('')
    try {
      setPreview(
        (await getPreview(
          { root, commit: sourceCommit },
          true
        ).unwrap()) as PackPreview
      )
    } catch {
      setPreviewError('无法生成所选提交的打包预览。')
    } finally {
      setPreviewing(false)
    }
  }
  const sourceCommits = status?.sourceCommits ?? emptySourceCommits
  useEffect(() => {
    if (!sourceCommits.some(item => item.sha === sourceCommit))
      setSourceCommit(sourceCommits[0]?.sha ?? '')
  }, [sourceCommits, sourceCommit])
  const publish = async () => {
    if (!confirming) {
      setConfirming(true)
      return
    }
    if (
      await onRun('npm-publish', {
        tag,
        token: tokenMode ? token : '',
        message: sourceCommit
      })
    ) {
      setToken('')
      setConfirming(false)
      setPreview(null)
      await refresh()
    }
  }
  if (loading)
    return (
      <p className="grid min-h-32 place-items-center text-sm text-slate-500">
        正在读取 npm 官方仓库与本机登录状态…
      </p>
    )
  if (statusError)
    return (
      <section className="grid min-h-32 place-items-center gap-3 rounded-xl border border-rose-200 bg-rose-50 p-5 text-sm text-rose-700 dark:border-rose-900 dark:bg-rose-950/30 dark:text-rose-300">
        <p className="m-0">无法读取 npm 发布状态。</p>
        <button
          className="inline-flex min-h-9 items-center justify-center rounded-lg border border-rose-300 bg-white px-3 text-xs font-semibold text-rose-700 dark:border-rose-800 dark:bg-slate-900 dark:text-rose-300"
          onClick={() => void refresh()}
        >
          重新检查
        </button>
      </section>
    )
  if (!status) return null
  const issues = status.issues ?? []
  const scripts = status.scripts ?? []
  const loginRequired = !status.loggedIn
  const otherIssues = issues.filter(issue => !issue.startsWith('尚未登录 npm'))
  const canPublish =
    preview !== null &&
    !!sourceCommit &&
    otherIssues.length === 0 &&
    (!loginRequired || (tokenMode && token.trim() !== ''))
  return (
    <section className="grid max-w-190 gap-4">
      <header className="workspace-page-header flex min-h-10 flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-3">
          <span className="workspace-page-header-icon">
            <Package className="size-4" />
          </span>
          <div className="workspace-page-header-meta">
            <strong>{status.name || '未命名包'}</strong>
            <small>npm 发布 · 管理标签、预览与发布状态</small>
          </div>
        </div>
        <div className="flex flex-wrap items-end justify-end gap-2">
          <label className="grid gap-1 text-[11px] font-semibold text-slate-500">
            <select
              className="h-9 rounded-lg border border-slate-200 bg-white px-2 text-xs font-semibold text-slate-700 outline-none focus:border-brand-600 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-200"
              value={tag}
              onChange={event => {
                setTag(event.target.value)
                setConfirming(false)
              }}
            >
              <option value="latest">latest</option>
              <option value="beta">beta</option>
              <option value="next">next</option>
            </select>
          </label>
          {confirming && (
            <button
              className="inline-flex size-9 items-center justify-center rounded-lg border border-slate-200 text-slate-500 dark:border-slate-600 dark:text-slate-300"
              onClick={() => setConfirming(false)}
              aria-label="取消发布"
              title="取消"
            >
              <X className="size-4" />
            </button>
          )}
          <button
            className="inline-flex size-9 items-center justify-center rounded-lg border border-slate-200 text-slate-500 disabled:opacity-40 dark:border-slate-600 dark:text-slate-300"
            disabled={busy}
            onClick={() => void refresh()}
            aria-label="刷新发布状态"
            title="刷新"
          >
            <RefreshCw className="size-4" />
          </button>
          <button
            className="inline-flex min-h-9 items-center justify-center rounded-lg bg-brand-600 px-3 text-xs font-semibold text-white hover:bg-brand-700 disabled:opacity-50"
            disabled={busy || !canPublish}
            onClick={() => void publish()}
          >
            {confirming ? '确认发布' : '发布到 npm'}
          </button>
        </div>
      </header>
      <section
        className="flex flex-wrap items-center divide-x divide-slate-200 rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-xs text-slate-500 dark:divide-slate-700 dark:border-slate-700 dark:bg-slate-800"
        aria-label="发布状态"
      >
        <span className="px-3 first:pl-0">
          本地{' '}
          <b className="ml-1 text-slate-800 dark:text-slate-200">
            {status.localVersion || '未设置'}
          </b>
        </span>
        <span className="px-3">
          npm{' '}
          <b className="ml-1 text-slate-800 dark:text-slate-200">
            {status.published ? status.latestVersion : '从未发布'}
          </b>
        </span>
        <span className="px-3 last:pr-0">
          账户{' '}
          <b
            className={cn(
              'ml-1',
              status.loggedIn
                ? 'text-brand-600 dark:text-brand-200'
                : 'text-amber-700 dark:text-amber-300'
            )}
          >
            {status.loggedIn ? status.username : '未登录'}
          </b>
        </span>
      </section>
      <section className="flex flex-wrap items-end justify-between gap-3 rounded-lg border border-slate-200 bg-white p-3">
        <div className="grid gap-1">
          <strong className="text-sm font-semibold text-slate-800">
            发布源码
          </strong>
          <small className="text-xs text-slate-500">
            npm 会从这个已提交版本打包，不会包含尚未提交的文件。
          </small>
        </div>
        <label className="grid min-w-65 gap-1 text-[11px] font-semibold text-slate-500">
          {status.branch || '当前分支'}
          <select
            className="h-9 rounded-md border border-slate-300 bg-white px-2 text-xs font-medium text-slate-700 outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
            value={sourceCommit}
            onChange={event => {
              setSourceCommit(event.target.value)
              setPreview(null)
              setConfirming(false)
            }}
            disabled={!sourceCommits.length}
          >
            {sourceCommits.length ? (
              sourceCommits.map(item => (
                <option key={item.sha} value={item.sha}>
                  {item.shortSha} · {item.subject} · {item.createdAt}
                </option>
              ))
            ) : (
              <option value="">暂无可选提交</option>
            )}
          </select>
        </label>
      </section>
      {status.suggestedVersion &&
        status.suggestedVersion !== status.localVersion && (
          <section className="flex items-center justify-between gap-3 rounded-lg border border-brand-200 bg-brand-50 px-3 py-2.5">
            <strong className="text-sm font-semibold text-brand-700">
              建议 v{status.suggestedVersion}
            </strong>
            <button
              className="secondary-button"
              disabled={busy}
              onClick={() => void applySuggestedVersion()}
            >
              采用
            </button>
          </section>
        )}
      {loginRequired && (
        <section className="grid gap-3 rounded-lg border border-amber-200 bg-amber-50 p-3">
          <div className="flex items-start gap-2">
            <i className="inline-flex size-5 shrink-0 items-center justify-center rounded-full bg-amber-600 text-xs font-bold text-white">
              !
            </i>
            <div className="grid gap-1">
              <strong className="text-sm font-semibold text-amber-900">
                先登录 npm
              </strong>
              <span className="text-xs text-amber-800">
                登录后刷新此页，即可继续发布。
              </span>
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <a
              className="secondary-button"
              href="https://www.npmjs.com/login"
              target="_blank"
              rel="noreferrer"
            >
              打开登录页
            </a>
            <button
              className="text-button"
              onClick={() => setTokenMode(value => !value)}
            >
              {tokenMode ? '改用网页登录' : '使用发布令牌'}
            </button>
            <a
              className="text-button"
              href="https://www.npmjs.com/settings/tokens"
              target="_blank"
              rel="noreferrer"
            >
              创建令牌
            </a>
          </div>
          {tokenMode && (
            <label className="grid gap-1 text-xs font-semibold text-amber-900">
              发布令牌
              <input
                className="h-9 rounded-md border border-amber-300 bg-white px-2.5 text-sm font-normal text-slate-800 outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
                type="password"
                value={token}
                onChange={event => {
                  setToken(event.target.value)
                  setConfirming(false)
                }}
                autoComplete="off"
                placeholder="npm_…"
              />
            </label>
          )}
        </section>
      )}
      {otherIssues.length > 0 && (
        <section className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2.5 text-sm text-amber-900">
          <ul className="grid list-disc gap-1 pl-5">
            {otherIssues.map(issue => (
              <li key={issue}>{issue}</li>
            ))}
          </ul>
        </section>
      )}
      <section className="grid gap-3 rounded-xl border border-slate-200 p-4">
        <header className="flex flex-wrap items-center justify-between gap-3">
          <div className="grid gap-1">
            <strong className="text-sm font-semibold text-slate-800">
              {preview ? '打包已就绪' : '确认打包内容'}
            </strong>
            <span className="text-xs text-slate-500">
              {preview
                ? `${preview.filename} · ${preview.fileCount} 个文件 · ${size(preview.unpackedSize)}`
                : '先生成预览，确认实际会上传的文件。'}
            </span>
          </div>
          <button
            className="secondary-button"
            disabled={busy || previewing}
            onClick={() => void createPreview()}
          >
            {previewing ? '预览中…' : preview ? '重新预览' : '查看打包内容'}
          </button>
        </header>
        {previewError && (
          <ErrorNotice
            message={previewError}
            onClose={() => setPreviewError('')}
          />
        )}
        {preview && (
          <details className="group">
            <summary className="cursor-pointer list-none text-xs font-semibold text-brand-700 [&::-webkit-details-marker]:hidden">
              查看文件清单
            </summary>
            <PackFileTree files={preview.files} />
          </details>
        )}
      </section>
      {scripts.length > 0 && (
        <details className="rounded-lg border border-slate-200 bg-slate-50 p-3">
          <summary className="cursor-pointer text-xs font-semibold text-slate-700">
            发布时 npm 会运行的脚本
          </summary>
          <div className="mt-2 grid gap-1">
            {scripts.map(script => (
              <code
                className="rounded bg-white px-2 py-1 font-mono text-xs text-brand-700"
                key={script}
              >
                {script}
              </code>
            ))}
          </div>
        </details>
      )}
    </section>
  )
}

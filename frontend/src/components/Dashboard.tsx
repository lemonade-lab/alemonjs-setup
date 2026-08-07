import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type MouseEvent,
  type ReactNode
} from 'react'
import { createPortal } from 'react-dom'
import { useDispatch, useSelector } from 'react-redux'
import cn from 'classnames'
import Markdown from 'markdown-to-jsx'
import {
  Archive,
  ArrowLeft,
  ArrowRight,
  ArrowRightLeft,
  Bot,
  Cable,
  Check,
  ChevronDown,
  ChevronRight,
  ClipboardList,
  Code2,
  Eye,
  EyeOff,
  Folder,
  GitBranch,
  Globe,
  Globe2,
  HardDrive,
  History,
  KeyRound,
  Link,
  MessageSquare,
  MoreVertical,
  Network,
  Package,
  Pencil,
  Pin,
  Play,
  Plug,
  Plus,
  Radio,
  RefreshCw,
  Route,
  Search,
  Send,
  Settings,
  Shield,
  Terminal,
  Trash2,
  Waypoints,
  Wifi,
  X,
  type LucideIcon
} from 'lucide-react'
import { RobotConfigForm } from './RobotConfigForm'
import { ThemeToggle } from './ThemeToggle'
import { Button } from './Button'
import { Tabs } from './Tabs'
import { NpmrcConfigForm } from './NpmrcConfigForm'
import { EnvConfigForm } from './EnvConfigForm'
import { NpmPublishPanel } from './NpmPublishPanel'
import { PackageManifestPanel } from './PackageManifestPanel'
import { SetupUpdateButton } from './SetupUpdateButton'
import { AgentChatPage } from './AgentChat'
import { ErrorNotice } from './ErrorNotice'
import { ConfirmDialog } from './ConfirmDialog'
import { Modal } from './Modal'
import { AuthControl } from './AuthControl'
import { RobotGitControl } from './RobotGitControl'
import { SSHControl } from './SSHControl'
import {
  workspaceApi,
  useCatalogDocumentQuery,
  useCatalogPackageConfigQuery,
  useCatalogQuery,
  useCatalogVersionsQuery,
  useGitStatusQuery,
  useGitWorkspaceQuery,
  useInitializeGitMutation,
  useLazyRobotConsoleQuery,
  useLazyRobotFileQuery,
  useLazyPackageConfigQuery,
  useLazyRobotRuntimePreflightQuery,
  useLazyRobotProjectQuery,
  useLocalPackagesQuery,
  useLocalPackageVersionsQuery,
  useLocalPackageReadmeQuery,
  usePackageConfigQuery,
  useRobotRuntimeQuery,
  useRobotPM2StatusQuery,
  useRobotPM2ProcessesQuery,
  useRobotTasksQuery,
  useSaveRobotLoginMutation,
  useRobotWebViewsQuery,
  useSetSetupPluginEnabledMutation,
  useSetupPluginsQuery,
  useStartRobotTaskMutation,
  useWritePackageConfigMutation,
  useWriteRobotFileMutation,
  type RuntimeOverview,
  type PM2Status,
  type RuntimePreflight,
  type RobotWebView,
  type SetupPlugin
} from '../store/workspaceApi'
import {
  addProjects,
  removeProject as removeWorkspaceProject,
  clearActiveWebviewTab,
  activateWebviewTab,
  closeWebviewTab,
  openWebviewTab,
  pruneWebviewTabs,
  selectProject,
  pinProject as pinWorkspaceProject,
  setDeveloperMode,
  setDraft
} from '../store/workspaceStore'
import {
  setProject as setGuideProject,
  type RootState
} from '../store/guideStore'

type Check = {
  id: string
  name: string
  status: 'ready' | 'missing' | 'warning'
  detail: string
  suggestion: string
}
type CatalogItem = {
  name: string
  description: string
  url: string
  install: string
}
type CatalogGroup = { title: string; items: CatalogItem[] }
type Page = 'robot' | 'build' | 'plugins' | 'connections'
type Section = 'backpack' | 'config' | 'npmrc' | 'env' | 'runtime'
type Project = { id: string; path: string; name: string }
type SystemFeature = string
type Props = {
  report: { checks: Check[] } | null
  checking: boolean
  error: string
  defaultPage: string
  onOpenGuide: () => void
  onClearError: () => void
  onCheck: () => void
  onFix: (check: Check) => void
  goals?: unknown
  goal?: unknown
  onSelect?: (id: string) => void
}

const coreFeatureCatalog: Array<{
  id: SystemFeature
  label: string
  icon: ReactNode
  status?: string
}> = [{ id: 'plugins', label: '插件', icon: <Plug /> }]
const directoryActions: Array<{
  id: Section | Page
  label: string
  icon: ReactNode
  kind: 'section' | 'page'
}> = [
  { id: 'runtime', label: '运行', icon: <Play />, kind: 'section' },
  { id: 'config', label: '配置', icon: <Settings />, kind: 'section' },
  { id: 'backpack', label: '背包', icon: <Archive />, kind: 'section' },
  { id: 'plugins', label: '插件', icon: <Package />, kind: 'page' },
  { id: 'connections', label: '连接', icon: <Link />, kind: 'page' },
  { id: 'build', label: '发布', icon: <Send />, kind: 'page' }
]
const emptyGitCommits: Array<{
  sha: string
  shortSha: string
  subject: string
  createdAt: string
}> = []
const emptyGitBranches: Array<{
  name: string
  commits: typeof emptyGitCommits
}> = []

const setupPluginIconMap: Record<string, LucideIcon> = {
  network: Network,
  forward: ArrowRightLeft,
  forwarding: ArrowRightLeft,
  interface: Radio,
  lan: Radio,
  wifi: Wifi,
  route: Route,
  dns: Globe,
  mirror: Globe,
  proxy: Globe,
  firewall: Shield,
  shield: Shield,
  port: Cable,
  traffic: Waypoints
}

function setupPluginIcon(icon?: string) {
  const Icon = icon ? setupPluginIconMap[icon] : undefined
  if (Icon) return <Icon />
  if (icon && icon.length === 1)
    return <span className="inline-block leading-none">{icon}</span>
  return <Plug />
}

function projectName(path: string) {
  return path.replace(/\/$/, '').split('/').pop() || path
}

// RTK Query rejects with a serialised object rather than an Error. Keep the
// server's explanation intact so a permission problem is never shown as the
// unhelpful generic "操作未完成".
function operationErrorMessage(reason: unknown, fallback: string) {
  if (reason instanceof Error && reason.message) return reason.message
  if (typeof reason === 'string' && reason) return reason
  if (reason && typeof reason === 'object') {
    const value = reason as {
      data?: { error?: unknown; message?: unknown } | string
      error?: unknown
      message?: unknown
    }
    const data = value.data
    if (typeof data === 'string' && data) return data
    if (data && typeof data === 'object') {
      if (typeof data.error === 'string' && data.error) return data.error
      if (typeof data.message === 'string' && data.message) return data.message
    }
    if (typeof value.error === 'string' && value.error) return value.error
    if (typeof value.message === 'string' && value.message) return value.message
  }
  return fallback
}

export function DirectoryPicker({
  open,
  multiple = true,
  priority = false,
  onClose,
  onSelect
}: {
  open: boolean
  multiple?: boolean
  priority?: boolean
  onClose: () => void
  onSelect: (paths: string[]) => void
}) {
  type Directory = { name: string; path: string }
  type DirectoryData = {
    path: string
    parent: string
    roots: string[]
    locations: Array<{
      name: string
      path: string
      kind: 'home' | 'disk' | 'volume'
    }>
    directories: Directory[]
  }
  const [path, setPath] = useState('')
  const [query, setQuery] = useState('')
  const [hidden, setHidden] = useState(false)
  const [data, setData] = useState<DirectoryData | null>(null)
  const [directoryError, setDirectoryError] = useState('')
  const [directoryReload, setDirectoryReload] = useState(0)
  const [selected, setSelected] = useState<string[]>([])
  const [history, setHistory] = useState<string[]>([])
  const [historyIndex, setHistoryIndex] = useState(-1)
  const [contextMenu, setContextMenu] = useState<{
    x: number
    y: number
    target?: Directory
  } | null>(null)
  const [newFolderName, setNewFolderName] = useState('')
  const [deleteTarget, setDeleteTarget] = useState<Directory | null>(null)

  const visit = (nextPath: string) => {
    if (!nextPath || nextPath === path) return
    setPath(nextPath)
    setSelected([])
    setHistory(entries => {
      const next = [...entries.slice(0, historyIndex + 1), nextPath]
      setHistoryIndex(next.length - 1)
      return next
    })
  }
  useEffect(() => {
    if (!open) return
    const controller = new AbortController()
    const parameters = new URLSearchParams(
      path ? { path, hidden: String(hidden) } : { hidden: String(hidden) }
    )
    void fetch(`/api/v1/directories?${parameters}`, {
      signal: controller.signal
    })
      .then(async response => {
        const body = await response.text()
        if (!response.ok) {
          try {
            const payload = JSON.parse(body) as { error?: string }
            throw new Error(payload.error || '目录无法读取。')
          } catch (reason) {
            if (reason instanceof Error) throw reason
            throw new Error('目录无法读取。')
          }
        }
        return JSON.parse(body) as DirectoryData
      })
      .then(result => {
        setData(result)
        setDirectoryError('')
        if (!path) {
          setPath(result.path)
          setHistory([result.path])
          setHistoryIndex(0)
        }
      })
      .catch((reason: unknown) => {
        if (!(reason instanceof DOMException && reason.name === 'AbortError')) {
          setDirectoryError(
            reason instanceof Error ? reason.message : '目录无法读取。'
          )
        }
      })
    return () => controller.abort()
  }, [directoryReload, hidden, open, path])
  if (!open) return null
  const items = (data?.directories ?? []).filter(item =>
    item.name.toLowerCase().includes(query.toLowerCase())
  )
  const selectDirectory = (
    itemPath: string,
    event: Pick<MouseEvent<HTMLButtonElement>, 'metaKey' | 'ctrlKey'>
  ) =>
    setSelected(current =>
      multiple && (event.metaKey || event.ctrlKey)
        ? current.includes(itemPath)
          ? current.filter(entry => entry !== itemPath)
          : [...current, itemPath]
        : [itemPath]
    )
  const home = data?.roots[0] ?? ''
  const favorites = [
    { name: 'home', path: home },
    ...['Desktop', 'Documents', 'Downloads', 'Pictures'].map(name => ({
      name,
      path: `${home}/${name}`
    }))
  ]
  const locations = data?.locations ?? []
  const goHistory = (step: number) => {
    const target = history[historyIndex + step]
    if (target) {
      setHistoryIndex(historyIndex + step)
      setPath(target)
      setSelected([])
    }
  }
  const directoryAction = async (
    method: 'POST' | 'DELETE',
    body: Record<string, string>
  ) => {
    try {
      const response = await fetch('/api/v1/directories', {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body)
      })
      const data = (await response.json()) as { error?: string }
      if (!response.ok) throw new Error(data.error || '目录操作未完成。')
      setDirectoryReload(current => current + 1)
      setDirectoryError('')
    } catch (reason) {
      setDirectoryError(
        reason instanceof Error ? reason.message : '目录操作未完成。'
      )
    }
  }
  return (
    <Modal
      open
      zIndex={priority ? 200 : 95}
      onBackdropClick={() => setContextMenu(null)}
      ariaLabel="选择目录"
    >
      <section
        className="directory-picker finder-picker grid h-[min(700px,calc(100vh-32px))] w-full max-w-5xl grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_24px_70px_rgb(28_26_23/0.26)]"
        role="dialog"
        aria-label="选择目录"
      >
        <header className="grid grid-cols-[auto_minmax(0,1fr)_minmax(180px,280px)] items-center gap-4 border-b border-slate-200 px-4 py-3">
          <div className="flex flex-row items-center gap-2">
            <nav className="flex items-center gap-1" aria-label="目录导航">
              <button
                className="icon-button size-8 p-0"
                disabled={historyIndex <= 0 && !data?.parent}
                onClick={() =>
                  historyIndex > 0 ? goHistory(-1) : visit(data?.parent ?? '')
                }
                title="后退"
                aria-label="后退"
              >
                <ArrowLeft className="size-4" />
              </button>
              <button
                className="icon-button size-8 p-0"
                disabled={historyIndex >= history.length - 1}
                onClick={() => goHistory(1)}
                title="前进"
                aria-label="前进"
              >
                <ArrowRight className="size-4" />
              </button>
              <button
                className="icon-button size-8 p-0"
                onClick={() => setHidden(value => !value)}
                title={hidden ? '隐藏隐藏目录' : '显示隐藏目录'}
                aria-label={hidden ? '隐藏隐藏目录' : '显示隐藏目录'}
              >
                {hidden ? (
                  <EyeOff className="size-4" />
                ) : (
                  <Eye className="size-4" />
                )}
              </button>
            </nav>
            <div className="hidden text-[11px] text-slate-400 lg:block">
              单击选择，⌘/Ctrl + 单击多选，双击打开
            </div>
          </div>
          <strong className="truncate text-center text-sm font-semibold text-slate-800">
            {data?.path
              ? /^[a-z]:[\\/]?$/i.test(data.path)
                ? `本地磁盘（${data.path.slice(0, 2).toUpperCase()}）`
                : data.path.split(/[\\/]/).filter(Boolean).pop() || '系统磁盘'
              : '选择目录'}
          </strong>
          <label className="flex h-9 items-center gap-2 rounded-md border border-slate-300 px-2.5 text-slate-400 focus-within:border-brand-600 focus-within:ring-2 focus-within:ring-brand-100">
            <Search className="size-4" />
            <input
              className="min-w-0 flex-1 bg-transparent text-xs text-slate-800 outline-none placeholder:text-slate-400"
              value={query}
              onChange={event => setQuery(event.target.value)}
              placeholder="搜索当前目录"
            />
          </label>
        </header>
        <section className="grid min-h-0 grid-cols-[190px_minmax(0,1fr)]">
          <aside className="grid content-start gap-1 overflow-auto border-r border-slate-200 bg-slate-50 p-3">
            <small className="mb-1 px-2 text-[11px] font-semibold text-slate-400">
              常用
            </small>
            {favorites.map(item => (
              <button
                className={cn(
                  'flex min-h-8 items-center gap-2 rounded-md px-2 text-xs font-medium transition',
                  item.path === data?.path
                    ? 'bg-slate-200 text-slate-900'
                    : 'text-slate-600 hover:bg-slate-100'
                )}
                key={item.path}
                onClick={() => visit(item.path)}
              >
                <Folder className="size-4 text-slate-500" />
                {item.name}
              </button>
            ))}
            {locations.length > 0 && (
              <>
                <small className="mb-1 mt-3 px-2 text-[11px] font-semibold text-slate-400">
                  磁盘与位置
                </small>
                {locations.map(location => (
                  <button
                    className={cn(
                      'flex min-h-8 items-center gap-2 rounded-md px-2 text-xs font-medium transition',
                      location.path === data?.path
                        ? 'bg-slate-200 text-slate-900'
                        : 'text-slate-600 hover:bg-slate-100'
                    )}
                    key={location.path}
                    onClick={() => visit(location.path)}
                    title={location.path}
                  >
                    {location.kind === 'home' ? (
                      <Folder className="size-4 text-slate-500" />
                    ) : (
                      <HardDrive className="size-4 text-slate-500" />
                    )}
                    {location.name}
                  </button>
                ))}
              </>
            )}
          </aside>
          <main className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)]">
            <header className="grid grid-cols-[minmax(0,1fr)_100px] border-b border-slate-200 px-4 py-2 text-[11px] font-semibold text-slate-400">
              <span>名称</span>
              <span>种类</span>
            </header>
            <div className="grid content-start gap-0.5 overflow-auto p-2">
              {directoryError && (
                <div className="m-2 grid gap-2 rounded-lg border border-red-200 bg-red-50 p-3 text-xs text-red-800">
                  <strong>需要访问授权</strong>
                  <span>{directoryError}</span>
                  <button
                    className="secondary-button justify-self-start"
                    onClick={() => setDirectoryReload(current => current + 1)}
                  >
                    重试
                  </button>
                </div>
              )}
              {items.map(item => (
                <button
                  className={cn(
                    'grid min-h-9 grid-cols-[minmax(0,1fr)_100px] items-center rounded-md px-2 text-left text-xs transition',
                    selected.includes(item.path)
                      ? 'bg-slate-200 text-slate-900'
                      : 'text-slate-700 hover:bg-slate-100'
                  )}
                  key={item.path}
                  onClick={event => selectDirectory(item.path, event)}
                  onDoubleClick={() => visit(item.path)}
                  onContextMenu={event => {
                    event.preventDefault()
                    event.stopPropagation()
                    setSelected([item.path])
                    setContextMenu({
                      x: event.clientX,
                      y: event.clientY,
                      target: item
                    })
                  }}
                >
                  <span className="flex min-w-0 items-center gap-2">
                    <Folder className="size-4 shrink-0 text-slate-500" />
                    <span className="truncate">{item.name}</span>
                  </span>
                  <small className="text-[11px] text-slate-400">文件夹</small>
                </button>
              ))}
            </div>
          </main>
        </section>
        <footer className="flex items-center justify-between gap-3 border-t border-slate-200 px-4 py-3">
          <span
            className="min-w-0 truncate text-xs text-slate-500"
            title={data?.path ?? ''}
          >
            {data?.path ?? '正在读取目录…'}
          </span>
          <div className="flex shrink-0 gap-2">
            <button className="secondary-button" onClick={onClose}>
              取消
            </button>
            <button
              className="primary-button"
              disabled={!selected.length}
              onClick={() => onSelect(selected)}
            >
              {multiple ? '添加' : '选择'}
            </button>
          </div>
        </footer>
      </section>
      {contextMenu && (
        <div
          className="fixed z-[210] grid min-w-32 overflow-hidden rounded-md border border-slate-200 bg-white py-1 shadow-lg"
          style={{ left: contextMenu.x, top: contextMenu.y }}
          role="menu"
          onClick={event => event.stopPropagation()}
        >
          <button
            className="px-3 py-2 text-left text-xs text-slate-700 hover:bg-slate-100"
            onClick={() => {
              setNewFolderName(' ')
              setContextMenu(null)
            }}
          >
            新建文件夹
          </button>
          {contextMenu.target && (
            <button
              className="px-3 py-2 text-left text-xs text-red-700 hover:bg-red-50"
              onClick={() => {
                setDeleteTarget(contextMenu.target ?? null)
                setContextMenu(null)
              }}
            >
              删除文件夹
            </button>
          )}
        </div>
      )}
      {newFolderName !== '' && (
        <Modal open zIndex={220} ariaLabel="新建文件夹">
          <form
            className="grid w-full max-w-sm gap-3 rounded-xl bg-white p-4 shadow-xl"
            onSubmit={event => {
              event.preventDefault()
              const name = newFolderName.trim()
              if (!name || !data?.path) return
              void directoryAction('POST', { path: data.path, name })
              setNewFolderName(' ')
            }}
          >
            <strong className="text-sm text-slate-800">新建文件夹</strong>
            <input
              autoFocus
              className="h-9 rounded-md border border-slate-300 px-2 text-sm outline-none focus:border-brand-600"
              value={newFolderName}
              onChange={event => setNewFolderName(event.target.value)}
              placeholder="文件夹名称"
            />
            <div className="flex justify-end gap-2">
              <button
                type="button"
                className="secondary-button"
                onClick={() => setNewFolderName('')}
              >
                取消
              </button>
              <button
                className="primary-button"
                disabled={!newFolderName.trim()}
              >
                新建
              </button>
            </div>
          </form>
        </Modal>
      )}
      {deleteTarget && (
        <ConfirmDialog
          open
          title="删除文件夹"
          subtitle={deleteTarget.name}
          message="将永久删除该文件夹及其中的全部内容，此操作无法撤销。"
          confirmLabel="删除"
          destructive
          onCancel={() => setDeleteTarget(null)}
          onConfirm={() => {
            void directoryAction('DELETE', { path: deleteTarget.path })
            setDeleteTarget(null)
          }}
        />
      )}
    </Modal>
  )
}

export function Dashboard({
  report,
  checking,
  error,
  defaultPage,
  onOpenGuide,
  onClearError,
  onCheck,
  onFix
}: Props) {
  const [page, setPage] = useState<Page>('robot')
  const [systemFeature, setSystemFeature] = useState<SystemFeature | null>(null)
  const [section, setSection] = useState<Section>('runtime')
  const [file, setFile] = useState('.npmrc')
  const [output, setOutput] = useState('')
  const [outputFailed, setOutputFailed] = useState(false)
  const [consoleOpen, setConsoleOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [catalogTitle, setCatalogTitle] = useState('')
  const [catalogItem, setCatalogItem] = useState<CatalogItem | null>(null)
  const [configEditor, setConfigEditor] = useState<'visual' | 'text'>('visual')
  const [buildMode, setBuildMode] = useState<'manifest' | 'npm' | 'git'>('git')
  const [releaseVersion, setReleaseVersion] = useState('')
  const [environmentOpen, setEnvironmentOpen] = useState(false)
  const [directoryPickerOpen, setDirectoryPickerOpen] = useState(false)
  const [cloneProgress, setCloneProgress] = useState(0)
  const [gitCloneOpen, setGitCloneOpen] = useState(false)
  const [gitDestinationPickerOpen, setGitDestinationPickerOpen] =
    useState(false)
  const [gitDestination, setGitDestination] = useState('')
  const [gitProject, setGitProject] = useState<Project | null>(null)
  const [invalidDirectory, setInvalidDirectory] = useState('')
  const [pendingBackpackRemoval, setPendingBackpackRemoval] = useState('')
  const [pendingProjectRemoval, setPendingProjectRemoval] = useState<
    string | null
  >(null)
  const [trackRuntimeTasks, setTrackRuntimeTasks] = useState(false)
  const [aiOpen, setAIOpen] = useState(false)
  const [agentSessions, setAgentSessions] = useState<
    Array<{ id: string; title: string; root: string; updated: string }>
  >([])
  const [agentSessionId, setAgentSessionId] = useState('')
  const [renameTarget, setRenameTarget] = useState<{
    id: string
    title: string
  } | null>(null)
  const [renameTitle, setRenameTitle] = useState('')
  const loadAgentSessions = useCallback(async () => {
    try {
      const response = await fetch('/api/v1/agent/sessions')
      if (!response.ok) return
      const data = (await response.json()) as Array<{
        id: string
        title: string
        root: string
        updated: string
      }>
      setAgentSessions(data)
    } catch {
      // 会话列表加载失败不阻塞
    }
  }, [])
  useEffect(() => {
    void loadAgentSessions()
  }, [loadAgentSessions])
  useEffect(() => {
    const refresh = () => {
      void loadAgentSessions()
    }
    const clearSession = () => {
      setAgentSessionId('')
    }
    window.addEventListener('alx:agent-session-created', refresh)
    window.addEventListener('alx:agent-new-session', clearSession)
    return () => {
      window.removeEventListener('alx:agent-session-created', refresh)
      window.removeEventListener('alx:agent-new-session', clearSession)
    }
  }, [loadAgentSessions])
  useEffect(() => {
    const closeWhenAnotherToolOpens = (event: Event) => {
      if ((event as CustomEvent<string>).detail !== 'environment')
        setEnvironmentOpen(false)
    }
    window.addEventListener('alx:top-tool-open', closeWhenAnotherToolOpens)
    return () =>
      window.removeEventListener('alx:top-tool-open', closeWhenAnotherToolOpens)
  }, [])
  const environmentChecked = useRef(false)
  const rootParamHandled = useRef(false)
  const dispatch = useDispatch()
  const rawProjects = useSelector(
    (state: RootState) => state.workspace.projects
  )
  // Keep a stable array reference so effects depending on it do not re-run on
  // every render when the selector briefly yields null/undefined.
  const projects = useMemo(
    () => (rawProjects ?? []) as Project[],
    [rawProjects]
  )
  const activeProjectID = useSelector(
    (state: RootState) => state.workspace.activeProjectID
  )
  const webviewTabs =
    useSelector((state: RootState) => state.workspace.webviewTabs) ?? []
  const activeWebviewTabKey = useSelector(
    (state: RootState) => state.workspace.activeWebviewTabKey
  )
  const developerMode = useSelector(
    (state: RootState) => state.workspace.developerMode
  )
  const activeProject = projects.find(item => item.id === activeProjectID)
  const root = activeProject?.path ?? ''
  const activeWebviewTab = webviewTabs.find(
    item => item.key === activeWebviewTabKey
  )
  const activeWebViewID =
    activeWebviewTab?.root === root ? activeWebviewTab.entryID : ''
  const draftKey = `${root}:${file}`
  const content = useSelector(
    (state: RootState) => state.workspace.drafts[draftKey] ?? ''
  )
  const configContent = useSelector(
    (state: RootState) =>
      state.workspace.drafts[`${root}:alemon.config.yaml`] ?? ''
  )
  const setContent = (nextContent: string) =>
    dispatch(setDraft({ key: draftKey, content: nextContent }))
  const catalogKind = page === 'plugins' ? 'apps' : 'environment'
  const {
    data: catalog = [],
    isFetching: catalogLoading,
    error: catalogQueryError
  } = useCatalogQuery(catalogKind, {
    skip: page !== 'plugins' && page !== 'connections',
    refetchOnMountOrArgChange: true
  })
  const {
    data: localPackages,
    isFetching: packagesLoading,
    error: packagesError,
    refetch: refetchPackages
  } = useLocalPackagesQuery(root, { skip: !root || section !== 'backpack' })
  const { data: robotWebViews = [], isLoading: webViewsLoading } =
    useRobotWebViewsQuery(root, {
      skip: !root
    })
  const {
    data: runtime,
    isFetching: runtimeLoading,
    refetch: refetchRuntime
  } = useRobotRuntimeQuery(root, { skip: !root })
  const {
    data: pm2Status,
    error: pm2StatusError,
    refetch: refetchPM2Status
  } = useRobotPM2StatusQuery(root, {
    // Always query once a root is selected. Skipping on pm2Configured meant a
    // freshly generated pm2.config.cjs (right after "修复后台运行") never woke
    // the query, so the card stayed on "启动服务" even after PM2 came online.
    skip: !root,
    refetchOnMountOrArgChange: true
  })
  const {
    data: currentPackageConfig,
    isFetching: currentPackageConfigLoading
  } = usePackageConfigQuery(
    { root, package: '' },
    { skip: !root || section !== 'config' || configEditor !== 'visual' }
  )
  const watchDevelopmentTask = page === 'robot' && section === 'runtime'
  const { data: operationTasks = [] } = useRobotTasksQuery(undefined, {
    skip: !watchDevelopmentTask,
    pollingInterval: trackRuntimeTasks ? 1200 : 0,
    refetchOnMountOrArgChange: true
  })
  useEffect(() => {
    setTrackRuntimeTasks(operationTasks.some(item => item.status === 'running'))
  }, [operationTasks])
  const [readRobotFile] = useLazyRobotFileQuery()
  const [validateRobot, { data: projectValidation }] =
    useLazyRobotProjectQuery()
  const [startRobotTask] = useStartRobotTaskMutation()
  const [writeRobotFile] = useWriteRobotFileMutation()
  const [writePackageConfig] = useWritePackageConfigMutation()
  const [saveRobotLogin] = useSaveRobotLoginMutation()
  const [initializeGit] = useInitializeGitMutation()
  const { data: setupPlugins = [] } = useSetupPluginsQuery(undefined, {
    pollingInterval: 3000
  })
  const catalogError = catalogQueryError ? '在线目录暂时无法读取。' : ''
  const showOutput = (message: string, failed = false) => {
    setOutput(message)
    setOutputFailed(failed)
  }
  const refreshConfigDraft = async () => {
    if (!root) return
    const result = await readRobotFile(
      { root, file: 'alemon.config.yaml' },
      true
    ).unwrap()
    dispatch(
      setDraft({
        key: `${root}:alemon.config.yaml`,
        content: result.output ?? ''
      })
    )
  }

  useEffect(() => {
    if (defaultPage === 'robot') setPage('robot')
  }, [defaultPage])
  // Open /dashboard/robot?root=<path> links by adding that directory (if it is
  // not already listed) and selecting it, restoring the pre-refactor behaviour
  // of the "前往管理机器人" links generated after project creation.
  useEffect(() => {
    if (rootParamHandled.current || !projects) return
    rootParamHandled.current = true
    const param = new URLSearchParams(window.location.search).get('root')
    if (!param) return
    const path = param
    if (projects.some(item => item.path === path)) {
      dispatch(selectProject(projects.find(item => item.path === path)!.id))
      return
    }
    void (async () => {
      try {
        const response = await fetch(
          `/api/v1/robot/validate?${new URLSearchParams({ root: path })}`
        )
        const data = (await response.json()) as { valid?: boolean }
        if (response.ok && data.valid === true) {
          dispatch(addProjects([{ id: path, path, name: projectName(path) }]))
        }
      } catch {
        // A missing/invalid directory must not break the dashboard render.
      }
    })()
  }, [dispatch, projects])
  useEffect(() => {
    if (report || checking || environmentChecked.current) return
    environmentChecked.current = true
    onCheck()
  }, [checking, onCheck, page, report])
  useEffect(() => {
    if (!catalogTitle && catalog.length) setCatalogTitle(catalog[0].title)
  }, [catalog, catalogTitle])
  useEffect(() => {
    if (root) void validateRobot(root)
  }, [root, validateRobot])
  useEffect(() => {
    if (root && !webViewsLoading)
      dispatch(
        pruneWebviewTabs({ root, entryIDs: robotWebViews.map(item => item.id) })
      )
  }, [dispatch, robotWebViews, root, webViewsLoading])
  useEffect(() => {
    if (!root || section !== 'config') return
    void readRobotFile({ root, file: 'alemon.config.yaml' }, true)
      .unwrap()
      .then(result =>
        dispatch(
          setDraft({
            key: `${root}:alemon.config.yaml`,
            content: result.output ?? ''
          })
        )
      )
      .catch(() =>
        dispatch(setDraft({ key: `${root}:alemon.config.yaml`, content: '' }))
      )
  }, [dispatch, readRobotFile, root, section])
  useEffect(() => {
    if (developerMode) return
    if (page === 'build') setPage('robot')
    if (section === 'npmrc' || section === 'env') setSection('config')
    setConsoleOpen(false)
  }, [developerMode, page, section])

  async function api(
    method: string,
    data: Record<string, string>
  ): Promise<boolean> {
    if (!root) {
      showOutput('请先在左侧添加机器人目录。', true)
      return false
    }
    setBusy(true)
    try {
      if (method === 'GET') {
        const result = await readRobotFile(
          { root: data.root, file: data.file },
          true
        ).unwrap()
        dispatch(
          setDraft({
            key: `${data.root}:${data.file}`,
            content: result.output ?? ''
          })
        )
        return true
      }
      if (method === 'PUT') {
        const result = await writeRobotFile({
          root: data.root,
          file: data.file,
          content: data.content
        }).unwrap()
        showOutput(result.output ?? '操作完成。')
        return true
      }
      const task = await startRobotTask(data).unwrap()
      if (data.action === 'dev' || data.action === 'app') {
        setOutput('')
        setConsoleOpen(true)
        return true
      }
      showOutput('操作已开始，正在等待完成…')
      for (;;) {
        await new Promise(resolve => window.setTimeout(resolve, 700))
        const response = await fetch(
          `/api/v1/robot/tasks?${new URLSearchParams({ id: task.id })}`
        )
        const current = (await response.json()) as {
          status: string
          output?: string
          error?: string
        }
        if (current.status === 'running') continue
        dispatch(
          workspaceApi.util.invalidateTags([{ type: 'Runtime', id: root }])
        )
        if (current.status === 'failed')
          throw new Error(current.error ?? '操作未完成。')
        if (
          [
            'install-package',
            'uninstall-package',
            'remove-local-package',
            'replace-local-package',
            'switch-local-package-version'
          ].includes(data.action)
        ) {
          // The task mutation invalidates when it starts, which is still too
          // early for a download. Invalidate once it has actually finished so
          // the backpack and WebView shortcuts update without a page reload.
          dispatch(
            workspaceApi.util.invalidateTags([
              { type: 'LocalPackages', id: root },
              { type: 'RobotWebViews', id: root },
              // Installing/uninstalling a connection package changes whether
              // its alemon.config.yaml section can be parsed, so drop any
              // cached PackageConfig for this root.
              { type: 'PackageConfig', id: root },
              { type: 'PackageConfig', id: `${root}:${data.package ?? ''}` }
            ])
          )
        }
        showOutput(current.output ?? '操作完成。')
        return true
      }
    } catch (reason) {
      showOutput(
        operationErrorMessage(
          reason,
          '操作未完成，请在右上角任务记录中查看详情。'
        ),
        true
      )
      return false
    } finally {
      setBusy(false)
    }
  }

  async function savePackageConfig(
    packageName: string,
    values: Record<string, string>
  ): Promise<boolean> {
    if (!root) return false
    setBusy(true)
    try {
      const result = await writePackageConfig({
        root,
        package: packageName,
        values
      }).unwrap()
      await refreshConfigDraft()
      showOutput(result.output ?? '机器人运行配置已保存。')
      return true
    } catch (reason) {
      showOutput(
        operationErrorMessage(reason, '配置未保存，请检查所选机器人目录。'),
        true
      )
      return false
    } finally {
      setBusy(false)
    }
  }

  async function saveRuntimeLogin(
    login: string,
    packageName = ''
  ): Promise<boolean> {
    if (!root || !login.trim()) return false
    setBusy(true)
    try {
      const result = await saveRobotLogin({
        root,
        login: login.trim(),
        package: packageName
      }).unwrap()
      await refreshConfigDraft()
      showOutput(result.output ?? '登录连接已保存。')
      return true
    } catch (reason) {
      showOutput(operationErrorMessage(reason, '登录连接未保存。'), true)
      return false
    } finally {
      setBusy(false)
    }
  }

  async function initializeProjectGit(values: {
    authorName: string
    authorEmail: string
    repository: string
    message: string
  }): Promise<boolean> {
    if (!root) return false
    setBusy(true)
    try {
      const result = await initializeGit({ root, ...values }).unwrap()
      showOutput(result.output ?? 'Git 仓库已初始化。')
      return true
    } catch (reason) {
      showOutput(
        operationErrorMessage(
          reason,
          'Git 初始化未完成，请检查所选机器人目录。'
        ),
        true
      )
      return false
    } finally {
      setBusy(false)
    }
  }

  async function chooseDirectories() {
    setDirectoryPickerOpen(true)
  }
  async function addSelectedDirectories(paths: string[]) {
    if (!paths.length) return
    const checks = await Promise.all(
      paths.map(async path => {
        try {
          const response = await fetch(
            `/api/v1/robot/validate?${new URLSearchParams({ root: path })}`
          )
          const data = (await response.json()) as { valid?: boolean }
          return { path, valid: response.ok && data.valid === true }
        } catch {
          return { path, valid: false }
        }
      })
    )
    const validPaths = checks.filter(item => item.valid).map(item => item.path)
    const invalid = checks.find(item => !item.valid)?.path
    if (validPaths.length)
      dispatch(
        addProjects(
          validPaths.map(path => ({ id: path, path, name: projectName(path) }))
        )
      )
    setDirectoryPickerOpen(false)
    if (invalid) {
      setInvalidDirectory(invalid)
      return
    }
    setPage('robot')
    setSection('config')
    setOutput('')
  }
  async function cloneRobotRepository(
    repository: string,
    branch: string,
    name: string,
    mirror: string,
    depth: number
  ) {
    if (!gitDestination) return
    setBusy(true)
    setCloneProgress(10)
    try {
      const response = await fetch('/api/v1/robot/git-clone', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          destination: gitDestination,
          repository,
          branch,
          name,
          mirror,
          depth
        })
      })
      const data = (await response.json()) as {
        id?: string
        output?: string
        error?: string
      }
      if (!response.ok || !data.id)
        throw new Error(data.error || '克隆仓库失败。')
      for (;;) {
        await new Promise(resolve => window.setTimeout(resolve, 550))
        const taskResponse = await fetch(
          `/api/v1/robot/tasks?${new URLSearchParams({ id: data.id })}`
        )
        const task = (await taskResponse.json()) as {
          status?: string
          progress?: number
          path?: string
          output?: string
          error?: string
        }
        setCloneProgress(task.progress ?? 10)
        if (task.status === 'running') continue
        if (task.status === 'failed')
          throw new Error(task.error || '克隆仓库失败。')
        const targetPath = task.path
        if (!targetPath) throw new Error('克隆完成，但无法识别机器人目录。')
        showOutput(task.output || '仓库已克隆。')
        setGitCloneOpen(false)
        await addSelectedDirectories([targetPath])
        return
      }
    } catch (reason) {
      showOutput(
        operationErrorMessage(reason, '克隆仓库失败，请检查 Git 地址和网络。'),
        true
      )
    } finally {
      setBusy(false)
      setCloneProgress(0)
    }
  }

  function removeProject(id: string) {
    setPendingProjectRemoval(id)
  }

  const pinProject = (id: string) => {
    dispatch(pinWorkspaceProject(id))
  }

  function confirmRemoveProject() {
    if (!pendingProjectRemoval) return
    dispatch(removeWorkspaceProject(pendingProjectRemoval))
    setPendingProjectRemoval(null)
    setOutput('')
  }

  // AI and an installed plugin WebView are content pages, not overlays. Any
  // normal navigation must leave them first so the control card always works.
  function closeTemporaryContentPage() {
    setAIOpen(false)
    dispatch(clearActiveWebviewTab())
  }

  function openSection(nextSection: Section) {
    closeTemporaryContentPage()
    setSection(nextSection)
    setOutput('')
    if (nextSection === 'npmrc') {
      setFile('.npmrc')
      api('GET', { root, file: '.npmrc' })
    }
    if (nextSection === 'env') {
      setFile('.env')
      api('GET', { root, file: '.env' })
    }
  }
  function openTextConfig() {
    setConfigEditor('text')
    setFile('alemon.config.yaml')
  }
  function selectPage(nextPage: Page) {
    closeTemporaryContentPage()
    setSystemFeature(null)
    setPage(nextPage)
    setCatalogItem(null)
    setOutput('')
  }
  function openAI(sessionID?: string) {
    closeTemporaryContentPage()
    setSystemFeature(null)
    setPage('robot')
    if (typeof sessionID === 'object' && sessionID !== null) {
      console.warn('openAI 收到对象参数，已忽略：', sessionID)
      sessionID = ''
    }
    setAgentSessionId(sessionID ?? '')
    setAIOpen(true)
    setOutput('')
    // 每次进入 Agent 都刷新会话列表，确保"记录"能看到新建的对话。
    void loadAgentSessions()
  }
  function requestRename(id: string, title: string) {
    setRenameTarget({ id, title })
    setRenameTitle(title)
  }
  async function archiveSession(id: string) {
    try {
      const response = await fetch(`/api/v1/agent/sessions/${id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ archived: true })
      })
      if (!response.ok) return
      if (id === agentSessionId) openAI()
      await loadAgentSessions()
    } catch {
      // 归档失败不阻塞
    }
  }
  async function renameSession(id: string) {
    if (!renameTarget || renameTitle.trim().length < 2) return
    try {
      const response = await fetch(`/api/v1/agent/sessions/${id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title: renameTitle.trim() })
      })
      if (!response.ok) return
      setRenameTarget(null)
      await loadAgentSessions()
    } catch {
      // 重命名失败不阻塞
    }
  }
  function selectSystemFeature(nextFeature: SystemFeature) {
    closeTemporaryContentPage()
    setSystemFeature(nextFeature)
    setOutput('')
  }

  const currentCatalog =
    catalog.find(group => group.title === catalogTitle) ?? catalog[0]
  const readyCount =
    report?.checks.filter(item => item.status === 'ready').length ?? 0
  const robotContent = aiOpen ? (
    <AgentChatPage root={root} initialSessionId={agentSessionId} />
  ) : (
    <section className="workspace-content">
      {section === 'backpack' && (
        <BackpackPanel
          root={root}
          items={localPackages?.items ?? []}
          loading={packagesLoading}
          failed={Boolean(packagesError)}
          onRefresh={() => void refetchPackages()}
          onOpenPlugins={() => selectPage('plugins')}
          busy={busy}
          onSaveConfig={savePackageConfig}
          onRemove={async packageName => setPendingBackpackRemoval(packageName)}
          onReplace={async (packageName, version) =>
            api('POST', {
              root,
              action: 'switch-local-package-version',
              package: packageName,
              version
            })
          }
        />
      )}
      {developerMode && section === 'npmrc' && (
        <NpmrcConfigForm
          content={content}
          busy={busy}
          onChange={setContent}
          onSave={nextContent =>
            api('PUT', { root, file: '.npmrc', content: nextContent })
          }
        />
      )}
      {developerMode && section === 'env' && (
        <EnvConfigForm
          content={content}
          busy={busy}
          onChange={setContent}
          onSave={nextContent =>
            api('PUT', { root, file: '.env', content: nextContent })
          }
        />
      )}
      {section === 'config' && (
        <section className="config-form">
          {configEditor === 'visual' ? (
            <>
              <RobotConfigForm
                content={configContent}
                busy={busy}
                onChange={next =>
                  dispatch(
                    setDraft({
                      key: `${root}:alemon.config.yaml`,
                      content: next
                    })
                  )
                }
                toolbar={
                  <EditorMode
                    active={configEditor}
                    onVisual={() => setConfigEditor('visual')}
                    onText={openTextConfig}
                  />
                }
                onSave={config =>
                  api('PUT', {
                    root,
                    file: 'alemon.config.yaml',
                    content: config
                  })
                }
              />
              <CurrentProjectConfigPanel
                config={currentPackageConfig}
                loading={currentPackageConfigLoading}
                busy={busy}
                onSave={values => savePackageConfig('', values)}
              />
            </>
          ) : (
            <FileEditor
              toolbar={
                <EditorMode
                  active={configEditor}
                  onVisual={() => setConfigEditor('visual')}
                  onText={openTextConfig}
                />
              }
              content={content}
              busy={busy}
              placeholder="配置内容"
              onChange={setContent}
              onSave={() => api('PUT', { root, file, content })}
            />
          )}
        </section>
      )}
      {section === 'runtime' && (
        <RuntimePanel
          overview={runtime}
          pm2Status={pm2Status}
          pm2StatusError={Boolean(pm2StatusError)}
          root={root}
          loading={runtimeLoading}
          busy={busy}
          developmentRunning={operationTasks.some(
            item =>
              item.root === root &&
              item.action === 'dev' &&
              item.status === 'running'
          )}
          foregroundRunning={operationTasks.some(
            item =>
              item.root === root &&
              item.action === 'app' &&
              item.status === 'running'
          )}
          developmentStopping={operationTasks.some(
            item =>
              item.root === root &&
              item.action === 'dev-stop' &&
              item.status === 'running'
          )}
          foregroundStopping={operationTasks.some(
            item =>
              item.root === root &&
              item.action === 'app-stop' &&
              item.status === 'running'
          )}
          onRefresh={() => {
            void refetchRuntime()
            void refetchPM2Status()
          }}
          onOpenConsole={() => setConsoleOpen(true)}
          onRun={(action, packageName) =>
            api('POST', {
              root,
              action,
              ...(packageName ? { package: packageName } : {})
            }).then(async success => {
              if (success) {
                // Refresh before the caller continues (e.g. installing a
                // connection package) so the runtime overview's per-platform
                // "installed" flag is already fresh. A failed refresh must not
                // reject the original operation result.
                try {
                  await refetchRuntime().unwrap()
                  // PM2 status is always refreshed here; gating it on the
                  // closure's pm2Configured would skip the fetch right after a
                  // "修复后台运行" generated the config, leaving the card stuck
                  // on "启动服务".
                  await refetchPM2Status()
                } catch {
                  // Keep the original result; the overview refetches on next poll.
                }
              }
              return success
            })
          }
          onRefreshOverview={async () => {
            try {
              return await refetchRuntime().unwrap()
            } catch {
              return runtime
            }
          }}
          pm2Running={Boolean(pm2Status?.running)}
          onSaveLogin={saveRuntimeLogin}
          onSavePackageConfig={savePackageConfig}
          developerMode={developerMode}
        />
      )}
    </section>
  )

  const catalogContent = (
    <section className="workspace-content">
      {catalogLoading && <p className="catalog-state">正在读取目录…</p>}
      {catalogError && <p className="catalog-state">{catalogError}</p>}
      {!catalogLoading &&
        !catalogError &&
        currentCatalog &&
        (catalogItem ? (
          <CatalogDetail
            item={catalogItem}
            group={currentCatalog.title}
            kind={page === 'connections' ? 'connection' : 'plugin'}
            busy={busy}
            onBack={() => setCatalogItem(null)}
            onRun={(action, packageName) =>
              api('POST', { root, action, package: packageName })
            }
            onSaveConfig={savePackageConfig}
          />
        ) : (
          <section className="grid max-w-[760px] gap-2">
            {currentCatalog.items.map(item => (
              <button
                className="flex items-center gap-3 rounded-lg border border-slate-200 bg-white p-3 text-left transition hover:border-slate-300 hover:bg-slate-50"
                key={`${currentCatalog.title}-${item.name}`}
                onClick={() => setCatalogItem(item)}
              >
                <span className="grid min-w-0 flex-1 gap-1">
                  <strong className="truncate text-sm font-semibold text-slate-800">
                    {item.name}
                  </strong>
                  <small className="truncate text-xs text-slate-500">
                    {item.description || '查看包说明、安装与配置'}
                  </small>
                </span>
                <ChevronRight className="size-4 shrink-0 text-slate-400" />
              </button>
            ))}
          </section>
        ))}
    </section>
  )
  const setupPlugin = setupPlugins.find(
    item => systemFeature === `setup:${item.id}`
  )
  const invalidProject = Boolean(
    activeProject && projectValidation && !projectValidation.valid
  )
  const activeWebView = robotWebViews.find(item => item.id === activeWebViewID)
  const workspace =
    systemFeature === 'plugins' ? (
      <SystemPluginCenter
        plugins={setupPlugins}
        onOpen={id => selectSystemFeature(`setup:${id}`)}
      />
    ) : setupPlugin ? (
      <SetupPluginCenter plugin={setupPlugin} />
    ) : invalidProject ? (
      <InvalidWorkspace
        project={activeProject!}
        reason={projectValidation?.error}
        onRemove={() => removeProject(activeProject!.id)}
        onChoose={chooseDirectories}
      />
    ) : activeProject ? (
      <>
        {activeWebView ? (
          <RobotPluginWebView
            root={root}
            entries={robotWebViews}
            tabs={webviewTabs
              .filter(tab => tab.root === root)
              .sort((left, right) =>
                left.openedAt.localeCompare(right.openedAt)
              )}
            activeTabKey={activeWebviewTabKey}
            onActivate={key => dispatch(activateWebviewTab(key))}
            onClose={key => dispatch(closeWebviewTab(key))}
          />
        ) : (
          <>
            {page === 'robot' && robotContent}
            {developerMode && page === 'build' && (
              <section className="workspace-content build-page">
                {buildMode === 'manifest' ? (
                  <PackageManifestPanel
                    root={root}
                    busy={busy}
                    onSaved={message => showOutput(message)}
                  />
                ) : buildMode === 'npm' ? (
                  <NpmPublishPanel
                    root={root}
                    busy={busy}
                    onRun={(action, values) =>
                      api('POST', { root, action, ...values })
                    }
                  />
                ) : (
                  <GitReleasePanelNext
                    root={root}
                    busy={busy}
                    version={releaseVersion}
                    onVersionChange={setReleaseVersion}
                    onInitialize={initializeProjectGit}
                  />
                )}
                {output && (
                  <OperationLog
                    output={output}
                    failed={outputFailed}
                    onClose={() => {
                      setOutput('')
                      setOutputFailed(false)
                    }}
                  />
                )}
              </section>
            )}
            {(page === 'plugins' || page === 'connections') && catalogContent}
            {page !== 'build' && output && (
              <OperationLog
                output={output}
                failed={outputFailed}
                onClose={() => {
                  setOutput('')
                  setOutputFailed(false)
                }}
              />
            )}
          </>
        )}
      </>
    ) : (
      <EmptyWorkspace
        onAdd={chooseDirectories}
        onClone={() => setGitCloneOpen(true)}
      />
    )

  const environmentReady = report
    ? `${readyCount}/${report.checks.length}`
    : '—'
  const environmentWarning = Boolean(
    report?.checks.some(item => item.status !== 'ready')
  )

  return (
    <>
      <main className="guide-shell">
        <section className="guide-window dashboard-window">
          <header className="flex min-h-[52px] min-w-0 items-center justify-between gap-3 border-b border-slate-200 bg-white/90 px-4">
            <div className="flex min-w-0 flex-1 items-center gap-2">
              <a
                className="truncate text-[0.84rem] font-extrabold tracking-[-0.01em] text-ink-950 no-underline transition hover:text-brand-600"
                href="https://alemonjs.com/"
                target="_blank"
                rel="noreferrer"
              >
                ALemonX
              </a>
              <SetupUpdateButton />
              <ThemeToggle />
            </div>
            <div className="ml-auto flex min-w-0 items-center gap-2">
              {developerMode && <McpControl />}
              <Button
                variant="secondary"
                className={cn(
                  'gap-1.5 px-2',
                  developerMode
                    ? 'border-blue-400 bg-slate-100 '
                    : 'border-slate-200 bg-white  hover:bg-slate-50'
                )}
                onClick={() => dispatch(setDeveloperMode(!developerMode))}
                aria-pressed={developerMode}
                title={
                  developerMode
                    ? '关闭开发模式，收起源码与发布工具'
                    : '开启开发模式，显示源码、终端与发布工具'
                }
              >
                <Code2 />
                <span
                  className={cn(
                    developerMode ? ' text-blue-700' : ' text-slate-500 '
                  )}
                >
                  Dev
                </span>
              </Button>
              <SSHControl />
              <AuthControl />
              <OperationTasksButton root={root} />
              <Button
                variant="secondary"
                className={cn(
                  'gap-1.5 px-2 disabled:cursor-wait disabled:opacity-60',
                  environmentWarning
                    ? 'border-amber-300 bg-amber-50 text-amber-800'
                    : 'border-slate-200 bg-slate-50 text-slate-700'
                )}
                onClick={() => {
                  window.dispatchEvent(
                    new CustomEvent('alx:top-tool-open', {
                      detail: 'environment'
                    })
                  )
                  setEnvironmentOpen(true)
                  onCheck()
                }}
                disabled={checking}
                title="查看并检查全局环境"
              >
                <i>{checking ? '◌' : environmentWarning ? '!' : '✓'}</i>
                <strong>{checking ? '检查中' : environmentReady}</strong>
              </Button>
              <Button
                variant="icon"
                className="text-sm font-semibold"
                onClick={onOpenGuide}
                aria-label="打开引导"
                title="打开引导"
              >
                ?
              </Button>
            </div>
          </header>
          <EnvironmentPanel
            open={environmentOpen}
            report={report}
            checking={checking}
            onClose={() => setEnvironmentOpen(false)}
            onRefresh={onCheck}
            onFix={onFix}
          />
          <ConfirmDialog
            open={Boolean(pendingBackpackRemoval)}
            title="从背包移除插件"
            subtitle="这会删除当前机器人 packages 目录中的本地插件文件。"
            message={`确定移除 ${pendingBackpackRemoval} 吗？此操作不会删除机器人主项目，但该插件需要重新安装后才能使用。`}
            confirmLabel="移除插件"
            destructive
            busy={busy}
            onCancel={() => setPendingBackpackRemoval('')}
            onConfirm={() => {
              const packageName = pendingBackpackRemoval
              if (!packageName) return
              void (async () => {
                if (
                  await api('POST', {
                    root,
                    action: 'remove-local-package',
                    package: packageName
                  })
                )
                  void refetchPackages()
                setPendingBackpackRemoval('')
              })()
            }}
          />
          <ConfirmDialog
            open={Boolean(pendingProjectRemoval)}
            title="移除机器人目录"
            subtitle="仅从管理列表中移除，不会删除磁盘上的项目文件。"
            message={`确定将「${pendingProjectRemoval ? (projects.find(p => p.id === pendingProjectRemoval)?.name ?? pendingProjectRemoval) : ''}」从机器人目录移除吗？其磁盘文件保持不变，可随时重新添加。`}
            confirmLabel="移除目录"
            destructive
            onCancel={() => setPendingProjectRemoval(null)}
            onConfirm={confirmRemoveProject}
          />
          <DirectoryPicker
            open={directoryPickerOpen}
            onClose={() => setDirectoryPickerOpen(false)}
            onSelect={paths => void addSelectedDirectories(paths)}
          />
          <DirectoryPicker
            open={gitDestinationPickerOpen}
            multiple={false}
            priority
            onClose={() => setGitDestinationPickerOpen(false)}
            onSelect={paths => {
              setGitDestination(paths[0] ?? '')
              setGitDestinationPickerOpen(false)
            }}
          />
          <GitCloneDialog
            open={gitCloneOpen}
            destination={gitDestination}
            busy={busy}
            progress={cloneProgress}
            onClose={() => setGitCloneOpen(false)}
            onChooseDestination={() => setGitDestinationPickerOpen(true)}
            onConfirm={cloneRobotRepository}
          />
          {renameTarget && (
            <Modal open zIndex={300} className="bg-slate-900/40">
              <div className="grid w-full max-w-sm gap-4 rounded-xl bg-white p-5 shadow-2xl">
                <h3 className="text-base font-semibold text-slate-900">
                  重命名对话
                </h3>
                <label className="grid gap-1.5 text-xs font-medium text-slate-600">
                  名称（2-8 个字）
                  <input
                    className="h-10 rounded-md border border-slate-300 px-3 text-sm outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
                    value={renameTitle}
                    onChange={event => setRenameTitle(event.target.value)}
                    maxLength={8}
                    autoFocus
                    onKeyDown={event => {
                      if (event.key === 'Enter') {
                        event.preventDefault()
                        if (renameTarget) void renameSession(renameTarget.id)
                      }
                    }}
                  />
                </label>
                <footer className="flex justify-end gap-2">
                  <button
                    className="secondary-button"
                    onClick={() => setRenameTarget(null)}
                  >
                    取消
                  </button>
                  <button
                    className="primary-button"
                    disabled={renameTitle.trim().length < 2}
                    onClick={() => {
                      if (renameTarget) void renameSession(renameTarget.id)
                    }}
                  >
                    确定
                  </button>
                </footer>
              </div>
            </Modal>
          )}
          <section className="console-layout">
            <ProjectRail
              feature={systemFeature}
              setupPlugins={setupPlugins}
              projects={projects}
              activeID={activeProjectID}
              agentSessions={agentSessions}
              onFeature={selectSystemFeature}
              onOpenAgent={openAI}
              onPinProject={pinProject}
              onRenameSession={requestRename}
              onArchiveSession={archiveSession}
              onAdd={chooseDirectories}
              onClone={() => setGitCloneOpen(true)}
              onSelect={id => {
                dispatch(selectProject(id))
                // Selecting a robot directory always returns to its runtime.
                // A plugin WebView is an explicit content page and must not
                // follow the directory selection.
                setAIOpen(false)
                dispatch(clearActiveWebviewTab())
                setSystemFeature(null)
                setPage('robot')
                setSection('runtime')
                setOutput('')
              }}
              onRemove={removeProject}
            />
            <section className="console-page">
              {workspace}
              {error && <ErrorNotice message={error} onClose={onClearError} />}
              {!systemFeature && activeProject && !invalidProject && (
                <ControlCard
                  page={page}
                  section={section}
                  project={activeProject}
                  buildMode={buildMode}
                  catalog={catalog}
                  catalogTitle={catalogTitle}
                  webViews={robotWebViews}
                  activeWebViewID={activeWebViewID}
                  developerMode={developerMode}
                  onOpenConsole={() => setConsoleOpen(true)}
                  onOpenAI={openAI}
                  onOpenWebView={id => {
                    closeTemporaryContentPage()
                    const entry = robotWebViews.find(item => item.id === id)
                    if (!entry || !root) return
                    dispatch(
                      openWebviewTab({
                        key: `${root}\u0000${id}`,
                        root,
                        entryID: id,
                        package: entry.package,
                        title: entry.name
                      })
                    )
                  }}
                  onPage={selectPage}
                  onSection={openSection}
                  onBuildMode={mode => {
                    setBuildMode(mode)
                    setOutput('')
                  }}
                  onCatalog={title => {
                    setCatalogTitle(title)
                    setCatalogItem(null)
                  }}
                  onGit={() => setGitProject(activeProject)}
                />
              )}
            </section>
          </section>
        </section>
      </main>
      <ReadonlyConsole
        open={consoleOpen}
        root={root}
        onClose={() => setConsoleOpen(false)}
      />
      <RobotGitControl
        project={gitProject}
        onClose={() => setGitProject(null)}
      />
      {invalidDirectory && (
        <InvalidDirectoryDialog
          path={invalidDirectory}
          onClose={() => setInvalidDirectory('')}
          onCreate={() => {
            dispatch(
              setGuideProject({
                destinationMode: 'custom',
                destination: invalidDirectory
              })
            )
            setInvalidDirectory('')
            onOpenGuide()
          }}
        />
      )}
    </>
  )
}

function ProjectRail({
  feature,
  setupPlugins,
  projects,
  activeID,
  agentSessions,
  onFeature,
  onAdd,
  onClone,
  onSelect,
  onRemove,
  onOpenAgent,
  onPinProject,
  onRenameSession,
  onArchiveSession
}: {
  feature: SystemFeature | null
  setupPlugins: SetupPlugin[]
  projects: Project[]
  activeID: string
  agentSessions: Array<{
    id: string
    title: string
    root: string
    updated: string
  }>
  onFeature: (feature: SystemFeature) => void
  onAdd: () => void
  onClone: () => void
  onSelect: (id: string) => void
  onRemove: (id: string) => void
  onOpenAgent: (sessionID?: string) => void
  onPinProject: (id: string) => void
  onRenameSession: (id: string, title: string) => void
  onArchiveSession: (id: string) => void
}) {
  const activePlugins = setupPlugins.filter(item => item.enabled)
  return (
    <aside className="project-rail flex min-h-0 min-w-0 flex-col border-r border-slate-200 bg-slate-50">
      <section
        className="border-b border-slate-200 p-3"
        aria-label="系统功能目录"
      >
        <header className="mb-2 px-2 text-[11px] font-semibold text-slate-400">
          <small>系统</small>
        </header>
        <nav>
          {coreFeatureCatalog.map(item => (
            <button
              className={cn(
                'flex min-h-9 w-full items-center gap-2 rounded-md px-2.5 text-left text-xs font-semibold transition',
                feature === item.id
                  ? 'bg-slate-200 text-slate-950'
                  : 'text-slate-600 hover:bg-slate-100'
              )}
              key={item.id}
              onClick={() => onFeature(item.id)}
            >
              <i className="inline-flex size-4 items-center justify-center not-italic">
                {item.icon}
              </i>
              <span className="min-w-0 flex-1 truncate">{item.label}</span>
              {item.status && (
                <small className="text-[10px] text-slate-400">
                  {item.status}
                </small>
              )}
            </button>
          ))}
        </nav>
        {activePlugins.length > 0 && (
          <>
            <span className="my-3 block border-t border-slate-200" />
            <nav className="grid gap-1">
              {activePlugins.map(item => (
                <button
                  className={cn(
                    'flex min-h-9 w-full items-center gap-2 rounded-md px-2.5 text-left text-xs font-semibold transition',
                    feature === `setup:${item.id}`
                      ? 'bg-slate-200 text-slate-950'
                      : 'text-slate-600 hover:bg-slate-100'
                  )}
                  key={item.id}
                  onClick={() => onFeature(`setup:${item.id}`)}
                >
                  <i className="inline-flex size-4 items-center justify-center not-italic">
                    {setupPluginIcon(item.navigation.icon)}
                  </i>
                  <span className="min-w-0 flex-1 truncate">
                    {item.navigation.label || item.name}
                  </span>
                  <small className="text-[10px] text-slate-400">已加载</small>
                </button>
              ))}
            </nav>
          </>
        )}
      </section>
      <section className="flex min-h-0 flex-1 flex-col">
        <header className="flex min-h-14 items-center justify-between gap-2 border-b border-slate-200 px-3">
          <div className="flex items-center gap-2 text-sm font-semibold text-slate-800">
            <strong>机器人目录</strong>
            <span className="rounded-full bg-slate-200 px-1.5 py-0.5 text-[10px] text-slate-500">
              {projects.length}
            </span>
          </div>
          <div className="flex items-center gap-1.5">
            <button
              className="icon-button size-8 p-0"
              onClick={onClone}
              aria-label="从 Git 克隆机器人"
              title="从 Git 克隆机器人"
            >
              <GitBranch className="size-4" />
            </button>
            <button
              className="icon-button size-8 p-0"
              onClick={onAdd}
              aria-label="添加本地机器人目录"
              title="添加本地机器人目录"
            >
              <Plus className="size-4" />
            </button>
          </div>
        </header>
        <div className="grid content-start gap-1.5 overflow-auto p-2 h-full">
          {projects.map(project => (
            <ProjectItem
              active={project.id === activeID}
              key={project.id}
              project={project}
              agentSessions={agentSessions}
              onSelect={onSelect}
              onRemove={onRemove}
              onOpenAgent={onOpenAgent}
              onPin={onPinProject}
              onRename={onRenameSession}
              onArchive={onArchiveSession}
            />
          ))}
          {!projects.length && (
            <p className="px-2 py-4 text-center text-xs text-slate-400">
              添加目录开始管理
            </p>
          )}
        </div>
      </section>
    </aside>
  )
}

function GitCloneDialog({
  open,
  destination,
  busy,
  progress,
  onClose,
  onChooseDestination,
  onConfirm
}: {
  open: boolean
  destination: string
  busy: boolean
  progress: number
  onClose: () => void
  onChooseDestination: () => void
  onConfirm: (
    repository: string,
    branch: string,
    name: string,
    mirror: string,
    depth: number
  ) => Promise<void>
}) {
  const [repository, setRepository] = useState('')
  const [branch, setBranch] = useState('')
  const [branches, setBranches] = useState<string[]>([])
  const [branchesLoading, setBranchesLoading] = useState(false)
  const [name, setName] = useState('')
  const [mirror, setMirror] = useState('official')
  const [depth, setDepth] = useState(1)
  const [connection, setConnection] = useState<'ssh' | 'https'>('https')
  const [sshKeys, setSSHKeys] = useState<Array<{ name: string }>>([])
  const [sshLoading, setSSHLoading] = useState(false)
  const [target, setTarget] = useState<{
    path: string
    exists: boolean
  } | null>(null)
  const [targetError, setTargetError] = useState('')
  useEffect(() => {
    if (open) {
      setRepository('')
      setBranch('')
      setBranches([])
      setBranchesLoading(false)
      setName('')
      setMirror('official')
      setDepth(1)
      setConnection('https')
      setSSHKeys([])
      setTarget(null)
      setTargetError('')
    }
  }, [open])
  useEffect(() => {
    if (!open) return
    let active = true
    setSSHLoading(true)
    void fetch('/api/v1/system/ssh')
      .then(async response => {
        const data = (await response.json()) as {
          keys?: Array<{ name: string }>
          error?: string
        }
        if (!response.ok) throw new Error(data.error || '无法读取 SSH 状态。')
        return data.keys ?? []
      })
      .then(keys => {
        if (!active) return
        setSSHKeys(keys)
        if (keys.length) setConnection('ssh')
      })
      .catch(() => {
        if (active) setSSHKeys([])
      })
      .finally(() => {
        if (active) setSSHLoading(false)
      })
    return () => {
      active = false
    }
  }, [open])
  useEffect(() => {
    if (!open || !destination || !repository.trim() || !name.trim()) {
      setTarget(null)
      setTargetError('')
      return
    }
    const controller = new AbortController()
    const timer = window.setTimeout(() => {
      void fetch(
        `/api/v1/robot/git-clone/check?${new URLSearchParams({ destination, repository, name })}`,
        { signal: controller.signal }
      )
        .then(async response => {
          const data = (await response.json()) as {
            path?: string
            exists?: boolean
            error?: string
          }
          if (!response.ok) throw new Error(data.error || '无法检查目标目录。')
          return data
        })
        .then(data => {
          setTarget({ path: data.path ?? '', exists: Boolean(data.exists) })
          setTargetError('')
        })
        .catch(reason => {
          if (!(
            reason instanceof DOMException && reason.name === 'AbortError'
          )) {
            setTarget(null)
            setTargetError(operationErrorMessage(reason, '无法检查目标目录。'))
          }
        })
    }, 260)
    return () => {
      controller.abort()
      window.clearTimeout(timer)
    }
  }, [destination, name, open, repository])
  useEffect(() => {
    const value = repository.trim()
    if (!open || !isCompleteGitRepositoryURL(value)) {
      setBranches([])
      setBranchesLoading(false)
      return
    }
    const controller = new AbortController()
    const timer = window.setTimeout(() => {
      setBranchesLoading(true)
      void fetch(
        `/api/v1/robot/git-clone/branches?${new URLSearchParams({ repository: value })}`,
        { signal: controller.signal }
      )
        .then(async response => {
          const data = (await response.json()) as {
            branches?: string[]
            defaultBranch?: string
            error?: string
          }
          if (!response.ok) throw new Error(data.error || '无法读取远程分支。')
          return data
        })
        .then(data => {
          setBranches(data.branches ?? [])
          setBranch(current =>
            data.branches?.includes(current)
              ? current
              : (data.defaultBranch ?? data.branches?.[0] ?? '')
          )
        })
        .catch(() => {
          // 地址输入过程中或私有仓库尚未授权时保持静默，不打断用户填写。
          if (!controller.signal.aborted) setBranches([])
        })
        .finally(() => {
          if (!controller.signal.aborted) setBranchesLoading(false)
        })
    }, 500)
    return () => {
      controller.abort()
      window.clearTimeout(timer)
    }
  }, [open, repository])
  if (!open) return null
  const usesSSH = /^(git@|ssh:\/\/)/.test(repository.trim())
  return (
    <Modal open ariaLabel="从 Git 克隆机器人">
      <section
        className="git-dialog git-clone-dialog grid max-h-[min(720px,calc(100vh-32px))] w-full max-w-xl grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_22px_58px_rgb(28_26_23/0.25)]"
        role="dialog"
        aria-label="从 Git 克隆机器人"
      >
        <header className="flex items-center justify-between gap-3 border-b border-slate-200 px-4 py-3">
          <div className="grid gap-1">
            <strong className="text-sm font-semibold text-slate-900">
              添加 Git 仓库
            </strong>
            <span className="text-xs text-slate-500">
              下载完成后会自动加入机器人目录。
            </span>
          </div>
          <button
            className="icon-button size-8 p-0"
            onClick={onClose}
            aria-label="关闭"
          >
            <X className="size-4" />
          </button>
        </header>
        <div className="grid gap-3 overflow-auto p-4">
          <section
            className="grid gap-2 rounded-lg border border-slate-200 bg-slate-50 p-3"
            aria-label="仓库连接方式"
          >
            <header className="flex items-center justify-between gap-2">
              <strong className="text-xs font-semibold text-slate-700">
                连接方式
              </strong>
              <small className="text-[11px] text-slate-500">
                {sshLoading
                  ? '正在检查 SSH…'
                  : sshKeys.length
                    ? `已检测到 SSH 密钥：${sshKeys[0].name}`
                    : '未配置 SSH 密钥'}
              </small>
            </header>
            <Tabs
              ariaLabel="仓库连接方式"
              items={[
                {
                  id: 'ssh',
                  icon: <KeyRound className="size-3.5" />,
                  label: 'SSH',
                  meta: sshKeys.length ? '推荐' : undefined
                },
                {
                  id: 'https',
                  icon: <Globe2 className="size-3.5" />,
                  label: 'HTTPS'
                }
              ]}
              onChange={setConnection}
              value={connection}
              variant="segmented"
            />
            <p className="m-0 text-xs leading-5 text-slate-500">
              {connection === 'ssh'
                ? sshKeys.length
                  ? '推荐 SSH：私有仓库需先将此公钥添加到代码平台。'
                  : '未配置 SSH 密钥；请在顶部 SSH 管理中生成并添加公钥，或改用 HTTPS。'
                : 'HTTPS 可直接使用；访问私有仓库时，需要在代码平台完成在线授权。'}
            </p>
          </section>
          <section className="grid gap-3">
            <label className="grid gap-1 text-xs font-semibold text-slate-600">
              仓库地址
              <input
                className="h-9 rounded-md border border-slate-300 bg-white px-2.5 text-sm font-normal text-slate-800 outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
                autoFocus
                value={repository}
                onChange={event => {
                  const value = event.target.value
                  setRepository(value)
                  setBranch('')
                  if (/^(git@|ssh:\/\/)/.test(value.trim()))
                    setConnection('ssh')
                  const derived =
                    value
                      .trim()
                      .replace(/\/$/, '')
                      .split('/')
                      .pop()
                      ?.replace(/\.git$/, '') ?? ''
                  setName(derived)
                }}
                placeholder={
                  connection === 'ssh'
                    ? 'git@github.com:组织/机器人仓库.git'
                    : 'https://github.com/组织/机器人仓库.git'
                }
              />
              {usesSSH && !sshLoading && !sshKeys.length && (
                <small className="font-normal text-red-700">
                  此 SSH 地址无法使用：请先在顶部 SSH
                  管理中生成密钥并添加公钥，或改用 HTTPS 地址。
                </small>
              )}
            </label>
            <div className="grid gap-3 sm:grid-cols-2">
              <label className="grid gap-1 text-xs font-semibold text-slate-600">
                分支{branchesLoading ? '（正在读取…）' : '（可选）'}
                <select
                  className="h-9 rounded-md border border-slate-300 bg-white px-2.5 text-sm font-normal text-slate-800 outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100 disabled:bg-slate-100"
                  value={branch}
                  onChange={event => setBranch(event.target.value)}
                  disabled={!branches.length || branchesLoading}
                >
                  <option value="">默认分支</option>
                  {branches.map(item => (
                    <option key={item} value={item}>
                      {formatBranchLabel(item)}
                    </option>
                  ))}
                </select>
                {branch && (
                  <small
                    className="truncate text-[11px] font-normal text-slate-500"
                    title={branch}
                  >
                    已选：{branch}
                  </small>
                )}
              </label>
              <label className="grid gap-1 text-xs font-semibold text-slate-600">
                克隆深度
                <select
                  className="h-9 rounded-md border border-slate-300 bg-white px-2.5 text-sm font-normal text-slate-800 outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
                  value={depth}
                  onChange={event => setDepth(Number(event.target.value))}
                >
                  <option value={1}>仅最新提交（推荐）</option>
                  <option value={50}>最近 50 条提交</option>
                  <option value={200}>最近 200 条提交</option>
                  <option value={0}>完整历史</option>
                </select>
              </label>
              <label className="grid gap-1 text-xs font-semibold text-slate-600">
                下载来源
                <select
                  className="h-9 rounded-md border border-slate-300 bg-white px-2.5 text-sm font-normal text-slate-800 outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
                  value={mirror}
                  onChange={event => setMirror(event.target.value)}
                >
                  <option value="official">Git 官方（推荐）</option>
                  <option value="gh-proxy">GitHub 加速 · gh-proxy</option>
                  <option value="ghproxy-net">GitHub 加速 · ghproxy.net</option>
                </select>
              </label>
            </div>
          </section>
          <section className="grid gap-3 sm:grid-cols-2">
            <label className="grid gap-1 text-xs font-semibold text-slate-600">
              所在文件夹
              <button
                type="button"
                className="h-9 truncate rounded-md border border-slate-300 bg-white px-2.5 text-left text-sm font-normal text-slate-700"
                onClick={onChooseDestination}
              >
                {gitDestinationLabel(destination)}
              </button>
            </label>
            <label className="grid gap-1 text-xs font-semibold text-slate-600">
              新目录名称
              <input
                className="h-9 rounded-md border border-slate-300 bg-white px-2.5 text-sm font-normal text-slate-800 outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
                value={name}
                onChange={event => setName(event.target.value)}
                placeholder="默认使用仓库名"
              />
              {target?.exists ? (
                <small className="text-xs text-red-700">
                  目标已存在：{target.path}
                </small>
              ) : target?.path ? null : targetError ? (
                <small className="text-xs text-red-700">{targetError}</small>
              ) : null}
            </label>
          </section>
        </div>
        <footer className="flex justify-end gap-2 border-t border-slate-200 px-4 py-3">
          {busy && (
            <div className="mr-auto grid min-w-44 gap-1 self-center">
              <div className="flex justify-between text-[11px] text-slate-500">
                <span>正在下载仓库</span>
                <span>{progress}%</span>
              </div>
              <div className="h-1.5 overflow-hidden rounded-full bg-slate-200">
                <div
                  className="h-full rounded-full bg-brand-600 transition-[width] duration-500"
                  style={{ width: `${Math.max(8, progress)}%` }}
                />
              </div>
            </div>
          )}
          <button className="secondary-button" onClick={onClose}>
            取消
          </button>
          <button
            className="primary-button"
            disabled={
              busy ||
              ((connection === 'ssh' || usesSSH) &&
                !sshLoading &&
                !sshKeys.length) ||
              !repository.trim() ||
              !destination ||
              !name.trim() ||
              !target ||
              target.exists ||
              Boolean(targetError)
            }
            onClick={() =>
              void onConfirm(
                repository.trim(),
                branch.trim(),
                name.trim(),
                mirror,
                depth
              )
            }
          >
            {busy ? '正在下载…' : '克隆并添加'}
          </button>
        </footer>
      </section>
    </Modal>
  )
}

function gitDestinationLabel(path: string) {
  return path || '选择存放位置'
}

function formatBranchLabel(branch: string) {
  const limit = 48
  return branch.length > limit ? `${branch.slice(0, limit - 1)}…` : branch
}

function isCompleteGitRepositoryURL(value: string) {
  return /^(https:\/\/(github\.com|gitee\.com)\/[\w.-]+\/[\w.-]+(?:\.git)?\/?|git@(github\.com|gitee\.com):[\w.-]+\/[\w.-]+(?:\.git)?)$/.test(
    value
  )
}

function GitInitializeDialog({
  open,
  values,
  busy,
  onClose,
  onChange,
  onConfirm
}: {
  open: boolean
  values: {
    authorName: string
    authorEmail: string
    repository: string
    message: string
  }
  busy: boolean
  onClose: () => void
  onChange: (values: {
    authorName: string
    authorEmail: string
    repository: string
    message: string
  }) => void
  onConfirm: () => Promise<void>
}) {
  if (!open) return null
  const update = (key: keyof typeof values, value: string) =>
    onChange({ ...values, [key]: value })
  return (
    <Modal open ariaLabel="填写 Git 初始化信息">
      <section
        className="git-dialog grid w-full max-w-lg grid-rows-[auto_1fr_auto] overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_22px_58px_rgb(28_26_23/0.25)]"
        role="dialog"
        aria-label="填写 Git 初始化信息"
      >
        <header className="flex items-center justify-between gap-3 border-b border-slate-200 px-4 py-3">
          <div className="grid gap-1">
            <strong className="text-sm font-semibold text-slate-900">
              初始化 Git 仓库
            </strong>
            <span className="text-xs text-slate-500">
              仅修改当前项目，不会改动你的全局 Git 身份。
            </span>
          </div>
          <button
            className="icon-button size-8 p-0"
            onClick={onClose}
            aria-label="关闭"
          >
            <X className="size-4" />
          </button>
        </header>
        <div className="grid gap-3 p-4">
          <label className="grid gap-1 text-xs font-semibold text-slate-600">
            提交姓名
            <input
              className="h-9 rounded-md border border-slate-300 px-2.5 text-sm font-normal outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
              autoFocus
              value={values.authorName}
              onChange={event => update('authorName', event.target.value)}
              placeholder="你的姓名"
            />
          </label>
          <label className="grid gap-1 text-xs font-semibold text-slate-600">
            提交邮箱
            <input
              className="h-9 rounded-md border border-slate-300 px-2.5 text-sm font-normal outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
              type="email"
              value={values.authorEmail}
              onChange={event => update('authorEmail', event.target.value)}
              placeholder="name@example.com"
            />
          </label>
          <label className="grid gap-1 text-xs font-semibold text-slate-600">
            远程仓库（可选）
            <input
              className="h-9 rounded-md border border-slate-300 px-2.5 text-sm font-normal outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
              value={values.repository}
              onChange={event => update('repository', event.target.value)}
              placeholder="https://github.com/owner/repo.git"
            />
          </label>
          <label className="grid gap-1 text-xs font-semibold text-slate-600">
            首个提交说明
            <input
              className="h-9 rounded-md border border-slate-300 px-2.5 text-sm font-normal outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
              value={values.message}
              onChange={event => update('message', event.target.value)}
            />
          </label>
        </div>
        <footer className="flex justify-end gap-2 border-t border-slate-200 px-4 py-3">
          <button className="secondary-button" onClick={onClose}>
            取消
          </button>
          <button
            className="primary-button"
            disabled={
              busy || !values.authorName.trim() || !values.authorEmail.trim()
            }
            onClick={() => void onConfirm()}
          >
            {busy ? '正在初始化…' : '确认初始化'}
          </button>
        </footer>
      </section>
    </Modal>
  )
}
function ProjectItem({
  project,
  active,
  agentSessions,
  onSelect,
  onRemove,
  onOpenAgent,
  onPin,
  onRename,
  onArchive
}: {
  project: Project
  active: boolean
  agentSessions: Array<{
    id: string
    title: string
    root: string
    updated: string
  }>
  onSelect: (id: string) => void
  onRemove: (id: string) => void
  onOpenAgent: (sessionID?: string) => void
  onPin: (id: string) => void
  onRename: (id: string, title: string) => void
  onArchive: (id: string) => void
}) {
  const [validate, { data }] = useLazyRobotProjectQuery()
  const [recordsOpen, setRecordsOpen] = useState(false)
  const [moreOpen, setMoreOpen] = useState(false)
  const [ctxMenu, setCtxMenu] = useState<{
    id: string
    title: string
    x: number
    y: number
  } | null>(null)
  const moreRef = useRef<HTMLDivElement | null>(null)
  const ctxRef = useRef<HTMLDivElement | null>(null)
  useEffect(() => {
    void validate(project.path)
  }, [project.path, validate])
  useEffect(() => {
    const close = (event: globalThis.MouseEvent) => {
      if (moreRef.current && !moreRef.current.contains(event.target as Node)) {
        setMoreOpen(false)
      }
      if (ctxRef.current && !ctxRef.current.contains(event.target as Node)) {
        setCtxMenu(null)
      }
    }
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [])
  const invalid = data?.valid === false
  const ownSessions = agentSessions.filter(item => item.root === project.path)
  return (
    <article
      className={cn(
        'relative rounded-lg border p-2 transition',
        active
          ? 'border-slate-300 bg-white shadow-sm'
          : 'border-transparent hover:border-slate-200 hover:bg-white/70',
        invalid ? 'border-amber-300 bg-amber-50' : ''
      )}
    >
      <button
        className="grid w-full gap-1 pr-14 text-left"
        onClick={() => onSelect(project.id)}
      >
        <strong className="flex min-w-0 items-center gap-1.5 truncate text-xs font-semibold text-slate-800">
          {project.name}
          {invalid && (
            <em className="not-italic text-[10px] font-semibold text-amber-700">
              目录不可用
            </em>
          )}
        </strong>
        <small
          className="block truncate text-[11px] text-slate-400"
          title={project.path}
        >
          {invalid ? data.error || project.path : project.path}
        </small>
      </button>
      <div className="absolute right-2 top-2 flex items-center gap-0.5">
        <button
          className={cn(
            'inline-flex size-6 items-center justify-center rounded transition',
            recordsOpen
              ? 'bg-slate-200 text-slate-700'
              : 'text-slate-400 hover:bg-slate-100 hover:text-slate-700'
          )}
          onClick={() => setRecordsOpen(value => !value)}
          aria-label={`${project.name} 的对话记录`}
          title="对话记录"
        >
          <History className="size-3.5" />
        </button>
        <div ref={moreRef} className="relative">
          <button
            className={cn(
              'inline-flex size-6 items-center justify-center rounded transition',
              moreOpen
                ? 'bg-slate-200 text-slate-700'
                : 'text-slate-400 hover:bg-slate-100 hover:text-slate-700'
            )}
            onClick={() => setMoreOpen(value => !value)}
            aria-label={`${project.name} 的更多操作`}
            title="更多操作"
          >
            <MoreVertical className="size-3.5" />
          </button>
          {moreOpen && (
            <div className="absolute right-0 top-6 z-20 grid min-w-36 gap-0.5 rounded-lg border border-slate-200 bg-white p-1 shadow-lg">
              <button
                className="flex min-h-8 items-center gap-2 rounded px-2 text-left text-xs text-slate-600 transition hover:bg-slate-100"
                onClick={() => {
                  onPin(project.id)
                  setMoreOpen(false)
                }}
              >
                <Pin className="size-3.5 text-slate-400" />
                置顶
              </button>
              <button
                className="flex min-h-8 items-center gap-2 rounded px-2 text-left text-xs text-red-600 transition hover:bg-red-50"
                onClick={() => {
                  onRemove(project.id)
                  setMoreOpen(false)
                }}
              >
                <Trash2 className="size-3.5" />
                移除
              </button>
            </div>
          )}
        </div>
      </div>
      {recordsOpen && (
        <div className="mt-2 grid gap-0.5 border-t border-slate-200 pt-2">
          {ownSessions.length === 0 ? (
            <p className="px-1.5 py-1 text-[11px] text-slate-400">
              还没有对话记录
            </p>
          ) : (
            ownSessions.map(item => (
              <button
                className="flex min-h-7 items-center gap-1.5 rounded px-1.5 text-left text-[11px] text-slate-600 transition hover:bg-slate-100"
                key={item.id}
                onClick={() => onOpenAgent(item.id)}
                onContextMenu={event => {
                  event.preventDefault()
                  setCtxMenu({
                    id: item.id,
                    title: item.title,
                    x: event.clientX,
                    y: event.clientY
                  })
                }}
                title={`${item.title}（右键操作）`}
              >
                <MessageSquare className="size-3 shrink-0 text-slate-400" />
                <span className="min-w-0 flex-1 truncate">{item.title}</span>
              </button>
            ))
          )}
        </div>
      )}
      {ctxMenu && (
        <div
          ref={ctxRef}
          className="fixed z-[200] grid min-w-36 gap-0.5 rounded-lg border border-slate-200 bg-white p-1 shadow-lg"
          style={{ left: ctxMenu.x, top: ctxMenu.y }}
        >
          <button
            className="flex min-h-8 items-center gap-2 rounded px-2 text-left text-xs text-slate-600 transition hover:bg-slate-100"
            onClick={() => {
              onRename(ctxMenu.id, ctxMenu.title)
              setCtxMenu(null)
            }}
          >
            <Pencil className="size-3.5 text-slate-400" />
            重命名
          </button>
          <button
            className="flex min-h-8 items-center gap-2 rounded px-2 text-left text-xs text-slate-600 transition hover:bg-slate-100"
            onClick={() => {
              onArchive(ctxMenu.id)
              setCtxMenu(null)
            }}
          >
            <Archive className="size-3.5 text-slate-400" />
            归档
          </button>
        </div>
      )}
    </article>
  )
}
function McpControl() {
  const [open, setOpen] = useState(false)
  const [transport, setTransport] = useState<'stdio' | 'http'>('stdio')
  const [copied, setCopied] = useState(false)
  const stdioConfig =
    '{\n  "mcpServers": {\n    "alemonx": {\n      "command": "alx",\n      "args": ["mcp"]\n    }\n  }\n}'
  const httpCommand =
    "MCP_TOKEN='请生成高强度随机值' alx --mcp-port 17391 mcp-http"
  const copy = async (value: string) => {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1800)
    } catch {
      setCopied(false)
    }
  }
  const http = transport === 'http'
  useEffect(() => {
    const closeWhenAnotherToolOpens = (event: Event) => {
      if ((event as CustomEvent<string>).detail !== 'mcp') setOpen(false)
    }
    window.addEventListener('alx:top-tool-open', closeWhenAnotherToolOpens)
    return () =>
      window.removeEventListener('alx:top-tool-open', closeWhenAnotherToolOpens)
  }, [])
  return (
    <div className="mcp-control relative">
      <button
        className="mcp-control-button inline-flex min-h-8 items-center gap-1.5 rounded-lg border border-blue-200 bg-blue-50 px-2.5 text-xs font-semibold text-blue-700 transition hover:bg-blue-100 dark:border-blue-900 dark:bg-blue-950/40 dark:text-blue-300 dark:hover:bg-blue-950/70"
        onClick={() =>
          setOpen(value => {
            const next = !value
            if (next)
              window.dispatchEvent(
                new CustomEvent('alx:top-tool-open', { detail: 'mcp' })
              )
            return next
          })
        }
        aria-expanded={open}
        title="连接 Codex 或其他本机 AI 客户端"
      >
        <i className="inline-flex size-4 items-center justify-center rounded-full bg-white text-[10px] not-italic dark:bg-slate-900">
          ✓
        </i>
        <span>MCP</span>
        <strong className="text-[11px] font-semibold">已开启</strong>
      </button>
      {open && (
        <section
          className="mcp-popover absolute right-0 top-10 z-30 grid w-[min(390px,calc(100vw-32px))] gap-3 rounded-xl border border-slate-200 bg-white p-4 shadow-2xl dark:border-slate-700 dark:bg-slate-900"
          role="dialog"
          aria-label="连接 MCP"
        >
          <header className="flex items-start justify-between gap-3">
            <div className="grid gap-0.5">
              <strong className="text-sm font-semibold text-slate-900 dark:text-slate-100">
                连接 Codex / 自定义 MCP
              </strong>
              <small className="text-xs font-semibold text-blue-600 dark:text-blue-400">
                两种标准传输均可用
              </small>
            </div>
            <button
              className="inline-flex size-7 items-center justify-center rounded-md text-lg leading-none text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800"
              onClick={() => setOpen(false)}
              aria-label="关闭 MCP 说明"
            >
              ×
            </button>
          </header>
          <p className="m-0 text-xs leading-5 text-slate-600 dark:text-slate-300">
            MCP 让 AI 在你的确认下管理
            AlemonJS：读取与修改项目、更新运行配置、启动机器人、构建、打包与发布。它不是网页远程控制；客户端只会连接本机服务。
          </p>
          <Tabs
            ariaLabel="MCP 接入类型"
            className="mcp-transport-tabs"
            items={[
              { id: 'stdio', label: 'STDIO', meta: '推荐' },
              { id: 'http', label: '流式 HTTP', meta: '本机' }
            ]}
            onChange={transport => setTransport(transport)}
            value={http ? 'http' : 'stdio'}
            variant="segmented"
          />
          {http ? (
            <>
              <p className="m-0 text-xs leading-5 text-slate-600 dark:text-slate-300">
                先在终端启动受 Token 保护的服务；随后在 Codex 的“连接至自定义
                MCP”中选择<strong> 流式 HTTP</strong>，填写地址与 Bearer Token。
              </p>
              <dl className="mcp-form-guide m-0 overflow-hidden rounded-lg border border-blue-100 dark:border-blue-900">
                <div className="grid grid-cols-[92px_minmax(0,1fr)] gap-2 border-b border-blue-100 px-2 py-2 last:border-b-0 dark:border-blue-900">
                  <dt className="text-xs font-semibold text-slate-500">名称</dt>
                  <dd className="m-0 min-w-0 break-words text-xs text-slate-700 dark:text-slate-200">
                    alemonx
                  </dd>
                </div>
                <div className="grid grid-cols-[92px_minmax(0,1fr)] gap-2 border-b border-blue-100 px-2 py-2 last:border-b-0 dark:border-blue-900">
                  <dt className="text-xs font-semibold text-slate-500">类型</dt>
                  <dd className="m-0 min-w-0 break-words text-xs text-slate-700 dark:text-slate-200">
                    流式 HTTP
                  </dd>
                </div>
                <div className="grid grid-cols-[92px_minmax(0,1fr)] gap-2 border-b border-blue-100 px-2 py-2 last:border-b-0 dark:border-blue-900">
                  <dt className="text-xs font-semibold text-slate-500">地址</dt>
                  <dd className="m-0 min-w-0 break-words text-xs text-slate-700 dark:text-slate-200">
                    <code>http://127.0.0.1:17391/mcp</code>
                  </dd>
                </div>
                <div className="grid grid-cols-[92px_minmax(0,1fr)] gap-2 border-b border-blue-100 px-2 py-2 last:border-b-0 dark:border-blue-900">
                  <dt className="text-xs font-semibold text-slate-500">认证</dt>
                  <dd className="m-0 min-w-0 break-words text-xs text-slate-700 dark:text-slate-200">
                    Bearer Token：<code>&lt;MCP_TOKEN&gt;</code>
                  </dd>
                </div>
                <div className="grid grid-cols-[92px_minmax(0,1fr)] gap-2 border-b border-blue-100 px-2 py-2 last:border-b-0 dark:border-blue-900">
                  <dt className="text-xs font-semibold text-slate-500">
                    启动命令
                  </dt>
                  <dd className="m-0 min-w-0 break-words text-xs text-slate-700 dark:text-slate-200">
                    <code>{httpCommand}</code>
                  </dd>
                </div>
              </dl>
              <button
                className="mcp-copy-button justify-self-end rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white transition hover:bg-blue-700"
                onClick={() => void copy(httpCommand)}
              >
                {copied ? '已复制启动命令' : '复制启动命令'}
              </button>
              <small className="mcp-note text-xs leading-5 text-slate-500">
                服务仅绑定 127.0.0.1；不要把地址、Token
                或端口转发到局域网和公网。流式 HTTP 兼容 MCP 的 POST
                请求，服务不提供独立 SSE 推送流。
              </small>
            </>
          ) : (
            <>
              <p className="m-0 text-xs leading-5 text-slate-600 dark:text-slate-300">
                在 Codex 的“连接至自定义 MCP”中选择<strong> STDIO</strong>
                ，把下列字段逐行填入。Codex 会直接启动本机 <code>alx</code>
                ，无需额外开启端口。
              </p>
              <dl className="mcp-form-guide m-0 overflow-hidden rounded-lg border border-blue-100 dark:border-blue-900">
                <div>
                  <dt>名称</dt>
                  <dd>alemonx</dd>
                </div>
                <div>
                  <dt>类型</dt>
                  <dd>STDIO</dd>
                </div>
                <div>
                  <dt>启动命令</dt>
                  <dd>
                    <code>alx</code>
                  </dd>
                </div>
                <div>
                  <dt>参数</dt>
                  <dd>
                    <code>mcp</code>
                  </dd>
                </div>
                <div>
                  <dt>环境变量（可选）</dt>
                  <dd>
                    <code>MCP_ALLOWED_ROOTS=/你的/机器人目录</code>
                  </dd>
                </div>
              </dl>
              <button
                className="mcp-copy-button"
                onClick={() => void copy(stdioConfig)}
              >
                {copied ? '已复制 JSON 配置' : '复制 JSON 配置'}
              </button>
              <small className="mcp-note">
                涉及安装、构建、写入或执行脚本时，助手仍必须取得你的本次确认；密钥、.env、.npmrc、Git
                元数据与依赖目录不开放。
              </small>
            </>
          )}
        </section>
      )}
    </div>
  )
}
function OperationTasksButton({ root }: { root: string }) {
  const [open, setOpen] = useState(false)
  const [trackTasks, setTrackTasks] = useState(false)
  const { data, isFetching } = useRobotTasksQuery(undefined, {
    skip: !open,
    pollingInterval: trackTasks ? 1200 : 0,
    refetchOnMountOrArgChange: true
  })
  const tasks = (Array.isArray(data) ? data : []).filter(
    item => !root || !item.root || item.root === root
  )
  const [selected, setSelected] = useState<string>('')
  const current = tasks.find(item => item.id === selected) ?? tasks[0]
  const running = tasks.filter(item => item.status === 'running').length
  useEffect(() => {
    setTrackTasks(running > 0)
  }, [running])
  useEffect(() => {
    const closeWhenAnotherToolOpens = (event: Event) => {
      if ((event as CustomEvent<string>).detail !== 'tasks') setOpen(false)
    }
    window.addEventListener('alx:top-tool-open', closeWhenAnotherToolOpens)
    return () =>
      window.removeEventListener('alx:top-tool-open', closeWhenAnotherToolOpens)
  }, [])
  const label = (action: string) =>
    action.startsWith('setup:')
      ? `系统插件 · ${action.split(':').slice(-1)[0]}`
      : ({
          'install': '安装依赖',
          'upgrade-alemon': '升级 AlemonJS 依赖',
          'dependency-status': '检查依赖',
          'dev': '开发启动',
          'dev-stop': '停止开发模式',
          'app': '前台运行',
          'app-stop': '停止前台运行',
          'pm2': '后台启动',
          'pm2-stop': '停止 PM2',
          'pm2-restart': '重启 PM2',
          'pm2-reload': '重载 PM2',
          'pm2-delete': '删除 PM2 进程',
          'pm2-status': '查看 PM2 状态',
          'pm2-logs': '查看 PM2 日志',
          'install-package': '安装插件',
          'uninstall-package': '卸载插件',
          'install-connection': 'Yarn 安装连接包',
          'uninstall-connection': 'Yarn 卸载连接包',
          'git-release': 'GIT 发布',
          'npm-publish': 'NPM 发布'
        }[action] ?? action)
  return (
    <div className="operation-tasks relative">
      <button
        className="operation-tasks-button relative inline-flex size-8 items-center justify-center rounded-lg border border-slate-200 bg-white text-slate-500 transition hover:border-brand-200 hover:text-brand-600 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300"
        onClick={() =>
          setOpen(value => {
            const next = !value
            if (next)
              window.dispatchEvent(
                new CustomEvent('alx:top-tool-open', { detail: 'tasks' })
              )
            return next
          })
        }
        aria-label="操作记录"
        title="当前目录操作记录"
      >
        <ClipboardList />
        {running > 0 && (
          <b className="absolute -right-1 -top-1 inline-flex min-w-4 items-center justify-center rounded-full bg-brand-600 px-1 text-[10px] text-white">
            {running}
          </b>
        )}
      </button>
      {open && (
        <section className="operation-tasks-popover absolute right-0 top-10 z-30 grid w-[min(360px,calc(100vw-32px))] gap-3 rounded-xl border border-slate-200 bg-white p-3 shadow-2xl dark:border-slate-700 dark:bg-slate-900">
          <header className="flex items-start justify-between gap-3">
            <div className="grid gap-0.5">
              <strong className="text-sm font-semibold text-slate-900 dark:text-slate-100">
                操作记录
              </strong>
              <small className="text-xs text-slate-500">
                {root ? '当前机器人与系统操作' : '系统操作'}
              </small>
            </div>
            <button
              className="inline-flex size-7 items-center justify-center rounded-md text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800"
              onClick={() => setOpen(false)}
              aria-label="关闭操作记录"
            >
              <X />
            </button>
          </header>
          {isFetching && !tasks.length ? (
            <p className="m-0 text-xs text-slate-500">正在读取任务…</p>
          ) : !tasks.length ? (
            <p className="m-0 text-xs text-slate-500">
              暂无与当前位置相关的操作记录。
            </p>
          ) : (
            <>
              <div className="task-list grid gap-1">
                {tasks.slice(0, 12).map(item => (
                  <button
                    key={item.id}
                    className={cn(
                      'flex items-center gap-2 rounded-lg px-2 py-2 text-left text-xs transition hover:bg-slate-50 dark:hover:bg-slate-800',
                      current?.id === item.id &&
                        'bg-brand-50 dark:bg-brand-100/40'
                    )}
                    onClick={() => setSelected(item.id)}
                  >
                    <i className="inline-flex size-5 shrink-0 items-center justify-center rounded-full bg-slate-100 text-[11px] not-italic text-slate-500 dark:bg-slate-800">
                      {item.status === 'running'
                        ? '◌'
                        : item.status === 'completed'
                          ? '✓'
                          : '!'}
                    </i>
                    <span className="grid gap-0.5 text-slate-700 dark:text-slate-200">
                      {label(item.action)}
                      <small className="text-[11px] text-slate-400">
                        {item.status === 'running'
                          ? '进行中'
                          : item.status === 'failed'
                            ? '需要处理'
                            : '已完成'}
                      </small>
                    </span>
                  </button>
                ))}
              </div>
              {current && (
                <pre className="max-h-48 overflow-auto rounded-lg bg-slate-950 p-2 text-[11px] leading-5 text-slate-200">
                  {current.status === 'failed'
                    ? current.error
                    : current.output || '正在执行…'}
                </pre>
              )}
            </>
          )}
        </section>
      )}
    </div>
  )
}
function EnvironmentPanel({
  open,
  report,
  checking,
  onClose,
  onRefresh,
  onFix
}: {
  open: boolean
  report: { checks: Check[] } | null
  checking: boolean
  onClose: () => void
  onRefresh: () => void
  onFix: (check: Check) => void
}) {
  if (!open) return null
  const checks = report?.checks ?? []
  const readyCount = checks.filter(check => check.status === 'ready').length
  return (
    <aside
      className="environment-panel fixed right-4 top-16 z-30 grid w-[min(380px,calc(100vw-32px))] gap-3 rounded-xl border border-slate-200 bg-white p-4 shadow-2xl dark:border-slate-700 dark:bg-slate-900"
      role="dialog"
      aria-label="全局环境详情"
    >
      <header className="flex items-center justify-between">
        <strong className="text-sm font-semibold text-slate-900 dark:text-slate-100">
          {checking
            ? '正在检查环境…'
            : checks.length
              ? `${readyCount}/${checks.length} 已就绪`
              : '等待检查'}
        </strong>
        <button
          className="inline-flex size-7 items-center justify-center rounded-md text-lg leading-none text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800"
          onClick={onClose}
          aria-label="关闭环境详情"
        >
          ×
        </button>
      </header>
      {checking && (
        <p className="environment-panel-state m-0 text-xs leading-5 text-slate-500">
          正在读取 Node.js、Git 和系统工具状态。
        </p>
      )}
      {!checking && checks.length > 0 && (
        <div className="environment-check-list grid gap-2">
          {checks.map(check => (
            <article
              className={cn(
                'flex items-start gap-2 rounded-lg border p-2',
                check.status === 'ready'
                  ? 'border-emerald-200 bg-emerald-50 dark:border-emerald-900 dark:bg-emerald-950/30'
                  : 'border-amber-200 bg-amber-50 dark:border-amber-900 dark:bg-amber-950/30'
              )}
              key={check.id}
            >
              <i className="inline-flex size-5 shrink-0 items-center justify-center rounded-full bg-white text-xs not-italic dark:bg-slate-900">
                {check.status === 'ready' ? '✓' : '!'}
              </i>
              <div className="grid min-w-0 flex-1 gap-0.5">
                <strong className="text-xs font-semibold text-slate-800 dark:text-slate-100">
                  {check.name}
                </strong>
                <span className="text-xs leading-5 text-slate-500">
                  {check.detail}
                </span>
                {check.status !== 'ready' && check.suggestion && (
                  <small className="text-xs leading-5 text-amber-700 dark:text-amber-300">
                    {check.suggestion}
                  </small>
                )}
              </div>
              {check.status !== 'ready' && (
                <button
                  className="shrink-0 self-center rounded-md px-2 py-1 text-xs font-semibold text-brand-600 hover:bg-white dark:text-brand-200 dark:hover:bg-slate-900"
                  onClick={() => onFix(check)}
                >
                  修复
                </button>
              )}
            </article>
          ))}
        </div>
      )}
      {!checking && !checks.length && (
        <p className="environment-panel-state m-0 text-xs text-slate-500">
          尚未获取检查结果。
        </p>
      )}
      <footer className="flex justify-end border-t border-slate-100 pt-3 dark:border-slate-800">
        <button
          className="inline-flex min-h-8 items-center justify-center rounded-lg border border-slate-200 bg-white px-3 text-xs font-semibold text-slate-600 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-300"
          disabled={checking}
          onClick={onRefresh}
        >
          重新检查
        </button>
      </footer>
    </aside>
  )
}
function EmptyWorkspace({
  onAdd,
  onClone
}: {
  onAdd: () => void
  onClone: () => void
}) {
  return (
    <section className="workspace-content empty-workspace">
      <span>◈</span>
      <div>
        <strong>开始管理你的机器人</strong>
        <p>选择已有目录，或从 Git 克隆一个新的机器人项目。</p>
      </div>
      <footer>
        <button className="secondary-button" onClick={onClone}>
          <GitBranch className="size-3.5" />从 Git 克隆
        </button>
        <button className="primary-button" onClick={onAdd}>
          添加本地目录
        </button>
      </footer>
    </section>
  )
}
function InvalidWorkspace({
  project,
  reason,
  onRemove,
  onChoose
}: {
  project: Project
  reason?: string
  onRemove: () => void
  onChoose: () => void
}) {
  return (
    <section className="workspace-content invalid-workspace">
      <i>!</i>
      <div>
        <strong>机器人目录不可用</strong>
        <span title={project.path}>{project.path}</span>
        <small>{reason || '目录不存在或不再是可管理的机器人项目。'}</small>
      </div>
      <footer>
        <button className="secondary-button" onClick={onRemove}>
          移除旧目录
        </button>
        <button className="primary-button" onClick={onChoose}>
          重新选择目录
        </button>
      </footer>
    </section>
  )
}
function InvalidDirectoryDialog({
  path,
  onClose,
  onCreate
}: {
  path: string
  onClose: () => void
  onCreate: () => void
}) {
  return (
    <div className="invalid-directory-backdrop" role="presentation">
      <section
        className="invalid-directory-dialog"
        role="dialog"
        aria-modal="true"
        aria-label="目录不是合法机器人项目"
      >
        <header>
          <i>!</i>
          <div>
            <strong>所选目录不是合法机器人目录</strong>
            <small title={path}>{path}</small>
          </div>
          <button
            className="icon-button"
            onClick={onClose}
            aria-label="关闭提示"
          >
            <X />
          </button>
        </header>
        <p>
          这里缺少 <code>package.json</code>
          ，因此不能作为已有机器人管理。你可以选择一个已有机器人目录，或在这里创建新机器人。
        </p>
        <footer>
          <button className="secondary-button" onClick={onClose}>
            重新选择
          </button>
          <button className="primary-button" onClick={onCreate}>
            前往引导创建
          </button>
        </footer>
      </section>
    </div>
  )
}
function SystemPluginCenter({
  plugins,
  onOpen
}: {
  plugins: SetupPlugin[]
  onOpen: (id: string) => void
}) {
  const [setEnabled, { isLoading }] = useSetSetupPluginEnabledMutation()
  const [message, setMessage] = useState('')
  const toggle = async (plugin: SetupPlugin) => {
    try {
      await setEnabled({
        pluginID: plugin.id,
        enabled: !plugin.enabled
      }).unwrap()
      setMessage(
        plugin.enabled ? `已卸载“${plugin.name}”。` : `已启用“${plugin.name}”。`
      )
    } catch (reason) {
      setMessage(operationErrorMessage(reason, '插件状态未更新。'))
    }
  }
  return (
    <section className="workspace-content setup-plugin-manager grid max-w-[900px] content-start gap-4">
      <header className="flex min-h-10 items-center justify-between border-b border-slate-200 pb-3 dark:border-slate-700">
        <div>
          <h1 className="m-0 flex items-center gap-2 text-base font-semibold tracking-tight text-slate-900 dark:text-slate-100">
            插件 <small>{plugins.filter(item => item.enabled).length}</small>
          </h1>
        </div>
      </header>
      {plugins.length ? (
        <section className="setup-plugin-cards grid overflow-hidden rounded-xl border border-slate-200 bg-white dark:border-slate-700 dark:bg-slate-900">
          {plugins.map(plugin => (
            <article
              className={cn(
                'flex items-center gap-2 border-b border-slate-100 px-3 py-2 last:border-b-0 dark:border-slate-800',
                !plugin.enabled && 'bg-slate-50 dark:bg-slate-950'
              )}
              key={plugin.id}
            >
              <button
                className="setup-plugin-open flex min-h-11 min-w-0 flex-1 items-center gap-2.5 rounded-lg bg-transparent p-0 text-left transition hover:bg-slate-50 disabled:cursor-default disabled:opacity-60 dark:hover:bg-slate-800"
                onClick={() =>
                  plugin.enabled && plugin.web && onOpen(plugin.id)
                }
                disabled={!plugin.enabled || !plugin.web}
              >
                <i className="inline-flex size-8 shrink-0 items-center justify-center rounded-lg bg-brand-50 text-brand-600 not-italic dark:bg-brand-100/60 dark:text-brand-200">
                  {setupPluginIcon(plugin.navigation.icon)}
                </i>
                <div className="grid min-w-0 gap-0.5">
                  <strong className="truncate text-sm font-semibold text-slate-800 dark:text-slate-100">
                    {plugin.name}
                  </strong>
                  <small className="text-xs text-slate-400">
                    v{plugin.version} ·{' '}
                    {plugin.online
                      ? '在线目录'
                      : !plugin.web
                        ? '缺少 Web 界面'
                        : plugin.enabled
                          ? '已启用'
                          : '已卸载'}
                  </small>
                </div>
                {plugin.enabled && plugin.web && (
                  <ChevronRight className="ml-auto size-4 shrink-0 text-slate-400" />
                )}
              </button>
              <Button
                variant={plugin.enabled ? 'secondary' : 'primary'}
                className="setup-plugin-toggle"
                disabled={isLoading}
                onClick={() => void toggle(plugin)}
              >
                {plugin.enabled ? '卸载' : '启用'}
              </Button>
            </article>
          ))}
        </section>
      ) : (
        <section className="setup-plugin-empty grid gap-1 rounded-xl border border-dashed border-slate-300 bg-slate-50 p-5 dark:border-slate-600 dark:bg-slate-900">
          <strong className="text-sm text-slate-600 dark:text-slate-300">
            暂未发现插件
          </strong>
          <span className="text-xs leading-5 text-slate-500">
            将插件目录放入 plugins 后刷新即可。
          </span>
        </section>
      )}
      {message && (
        <p className="setup-plugin-message rounded-lg border border-brand-200 bg-brand-50 px-3 py-2 text-xs text-brand-600 dark:border-brand-200 dark:bg-brand-100/40 dark:text-brand-200">
          {message}
        </p>
      )}
    </section>
  )
}
function SetupPluginCenter({ plugin }: { plugin: SetupPlugin }) {
  // A setup plugin's interface is its web UI, served same-origin by alx. The
  // declarative action-list model was removed; the web view calls the plugin's
  // action forward API itself.
  const hasWeb = Boolean(plugin.web && plugin.runnable && !plugin.online)
  const theme = document.documentElement.dataset.theme ?? 'light'
  const webSrc = `/api/v1/setup/plugins/web/${plugin.id}/index.html?theme=${theme}`

  return (
    <section>
      {hasWeb ? (
        <iframe
          className="h-[640px] w-full border-0"
          src={webSrc}
          title={`${plugin.name} 界面`}
        />
      ) : (
        <div className="setup-plugin-web-missing grid gap-2 rounded-xl border border-dashed border-slate-300 bg-slate-50 p-6 text-center dark:border-slate-600 dark:bg-slate-900">
          <strong className="text-sm text-slate-700 dark:text-slate-200">
            此插件需要 Web 界面
          </strong>
          <p className="m-0 text-xs leading-5 text-slate-500">
            {plugin.online
              ? '该插件由在线目录识别，安装到本机后才能打开其界面。'
              : '插件清单未声明 web 目录，或缺少可用的执行器，因此无法展示界面。'}
          </p>
        </div>
      )}
    </section>
  )
}
function BackpackPanel({
  root,
  items,
  loading,
  failed,
  onRefresh,
  onOpenPlugins,
  busy,
  onSaveConfig,
  onRemove,
  onReplace
}: {
  root: string
  items: Array<{
    name: string
    version?: string
    description?: string
    path: string
    valid: boolean
  }>
  loading: boolean
  failed: boolean
  onRefresh: () => void
  onOpenPlugins: () => void
  busy: boolean
  onSaveConfig: (
    packageName: string,
    values: Record<string, string>
  ) => Promise<boolean>
  onRemove: (packageName: string) => Promise<void>
  onReplace: (packageName: string, version: string) => Promise<boolean>
}) {
  const [selectedName, setSelectedName] = useState('')
  useEffect(() => {
    if (selectedName && !items.some(item => item.name === selectedName))
      setSelectedName('')
  }, [items, selectedName])
  const selected = items.find(item => item.name === selectedName)
  if (selected)
    return (
      <BackpackPackageManager
        root={root}
        item={selected}
        busy={busy}
        onSave={onSaveConfig}
        onRemove={onRemove}
        onReplace={onReplace}
        onBack={() => setSelectedName('')}
        onRefresh={onRefresh}
      />
    )
  return (
    <section className="backpack-panel grid max-w-[760px] gap-4">
      <header className="flex items-center justify-between gap-3 border-b border-slate-200 pb-3">
        <div className="grid gap-1">
          <p className="m-0 text-lg font-semibold text-ink-950">背包</p>
          <small className="text-xs text-slate-500" title={`${root}/packages`}>
            packages
          </small>
        </div>
        <div className="flex items-center gap-2">
          <button className="text-button" onClick={onOpenPlugins}>
            插件中心
          </button>
          <button
            className="secondary-button"
            disabled={loading}
            onClick={onRefresh}
            aria-label="刷新背包"
            title="刷新背包"
          >
            {loading ? '读取中…' : <RefreshCw />}
          </button>
        </div>
      </header>
      {loading ? (
        <p className="grid min-h-32 place-items-center text-sm text-slate-500">
          正在读取本地插件包…
        </p>
      ) : items.length ? (
        <div className="grid gap-2">
          {items.map(item => (
            <article
              className={cn(
                'rounded-lg border bg-white transition hover:border-slate-300',
                item.valid ? 'border-slate-200' : 'border-amber-200 bg-amber-50'
              )}
              key={item.path}
            >
              <button
                type="button"
                className="flex w-full items-center gap-3 p-3 text-left"
                onClick={() => setSelectedName(item.name)}
              >
                <div>
                  <strong className="flex items-center gap-2 text-sm font-semibold text-slate-800">
                    {item.name}
                    {item.version && (
                      <em className="not-italic text-xs text-slate-400">
                        v{item.version}
                      </em>
                    )}
                  </strong>
                  <span className="text-xs text-slate-500">
                    {item.valid
                      ? item.description || '本地 AlemonJS 插件包'
                      : '缺少有效 package.json，暂不能作为插件运行。'}
                  </span>
                  <small
                    className="truncate text-[11px] text-slate-400"
                    title={item.path}
                  >
                    {item.path}
                  </small>
                </div>
                <ChevronRight
                  className="size-4 shrink-0 text-slate-400"
                  aria-hidden="true"
                />
              </button>
            </article>
          ))}
        </div>
      ) : (
        <section className="grid min-h-40 place-items-center gap-2 rounded-xl border border-dashed border-slate-300 p-6 text-center">
          <strong className="text-sm font-semibold text-slate-700">
            暂无插件包
          </strong>
          <span className="text-xs text-slate-500">
            {failed
              ? '暂未能读取本地 packages 目录，你仍可从插件页安装。'
              : '安装后的本地插件包会显示在这里。'}
          </span>
          <button className="secondary-button" onClick={onOpenPlugins}>
            前往插件
          </button>
        </section>
      )}
    </section>
  )
}

function BackpackPackageManager({
  root,
  item,
  busy,
  onSave,
  onRemove,
  onReplace,
  onBack,
  onRefresh
}: {
  root: string
  item: {
    name: string
    version?: string
    description?: string
    path: string
    valid: boolean
  }
  busy: boolean
  onSave: (
    packageName: string,
    values: Record<string, string>
  ) => Promise<boolean>
  onRemove: (packageName: string) => Promise<void>
  onReplace: (packageName: string, version: string) => Promise<boolean>
  onBack: () => void
  onRefresh: () => void
}) {
  const [tab, setTab] = useState<'readme' | 'config' | 'version'>('readme')
  const [version, setVersion] = useState('')
  const { data, isFetching, error } = usePackageConfigQuery(
    { root, package: item.name },
    { skip: !item.valid }
  )
  const {
    data: readme,
    isFetching: isReadmeFetching,
    error: readmeError
  } = useLocalPackageReadmeQuery(
    { root, package: item.name },
    { skip: !item.valid || tab !== 'readme' }
  )
  const {
    data: versions,
    isFetching: versionsFetching,
    error: versionsError
  } = useLocalPackageVersionsQuery(
    { root, package: item.name },
    {
      skip: !item.valid || tab !== 'version'
    }
  )
  const [values, setValues] = useState<Record<string, string>>({})
  useEffect(() => {
    if (data)
      setValues(
        Object.fromEntries(
          data.fields.map(field => [field.name, data.values[field.name] ?? ''])
        )
      )
  }, [data])
  useEffect(() => {
    if (versions?.latest) setVersion(versions.latest)
  }, [versions])
  return (
    <section className="backpack-manager grid max-w-[760px] gap-4">
      <header className="flex flex-wrap items-start justify-between gap-3 border-b border-slate-200 pb-3">
        <div className="grid min-w-0 gap-1">
          <button className="text-button justify-self-start" onClick={onBack}>
            ‹ 返回背包
          </button>
          <h2 className="m-0 break-all text-lg font-semibold text-ink-950">
            {item.name}
            {item.version && (
              <em className="ml-2 not-italic text-xs text-slate-400">
                v{item.version}
              </em>
            )}
          </h2>
          <small className="truncate text-xs text-slate-500" title={item.path}>
            {item.path}
          </small>
        </div>
        <div className="flex items-center gap-2">
          <button
            className="icon-button size-9 p-0"
            onClick={onRefresh}
            title="刷新背包"
          >
            <RefreshCw className="size-4" />
          </button>
          <button
            className="inline-flex min-h-8 items-center justify-center rounded-md border border-red-200 px-3 text-xs font-semibold text-red-700 transition hover:bg-red-50"
            disabled={busy}
            onClick={() => void onRemove(item.name)}
          >
            卸载
          </button>
        </div>
      </header>
      <Tabs
        ariaLabel="插件详情"
        items={[
          { id: 'readme', label: '文档' },
          { id: 'config', label: '配置' },
          { id: 'version', label: '版本' }
        ]}
        onChange={setTab}
        value={tab}
      />
      <div className="grid gap-3">
        {!item.valid ? (
          <p className="rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs leading-5 text-amber-800">
            这个目录没有有效的 package.json，因此只能从文件系统修复或移除。
          </p>
        ) : tab === 'readme' ? (
          isReadmeFetching ? (
            <p className="rounded-lg border border-slate-200 bg-slate-50 p-3 text-xs text-slate-500">
              正在读取 README.md…
            </p>
          ) : readmeError || !readme ? (
            <p className="rounded-lg border border-slate-200 bg-slate-50 p-3 text-xs leading-5 text-slate-500">
              这个插件没有 README.md；请在“配置”页查看可用设置。
            </p>
          ) : (
            <MarkdownPage markdown={readme.output} />
          )
        ) : tab === 'config' ? (
          isFetching ? (
            <p className="backpack-manager-note">正在读取插件的配置声明…</p>
          ) : error || !data ? (
            <p className="backpack-manager-note">
              该插件没有声明可视化配置。使用方式请查看“文档”页。
            </p>
          ) : (
            <div className="package-config-panel grid gap-4 rounded-xl border border-slate-200 bg-white p-4">
              <header className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-200 pb-3">
                <div className="grid gap-1">
                  <strong className="text-sm font-semibold text-slate-800">
                    插件配置
                  </strong>
                  <span className="text-xs text-slate-500">
                    保存到当前机器人的 alemon.config.yaml · {data.namespace}.*
                  </span>
                </div>
                <button
                  className="primary-button"
                  disabled={busy}
                  onClick={() => void onSave(item.name, values)}
                >
                  保存配置
                </button>
              </header>
              <div className="grid gap-3 sm:grid-cols-2">
                {data.fields.map(field => (
                  <label
                    className="grid gap-1 text-xs font-semibold text-slate-600"
                    key={field.name}
                  >
                    {field.description || field.name}
                    {field.required && (
                      <em className="not-italic text-amber-700">必填</em>
                    )}
                    {field.type === 'boolean' || field.type === 'bool' ? (
                      <select
                        className="h-9 rounded-md border border-slate-300 bg-white px-2.5 text-sm font-normal text-slate-800 outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
                        value={values[field.name] ?? ''}
                        onChange={event =>
                          setValues({
                            ...values,
                            [field.name]: event.target.value
                          })
                        }
                      >
                        <option value="">不设置</option>
                        <option value="true">开启</option>
                        <option value="false">关闭</option>
                      </select>
                    ) : (
                      <input
                        className="h-9 rounded-md border border-slate-300 bg-white px-2.5 text-sm font-normal text-slate-800 outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
                        value={values[field.name] ?? ''}
                        type={
                          field.type === 'number' || field.type === 'integer'
                            ? 'number'
                            : 'text'
                        }
                        onChange={event =>
                          setValues({
                            ...values,
                            [field.name]: event.target.value
                          })
                        }
                      />
                    )}
                  </label>
                ))}
              </div>
            </div>
          )
        ) : versionsFetching ? (
          <p className="backpack-manager-note">正在读取可安装版本…</p>
        ) : versionsError || !versions?.versions.length ? (
          <p className="backpack-manager-note">
            暂时无法读取此插件的版本。当前本地版本为 {item.version || '未知'}。
          </p>
        ) : (
          <section className="grid gap-4 rounded-xl border border-slate-200 bg-white p-4">
            <div className="grid gap-1">
              <strong className="text-sm font-semibold text-slate-800">
                {versions.source === 'git' ? 'Git 版本' : 'npm 版本'}
              </strong>
              <span className="text-xs leading-5 text-slate-500">
                当前使用 {versions.current || item.version || '未知'}；
                {versions.source === 'git'
                  ? '此插件是 Git 工作区，版本以标签为准。'
                  : '未检测到 Git，使用 npm 已发布版本。'}
              </span>
            </div>
            <div className="flex flex-wrap items-center justify-end gap-2 border-t border-slate-200 pt-3">
              <select
                className="h-9 min-w-40 rounded-md border border-slate-300 bg-white px-2.5 text-xs font-medium text-slate-700 outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
                value={version}
                onChange={event => setVersion(event.target.value)}
              >
                {versions.versions.map(candidate => (
                  <option key={candidate} value={candidate}>
                    {versions.source === 'npm' ? `v${candidate}` : candidate}
                  </option>
                ))}
              </select>
              <button
                className="primary-button"
                disabled={
                  busy ||
                  !version ||
                  version === versions.current ||
                  version.replace(/^v/, '') === item.version
                }
                onClick={() => void onReplace(item.name, version)}
              >
                切换版本
              </button>
            </div>
          </section>
        )}
      </div>
    </section>
  )
}
function CatalogDetail({
  item,
  group,
  kind,
  busy,
  onBack,
  onRun,
  onSaveConfig
}: {
  item: CatalogItem
  group: string
  kind: 'connection' | 'plugin'
  busy: boolean
  onBack: () => void
  onRun: (action: string, packageName: string) => void
  onSaveConfig: (
    packageName: string,
    values: Record<string, string>
  ) => Promise<boolean>
}) {
  const [version, setVersion] = useState('')
  const [configOpen, setConfigOpen] = useState(false)
  const {
    data: document,
    isFetching,
    error
  } = useCatalogDocumentQuery(item.url, { skip: !item.url })
  const packageName =
    item.install ||
    (item.name === 'alemonjs' || item.name.startsWith('@alemonjs/')
      ? item.name
      : '')
  const repositoryInstall = packageName.startsWith('git+')
  const npmPackage = Boolean(packageName && !repositoryInstall)
  const {
    data: packageVersions,
    isFetching: versionsLoading,
    error: versionsError
  } = useCatalogVersionsQuery(packageName, { skip: !packageName })
  useEffect(() => {
    setVersion('')
  }, [packageName])
  useEffect(() => {
    if (!version && packageVersions?.latest) setVersion(packageVersions.latest)
  }, [packageVersions?.latest, version])
  const noRepositoryTag =
    repositoryInstall &&
    !versionsLoading &&
    !versionsError &&
    packageVersions?.versions.length === 0
  const installTarget = version.trim()
    ? npmPackage
      ? `${packageName}@${version.trim()}`
      : `${packageName.split('#')[0]}#${version.trim()}`
    : packageName
  const installAction =
    kind === 'connection' ? 'install-connection' : 'install-package'
  const uninstallAction =
    kind === 'connection' ? 'uninstall-connection' : 'uninstall-package'
  return (
    <section className="catalog-detail grid max-w-[760px] gap-4">
      <header className="flex items-center gap-3 border-b border-slate-200 pb-3">
        <button className="text-button" onClick={onBack}>
          ‹ 返回目录
        </button>
        <span className="text-xs font-semibold text-slate-400">{group}</span>
      </header>
      <section className="catalog-control flex flex-wrap items-start justify-between gap-4 rounded-xl border border-slate-200 bg-white p-4">
        <div className="grid min-w-0 gap-1">
          <h1 className="m-0 break-all text-lg font-semibold text-ink-950">
            {item.name}
          </h1>
          <p className="m-0 text-sm text-slate-500">
            {item.description || '在线生态目录条目'}
          </p>
        </div>
        <div className="flex flex-wrap items-end justify-end gap-2">
          {packageName ? (
            <label className="grid gap-1 text-[11px] font-semibold text-slate-500">
              {repositoryInstall ? '插件版本' : '版本'}
              <select
                className="h-9 min-w-32 rounded-md border border-slate-300 bg-white px-2 text-xs font-medium text-slate-700 outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
                value={version}
                onChange={event => setVersion(event.target.value)}
                disabled={
                  versionsLoading || Boolean(versionsError) || noRepositoryTag
                }
              >
                {versionsLoading && <option value="">读取版本…</option>}
                {versionsError && <option value="">版本读取失败</option>}
                {noRepositoryTag && (
                  <option value="">该插件没有可用的正式 Release</option>
                )}
                {packageVersions?.versions.map(itemVersion => (
                  <option key={itemVersion} value={itemVersion}>
                    {itemVersion}
                    {itemVersion === packageVersions.latest ? ' · 最新版' : ''}
                  </option>
                ))}
              </select>
            </label>
          ) : (
            <span className="rounded-md bg-slate-100 px-2.5 py-2 text-xs font-semibold text-slate-600">
              {repositoryInstall ? 'Git' : '拒绝'}
            </span>
          )}
          <button
            className="primary-button"
            disabled={
              busy ||
              !packageName ||
              versionsLoading ||
              Boolean(versionsError) ||
              noRepositoryTag ||
              (repositoryInstall && !version.trim())
            }
            onClick={() => onRun(installAction, installTarget)}
          >
            {busy ? '处理中…' : kind === 'connection' ? '安装' : '安装'}
          </button>
          <button
            className="secondary-button"
            disabled={
              busy || !packageName || (kind === 'plugin' && repositoryInstall)
            }
            title={
              repositoryInstall && kind === 'plugin'
                ? '仓库插件请按文档卸载'
                : '卸载当前包'
            }
            onClick={() => onRun(uninstallAction, packageName)}
          >
            卸载
          </button>
          <button
            className="secondary-button"
            disabled={busy || !item.url}
            onClick={() => setConfigOpen(open => !open)}
          >
            配置
          </button>
        </div>
      </section>
      {repositoryInstall && noRepositoryTag && (
        <p className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">
          该插件仓库没有正式 Release，不能作为可复现的版本安装。
        </p>
      )}
      {repositoryInstall && versionsError && (
        <p className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">
          无法读取插件 Release，请检查网络后重试。
        </p>
      )}
      {configOpen && (
        <PackageConfigPanel
          source={item.url}
          busy={busy}
          onSave={onSaveConfig}
        />
      )}
      <section className="catalog-document grid gap-3 rounded-xl border border-slate-200 bg-white p-4">
        <header className="flex items-center justify-between gap-3 border-b border-slate-200 pb-3">
          <strong className="text-sm font-semibold text-slate-800">
            在线文档
          </strong>
          {item.url && (
            <a
              className="text-xs font-semibold text-slate-600 hover:text-slate-900"
              href={item.url}
              target="_blank"
              rel="noreferrer"
            >
              在浏览器打开 ↗
            </a>
          )}
        </header>
        {isFetching && (
          <p className="text-sm text-slate-500">正在读取 README.md…</p>
        )}
        {error && (
          <p className="text-sm text-slate-500">
            在线文档暂时无法读取，请使用右上角链接查看。
          </p>
        )}
        {document && <MarkdownPage markdown={document.markdown} />}
      </section>
    </section>
  )
}
function PackageConfigPanel({
  source,
  busy,
  onSave
}: {
  source: string
  busy: boolean
  onSave: (
    packageName: string,
    values: Record<string, string>
  ) => Promise<boolean>
}) {
  const { data, isFetching, error } = useCatalogPackageConfigQuery(source, {
    skip: !source
  })
  const [values, setValues] = useState<Record<string, string>>({})
  useEffect(() => {
    if (data)
      setValues(
        Object.fromEntries(
          data.fields.map(field => [field.name, data.values[field.name] ?? ''])
        )
      )
  }, [data])
  if (isFetching)
    return (
      <section className="package-config-panel grid gap-3 rounded-xl border border-slate-200 bg-white p-4 text-sm text-slate-500">
        <p>正在读取包配置声明…</p>
      </section>
    )
  if (error || !data)
    return (
      <section className="package-config-panel rounded-xl border border-slate-200 bg-white p-4 text-sm text-slate-500">
        <p>该条目没有可读取的 alemonjs.config 声明。</p>
      </section>
    )
  return (
    <section className="package-config-panel grid gap-4 rounded-xl border border-slate-200 bg-white p-4">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-200 pb-3">
        <div className="grid gap-1">
          <strong className="text-sm font-semibold text-slate-800">
            运行配置
          </strong>
          <span className="text-xs text-slate-500">
            保存至 alemon.config.yaml · {data.namespace}.*
          </span>
        </div>
        <button
          className="primary-button"
          disabled={busy}
          onClick={() => void onSave(data.package, values)}
        >
          保存配置
        </button>
      </header>
      <div className="grid gap-3 sm:grid-cols-2">
        {data.fields.map(field => (
          <label
            className="grid gap-1 text-xs font-semibold text-slate-600"
            key={field.name}
          >
            {field.description || field.name}
            {field.required && (
              <em className="not-italic text-amber-700">必填</em>
            )}
            {field.type === 'boolean' || field.type === 'bool' ? (
              <select
                className="h-9 rounded-md border border-slate-300 bg-white px-2.5 text-sm font-normal text-slate-800 outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
                value={values[field.name] ?? ''}
                onChange={event =>
                  setValues({ ...values, [field.name]: event.target.value })
                }
              >
                <option value="">不设置</option>
                <option value="true">开启</option>
                <option value="false">关闭</option>
              </select>
            ) : (
              <input
                className="h-9 rounded-md border border-slate-300 bg-white px-2.5 text-sm font-normal text-slate-800 outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
                value={values[field.name] ?? ''}
                type={
                  field.type === 'number' || field.type === 'integer'
                    ? 'number'
                    : 'text'
                }
                onChange={event =>
                  setValues({ ...values, [field.name]: event.target.value })
                }
                placeholder={field.name}
              />
            )}
          </label>
        ))}
      </div>
    </section>
  )
}
function CurrentProjectConfigPanel({
  config,
  loading,
  busy,
  onSave
}: {
  config?: {
    package: string
    namespace: string
    fields: Array<{
      name: string
      type: string
      required: boolean
      description: string
    }>
    values: Record<string, string>
  }
  loading: boolean
  busy: boolean
  onSave: (values: Record<string, string>) => Promise<boolean>
}) {
  const [values, setValues] = useState<Record<string, string>>({})
  useEffect(() => {
    if (config) setValues(config.values)
  }, [config])
  // A config declaration is optional. Do not turn its absence into an error
  // for ordinary robots that do not expose project-specific settings.
  if (loading)
    return (
      <section className="project-config-panel rounded-xl border border-slate-200 bg-white p-4 text-sm text-slate-500">
        <p>正在识别当前项目的扩展配置…</p>
      </section>
    )
  if (!config?.fields.length) return null
  return (
    <section className="project-config-panel grid gap-4 rounded-xl border border-slate-200 bg-white p-4">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-200 pb-3">
        <div className="grid gap-1">
          <strong className="text-sm font-semibold text-slate-800">
            项目扩展配置
          </strong>
          <span className="text-xs text-slate-500">
            {config.package} · 保存至 alemon.config.yaml 的 {config.namespace}{' '}
            区域
          </span>
        </div>
        <button
          className="primary-button"
          disabled={busy}
          onClick={() => void onSave(values)}
        >
          保存
        </button>
      </header>
      <div className="grid gap-3 sm:grid-cols-2">
        {config.fields.map(field => (
          <label
            className="grid gap-1 text-xs font-semibold text-slate-600"
            key={field.name}
          >
            {field.description || field.name}
            {field.required && (
              <em className="not-italic text-amber-700">必填</em>
            )}
            {field.type === 'boolean' || field.type === 'bool' ? (
              <select
                className="h-9 rounded-md border border-slate-300 bg-white px-2.5 text-sm font-normal text-slate-800 outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
                value={values[field.name] ?? ''}
                onChange={event =>
                  setValues({ ...values, [field.name]: event.target.value })
                }
              >
                <option value="">不设置</option>
                <option value="true">开启</option>
                <option value="false">关闭</option>
              </select>
            ) : (
              <input
                className="h-9 rounded-md border border-slate-300 bg-white px-2.5 text-sm font-normal text-slate-800 outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
                value={values[field.name] ?? ''}
                type={
                  field.type === 'number' || field.type === 'integer'
                    ? 'number'
                    : 'text'
                }
                onChange={event =>
                  setValues({ ...values, [field.name]: event.target.value })
                }
                placeholder={field.name}
              />
            )}
          </label>
        ))}
      </div>
    </section>
  )
}
function MarkdownPage({ markdown }: { markdown: string }) {
  return (
    <article className="markdown-page">
      <Markdown
        options={{
          forceBlock: true,
          overrides: {
            a: {
              component: ({
                href,
                children,
                ...props
              }: {
                href?: string
                children?: ReactNode
              }) => (
                <a href={href} target="_blank" rel="noreferrer" {...props}>
                  {children}
                </a>
              )
            }
          }
        }}
      >
        {markdown}
      </Markdown>
    </article>
  )
}
function RuntimePanel({
  overview,
  pm2Status,
  pm2StatusError,
  root,
  loading,
  busy,
  developmentRunning,
  foregroundRunning,
  developmentStopping,
  foregroundStopping,
  pm2Running,
  onRefresh,
  onRefreshOverview,
  onOpenConsole,
  onRun,
  onSaveLogin,
  onSavePackageConfig,
  developerMode
}: {
  overview?: RuntimeOverview
  pm2Status?: PM2Status
  pm2StatusError: boolean
  root: string
  loading: boolean
  busy: boolean
  developmentRunning: boolean
  foregroundRunning: boolean
  developmentStopping: boolean
  foregroundStopping: boolean
  pm2Running: boolean
  onRefresh: () => void
  onRefreshOverview: () => Promise<RuntimeOverview | undefined>
  onOpenConsole: () => void
  onRun: (action: string, packageName?: string) => Promise<boolean>
  onSaveLogin: (login: string, packageName?: string) => Promise<boolean>
  onSavePackageConfig: (
    packageName: string,
    values: Record<string, string>
  ) => Promise<boolean>
  developerMode: boolean
}) {
  type PendingAction = { label: string; note: string; execute: () => void }
  type LoginChoice = {
    action: string
    label: string
    note: string
    preflight: RuntimePreflight
  }
  const [customLogin, setCustomLogin] = useState('')
  const [customPackage, setCustomPackage] = useState('')
  const [selectedPlatform, setSelectedPlatform] = useState('')
  const [pending, setPending] = useState<PendingAction | null>(null)
  const [validationMessage, setValidationMessage] = useState('')
  const [validationTitle, setValidationTitle] = useState('运行前配置不完整')
  const [loadPackageConfig] = useLazyPackageConfigQuery()
  const [loadRuntimePreflight] = useLazyRobotRuntimePreflightQuery()
  const [loginChoice, setLoginChoice] = useState<LoginChoice | null>(null)
  const [connectionConfig, setConnectionConfig] = useState<{
    package: string
    fields: Array<{
      name: string
      type: string
      required: boolean
      description: string
    }>
    values: Record<string, string>
  } | null>(null)
  const [connectionValues, setConnectionValues] = useState<
    Record<string, string>
  >({})
  const [loginDialogError, setLoginDialogError] = useState('')
  const [loginDialogBusy, setLoginDialogBusy] = useState(false)
  const [pm2LogsOpen, setPM2LogsOpen] = useState(false)
  const [pm2ProcessesOpen, setPM2ProcessesOpen] = useState(false)
  const persistentReady = overview?.pm2Configured && overview.hasStartScript
  const pm2Managed = Boolean(pm2Status?.managed)
  const pm2LocalRunning = pm2Running
  const localRunning = developmentRunning || foregroundRunning
  // A missing dependency blocks every run action until dependencies install.
  const depsMissing = overview ? !overview.dependenciesComplete : false
  const knownPlatform = (overview?.platforms ?? []).find(
    item => item.id === selectedPlatform
  )
  const packageTarget = knownPlatform?.package || customPackage.trim()
  const ask = (label: string, note: string, execute: () => void) =>
    setPending({ label, note, execute })
  const confirm = () => {
    pending?.execute()
    setPending(null)
  }
  // A local process and a background PM2 service both start the same robot
  // directory. Block the second one before the start dialog even opens.
  const askStart = async (action: string, label: string, note: string) => {
    const startingLocal = action === 'dev' || action === 'app'
    const startingPM2 = action === 'pm2' || action === 'pm2-reload'
    if (startingLocal && pm2LocalRunning) {
      setValidationTitle('已有进程在运行')
      setValidationMessage(
        '当前目录正在后台（PM2）运行；请先在“后台运行”中停止服务，再启动本机进程。'
      )
      return
    }
    if (startingPM2 && localRunning) {
      setValidationTitle('已有进程在运行')
      setValidationMessage(
        '当前目录正在本机（开发/前台）运行；请先停止本机进程，再启动后台服务。'
      )
      return
    }
    try {
      // Bypass the 1-hour query cache so a just-installed connection package is
      // already reflected when the start dialog opens.
      setValidationTitle('运行前配置不完整')
      const preflight = await loadRuntimePreflight(root, false).unwrap()
      const freshOverview = await onRefreshOverview()
      const platform = (freshOverview?.platforms ?? []).find(
        item => item.id === preflight.login
      )
      setCustomLogin(preflight.login)
      setSelectedPlatform(platform?.id ?? '')
      setCustomPackage(platform?.package ?? '')
      setConnectionConfig(null)
      setConnectionValues({})
      if (platform?.installed && platform.package)
        void loadConnectionConfig(platform.package)
      setLoginDialogError('')
      setLoginChoice({ action, label, note, preflight })
    } catch (reason) {
      setValidationMessage(
        operationErrorMessage(reason, '无法完成运行前检查。')
      )
    }
  }
  const closeLoginDialog = () => {
    setLoginChoice(null)
    setLoginDialogError('')
  }
  const loadConnectionConfig = async (packageName: string) => {
    if (!packageName) {
      setConnectionConfig(null)
      setConnectionValues({})
      return
    }
    try {
      const config = await loadPackageConfig({
        root,
        package: packageName
      }).unwrap()
      setConnectionConfig(config)
      setConnectionValues(config.values)
    } catch (reason) {
      const message = operationErrorMessage(reason, '无法读取连接包配置。')
      // A config declaration is optional; it is valid to continue without a
      // form when the installed package declares no alemonjs.config fields.
      if (message.includes('没有声明 alemonjs.config')) {
        setConnectionConfig({ package: packageName, fields: [], values: {} })
        setConnectionValues({})
        return
      }
      setLoginDialogError(message)
    }
  }
  const choosePlatform = async (id: string) => {
    setSelectedPlatform(id)
    if (!id) {
      // "不选择" clears every login trace so the dialog looks untouched.
      setCustomLogin('')
      setCustomPackage('')
      setConnectionConfig(null)
      setConnectionValues({})
      setLoginDialogError('')
      return
    }
    // Use the freshest installed flag, not the possibly-stale render snapshot,
    // so a package installed moments ago loads its config (with any saved
    // required values) instead of being treated as "not installed".
    const freshOverview = await onRefreshOverview()
    const platform = (
      freshOverview?.platforms ??
      overview?.platforms ??
      []
    ).find(item => item.id === id)
    if (platform) {
      setCustomLogin(platform.id)
      setCustomPackage(platform.package)
      void loadConnectionConfig(platform.package)
    }
  }
  const installSelectedConnection = async () => {
    if (!packageTarget) return
    setLoginDialogBusy(true)
    try {
      if (await onRun('install-connection', packageTarget)) {
        await loadConnectionConfig(packageTarget)
        // Re-read the preflight so the "确认启动" gate sees the package as
        // installed instead of keeping it disabled on the pre-install snapshot.
        const preflight = await loadRuntimePreflight(root, false).unwrap()
        setLoginChoice(current =>
          current ? { ...current, preflight } : current
        )
        setLoginDialogError('连接包已安装。请填写下方配置后点击“启动”。')
      }
    } catch (reason) {
      setLoginDialogError(operationErrorMessage(reason, '连接包安装未完成。'))
    } finally {
      setLoginDialogBusy(false)
    }
  }
  // Unified start action for the login dialog. It always saves the current
  // connection config (and login when one is chosen), then starts the robot.
  // Without a login it starts directly; with one it persists the required
  // fields silently before launching.
  const startFromDialog = async () => {
    if (!loginChoice) return
    // A login is the user's typed value, or the one already configured in the
    // file when the user did not touch the login field.
    const login = customLogin.trim() || loginChoice.preflight.login || ''
    const hasLogin = Boolean(login || selectedPlatform)
    const missing = (connectionConfig?.fields ?? [])
      .filter(field => field.required && !connectionValues[field.name]?.trim())
      .map(field => field.description || field.name)
    if (hasLogin && missing.length) {
      setLoginDialogError(`请先填写必填项：${missing.join('、')} 再启动。`)
      return
    }
    setLoginDialogBusy(true)
    try {
      // Persist required fields silently when a connection package is selected,
      // then save the login before launching.
      if (packageTarget && connectionConfig?.fields.length) {
        if (!(await onSavePackageConfig(packageTarget, connectionValues)))
          return
      }
      if (login) {
        if (!(await onSaveLogin(login, packageTarget))) return
      }
      if (await onRun(loginChoice.action)) closeLoginDialog()
    } catch (reason) {
      setLoginDialogError(
        operationErrorMessage(reason, '启动失败，请查看操作记录。')
      )
    } finally {
      setLoginDialogBusy(false)
    }
  }
  return (
    <section className="runtime-overview grid max-w-[760px] gap-4">
      <header className="flex items-start justify-between gap-4 border-b border-slate-200 pb-4">
        <div className="grid min-w-0 gap-1">
          <p className="m-0 text-xs font-semibold text-slate-500">运行</p>
          <h1 className="m-0 truncate text-xl font-semibold tracking-tight text-ink-950">
            {overview?.name || '正在读取项目…'}
          </h1>
          <small className="text-xs text-slate-500">
            {overview
              ? `${overview.version || '未设置版本'} · ${overview.packageManager} · ${overview.hasDevScript ? '已配置开发命令' : '未配置 dev 命令'}`
              : '读取包信息、平台包与运行状态。'}
          </small>
        </div>
        <button
          className="icon-button size-9 shrink-0 p-0"
          disabled={loading}
          onClick={onRefresh}
        >
          <RefreshCw className="size-4" />
        </button>
      </header>
      <ConfirmDialog
        open={Boolean(pending)}
        title={pending?.label ?? ''}
        message={pending?.note ?? ''}
        busy={busy}
        onCancel={() => setPending(null)}
        onConfirm={confirm}
      />
      <ConfirmDialog
        open={Boolean(validationMessage)}
        title={validationTitle}
        subtitle={
          validationTitle === '已有进程在运行'
            ? '同一机器人目录同时只能以一种方式运行。'
            : '请先填写连接包声明的必填字段。'
        }
        message={validationMessage}
        confirmLabel="知道了"
        cancelLabel="关闭"
        onCancel={() => setValidationMessage('')}
        onConfirm={() => setValidationMessage('')}
      />
      {loginChoice &&
        createPortal(
          <div
            className="fixed inset-0 z-[96] flex items-center justify-center bg-slate-950/25 p-6"
            role="presentation"
          >
            <section
              className="grid max-h-[min(720px,calc(100vh-48px))] w-full max-w-2xl grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_20px_58px_rgb(28_26_23/0.22)]"
              role="dialog"
              aria-modal="true"
              aria-label={loginChoice.label}
            >
              <header className="flex items-center justify-between border-b border-slate-200 px-5 py-4">
                <div>
                  <strong className="text-sm text-ink-950">
                    {loginChoice.label}
                  </strong>
                  <p className="mt-1 text-xs text-slate-500">
                    {loginChoice.preflight.login
                      ? `将使用 ${loginChoice.preflight.login} 登录连接启动。`
                      : '尚未配置 login；可在这里完成连接配置后启动。'}
                  </p>
                </div>
                <button
                  className="icon-button"
                  onClick={closeLoginDialog}
                  aria-label="关闭"
                >
                  <X />
                </button>
              </header>
              <div className="grid min-h-0 gap-4 overflow-auto p-5">
                {loginDialogError && (
                  <p className="m-0 rounded-md border border-orange-200 bg-orange-50 px-3 py-2 text-xs leading-5 text-orange-800">
                    {loginDialogError}
                  </p>
                )}
                <section className="rounded-lg border border-slate-200">
                  <header className="border-b border-slate-200 bg-slate-50 px-3 py-2">
                    <strong className="text-xs text-slate-700">
                      选择登录平台
                    </strong>
                  </header>
                  <div className="grid gap-3 p-3 sm:grid-cols-3">
                    <label className="grid gap-1 text-xs font-semibold text-slate-600">
                      已识别平台
                      <select
                        value={selectedPlatform}
                        onChange={event => choosePlatform(event.target.value)}
                      >
                        <option value="">不选择，直接输入</option>
                        {(overview?.platforms ?? []).map(item => (
                          <option key={item.id} value={item.id}>
                            {item.label}
                            {item.installed ? ' · 已安装' : ' · 需安装'}
                          </option>
                        ))}
                      </select>
                    </label>
                    <label className="grid gap-1 text-xs font-semibold text-slate-600">
                      登录连接
                      <input
                        value={customLogin}
                        onChange={event => {
                          setSelectedPlatform('')
                          setCustomLogin(event.target.value)
                        }}
                        placeholder="如 onebot"
                      />
                    </label>
                    <label className="grid gap-1 text-xs font-semibold text-slate-600">
                      连接包（可选）
                      <input
                        value={customPackage}
                        onChange={event => {
                          setSelectedPlatform('')
                          setCustomPackage(event.target.value)
                          setConnectionConfig(null)
                        }}
                        placeholder="如 @alemonjs/onebot"
                      />
                    </label>
                  </div>
                  {packageTarget &&
                    (!knownPlatform || !knownPlatform.installed) && (
                      <footer className="flex items-center justify-between border-t border-slate-200 bg-slate-50 px-3 py-2">
                        <small className="text-xs text-slate-500">
                          {packageTarget} 尚未安装；安装后才能读取它的连接配置。
                        </small>
                        <button
                          className="secondary-button"
                          disabled={loginDialogBusy || busy}
                          onClick={() => void installSelectedConnection()}
                        >
                          安装连接包
                        </button>
                      </footer>
                    )}
                </section>
                {connectionConfig?.fields.length ? (
                  <section className="rounded-lg border border-slate-200">
                    <header className="border-b border-slate-200 bg-slate-50 px-3 py-2">
                      <strong className="text-xs text-slate-700">
                        连接配置
                      </strong>
                      <small className="ml-2 text-[11px] text-slate-400">
                        保存到 alemon.config.yaml
                      </small>
                    </header>
                    <div className="grid gap-3 p-3 sm:grid-cols-2">
                      {connectionConfig.fields.map(field => (
                        <label
                          key={field.name}
                          className="grid gap-1 text-xs font-semibold text-slate-600"
                        >
                          {field.description || field.name}
                          {field.required && (
                            <em className="not-italic text-orange-700">必填</em>
                          )}
                          {field.type === 'boolean' || field.type === 'bool' ? (
                            <select
                              value={connectionValues[field.name] ?? ''}
                              onChange={event =>
                                setConnectionValues({
                                  ...connectionValues,
                                  [field.name]: event.target.value
                                })
                              }
                            >
                              <option value="">不设置</option>
                              <option value="true">开启</option>
                              <option value="false">关闭</option>
                            </select>
                          ) : (
                            <input
                              type={
                                field.type === 'number' ||
                                field.type === 'integer'
                                  ? 'number'
                                  : 'text'
                              }
                              value={connectionValues[field.name] ?? ''}
                              onChange={event =>
                                setConnectionValues({
                                  ...connectionValues,
                                  [field.name]: event.target.value
                                })
                              }
                              placeholder={field.name}
                            />
                          )}
                        </label>
                      ))}
                    </div>
                  </section>
                ) : packageTarget && knownPlatform?.installed ? (
                  <p className="m-0 text-xs text-slate-500">
                    该连接包没有声明可填写的 alemonjs.config，保存 login
                    后即可启动。
                  </p>
                ) : null}
              </div>
              <footer className="flex flex-wrap items-center justify-end gap-2 border-t border-slate-200 px-5 py-3">
                {(() => {
                  // Whether the user actively chose a login (picked a platform
                  // or typed one). The configured login is ignored here so the
                  // button stays enabled when the user makes no choice.
                  const userLogin = Boolean(
                    customLogin.trim() || selectedPlatform
                  )
                  // Missing required fields only block start when a login is
                  // chosen; without a login the robot starts directly.
                  const missing = (connectionConfig?.fields ?? [])
                    .filter(
                      field =>
                        field.required && !connectionValues[field.name]?.trim()
                    )
                    .map(field => field.description || field.name)
                  const blocked = userLogin && missing.length > 0
                  return (
                    <button
                      className="primary-button"
                      disabled={loginDialogBusy || busy || blocked}
                      title={
                        blocked
                          ? `请先填写必填项：${missing.join('、')}`
                          : userLogin
                            ? '会先保存当前连接配置，再启动机器人。'
                            : '无 login 启动机器人。'
                      }
                      onClick={() => void startFromDialog()}
                    >
                      {loginDialogBusy || busy ? '启动中…' : '启动'}
                    </button>
                  )
                })()}
              </footer>
            </section>
          </div>,
          document.body
        )}
      <section className="grid gap-3">
        <section className="overflow-hidden rounded-xl border border-slate-200 bg-white">
          <div className="divide-y divide-slate-200">
            <section className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
              <div>
                <strong className="block text-sm font-semibold text-slate-700">
                  依赖
                  <StatusDot
                    active={overview?.dependenciesComplete}
                    label={overview?.dependenciesComplete ? '已安装' : '未安装'}
                  />
                </strong>
                <span className="block text-xs text-slate-500">
                  {overview?.dependenciesComplete
                    ? '依赖完整；可只升级 AlemonJS 相关依赖，或重新安装全部依赖。'
                    : '依赖未安装或缺失，请先安装后再运行。'}
                </span>
              </div>
              <div className="ml-auto flex shrink-0 flex-wrap justify-end gap-2">
                <button
                  className="secondary-button"
                  disabled={busy || !overview?.dependenciesComplete}
                  title={
                    !overview?.dependenciesComplete ? '请先安装依赖。' : ''
                  }
                  onClick={() =>
                    ask(
                      '升级 AlemonJS',
                      '会升级 package.json 中直接声明的 alemonjs 和 @alemonjs/ 相关依赖到最新稳定版，并更新锁文件；不会升级其他业务依赖。',
                      () => onRun('upgrade-alemon')
                    )
                  }
                >
                  一键升级
                </button>
                <button
                  className={
                    overview?.dependenciesComplete
                      ? 'secondary-button'
                      : 'danger-button'
                  }
                  disabled={busy}
                  onClick={() =>
                    ask(
                      overview?.dependenciesComplete
                        ? '重新安装依赖'
                        : '安装依赖',
                      overview?.dependenciesComplete
                        ? '会根据 package.json 重新安装当前机器人的全部依赖。'
                        : '会安装 package.json 声明的全部依赖。',
                      () => onRun('install')
                    )
                  }
                >
                  {overview?.dependenciesComplete ? '重新安装' : '安装'}
                </button>
              </div>
            </section>
            <section className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
              <div>
                <strong className="flex items-center gap-2 text-sm font-semibold text-slate-700">
                  前台运行
                  <StatusDot
                    active={foregroundRunning}
                    stopping={foregroundStopping}
                  />
                </strong>
                <span className="block text-xs text-slate-500">
                  {overview?.hasAppScript
                    ? foregroundStopping
                      ? '正在停止…'
                      : foregroundRunning
                        ? '正在运行，可随时停止。'
                        : developmentRunning
                          ? '当前正在开发运行，请先停止开发进程。'
                          : '直接启动机器人，方便查看输出。'
                    : '还没有前台运行命令。'}
                </span>
              </div>
              <div className="ml-auto flex gap-2 shrink-0 justify-end">
                <button className="secondary-button" onClick={onOpenConsole}>
                  运行终端
                </button>
                {overview?.hasAppScript ? (
                  <button
                    className={
                      foregroundRunning || foregroundStopping
                        ? 'secondary-button'
                        : 'primary-button'
                    }
                    disabled={
                      busy ||
                      depsMissing ||
                      developmentRunning ||
                      foregroundStopping ||
                      pm2LocalRunning
                    }
                    title={
                      depsMissing
                        ? '请先安装依赖。'
                        : pm2LocalRunning
                          ? '当前目录正在后台运行，请先停止服务。'
                          : developmentRunning
                            ? '当前目录正在开发运行，请先停止。'
                            : ''
                    }
                    onClick={() =>
                      foregroundRunning
                        ? ask(
                            '停止前台运行',
                            '会停止当前项目的前台运行。',
                            () => onRun('app-stop')
                          )
                        : void askStart(
                            'app',
                            '启动前台',
                            '会直接启动机器人，并打开运行日志。'
                          )
                    }
                  >
                    {foregroundStopping
                      ? '正在停止…'
                      : foregroundRunning
                        ? '停止运行'
                        : '启动前台'}
                  </button>
                ) : developerMode ? (
                  <button
                    className="secondary-button"
                    disabled={busy}
                    onClick={() =>
                      ask('修复前台运行', '会补齐前台运行所需的命令。', () =>
                        onRun('repair-app')
                      )
                    }
                  >
                    修复
                  </button>
                ) : (
                  <small>还没有可直接运行的命令。</small>
                )}
              </div>
            </section>
          </div>
        </section>
        {developerMode && (
          <section className="overflow-hidden rounded-xl border border-slate-200 bg-white">
            <header className="flex items-center justify-between gap-3 border-b border-slate-200 px-4 py-3">
              <div className="grid gap-1">
                <strong className="flex items-center gap-2 text-sm font-semibold text-slate-800">
                  开发运行
                  <StatusDot
                    active={developmentRunning}
                    stopping={developmentStopping}
                  />
                </strong>
                <span className="block text-xs text-slate-500">
                  {developmentStopping
                    ? '正在停止…'
                    : developmentRunning
                      ? '正在运行，可随时停止。'
                      : foregroundRunning
                        ? '当前正在前台运行，请先停止前台进程。'
                        : pm2LocalRunning
                          ? '当前正在后台运行，请先停止后台服务。'
                          : overview?.hasDevScript
                            ? '适合改代码、排查问题。'
                            : '还没有开发命令。'}
                </span>
              </div>
              <div className="flex shrink-0 flex-wrap justify-end gap-2">
                {overview?.hasBuildScript && (
                  <button
                    className="secondary-button"
                    disabled={busy || depsMissing}
                    title={depsMissing ? '请先安装依赖。' : ''}
                    onClick={() =>
                      ask(
                        '构建脚本',
                        '会运行 build 脚本生成构建产物；后台运行需要先构建才能识别本地应用。',
                        () => onRun('build')
                      )
                    }
                  >
                    构建脚本
                  </button>
                )}
                {overview?.hasDevScript ? (
                  <button
                    className={
                      developmentRunning || developmentStopping
                        ? 'secondary-button'
                        : 'primary-button'
                    }
                    disabled={
                      busy ||
                      depsMissing ||
                      foregroundRunning ||
                      developmentStopping ||
                      pm2LocalRunning
                    }
                    title={
                      depsMissing
                        ? '请先安装依赖。'
                        : pm2LocalRunning
                          ? '当前目录正在后台运行，请先停止服务。'
                          : foregroundRunning
                            ? '当前目录正在前台运行，请先停止。'
                            : ''
                    }
                    onClick={() =>
                      developmentRunning
                        ? ask('停止开发', '会停止当前项目的开发运行。', () =>
                            onRun('dev-stop')
                          )
                        : void askStart(
                            'dev',
                            '启动开发',
                            '会以开发模式启动，并打开运行日志。'
                          )
                    }
                  >
                    {developmentStopping
                      ? '正在停止…'
                      : developmentRunning
                        ? '停止开发'
                        : '启动开发'}
                  </button>
                ) : (
                  <button
                    className="secondary-button"
                    disabled={busy}
                    onClick={() =>
                      ask(
                        '修复开发命令',
                        '会补齐开发所需的运行命令，并保留现有设置。',
                        () => onRun('repair-dev')
                      )
                    }
                  >
                    修复
                  </button>
                )}
              </div>
            </header>
          </section>
        )}
        <section className="overflow-hidden rounded-xl border border-slate-200 bg-white">
          <header className="flex items-center justify-between gap-3 border-b border-slate-200 px-4 py-3">
            <div className="grid gap-1">
              <strong className="flex items-center gap-2 text-sm font-semibold text-slate-800">
                后台运行
                {persistentReady && (
                  <StatusDot
                    active={pm2Running}
                    label={
                      pm2Running
                        ? '运行中'
                        : pm2Managed
                          ? `已注册 · ${pm2Status?.status || '未知'}`
                          : '未启动'
                    }
                  />
                )}
              </strong>
              <span className="text-xs text-slate-500">
                {persistentReady
                  ? pm2Status
                    ? pm2Running
                      ? '服务运行中；关闭本窗口后仍会继续运行。'
                      : pm2Managed
                        ? '服务已注册，当前未运行；可重启或删除。'
                        : '服务尚未启动。'
                    : pm2StatusError
                      ? '无法读取服务状态；仍可尝试启动服务。'
                      : '正在读取服务状态。'
                  : '还未准备好，修复后可长期在线。'}
              </span>
            </div>
            <button
              className="primary-button"
              disabled={busy || depsMissing || !persistentReady || localRunning}
              title={
                depsMissing
                  ? '请先安装依赖。'
                  : localRunning
                    ? '当前目录正在本机运行，请先停止本机进程。'
                    : !persistentReady
                      ? '补齐 start 脚本和 PM2 配置后可使用。'
                      : ''
              }
              onClick={() =>
                void askStart(
                  pm2Running ? 'pm2-reload' : 'pm2',
                  pm2Running ? '应用服务设置' : '启动服务',
                  pm2Running
                    ? '会尽量不中断服务地应用最新设置。'
                    : '会在后台启动机器人。'
                )
              }
            >
              {pm2Running ? '应用设置' : '启动服务'}
            </button>
          </header>
          <div className="flex flex-wrap items-center justify-end gap-2 px-4 py-3">
            {/* PM2 status detection can be unreliable (daemon/version mismatch,
                sandboxed reads), so these actions stay clickable regardless of
                the detected state. The backend reports errors per action. */}
            <button
              className="secondary-button"
              disabled={busy}
              onClick={() =>
                ask('停止服务', '会停止当前项目在后台运行的机器人。', () =>
                  onRun('pm2-stop')
                )
              }
            >
              停止服务
            </button>
            <button
              className="secondary-button"
              disabled={busy}
              onClick={() =>
                ask('重启服务', '会停止并重新启动后台运行的机器人。', () =>
                  onRun('pm2-restart')
                )
              }
            >
              重启
            </button>
            <button
              className="secondary-button"
              disabled={busy}
              onClick={() =>
                ask('更新服务', '会尽量不中断服务地应用最新设置。', () =>
                  onRun('pm2-reload')
                )
              }
            >
              重载
            </button>
            {!persistentReady && (
              <button
                className="secondary-button"
                disabled={busy}
                onClick={() =>
                  ask('修复后台运行', '会补齐后台运行所需的设置和依赖。', () =>
                    onRun('repair-pm2')
                  )
                }
              >
                修复
              </button>
            )}
            <div className="runtime-persistent-utilities">
              <button
                className="text-button"
                disabled={busy}
                onClick={() => setPM2ProcessesOpen(true)}
              >
                状态
              </button>
              <button
                className="text-button"
                disabled={busy}
                onClick={() => setPM2LogsOpen(true)}
              >
                日志
              </button>
              <button
                className="text-button danger-action"
                disabled={busy}
                onClick={() =>
                  ask(
                    '移除后台服务',
                    '会移除后台运行记录；以后仍可再次启动。',
                    () => onRun('pm2-delete')
                  )
                }
              >
                删除
              </button>
            </div>
          </div>
        </section>
      </section>
      <PM2LogsPanel
        open={pm2LogsOpen}
        root={root}
        onClose={() => setPM2LogsOpen(false)}
      />
      <PM2ProcessesPanel
        open={pm2ProcessesOpen}
        root={root}
        onClose={() => setPM2ProcessesOpen(false)}
      />
    </section>
  )
}
function RobotPluginWebView({
  root,
  entries,
  tabs,
  activeTabKey,
  onActivate,
  onClose
}: {
  root: string
  entries: RobotWebView[]
  tabs: Array<{ key: string; entryID: string; title: string; package: string }>
  activeTabKey: string
  onActivate: (key: string) => void
  onClose: (key: string) => void
}) {
  const active = tabs.find(tab => tab.key === activeTabKey)
  return (
    <section className="workspace-content robot-plugin-webview grid min-h-0 grid-rows-[auto_minmax(0,1fr)]">
      <header className="flex min-h-12 items-center justify-between gap-3 border-b border-slate-200 px-4">
        <div className="flex min-w-0 items-center gap-1 overflow-auto">
          {tabs.map(tab => (
            <button
              className={cn(
                'flex shrink-0 items-center gap-2 rounded-md px-2.5 py-1.5 text-xs font-semibold transition',
                tab.key === activeTabKey
                  ? 'bg-brand-50 text-brand-700'
                  : 'text-slate-500 hover:bg-slate-100'
              )}
              key={tab.key}
              onClick={() => onActivate(tab.key)}
              title={tab.package}
            >
              {tab.title}
              <span
                className="text-slate-400 hover:text-slate-800"
                onClick={event => {
                  event.stopPropagation()
                  onClose(tab.key)
                }}
              >
                ×
              </span>
            </button>
          ))}
        </div>
        <div className="flex min-w-0 items-center gap-2">
          <strong className="truncate text-xs font-semibold text-slate-700">
            {active?.title}
          </strong>
        </div>
      </header>
      <div className="robot-plugin-webview-frame relative min-h-0 overflow-hidden">
        {tabs.map(tab => {
          const entry = entries.find(item => item.id === tab.entryID)
          return entry ? (
            <PluginWebViewFrame
              key={tab.key}
              root={root}
              entry={entry}
              active={tab.key === activeTabKey}
            />
          ) : null
        })}
      </div>
    </section>
  )
}

function PluginWebViewFrame({
  root,
  entry,
  active
}: {
  root: string
  entry: RobotWebView
  active: boolean
}) {
  const [reloadKey, setReloadKey] = useState(0)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [apiError, setApiError] = useState('')
  const frameRef = useRef<HTMLIFrameElement>(null)
  const loadedRef = useRef(false)
  const apiErrorRef = useRef('')
  const rootToken = btoa(String.fromCharCode(...new TextEncoder().encode(root)))
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=/g, '')
  const pluginHost = `r-${rootToken.slice(0, 20).toLowerCase()}.localhost`
  const source = `${window.location.protocol}//${pluginHost}${window.location.port ? `:${window.location.port}` : ''}/api/v1/robot/webview/${rootToken}/${entry.id}/`
  useEffect(() => {
    const origin = new URL(source).origin
    const forward = (event: MessageEvent) => {
      if (
        event.origin !== origin ||
        event.source !== frameRef.current?.contentWindow
      )
        return
      const message = event.data as {
        source?: string
        type?: string
        value?: { status?: number; message?: string }
      }
      if (message?.source !== 'alx-webview') return
      if (message.type === 'ready') {
        frameRef.current?.contentWindow?.postMessage(
          {
            source: 'alx-parent',
            value: {
              type: 'theme',
              data: document.documentElement.dataset.theme ?? 'light'
            }
          },
          origin
        )
        return
      }
      if (message.type === 'api-error') {
        const status = message.value?.status
        const next =
          message.value?.message ||
          (status === 502 || status === 503
            ? '机器人 API 未连接：请在“运行”中启动机器人后重试。'
            : `插件接口请求失败${status ? `（${status}）` : ''}。`)
        if (apiErrorRef.current !== next) {
          apiErrorRef.current = next
          setApiError(next)
        }
      }
    }
    window.addEventListener('message', forward)
    return () => window.removeEventListener('message', forward)
  }, [source, reloadKey])
  useEffect(() => {
    loadedRef.current = false
    apiErrorRef.current = ''
    setLoading(true)
    setLoadError('')
    setApiError('')
    const timer = window.setTimeout(() => {
      if (!loadedRef.current) {
        setLoading(false)
        setLoadError(
          '页面加载超时。请确认插件正在正常安装，并检查插件的 Web 页面是否完整。'
        )
      }
    }, 15_000)
    return () => window.clearTimeout(timer)
  }, [source, reloadKey])
  const reload = () => {
    setReloadKey(value => value + 1)
  }
  return (
    <div
      className={cn(
        'robot-plugin-webview-instance absolute inset-0',
        active ? 'active block' : 'hidden'
      )}
    >
      {loading && active && (
        <span className="absolute left-1/2 top-1/2 z-10 -translate-x-1/2 -translate-y-1/2 rounded-lg border border-slate-200 bg-white px-3 py-2 text-xs text-slate-500 shadow-sm">
          正在加载 {entry.name}…
        </span>
      )}
      {apiError && active && (
        <div
          className="absolute left-3 right-3 top-3 z-20 flex items-center justify-between gap-3 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-800"
          role="status"
        >
          <span>{apiError}</span>
          <button
            className="icon-button size-7 p-0"
            onClick={() => {
              apiErrorRef.current = ''
              setApiError('')
            }}
            aria-label="关闭接口错误提示"
            title="关闭"
          >
            <X className="size-3.5" />
          </button>
        </div>
      )}
      {loadError && active && (
        <div className="absolute left-1/2 top-1/2 z-10 grid -translate-x-1/2 -translate-y-1/2 gap-2 rounded-xl border border-slate-200 bg-white p-5 text-center shadow-lg">
          <strong className="text-sm font-semibold text-slate-800">
            无法打开插件页面
          </strong>
          <p className="max-w-sm text-xs leading-5 text-slate-500">
            {loadError}
          </p>
          <button
            className="secondary-button justify-self-center"
            onClick={reload}
          >
            <RefreshCw className="mr-1.5 size-3.5" />
            重新加载
          </button>
        </div>
      )}
      <button
        className="icon-button absolute right-3 top-3 z-10 size-8 p-0"
        onClick={reload}
        aria-label="重新加载插件页面"
        title="重新加载"
      >
        <RefreshCw className="size-4" />
      </button>
      <iframe
        className="size-full border-0 bg-white"
        ref={frameRef}
        key={reloadKey}
        src={source}
        title={`${entry.name} 插件页面`}
        sandbox="allow-scripts allow-same-origin allow-forms allow-popups allow-downloads"
        referrerPolicy="no-referrer"
        onLoad={() => {
          loadedRef.current = true
          setLoading(false)
          setLoadError('')
        }}
        onError={() => {
          loadedRef.current = true
          setLoading(false)
          setLoadError(
            '浏览器无法载入此插件页面。请重新加载，或确认插件的 dist 文件已完整安装。'
          )
        }}
      />
    </div>
  )
}

function ControlCard({
  page,
  section,
  project,
  buildMode,
  catalog,
  catalogTitle,
  webViews,
  activeWebViewID,
  developerMode,
  onOpenConsole,
  onOpenAI,
  onOpenWebView,
  onPage,
  onSection,
  onBuildMode,
  onCatalog,
  onGit
}: {
  page: Page
  section: Section
  project?: Project
  buildMode: 'manifest' | 'npm' | 'git'
  catalog: CatalogGroup[]
  catalogTitle: string
  webViews: RobotWebView[]
  activeWebViewID: string
  developerMode: boolean
  onOpenConsole: () => void
  onOpenAI: () => void
  onOpenWebView: (id: string) => void
  onPage: (page: Page) => void
  onSection: (section: Section) => void
  onBuildMode: (mode: 'manifest' | 'npm' | 'git') => void
  onCatalog: (title: string) => void
  onGit: () => void
}) {
  const gitRoot = project?.path ?? ''
  const gitOverviewBranchesArgs = useMemo(
    () => ({ root: gitRoot, view: 'branch' as const }),
    [gitRoot]
  )
  const gitOverviewChangesArgs = useMemo(
    () => ({ root: gitRoot, view: 'commit' as const }),
    [gitRoot]
  )
  const { data: gitBranches } = useGitWorkspaceQuery(gitOverviewBranchesArgs, {
    skip: !gitRoot || page !== 'robot' || section !== 'runtime'
  })
  const { data: gitChanges } = useGitWorkspaceQuery(gitOverviewChangesArgs, {
    skip: !gitRoot || page !== 'robot' || section !== 'runtime'
  })
  const gitOverview =
    gitBranches && gitChanges
      ? { ...gitBranches, changes: gitChanges.changes }
      : (gitBranches ?? gitChanges ?? undefined)
  const activePrimary =
    page === 'robot'
      ? section === 'backpack'
        ? 'backpack'
        : section === 'runtime'
          ? 'runtime'
          : 'config'
      : page
  const subitems =
    activePrimary === 'config'
      ? developerMode
        ? [
            { id: 'npmrc', label: 'npm 源' },
            { id: 'env', label: '环境变量' }
          ]
        : []
      : activePrimary === 'build'
        ? [
            { id: 'manifest', label: '包配置' },
            { id: 'git', label: 'GIT 发布' },
            { id: 'npm', label: 'NPM 发布' }
          ]
        : activePrimary === 'backpack' || activePrimary === 'runtime'
          ? []
          : catalog.map(item => ({ id: item.title, label: item.title }))
  const activeSecondary =
    activePrimary === 'config'
      ? section
      : activePrimary === 'build'
        ? buildMode
        : catalogTitle
  function selectPrimary(item: (typeof directoryActions)[number]) {
    if (item.kind === 'section') {
      onPage('robot')
      onSection(item.id as Section)
      return
    }
    onPage(item.id as Page)
  }
  function selectSecondary(id: string) {
    if (activePrimary === 'config') {
      onSection(id as Section)
      return
    }
    if (activePrimary === 'build') {
      onBuildMode(id as 'manifest' | 'npm' | 'git')
      return
    }
    onCatalog(id)
  }
  return (
    <aside
      className="control-dock flex min-h-0 flex-col gap-3"
      aria-label="目录操作"
    >
      <section className="control-card overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm">
        <header className="flex items-center justify-between gap-3 border-b border-slate-200 px-3.5 py-3">
          <div className="grid min-w-0 gap-1">
            <span className="text-[11px] font-medium text-slate-400">
              当前机器人
            </span>
            <strong className="truncate text-sm font-semibold text-slate-800">
              {project?.name ?? '未选择目录'}
            </strong>
          </div>
          <button
            className="inline-flex size-8 shrink-0 items-center justify-center rounded-md border border-brand-200 bg-brand-50 text-brand-700 transition hover:bg-brand-100"
            onClick={onGit}
            aria-label={`管理 ${project?.name ?? '当前机器人'} 的 Git`}
            title="Git 管理"
          >
            <GitBranch className="size-4" />
          </button>
        </header>
        {project && gitOverview && (
          <div className="grid gap-1 border-b border-slate-100 px-3.5 py-2.5">
            {gitOverview.repository ? (
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px]">
                <span className="inline-flex items-center gap-1 font-medium text-slate-700">
                  <GitBranch className="size-3 text-slate-400" />
                  {gitOverview.branch || '未知分支'}
                </span>
                <span
                  className={
                    gitOverview.changes.length
                      ? 'inline-flex items-center gap-1 font-medium text-amber-700'
                      : 'inline-flex items-center gap-1 font-medium text-emerald-600'
                  }
                  title={
                    gitOverview.changes.length
                      ? `${gitOverview.changes.length} 个文件有未提交改动`
                      : '工作区干净'
                  }
                >
                  {gitOverview.changes.length
                    ? `${gitOverview.changes.length} 项未提交`
                    : '已提交'}
                </span>
                {gitOverview.remotes.length > 0 && (
                  <span
                    className="inline-flex items-center gap-1 text-slate-400"
                    title={
                      gitOverview.upstream
                        ? `领先 ${gitOverview.ahead} · 落后 ${gitOverview.behind}`
                        : '分支尚未关联远程'
                    }
                  >
                    <Network className="size-3" />
                    {gitOverview.upstream
                      ? `领先 ${gitOverview.ahead} · 落后 ${gitOverview.behind}`
                      : '未关联远程'}
                  </span>
                )}
              </div>
            ) : (
              <div className="flex items-center justify-between gap-2 text-[11px] text-slate-500">
                <span>尚未初始化 Git</span>
                <button
                  className="font-medium text-brand-600 hover:underline"
                  onClick={onGit}
                >
                  初始化
                </button>
              </div>
            )}
          </div>
        )}
        <div className="grid gap-1 p-2">
          {directoryActions
            .filter(item => developerMode || item.id !== 'build')
            .map(item => (
              <button
                className={cn(
                  'flex min-h-9 items-center gap-2 rounded-md px-2.5 text-left text-sm font-semibold transition',
                  activePrimary === item.id
                    ? 'bg-brand-50 text-brand-700'
                    : 'text-slate-600 hover:bg-slate-50 hover:text-slate-900'
                )}
                onClick={() => selectPrimary(item)}
                key={item.id}
              >
                <i className="inline-flex size-4 items-center justify-center not-italic">
                  {item.icon}
                </i>
                <span className="min-w-0 flex-1">{item.label}</span>
                <ChevronRight className="size-4 text-slate-400" />
              </button>
            ))}
        </div>
        {subitems.length > 0 && (
          <>
            <span className="mx-2 block border-t border-slate-200" />
            <div className="grid gap-1 p-2 pt-1">
              {subitems.map(item => (
                <button
                  className={cn(
                    'flex min-h-8 items-center justify-between rounded-md px-2.5 text-xs font-semibold transition',
                    activeSecondary === item.id
                      ? 'bg-brand-50 text-brand-700'
                      : 'text-slate-500 hover:bg-slate-50 hover:text-slate-800'
                  )}
                  onClick={() => selectSecondary(item.id)}
                  key={item.id}
                >
                  {item.label}
                  <ChevronRight className="size-3.5 text-slate-400" />
                </button>
              ))}
            </div>
          </>
        )}
        {project && (
          <footer
            className="flex gap-2 border-t border-slate-200 px-3 py-2"
            title={project.path}
          >
            <button
              className="icon-button size-8 p-0"
              onClick={onOpenConsole}
              aria-label="查看运行终端"
              title="查看运行终端"
            >
              <Terminal className="size-4" />
            </button>
            <button
              className="icon-button size-8 p-0"
              onClick={onOpenAI}
              aria-label="打开 Agent"
              title="打开 Agent"
            >
              <Bot className="size-4" />
            </button>
          </footer>
        )}
      </section>
      {webViews.length > 0 && (
        <section className="grid gap-2" aria-label="机器人插件 Web 页面">
          {webViews.map(item => (
            <button
              className={cn(
                'flex min-h-10 items-center gap-2 rounded-lg border px-3 text-left text-xs font-semibold transition',
                item.id === activeWebViewID
                  ? 'border-brand-200 bg-brand-50 text-brand-700'
                  : 'border-slate-200 bg-white text-slate-600 hover:bg-slate-50'
              )}
              key={item.id}
              onClick={() => onOpenWebView(item.id)}
              title={item.description || item.package}
            >
              <Package className="size-4" />
              <span className="min-w-0 flex-1 truncate">{item.name}</span>
              <ChevronRight className="size-4 text-slate-400" />
            </button>
          ))}
        </section>
      )}
    </aside>
  )
}
// StatusDot is a small state indicator for a running process. active means
// running (pulsing green dot), stopping means a graceful shutdown is in
// progress (amber), and muted with a label means "registered but not running".
function StatusDot({
  active,
  stopping,
  label
}: {
  active?: boolean
  stopping?: boolean
  label?: string
}) {
  if (!active && !stopping && !label) return null
  const tone = active
    ? 'bg-emerald-500'
    : stopping
      ? 'bg-amber-500'
      : 'bg-slate-300 dark:bg-slate-600'
  return (
    <span className="inline-flex items-center gap-1.5">
      <i
        className={cn(
          'inline-block size-2 rounded-full',
          tone,
          active && 'animate-pulse'
        )}
        aria-hidden="true"
      />
      {label && (
        <span className="text-[11px] font-medium text-slate-500 dark:text-slate-400">
          {label}
        </span>
      )}
    </span>
  )
}
function ReadonlyConsole({
  open,
  root,
  onClose
}: {
  open: boolean
  root: string
  onClose: () => void
}) {
  const [load, { data, error, isFetching }] = useLazyRobotConsoleQuery()
  const outputRef = useRef<HTMLPreElement>(null)
  const followLatest = useRef(true)
  const [showSnapshot, setShowSnapshot] = useState(false)
  const runError = error
    ? operationErrorMessage(error, '无法读取当前目录的运行终端信息。')
    : ''
  const running = Boolean(data?.running)
  const message = runError || data?.output || ''
  // The live output changes frequently; the static project snapshot (pwd,
  // scripts, git status, node version) barely changes. Poll fast only while a
  // process is running, and reuse the server-side snapshot cache otherwise.
  useEffect(() => {
    if (!open || !root) return
    void load({ root })
    const timer = window.setInterval(
      () => {
        void load({ root })
      },
      running ? 900 : 5000
    )
    return () => window.clearInterval(timer)
  }, [load, open, root, running])
  useEffect(() => {
    if (open) followLatest.current = true
  }, [open])
  useEffect(() => {
    if (!open || !followLatest.current) return
    const frame = window.requestAnimationFrame(() => {
      const output = outputRef.current
      if (output) output.scrollTop = output.scrollHeight
    })
    return () => window.cancelAnimationFrame(frame)
  }, [message, open])
  if (!open) return null
  return (
    <Modal
      open
      zIndex={40}
      className="readonly-console-backdrop bg-slate-950/35 backdrop-blur-sm"
      ariaLabel="运行终端"
    >
      <section
        className="readonly-console grid max-h-[min(720px,calc(100vh-32px))] w-[min(860px,100%)] grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-2xl dark:border-slate-700 dark:bg-slate-900"
        role="dialog"
        aria-modal="true"
        aria-label="运行终端"
      >
        <header className="flex items-center justify-between gap-3 border-b border-slate-200 px-4 py-3 dark:border-slate-700">
          <div className="flex min-w-0 items-center gap-2">
            <Terminal className="size-4 shrink-0 text-brand-600 dark:text-brand-200" />
            <strong className="text-sm font-semibold text-slate-900 dark:text-slate-100">
              运行终端
            </strong>
            <small className="hidden text-xs text-slate-400 sm:inline">
              {running
                ? `${data?.mode ?? '进程'}实时输出 · 不支持输入命令`
                : '查看最近运行输出 · 不支持输入命令'}
            </small>
          </div>
          <div className="flex items-center gap-1">
            <button
              className="inline-flex size-8 items-center justify-center rounded-lg border border-slate-200 text-slate-500 hover:bg-slate-50 dark:border-slate-600 dark:text-slate-300 dark:hover:bg-slate-800"
              disabled={isFetching}
              onClick={() => void load({ root, refresh: true })}
              aria-label="刷新运行终端"
              title="刷新"
            >
              <RefreshCw />
            </button>
            <button
              className="inline-flex size-8 items-center justify-center rounded-lg border border-slate-200 text-slate-500 hover:bg-slate-50 dark:border-slate-600 dark:text-slate-300 dark:hover:bg-slate-800"
              onClick={onClose}
              aria-label="关闭运行终端"
              title="关闭"
            >
              <X />
            </button>
          </div>
        </header>
        <div className="grid min-h-0 grid-rows-[minmax(0,1fr)_auto_auto]">
          <pre
            ref={outputRef}
            className="m-0 min-h-0 overflow-auto bg-slate-950 p-4 font-mono text-xs leading-5 text-emerald-200"
            onScroll={event => {
              const output = event.currentTarget
              followLatest.current =
                output.scrollHeight - output.scrollTop - output.clientHeight <
                24
            }}
          >
            {isFetching && !message ? '正在读取当前目录…' : message}
          </pre>
          {data?.snapshot && (
            <button
              className="flex items-center justify-center gap-1 border-t border-slate-700 bg-slate-900 px-3 py-1.5 text-[11px] font-semibold text-slate-400 transition hover:text-slate-200"
              onClick={() => setShowSnapshot(value => !value)}
              aria-expanded={showSnapshot}
            >
              {showSnapshot ? '收起' : '展开'}目录信息（版本 / 脚本 / Git）
              <ChevronDown
                className={cn(
                  'size-3.5 transition-transform',
                  showSnapshot && 'rotate-180'
                )}
              />
            </button>
          )}
          {showSnapshot && data?.snapshot && (
            <pre className="m-0 max-h-48 overflow-auto border-t border-slate-700 bg-slate-900 px-4 py-3 font-mono text-[11px] leading-5 text-slate-400">
              {data.snapshot}
            </pre>
          )}
        </div>
      </section>
    </Modal>
  )
}

function PM2LogsPanel({
  open,
  root,
  onClose
}: {
  open: boolean
  root: string
  onClose: () => void
}) {
  const [page, setPage] = useState(1)
  const [data, setData] = useState<{
    output: string
    page: number
    hasOlder: boolean
  } | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const load = useCallback(
    async (targetPage: number) => {
      setLoading(true)
      try {
        const response = await fetch(
          `/api/v1/robot/pm2-logs?${new URLSearchParams({ root, page: String(targetPage) })}`
        )
        const result = (await response.json()) as {
          output?: string
          page?: number
          hasOlder?: boolean
          error?: string
        }
        if (!response.ok) throw new Error(result.error || '无法读取 PM2 日志。')
        setData({
          output: result.output ?? 'PM2 暂无可读取的日志。',
          page: result.page ?? targetPage,
          hasOlder: Boolean(result.hasOlder)
        })
        setError('')
      } catch (reason) {
        setError(operationErrorMessage(reason, '无法读取 PM2 日志。'))
      } finally {
        setLoading(false)
      }
    },
    [root]
  )
  useEffect(() => {
    if (open) setPage(1)
  }, [open])
  useEffect(() => {
    if (open && root) void load(page)
  }, [load, open, page, root])
  if (!open) return null
  return (
    <Modal
      open
      zIndex={40}
      className="readonly-console-backdrop bg-slate-950/35 backdrop-blur-sm"
      ariaLabel="PM2 日志"
    >
      <section
        className="readonly-console pm2-log-panel grid max-h-[min(720px,calc(100vh-32px))] w-[min(860px,100%)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-2xl dark:border-slate-700 dark:bg-slate-900"
        role="dialog"
        aria-modal="true"
        aria-label="PM2 日志"
      >
        <header className="flex items-center justify-between gap-3 border-b border-slate-200 px-4 py-3 dark:border-slate-700">
          <div className="flex min-w-0 items-center gap-2">
            <Terminal className="size-4 shrink-0 text-brand-600 dark:text-brand-200" />
            <strong className="text-sm font-semibold text-slate-900 dark:text-slate-100">
              PM2 运行日志
            </strong>
            <small className="hidden text-xs text-slate-400 sm:inline">
              默认显示最新一页；每页 120 行，只能查看。
            </small>
          </div>
          <div className="flex items-center gap-1">
            <button
              className="inline-flex size-8 items-center justify-center rounded-lg border border-slate-200 text-slate-500 hover:bg-slate-50 dark:border-slate-600 dark:text-slate-300 dark:hover:bg-slate-800"
              disabled={loading}
              onClick={() => void load(page)}
              aria-label="刷新 PM2 日志"
              title="刷新"
            >
              <RefreshCw className="size-4" />
            </button>
            <button
              className="inline-flex size-8 items-center justify-center rounded-lg border border-slate-200 text-slate-500 hover:bg-slate-50 dark:border-slate-600 dark:text-slate-300 dark:hover:bg-slate-800"
              onClick={onClose}
              aria-label="关闭 PM2 日志"
              title="关闭"
            >
              <X className="size-4" />
            </button>
          </div>
        </header>
        <pre className="m-0 min-h-0 overflow-auto bg-slate-950 p-4 font-mono text-xs leading-5 text-emerald-200">
          {loading && !data
            ? '正在读取最新 PM2 日志…'
            : error || data?.output || '暂无日志。'}
        </pre>
        <footer className="flex items-center justify-between gap-2 border-t border-slate-200 px-4 py-3 dark:border-slate-700">
          <button
            className="inline-flex min-h-8 items-center justify-center rounded-lg border border-slate-200 bg-white px-3 text-xs font-semibold text-slate-600 disabled:opacity-40 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-300"
            disabled={loading || page <= 1}
            onClick={() => setPage(current => current - 1)}
          >
            更新
          </button>
          <span className="text-xs text-slate-500">
            第 {data?.page ?? page} 页 · 每页 120 行
          </span>
          <button
            className="inline-flex min-h-8 items-center justify-center rounded-lg border border-slate-200 bg-white px-3 text-xs font-semibold text-slate-600 disabled:opacity-40 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-300"
            disabled={loading || !data?.hasOlder}
            onClick={() => setPage(current => current + 1)}
          >
            更早
          </button>
        </footer>
      </section>
    </Modal>
  )
}
function PM2ProcessesPanel({
  open,
  root,
  onClose
}: {
  open: boolean
  root: string
  onClose: () => void
}) {
  const { data, isFetching, isError, error, refetch } =
    useRobotPM2ProcessesQuery(root, {
      skip: !open || !root
    })
  const items = data?.items ?? []
  if (!open) return null
  const formatBytes = (value: number) => {
    if (!value) return '—'
    const units = ['B', 'KB', 'MB', 'GB']
    let size = value
    let index = 0
    while (size >= 1024 && index < units.length - 1) {
      size /= 1024
      index++
    }
    return `${size.toFixed(index === 0 ? 0 : 1)} ${units[index]}`
  }
  const formatUptime = (value: number) => {
    if (!value) return '—'
    const seconds = Math.floor((Date.now() - value) / 1000)
    if (seconds < 60) return `${seconds}s`
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
    if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`
    return `${Math.floor(seconds / 86400)}d`
  }
  const statusTone = (status: string) =>
    status === 'online'
      ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300'
      : status === 'launching'
        ? 'bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300'
        : 'bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-400'
  return (
    <Modal
      open
      zIndex={40}
      className="readonly-console-backdrop bg-slate-950/35 backdrop-blur-sm"
      ariaLabel="PM2 进程"
    >
      <section
        className="grid max-h-[min(720px,calc(100vh-32px))] w-[min(860px,100%)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-2xl dark:border-slate-700 dark:bg-slate-900"
        role="dialog"
        aria-modal="true"
        aria-label="PM2 进程"
      >
        <header className="flex items-center justify-between gap-3 border-b border-slate-200 px-4 py-3 dark:border-slate-700">
          <div className="flex min-w-0 items-center gap-2">
            <Terminal className="size-4 shrink-0 text-brand-600 dark:text-brand-200" />
            <strong className="text-sm font-semibold text-slate-900 dark:text-slate-100">
              PM2 进程
            </strong>
            <small className="hidden text-xs text-slate-400 sm:inline">
              当前机器人的 PM2 守护进程管理的全部服务
            </small>
          </div>
          <div className="flex items-center gap-1">
            <button
              className="inline-flex size-8 items-center justify-center rounded-lg border border-slate-200 text-slate-500 hover:bg-slate-50 dark:border-slate-600 dark:text-slate-300 dark:hover:bg-slate-800"
              disabled={isFetching}
              onClick={() => void refetch()}
              aria-label="刷新 PM2 进程"
              title="刷新"
            >
              <RefreshCw className="size-4" />
            </button>
            <button
              className="inline-flex size-8 items-center justify-center rounded-lg border border-slate-200 text-slate-500 hover:bg-slate-50 dark:border-slate-600 dark:text-slate-300 dark:hover:bg-slate-800"
              onClick={onClose}
              aria-label="关闭 PM2 进程"
              title="关闭"
            >
              <X className="size-4" />
            </button>
          </div>
        </header>
        {isError ? (
          <div className="grid min-h-0 place-items-center gap-3 p-8 text-center">
            <p className="text-xs text-slate-500">
              {operationErrorMessage(error, '无法读取 PM2 进程。')}
            </p>
            <button className="secondary-button" onClick={() => void refetch()}>
              重试
            </button>
          </div>
        ) : isFetching && !items.length ? (
          <div className="grid min-h-0 place-items-center p-8 text-xs text-slate-400">
            正在读取 PM2 进程…
          </div>
        ) : !items.length ? (
          <div className="grid min-h-0 place-items-center p-8 text-xs text-slate-400">
            当前没有正在运行的 PM2 进程。
          </div>
        ) : (
          <div className="min-h-0 overflow-auto">
            <table className="w-full border-collapse text-left text-xs">
              <thead className="sticky top-0 bg-slate-50 text-slate-400 dark:bg-slate-800 dark:text-slate-300">
                <tr>
                  <th className="px-3 py-2 font-medium">ID</th>
                  <th className="px-3 py-2 font-medium">名称</th>
                  <th className="px-3 py-2 font-medium">状态</th>
                  <th className="px-3 py-2 font-medium">PID</th>
                  <th className="px-3 py-2 font-medium">内存</th>
                  <th className="px-3 py-2 font-medium">CPU</th>
                  <th className="px-3 py-2 font-medium">重启</th>
                  <th className="px-3 py-2 font-medium">运行时长</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                {items.map(item => (
                  <tr
                    key={item.id}
                    className="text-slate-700 dark:text-slate-200"
                  >
                    <td className="px-3 py-2 font-mono text-slate-400">
                      {item.id}
                    </td>
                    <td className="px-3 py-2 font-semibold">{item.name}</td>
                    <td className="px-3 py-2">
                      <span
                        className={`inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-medium ${statusTone(item.status)}`}
                      >
                        <i className="inline-block size-1.5 rounded-full bg-current" />
                        {item.status}
                      </span>
                    </td>
                    <td className="px-3 py-2 font-mono">{item.pid || '—'}</td>
                    <td className="px-3 py-2 font-mono">
                      {formatBytes(item.memory)}
                    </td>
                    <td className="px-3 py-2 font-mono">
                      {item.cpu ? `${item.cpu.toFixed(1)}%` : '—'}
                    </td>
                    <td className="px-3 py-2 font-mono">{item.restarts}</td>
                    <td className="px-3 py-2 font-mono">
                      {formatUptime(item.uptime)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <footer className="flex items-center justify-between border-t border-slate-200 px-4 py-3 dark:border-slate-700">
          <span className="text-xs text-slate-500">
            共 {items.length} 个进程
          </span>
          <button
            className="secondary-button"
            disabled={isFetching}
            onClick={() => void refetch()}
          >
            刷新
          </button>
        </footer>
      </section>
    </Modal>
  )
}
function EditorMode({
  active,
  onVisual,
  onText
}: {
  active: 'visual' | 'text'
  onVisual: () => void
  onText: () => void
}) {
  return (
    <Tabs
      ariaLabel="配置编辑模式"
      value={active}
      onChange={value => (value === 'text' ? onText() : onVisual())}
      variant="segmented"
      items={[
        { id: 'visual', label: '表单' },
        { id: 'text', label: '文本' }
      ]}
    />
  )
}
function FileEditor({
  toolbar,
  content,
  busy,
  placeholder,
  onChange,
  onSave
}: {
  toolbar?: ReactNode
  content: string
  busy: boolean
  placeholder: string
  onChange: (value: string) => void
  onSave: () => void
}) {
  return (
    <section className="file-editor grid gap-3">
      <header className="flex items-center justify-between gap-3">
        {toolbar}
        <button className="primary-button" disabled={busy} onClick={onSave}>
          保存
        </button>
      </header>
      <textarea
        className="min-h-[420px] w-full resize-y rounded-lg border border-slate-300 bg-white p-3 font-mono text-xs leading-5 text-slate-800 outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
        value={content}
        onChange={event => onChange(event.target.value)}
        placeholder={placeholder}
      />
    </section>
  )
}
function OperationLog({
  output,
  failed,
  onClose
}: {
  output: string
  failed: boolean
  onClose: () => void
}) {
  const needsPermission =
    failed && /没有权限|访问权限|permission denied|eacces/i.test(output)
  return (
    <aside
      className={`robot-output ${failed ? 'failed' : 'completed'}`}
      aria-live="polite"
      aria-label="最近操作结果"
    >
      <header>
        <div>
          <i>{failed ? '!' : '✓'}</i>
          <strong>
            {needsPermission
              ? '需要访问授权'
              : failed
                ? '操作未完成'
                : '操作已完成'}
          </strong>
        </div>
        <button onClick={onClose} aria-label="关闭操作结果">
          ×
        </button>
      </header>
      <pre>{output}</pre>
      <small>
        {needsPermission
          ? '授权完成后，请回到这里重新执行本次操作。'
          : '完整记录可在右上角的任务按钮中查看。'}
      </small>
    </aside>
  )
}

function GitReleasePanelNext({
  root,
  busy,
  version,
  onVersionChange,
  onInitialize
}: {
  root: string
  busy: boolean
  version: string
  onVersionChange: (value: string) => void
  onInitialize: (values: {
    authorName: string
    authorEmail: string
    repository: string
    message: string
  }) => Promise<boolean>
}) {
  type SourceCommit = {
    sha: string
    shortSha: string
    subject: string
    createdAt: string
  }
  type GitStatus = {
    packageName?: string
    packageVersion?: string
    packageManager?: string
    repository?: string
    branch?: string
    remoteBranch?: string
    remoteReachable?: boolean
    remoteAdvice?: string
    suggestedVersion?: string
    sourceCommits?: SourceCommit[]
    sourceBranches?: Array<{ name: string; commits: SourceCommit[] }>
    gitReady?: boolean
    checks?: string[]
    issues?: string[]
  }
  type BuildSession = {
    sessionId: string
    branch: string
    commit: string
    target: string
    files: string[]
    logs: string
    expiresAt: string
  }
  type PublishResult = { output?: string; path?: string }
  const {
    data,
    isFetching: loading,
    error,
    refetch
  } = useGitStatusQuery(root, { skip: !root })
  const [initializing, setInitializing] = useState(false)
  const [gitInitOpen, setGitInitOpen] = useState(false)
  const [sourceCommit, setSourceCommit] = useState('')
  const [sourceBranch, setSourceBranch] = useState('')
  const [phase, setPhase] = useState<
    'source' | 'building' | 'artifacts' | 'confirm' | 'published'
  >('source')
  const [session, setSession] = useState<BuildSession | null>(null)
  const [artifacts, setArtifacts] = useState<string[]>([])
  const [expandedArtifacts, setExpandedArtifacts] = useState<string[]>([])
  const [requestError, setRequestError] = useState('')
  const [result, setResult] = useState<PublishResult | null>(null)
  const [retryingTag, setRetryingTag] = useState(false)
  const remoteBranchesRefreshed = useRef('')
  const [gitInit, setGitInit] = useState({
    authorName: '',
    authorEmail: '',
    repository: '',
    message: 'chore: initialize project'
  })
  const status = error
    ? { issues: ['无法读取 Git 发布状态。'] }
    : (data as GitStatus | undefined)
  const branches = status?.sourceBranches ?? emptyGitBranches
  const selectedBranch =
    branches.find(item => item.name === sourceBranch) ??
    branches.find(item => item.name === status?.branch) ??
    branches[0]
  const targetReleaseBranch =
    selectedBranch?.name === status?.remoteBranch
      ? 'release'
      : `${(selectedBranch?.name || 'source').replace(/[\s/]+/g, '-')}-release`
  const commits =
    selectedBranch?.commits ?? status?.sourceCommits ?? emptyGitCommits
  useEffect(() => {
    if (!root || !status?.gitReady || remoteBranchesRefreshed.current === root)
      return
    remoteBranchesRefreshed.current = root
    void fetch('/api/v1/publish/git/refresh-branches', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ root })
    })
      .then(response => {
        if (response.ok) return refetch()
      })
      .catch(() => {
        // 远程不可用时仍可使用已缓存的本地分支，不打断发布页。
      })
  }, [refetch, root, status?.gitReady])
  useEffect(() => {
    if (!branches.some(item => item.name === sourceBranch))
      setSourceBranch(status?.branch || branches[0]?.name || '')
    if (!commits.some(item => item.sha === sourceCommit))
      setSourceCommit(commits[0]?.sha ?? '')
  }, [branches, commits, sourceBranch, sourceCommit, status?.branch])
  const issues = status?.issues ?? []
  const blockingIssues = issues
  const ready = !loading && blockingIssues.length === 0 && !!sourceCommit
  const needsInitialize =
    !status?.gitReady ||
    issues.some(item => item.includes('不是 Git 仓库根目录'))
  const submitInitialize = async () => {
    setInitializing(true)
    try {
      if (await onInitialize(gitInit)) await refetch()
    } finally {
      setInitializing(false)
    }
  }
  const refresh = () => {
    setPhase('source')
    setSession(null)
    setArtifacts([])
    setRequestError('')
    setResult(null)
    void refetch()
  }
  const post = async <T,>(url: string, body: object): Promise<T> => {
    const response = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    })
    const payload = (await response.json().catch(() => ({}))) as T & {
      error?: string
    }
    if (!response.ok) throw new Error(payload.error || '请求失败，请稍后重试。')
    return payload
  }
  const prepareBuild = async () => {
    if (!selectedBranch?.name || !sourceCommit) return
    setPhase('building')
    setRequestError('')
    setResult(null)
    try {
      const next = await post<BuildSession>('/api/v1/publish/git/prepare', {
        root,
        branch: selectedBranch.name,
        commit: sourceCommit
      })
      setSession(next)
      setArtifacts(
        ['lib', 'dist', 'README.md'].filter(item => next.files.includes(item))
      )
      setPhase('artifacts')
    } catch (err) {
      setRequestError(
        err instanceof Error ? err.message : '构建失败，请重新构建。'
      )
      setPhase('source')
    }
  }
  const publish = async () => {
    if (!session || !artifacts.length) return
    setRequestError('')
    try {
      const next = await post<PublishResult>('/api/v1/publish/git/publish', {
        sessionId: session.sessionId,
        version,
        artifacts,
        confirm: true
      })
      setResult(next)
      setPhase('published')
    } catch (err) {
      setRequestError(
        err instanceof Error ? err.message : '发布失败，请检查日志后重试。'
      )
    }
  }
  const retryTag = async () => {
    if (!session) return
    setRetryingTag(true)
    setRequestError('')
    try {
      const next = await post<PublishResult>('/api/v1/publish/git/retry-tag', {
        sessionId: session.sessionId
      })
      setResult(next)
      setPhase('published')
    } catch (err) {
      setRequestError(
        err instanceof Error ? err.message : '标签重试失败，请稍后再试。'
      )
    } finally {
      setRetryingTag(false)
    }
  }
  const artifactIndex = useMemo(() => {
    const files = session?.files ?? []
    const directories = new Set<string>()
    const children = new Map<string, string[]>()
    for (const path of files) {
      const pieces = path.split('/')
      for (let index = 1; index < pieces.length; index += 1) {
        const parent = pieces.slice(0, index).join('/')
        const child = pieces.slice(0, index + 1).join('/')
        directories.add(parent)
        const current = children.get(parent) ?? []
        if (!current.includes(child)) children.set(parent, [...current, child])
      }
    }
    const leaves = files.filter(path => !directories.has(path))
    const descendants = new Map<string, string[]>()
    for (const leaf of leaves) {
      descendants.set(leaf, [leaf])
      const pieces = leaf.split('/')
      for (let index = 1; index < pieces.length; index += 1) {
        const parent = pieces.slice(0, index).join('/')
        descendants.set(parent, [...(descendants.get(parent) ?? []), leaf])
      }
    }
    return {
      directories,
      children,
      descendants,
      top: files.filter(path => !path.includes('/'))
    }
  }, [session])
  const selectedArtifacts = useMemo(() => new Set(artifacts), [artifacts])
  const descendantFiles = (item: string) =>
    artifactIndex.descendants.get(item) ?? []
  const isDirectory = (item: string) => artifactIndex.directories.has(item)
  const artifactSelected = (item: string) => {
    const leaves = descendantFiles(item)
    return (
      leaves.length > 0 &&
      leaves.every(leaf => {
        const parts = leaf.split('/')
        return parts.some((_, index) =>
          selectedArtifacts.has(parts.slice(0, index + 1).join('/'))
        )
      })
    )
  }
  const toggleArtifact = (item: string) => {
    setArtifacts(current =>
      current.includes(item)
        ? current.filter(value => value !== item)
        : [...current, item]
    )
  }
  return (
    <section className="git-release-panel grid max-w-[920px] content-start gap-4">
      <header className="release-toolbar flex flex-wrap items-end justify-between gap-3 border-b border-slate-200 pb-3 dark:border-slate-700">
        <span className="min-w-0 truncate text-sm font-semibold text-slate-900 dark:text-slate-100">
          {status?.packageName
            ? `${status.packageName}@${status.packageVersion || '未设置版本'} · ${status.packageManager}`
            : 'GIT 发布'}
        </span>
        <div className="release-toolbar-actions flex flex-wrap items-end justify-end gap-2">
          {(phase === 'artifacts' || phase === 'confirm') && (
            <button
              className="inline-flex min-h-9 items-center justify-center rounded-lg border border-slate-200 bg-white px-3 text-xs font-semibold text-slate-600 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-300"
              onClick={() =>
                setPhase(phase === 'confirm' ? 'artifacts' : 'source')
              }
            >
              上一步
            </button>
          )}
          <button
            className="inline-flex min-h-9 items-center justify-center rounded-lg border border-slate-200 bg-white px-3 text-xs font-semibold text-slate-600 disabled:opacity-40 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-300"
            onClick={refresh}
            disabled={loading || busy}
          >
            <RefreshCw className="size-4" />
          </button>
          <button
            className="inline-flex min-h-9 items-center justify-center rounded-lg bg-brand-600 px-3 text-xs font-semibold text-white transition hover:bg-brand-700 disabled:cursor-not-allowed disabled:opacity-50"
            disabled={
              busy ||
              loading ||
              phase === 'building' ||
              (phase === 'source' && !ready) ||
              (phase === 'artifacts' && !artifacts.length)
            }
            onClick={() => {
              if (phase === 'source') void prepareBuild()
              else if (phase === 'artifacts') setPhase('confirm')
              else if (phase === 'confirm') void publish()
              else if (phase === 'published') refresh()
            }}
          >
            {busy || phase === 'building'
              ? '构建中…'
              : phase === 'source'
                ? '开始构建'
                : phase === 'artifacts'
                  ? '继续确认'
                  : phase === 'confirm'
                    ? '确认发布'
                    : '重新开始'}
          </button>
        </div>
      </header>
      {loading ? (
        <p className="m-0 rounded-lg bg-slate-50 px-3 py-2 text-xs text-slate-500 dark:bg-slate-800 dark:text-slate-400">
          正在读取所选目录的 Git 状态…
        </p>
      ) : (
        <>
          {phase === 'source' && (
            <section className="release-source-card grid gap-3 rounded-xl border border-slate-200 bg-white p-4 dark:border-slate-700 dark:bg-slate-900">
              <div className="grid gap-1">
                <strong className="text-sm font-semibold text-slate-900 dark:text-slate-100">
                  1. 选择源码提交
                </strong>
                <p className="m-0 text-xs leading-5 text-slate-500">
                  只会构建这次已提交的代码，不会包含你还没提交的本地修改。
                </p>
              </div>
              <label className="release-field grid gap-1 text-xs font-semibold text-slate-500">
                源码分支{' '}
                <select
                  className="min-h-9 rounded-lg border border-slate-200 bg-white px-2 text-sm font-normal text-slate-700 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-200"
                  value={selectedBranch?.name || ''}
                  disabled={phase !== 'source'}
                  onChange={event => setSourceBranch(event.target.value)}
                >
                  {branches.map(item => (
                    <option key={item.name} value={item.name}>
                      {item.name}
                    </option>
                  ))}
                </select>
              </label>
              <label className="release-field grid gap-1 text-xs font-semibold text-slate-500">
                发布目标{' '}
                <input
                  className="min-h-9 rounded-lg border border-slate-200 bg-slate-50 px-2 text-sm font-normal text-slate-500 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-400"
                  value={targetReleaseBranch}
                  readOnly
                />
              </label>
              <label className="release-field release-commit-field grid gap-1 text-xs font-semibold text-slate-500">
                提交{' '}
                <select
                  value={sourceCommit}
                  onChange={event => setSourceCommit(event.target.value)}
                  disabled={!commits.length || phase !== 'source'}
                >
                  {commits.length ? (
                    commits.map(item => (
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
          )}
          {phase === 'confirm' && (
            <section className="release-source-card compact grid gap-3 rounded-xl border border-slate-200 bg-white p-4 dark:border-slate-700 dark:bg-slate-900">
              <div className="grid gap-1">
                <strong className="text-sm font-semibold text-slate-900 dark:text-slate-100">
                  2. 设置发布版本
                </strong>
                <p className="m-0 text-xs leading-5 text-slate-500">
                  发布时会创建不可覆盖的 Git Tag，并推送到{' '}
                  {session?.target || targetReleaseBranch}。
                </p>
              </div>
              <label className="grid max-w-xs gap-1 text-xs font-semibold text-slate-500">
                版本{' '}
                <input
                  value={version || status?.suggestedVersion || ''}
                  onChange={event => onVersionChange(event.target.value)}
                  className="min-h-9 rounded-lg border border-slate-200 bg-white px-2 text-sm font-normal text-slate-700 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-200"
                  placeholder="v0.0.1"
                />
              </label>
            </section>
          )}
          {phase === 'building' && (
            <section className="release-source-card grid gap-1 rounded-xl border border-slate-200 bg-white p-4 dark:border-slate-700 dark:bg-slate-900">
              <div>
                <strong className="text-sm font-semibold text-slate-900 dark:text-slate-100">
                  正在构建
                </strong>
                <p className="m-0 text-xs leading-5 text-slate-500">
                  正在隔离目录中安装依赖并执行 build。完成前不能选择产物。
                </p>
              </div>
            </section>
          )}
          {session && phase === 'artifacts' && (
            <section className="release-source-card release-artifact-card grid gap-3 rounded-xl border border-slate-200 bg-white p-4 dark:border-slate-700 dark:bg-slate-900">
              <div className="grid gap-1">
                <strong className="text-sm font-semibold text-slate-900 dark:text-slate-100">
                  3. 选择最终产物
                </strong>
                <p className="m-0 text-xs leading-5 text-slate-500">
                  以下是本次构建实际生成的可发布文件。默认全选；依赖、隐藏文件和
                  package.json 不会显示。
                </p>
              </div>
              <div className="release-artifacts flex flex-wrap gap-2">
                {artifactIndex.top.map(item => (
                  <div className="release-artifact-tree" key={item}>
                    <label
                      className={cn(
                        'inline-flex items-center gap-1.5 rounded-lg border px-2.5 py-2 text-xs transition',
                        artifactSelected(item)
                          ? 'border-brand-200 bg-brand-50 text-brand-600 dark:border-brand-700 dark:bg-brand-100/30 dark:text-brand-200'
                          : 'border-slate-200 bg-slate-50 text-slate-600 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-300'
                      )}
                    >
                      <input
                        className="accent-brand-600"
                        type="checkbox"
                        checked={artifactSelected(item)}
                        onChange={() => toggleArtifact(item)}
                      />
                      {isDirectory(item) && (
                        <button
                          type="button"
                          className="inline-flex size-5 items-center justify-center"
                          onClick={() =>
                            setExpandedArtifacts(current =>
                              current.includes(item)
                                ? current.filter(value => value !== item)
                                : [...current, item]
                            )
                          }
                        >
                          <ChevronRight
                            className={cn(
                              'size-3.5 transition',
                              expandedArtifacts.includes(item) && 'rotate-90'
                            )}
                          />
                        </button>
                      )}
                      <span>{item}</span>
                    </label>
                    {expandedArtifacts.includes(item) &&
                      (artifactIndex.children.get(item) ?? []).map(child => (
                        <label
                          className="artifact-child ml-5 mt-1 flex items-center gap-1.5 text-xs text-slate-500"
                          key={child}
                        >
                          <input
                            className="accent-brand-600"
                            type="checkbox"
                            checked={artifactSelected(child)}
                            onChange={() => toggleArtifact(child)}
                          />
                          <span>{child.slice(item.length + 1)}</span>
                        </label>
                      ))}
                  </div>
                ))}
              </div>
              <p className="m-0 text-xs text-slate-500">
                已选择 {artifacts.length} 项，将发布到{' '}
                <code className="rounded bg-slate-100 px-1 dark:bg-slate-800">
                  {session.target}
                </code>
                。本次构建保留至{' '}
                {new Date(session.expiresAt).toLocaleTimeString([], {
                  hour: '2-digit',
                  minute: '2-digit'
                })}
                。
              </p>
              {session.logs && (
                <details className="release-build-log">
                  <summary>查看构建日志</summary>
                  <pre>{session.logs}</pre>
                </details>
              )}
            </section>
          )}
          {phase === 'source' && (
            <p
              className={cn(
                'rounded-lg border px-3 py-2 text-xs font-semibold',
                ready
                  ? 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-300'
                  : 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-300'
              )}
            >
              {ready ? '✓ 可以从所选提交开始构建' : '！ 发布前需要处理以下问题'}
            </p>
          )}
          {phase === 'source' && blockingIssues.length > 0 && (
            <section className="grid gap-3 rounded-xl border border-amber-200 bg-amber-50 p-4 dark:border-amber-900 dark:bg-amber-950/30">
              <ul className="m-0 grid gap-1 pl-4 text-xs leading-5 text-amber-800 dark:text-amber-300">
                {blockingIssues.map(item => (
                  <li key={item}>{item}</li>
                ))}
              </ul>
              {needsInitialize && (
                <button
                  className="inline-flex min-h-9 w-fit items-center justify-center rounded-lg bg-brand-600 px-3 text-xs font-semibold text-white transition hover:bg-brand-700 disabled:opacity-50"
                  disabled={busy || initializing}
                  onClick={() => setGitInitOpen(true)}
                >
                  填写 Git 信息并初始化
                </button>
              )}
              {status?.repository && (
                <p className="m-0 text-xs text-amber-800 dark:text-amber-300">
                  远程仓库：
                  <code className="break-all">{status.repository}</code>
                  {status.remoteAdvice ? ` · ${status.remoteAdvice}` : ''}
                </p>
              )}
            </section>
          )}
          {phase === 'confirm' && session && (
            <p className="m-0 rounded-lg border border-brand-200 bg-brand-50 px-3 py-2 text-xs leading-5 text-brand-700 dark:border-brand-200 dark:bg-brand-100/30 dark:text-brand-200">
              即将把 {artifacts.length} 项构建产物发布到{' '}
              <code>{session.target}</code>，并创建标签{' '}
              <code>{version || status?.suggestedVersion}</code>。
            </p>
          )}
          {requestError && (
            <p className="m-0 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs leading-5 text-rose-700 dark:border-rose-900 dark:bg-rose-950/30 dark:text-rose-300">
              ！ {requestError}
            </p>
          )}
          {session && requestError.includes('release 分支已推送') && (
            <button
              className="inline-flex min-h-9 w-fit items-center justify-center rounded-lg bg-brand-600 px-3 text-xs font-semibold text-white hover:bg-brand-700 disabled:opacity-50"
              disabled={retryingTag}
              onClick={() => void retryTag()}
            >
              {retryingTag ? '正在重试标签…' : '重试推送 Tag'}
            </button>
          )}
          {phase === 'published' && result?.output && (
            <pre className="release-result">{result.output}</pre>
          )}
        </>
      )}
      <GitInitializeDialog
        open={gitInitOpen}
        values={gitInit}
        busy={busy || initializing}
        onClose={() => setGitInitOpen(false)}
        onChange={setGitInit}
        onConfirm={async () => {
          await submitInitialize()
          setGitInitOpen(false)
        }}
      />
    </section>
  )
}

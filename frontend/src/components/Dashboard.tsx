import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { useDispatch, useSelector } from 'react-redux'
import cn from 'classnames'
import Markdown from 'markdown-to-jsx'
import {
  Archive,
  Bot,
  ArrowLeft,
  ArrowRight,
  Check,
  ChevronRight,
  ClipboardList,
  Code2,
  Eye,
  EyeOff,
  Folder,
  HardDrive,
  GitBranch,
  Globe2,
  KeyRound,
  Link,
  Network,
  Package,
  Play,
  Plug,
  Plus,
  RefreshCw,
  Search,
  Send,
  Settings,
  Terminal,
  X
} from 'lucide-react'
import { RobotConfigForm } from './RobotConfigForm'
import { ThemeToggle } from './ThemeToggle'
import { NpmrcConfigForm } from './NpmrcConfigForm'
import { EnvConfigForm } from './EnvConfigForm'
import { NpmPublishPanel } from './NpmPublishPanel'
import { PackageManifestPanel } from './PackageManifestPanel'
import { SetupUpdateButton } from './SetupUpdateButton'
import { AIChatPage } from './AIControl'
import { ErrorNotice } from './ErrorNotice'
import { ConfirmDialog } from './ConfirmDialog'
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
  useRobotTasksQuery,
  useSaveRobotLoginMutation,
  useRobotWebViewsQuery,
  useSetSetupPluginEnabledMutation,
  useSetupPluginsQuery,
  useStartRobotTaskMutation,
  useStartSetupPluginTaskMutation,
  useWritePackageConfigMutation,
  useWriteRobotFileMutation,
  type RuntimeOverview,
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

function setupPluginIcon(icon?: string) {
  return icon === 'network' ? <Network /> : <Plug />
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
    locations: Array<{ name: string; path: string; kind: 'home' | 'disk' | 'volume' }>
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
          setDirectoryError(reason instanceof Error ? reason.message : '目录无法读取。')
        }
      })
    return () => controller.abort()
  }, [directoryReload, hidden, open, path])
  if (!open) return null
  const items = (data?.directories ?? []).filter(item =>
    item.name.toLowerCase().includes(query.toLowerCase())
  )
  const toggleSelection = (itemPath: string) =>
    setSelected(current =>
      multiple
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
  return (
    <div className={`directory-picker-backdrop ${priority ? 'directory-picker-backdrop-priority' : ''}`}>
      <section
        className="directory-picker finder-picker"
        role="dialog"
        aria-label="选择目录"
      >
        <header className="finder-toolbar">
          <div className="finder-tools">
            <nav className="finder-navigation" aria-label="目录导航">
              <button
                className="icon-button"
                disabled={historyIndex <= 0 && !data?.parent}
                onClick={() =>
                  historyIndex > 0 ? goHistory(-1) : visit(data?.parent ?? '')
                }
                title="后退"
                aria-label="后退"
              >
                <ArrowLeft />
              </button>
              <button
                className="icon-button"
                disabled={historyIndex >= history.length - 1}
                onClick={() => goHistory(1)}
                title="前进"
                aria-label="前进"
              >
                <ArrowRight />
              </button>
              <button
                className="icon-button"
                onClick={() => setHidden(value => !value)}
                title={hidden ? '隐藏隐藏目录' : '显示隐藏目录'}
                aria-label={hidden ? '隐藏隐藏目录' : '显示隐藏目录'}
              >
                {hidden ? <EyeOff /> : <Eye />}
              </button>
            </nav>
            <small>单击选择，双击打开</small>
          </div>
          <strong>
            {data?.path
              ? /^[a-z]:[\\/]?$/i.test(data.path)
                ? `本地磁盘（${data.path.slice(0, 2).toUpperCase()}）`
                : data.path.split(/[\\/]/).filter(Boolean).pop() || '系统磁盘'
              : '选择目录'}
          </strong>
          <label className="finder-search">
            <Search />
            <input
              value={query}
              onChange={event => setQuery(event.target.value)}
              placeholder="搜索当前目录"
            />
          </label>
        </header>
        <section className="finder-body">
          <aside className="finder-sidebar">
            <small>常用</small>
            {favorites.map(item => (
              <button
                className={item.path === data?.path ? 'active' : ''}
                key={item.path}
                onClick={() => visit(item.path)}
              >
                <Folder />
                {item.name}
              </button>
            ))}
            {locations.length > 0 && (
              <>
                <small>磁盘与位置</small>
                {locations.map(location => (
                  <button
                    className={location.path === data?.path ? 'active' : ''}
                    key={location.path}
                    onClick={() => visit(location.path)}
                    title={location.path}
                  >
                    {location.kind === 'home' ? <Folder /> : <HardDrive />}
                    {location.name}
                  </button>
                ))}
              </>
            )}
          </aside>
          <main className="finder-content">
            <header>
              <span>名称</span>
              <span>种类</span>
            </header>
            <div className="directory-picker-list">
              {directoryError && <div className="directory-picker-error"><strong>需要访问授权</strong><span>{directoryError}</span><button className="secondary-button" onClick={() => setDirectoryReload(current => current + 1)}>重试</button></div>}
              {items.map(item => (
                <button
                  className={selected.includes(item.path) ? 'selected' : ''}
                  key={item.path}
                  onClick={() => toggleSelection(item.path)}
                  onDoubleClick={() => visit(item.path)}
                >
                  <Folder />
                  <span>{item.name}</span>
                  <small>文件夹</small>
                </button>
              ))}
            </div>
          </main>
        </section>
        <footer>
          <span title={data?.path ?? ''}>{data?.path ?? '正在读取目录…'}</span>
          <div>
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
    </div>
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
  const [gitCloneOpen, setGitCloneOpen] = useState(false)
  const [gitDestinationPickerOpen, setGitDestinationPickerOpen] = useState(false)
  const [gitDestination, setGitDestination] = useState('')
  const [gitProject, setGitProject] = useState<Project | null>(null)
  const [invalidDirectory, setInvalidDirectory] = useState('')
  const [pendingBackpackRemoval, setPendingBackpackRemoval] = useState('')
  const [trackRuntimeTasks, setTrackRuntimeTasks] = useState(false)
  const [aiOpen, setAIOpen] = useState(false)
  const environmentChecked = useRef(false)
  const dispatch = useDispatch()
  const projects = useSelector(
    (state: RootState) => state.workspace.projects
  ) as Project[]
  const activeProjectID = useSelector(
    (state: RootState) => state.workspace.activeProjectID
  )
  const webviewTabs = useSelector((state: RootState) => state.workspace.webviewTabs)
  const activeWebviewTabKey = useSelector((state: RootState) => state.workspace.activeWebviewTabKey)
  const developerMode = useSelector(
    (state: RootState) => state.workspace.developerMode
  )
  const activeProject = projects.find(item => item.id === activeProjectID)
  const root = activeProject?.path ?? ''
  const activeWebviewTab = webviewTabs.find(item => item.key === activeWebviewTabKey)
  const activeWebViewID = activeWebviewTab?.root === root ? activeWebviewTab.entryID : ''
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
  const { data: robotWebViews = [], isLoading: webViewsLoading } = useRobotWebViewsQuery(root, {
    skip: !root
  })
  const {
    data: runtime,
    isFetching: runtimeLoading,
    refetch: refetchRuntime
  } = useRobotRuntimeQuery(root, { skip: !root })
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
  const { data: setupPlugins = [] } = useSetupPluginsQuery()
  const catalogError = catalogQueryError ? '在线目录暂时无法读取。' : ''
  const showOutput = (message: string, failed = false) => {
    setOutput(message)
    setOutputFailed(failed)
  }

  useEffect(() => {
    if (defaultPage === 'robot') setPage('robot')
  }, [defaultPage])
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
  useEffect(() => { if (root && !webViewsLoading) dispatch(pruneWebviewTabs({ root, entryIDs: robotWebViews.map(item => item.id) })) }, [dispatch, robotWebViews, root, webViewsLoading])
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
    if (configEditor === 'text') setConfigEditor('visual')
    setConsoleOpen(false)
  }, [configEditor, developerMode, page, section])

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
      if (data.action === 'dev') {
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
              { type: 'RobotWebViews', id: root }
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
  async function cloneRobotRepository(repository: string, branch: string, name: string, mirror: string) {
    if (!gitDestination) return
    setBusy(true)
    try {
      const response = await fetch('/api/v1/robot/git-clone', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ destination: gitDestination, repository, branch, name, mirror })
      })
      const data = (await response.json()) as { path?: string; output?: string; error?: string }
      if (!response.ok || !data.path) throw new Error(data.error || '克隆仓库失败。')
      showOutput(data.output || '仓库已克隆。')
      setGitCloneOpen(false)
      await addSelectedDirectories([data.path])
    } catch (reason) {
      showOutput(operationErrorMessage(reason, '克隆仓库失败，请检查 Git 地址和网络。'), true)
    } finally {
      setBusy(false)
    }
  }

  function removeProject(id: string) {
    dispatch(removeWorkspaceProject(id))
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
    api('GET', { root, file: 'alemon.config.yaml' })
  }
  function selectPage(nextPage: Page) {
    closeTemporaryContentPage()
    setSystemFeature(null)
    setPage(nextPage)
    setCatalogItem(null)
    setOutput('')
  }
  function openAI() {
    closeTemporaryContentPage()
    setSystemFeature(null)
    setPage('robot')
    setAIOpen(true)
    setOutput('')
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
  const robotContent = aiOpen ? <AIChatPage root={root} /> : (
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
                toolbar={
                  developerMode ? (
                    <EditorMode
                      active={configEditor}
                      onVisual={() => setConfigEditor('visual')}
                      onText={openTextConfig}
                    />
                  ) : undefined
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
          onRefresh={() => void refetchRuntime()}
          onOpenConsole={() => setConsoleOpen(true)}
          onRun={(action, packageName) =>
            api('POST', {
              root,
              action,
              ...(packageName ? { package: packageName } : {})
            }).then(success => {
              void refetchRuntime()
              return success
            })
          }
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
          <section className="catalog-items">
            {currentCatalog.items.map(item => (
              <button
                className="catalog-item"
                key={`${currentCatalog.title}-${item.name}`}
                onClick={() => setCatalogItem(item)}
              >
                <span className="catalog-item-copy">
                  <strong>{item.name}</strong>
                  <small>{item.description || '查看包说明、安装与配置'}</small>
                </span>
                <ChevronRight />
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
            tabs={webviewTabs.filter(tab => tab.root === root).sort((left, right) => left.openedAt.localeCompare(right.openedAt))}
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
      <EmptyWorkspace onAdd={chooseDirectories} />
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
                ALEMONJS
              </a>
              <SetupUpdateButton />
              <ThemeToggle />
            </div>
            <div className="ml-auto flex min-w-0 items-center gap-2">
              <SSHControl />
              <AuthControl />
              {developerMode && <McpControl />}
              <OperationTasksButton root={root} />
              <button
                className={cn('inline-flex min-h-8 items-center gap-1.5 rounded-md border px-2 text-xs font-semibold transition', developerMode ? 'border-slate-400 bg-slate-100 text-slate-900' : 'border-slate-200 bg-white text-slate-500 hover:bg-slate-50')}
                onClick={() => dispatch(setDeveloperMode(!developerMode))}
                aria-pressed={developerMode}
                title={
                  developerMode
                    ? '关闭开发模式，收起源码与发布工具'
                    : '开启开发模式，显示源码、终端与发布工具'
                }
              >
                <Code2 />
                <span>开发</span>
              </button>
              <button
                className={cn('inline-flex min-h-8 items-center gap-1.5 rounded-md border px-2 text-xs font-semibold transition disabled:cursor-wait disabled:opacity-60', environmentWarning ? 'border-amber-300 bg-amber-50 text-amber-800' : 'border-slate-200 bg-slate-50 text-slate-700')}
                onClick={() => {
                  setEnvironmentOpen(true)
                  onCheck()
                }}
                disabled={checking}
                title="查看并检查全局环境"
              >
                <i>{checking ? '◌' : environmentWarning ? '!' : '✓'}</i>
                <span>环境</span>
                <strong>{checking ? '检查中' : environmentReady}</strong>
              </button>
              <button
                className="inline-flex size-8 items-center justify-center rounded-md border border-slate-200 bg-white text-sm font-semibold text-slate-500 transition hover:bg-slate-50 hover:text-slate-900"
                onClick={onOpenGuide}
                aria-label="打开引导"
                title="打开引导"
              >
                ?
              </button>
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
                if (await api('POST', { root, action: 'remove-local-package', package: packageName }))
                  void refetchPackages()
                setPendingBackpackRemoval('')
              })()
            }}
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
            onClose={() => setGitCloneOpen(false)}
            onChooseDestination={() => setGitDestinationPickerOpen(true)}
            onConfirm={cloneRobotRepository}
          />
          <section className="console-layout">
            <ProjectRail
              feature={systemFeature}
              setupPlugins={setupPlugins}
              projects={projects}
              activeID={activeProjectID}
              onFeature={selectSystemFeature}
              onAdd={chooseDirectories}
              onClone={() => setGitCloneOpen(true)}
              onSelect={id => {
                dispatch(selectProject(id))
                // Keep each robot's most recently used WebView active when
                // the user returns to it. AI remains local to the previous
                // robot and must never follow the directory switch.
                setAIOpen(false)
                setSystemFeature(null)
                setPage('robot')
                setSection('config')
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
                    dispatch(openWebviewTab({ key: `${root}\u0000${id}`, root, entryID: id, package: entry.package, title: entry.name }))
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
      <RobotGitControl project={gitProject} onClose={() => setGitProject(null)} />
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
  onFeature,
  onAdd,
  onClone,
  onSelect,
  onRemove
}: {
  feature: SystemFeature | null
  setupPlugins: SetupPlugin[]
  projects: Project[]
  activeID: string
  onFeature: (feature: SystemFeature) => void
  onAdd: () => void
  onClone: () => void
  onSelect: (id: string) => void
  onRemove: (id: string) => void
}) {
  const activePlugins = setupPlugins.filter(item => item.enabled)
  return (
    <aside className="project-rail flex min-h-0 min-w-0 flex-col border-r border-slate-200 bg-slate-50">
      <section className="border-b border-slate-200 p-3" aria-label="系统功能目录">
        <header className="mb-2 px-2 text-[11px] font-semibold text-slate-400">
          <small>系统</small>
        </header>
        <nav>
          {coreFeatureCatalog.map(item => (
            <button
              className={cn('flex min-h-9 w-full items-center gap-2 rounded-md px-2.5 text-left text-xs font-semibold transition', feature === item.id ? 'bg-slate-200 text-slate-950' : 'text-slate-600 hover:bg-slate-100')}
              key={item.id}
              onClick={() => onFeature(item.id)}
            >
              <i className="inline-flex size-4 items-center justify-center not-italic">{item.icon}</i>
              <span className="min-w-0 flex-1 truncate">{item.label}</span>
              {item.status && <small className="text-[10px] text-slate-400">{item.status}</small>}
            </button>
          ))}
        </nav>
        {activePlugins.length > 0 && (
          <>
            <span className="my-3 block border-t border-slate-200" />
            <nav className="grid gap-1">
              {activePlugins.map(item => (
                <button
                  className={cn('flex min-h-9 w-full items-center gap-2 rounded-md px-2.5 text-left text-xs font-semibold transition', feature === `setup:${item.id}` ? 'bg-slate-200 text-slate-950' : 'text-slate-600 hover:bg-slate-100')}
                  key={item.id}
                  onClick={() => onFeature(`setup:${item.id}`)}
                >
                  <i className="inline-flex size-4 items-center justify-center not-italic">{setupPluginIcon(item.navigation.icon)}</i>
                  <span className="min-w-0 flex-1 truncate">{item.navigation.label || item.name}</span>
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
            <span className="rounded-full bg-slate-200 px-1.5 py-0.5 text-[10px] text-slate-500">{projects.length}</span>
          </div>
          <div className="flex items-center gap-1.5">
            <button className="icon-button size-8 p-0" onClick={onClone} aria-label="从 Git 克隆机器人" title="从 Git 克隆机器人"><GitBranch className="size-4" /></button>
            <button className="icon-button size-8 p-0" onClick={onAdd} aria-label="添加本地机器人目录" title="添加本地机器人目录"><Plus className="size-4" /></button>
          </div>
        </header>
        <div className="grid content-start gap-1.5 overflow-auto p-2">
          {projects.map(project => (
            <ProjectItem
              active={project.id === activeID}
              key={project.id}
              project={project}
              onSelect={onSelect}
              onRemove={onRemove}
            />
          ))}
          {!projects.length && <p className="px-2 py-4 text-center text-xs text-slate-400">添加目录开始管理</p>}
        </div>
      </section>
    </aside>
  )
}

function GitCloneDialog({
  open,
  destination,
  busy,
  onClose,
  onChooseDestination,
  onConfirm
}: {
  open: boolean
  destination: string
  busy: boolean
  onClose: () => void
  onChooseDestination: () => void
  onConfirm: (repository: string, branch: string, name: string, mirror: string) => Promise<void>
}) {
  const [repository, setRepository] = useState('')
  const [branch, setBranch] = useState('')
  const [name, setName] = useState('')
  const [mirror, setMirror] = useState('official')
  const [connection, setConnection] = useState<'ssh' | 'https'>('https')
  const [sshKeys, setSSHKeys] = useState<Array<{ name: string }>>([])
  const [sshLoading, setSSHLoading] = useState(false)
  const [target, setTarget] = useState<{ path: string; exists: boolean } | null>(null)
  const [targetError, setTargetError] = useState('')
  useEffect(() => {
    if (open) {
      setRepository('')
      setBranch('')
      setName('')
      setMirror('official')
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
        const data = await response.json() as { keys?: Array<{ name: string }>; error?: string }
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
      .finally(() => { if (active) setSSHLoading(false) })
    return () => { active = false }
  }, [open])
  useEffect(() => {
    if (!open || !destination || !repository.trim() || !name.trim()) { setTarget(null); setTargetError(''); return }
    const controller = new AbortController()
    const timer = window.setTimeout(() => {
      void fetch(`/api/v1/robot/git-clone/check?${new URLSearchParams({ destination, repository, name })}`, { signal: controller.signal })
        .then(async response => { const data = await response.json() as { path?: string; exists?: boolean; error?: string }; if (!response.ok) throw new Error(data.error || '无法检查目标目录。'); return data })
        .then(data => { setTarget({ path: data.path ?? '', exists: Boolean(data.exists) }); setTargetError('') })
        .catch(reason => { if (!(reason instanceof DOMException && reason.name === 'AbortError')) { setTarget(null); setTargetError(operationErrorMessage(reason, '无法检查目标目录。')) } })
    }, 260)
    return () => { controller.abort(); window.clearTimeout(timer) }
  }, [destination, name, open, repository])
  if (!open) return null
  return <div className="directory-picker-backdrop"><section className="git-dialog git-clone-dialog" role="dialog" aria-label="从 Git 克隆机器人">
    <header><div><strong>添加 Git 仓库</strong><span>下载完成后会自动加入机器人目录。</span></div><button className="icon-button" onClick={onClose} aria-label="关闭"><X /></button></header>
    <div className="git-dialog-form git-clone-form">
      <section className="git-clone-access" aria-label="仓库连接方式">
        <header><strong>连接方式</strong><small>{sshLoading ? '正在检查 SSH…' : sshKeys.length ? `已检测到 SSH 密钥：${sshKeys[0].name}` : '未配置 SSH 密钥'}</small></header>
        <div>
          <button type="button" className={connection === 'ssh' ? 'active' : ''} onClick={() => setConnection('ssh')}><KeyRound />SSH{sshKeys.length ? '（推荐）' : ''}</button>
          <button type="button" className={connection === 'https' ? 'active' : ''} onClick={() => setConnection('https')}><Globe2 />HTTPS</button>
        </div>
        <p>{connection === 'ssh' ? sshKeys.length ? '推荐 SSH：私有仓库需先将此公钥添加到代码平台。' : '未配置 SSH 密钥；请在顶部 SSH 管理中生成并添加公钥，或改用 HTTPS。' : 'HTTPS 可直接使用；访问私有仓库时，需要在代码平台完成在线授权。'}</p>
      </section>
      <section className="git-clone-section">
        <header><strong>仓库信息</strong><small>粘贴仓库的克隆地址。</small></header>
        <label>仓库地址<input autoFocus value={repository} onChange={event => { const value = event.target.value; setRepository(value); const derived = value.trim().replace(/\/$/, '').split('/').pop()?.replace(/\.git$/, '') ?? ''; setName(derived) }} placeholder={connection === 'ssh' ? 'git@github.com:组织/机器人仓库.git' : 'https://github.com/组织/机器人仓库.git'} /><small>{connection === 'ssh' ? 'SSH 地址通常以 git@ 开头。' : 'HTTPS 地址通常以 https:// 开头。'}</small></label>
        <div className="git-clone-fields"><label>分支（可选）<input value={branch} onChange={event => setBranch(event.target.value)} placeholder="默认分支" /></label><label>下载来源<select value={mirror} onChange={event => setMirror(event.target.value)}><option value="official">Git 官方（推荐）</option><option value="gh-proxy">GitHub 加速 · gh-proxy</option><option value="ghproxy-net">GitHub 加速 · ghproxy.net</option></select></label></div>
      </section>
      <section className="git-clone-section git-clone-destination">
        <header><strong>保存位置</strong><small>会在所选文件夹中创建一个新目录。</small></header>
        <label>所在文件夹<button type="button" className="directory-choice" onClick={onChooseDestination}>{gitDestinationLabel(destination)}</button></label>
        <label>新目录名称<input value={name} onChange={event => setName(event.target.value)} placeholder="默认使用仓库名" />{target?.exists ? <small className="git-target-error">目标已存在：{target.path}</small> : target?.path ? <small className="git-target-ready">将下载到：{target.path}</small> : targetError ? <small className="git-target-error">{targetError}</small> : null}</label>
      </section>
    </div>
    <footer><button className="secondary-button" onClick={onClose}>取消</button><button className="primary-button" disabled={busy || !repository.trim() || !destination || !name.trim() || !target || target.exists || Boolean(targetError)} onClick={() => void onConfirm(repository.trim(), branch.trim(), name.trim(), mirror)}>{busy ? '正在下载…' : '克隆并添加'}</button></footer>
  </section></div>
}

function gitDestinationLabel(path: string) {
  return path || '选择存放位置'
}

function GitInitializeDialog({ open, values, busy, onClose, onChange, onConfirm }: { open: boolean; values: { authorName: string; authorEmail: string; repository: string; message: string }; busy: boolean; onClose: () => void; onChange: (values: { authorName: string; authorEmail: string; repository: string; message: string }) => void; onConfirm: () => Promise<void> }) {
  if (!open) return null
  const update = (key: keyof typeof values, value: string) => onChange({ ...values, [key]: value })
  return <div className="directory-picker-backdrop"><section className="git-dialog" role="dialog" aria-label="填写 Git 初始化信息">
    <header><div><strong>初始化 Git 仓库</strong><span>仅修改当前项目，不会改动你的全局 Git 身份。</span></div><button className="icon-button" onClick={onClose} aria-label="关闭"><X /></button></header>
    <div className="git-dialog-form"><label>提交姓名<input autoFocus value={values.authorName} onChange={event => update('authorName', event.target.value)} placeholder="你的姓名" /></label><label>提交邮箱<input type="email" value={values.authorEmail} onChange={event => update('authorEmail', event.target.value)} placeholder="name@example.com" /></label><label>远程仓库（可选）<input value={values.repository} onChange={event => update('repository', event.target.value)} placeholder="https://github.com/owner/repo.git" /></label><label>首个提交说明<input value={values.message} onChange={event => update('message', event.target.value)} /></label></div>
    <footer><button className="secondary-button" onClick={onClose}>取消</button><button className="primary-button" disabled={busy || !values.authorName.trim() || !values.authorEmail.trim()} onClick={() => void onConfirm()}>{busy ? '正在初始化…' : '确认初始化'}</button></footer>
  </section></div>
}
function ProjectItem({
  project,
  active,
  onSelect,
  onRemove
}: {
  project: Project
  active: boolean
  onSelect: (id: string) => void
  onRemove: (id: string) => void
}) {
  const [validate, { data }] = useLazyRobotProjectQuery()
  useEffect(() => {
    void validate(project.path)
  }, [project.path, validate])
  const invalid = data?.valid === false
  return (
    <article
      className={cn('relative rounded-lg border p-2 transition', active ? 'border-slate-300 bg-white shadow-sm' : 'border-transparent hover:border-slate-200 hover:bg-white/70', invalid ? 'border-amber-300 bg-amber-50' : '')}
    >
      <button className="grid w-full gap-1 pr-6 text-left" onClick={() => onSelect(project.id)}>
        <strong className="flex min-w-0 items-center gap-1.5 truncate text-xs font-semibold text-slate-800">
          {project.name}
          {invalid && <em className="not-italic text-[10px] font-semibold text-amber-700">目录不可用</em>}
        </strong>
        <small className="block truncate text-[11px] text-slate-400" title={project.path}>
          {invalid ? data.error || project.path : project.path}
        </small>
      </button>
      <button
        className="absolute right-2 top-2 inline-flex size-6 items-center justify-center rounded text-slate-400 transition hover:bg-slate-100 hover:text-slate-700"
        onClick={() => onRemove(project.id)}
        aria-label={`移除 ${project.name}`}
        title="移除目录"
      >
        <X className="size-3.5" />
      </button>
    </article>
  )
}
function McpControl() {
  const [open, setOpen] = useState(false)
  const [transport, setTransport] = useState<'stdio' | 'http'>('stdio')
  const [copied, setCopied] = useState(false)
  const stdioConfig =
    '{\n  "mcpServers": {\n    "alemonjs-setup": {\n      "command": "albs",\n      "args": ["mcp"]\n    }\n  }\n}'
  const httpCommand =
    "MCP_TOKEN='请生成高强度随机值' albs --mcp-port 17391 mcp-http"
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
  return (
    <div className="mcp-control">
      <button
        className="mcp-control-button"
        onClick={() => setOpen(value => !value)}
        aria-expanded={open}
        title="连接 Codex 或其他本机 AI 客户端"
      >
        <i>✓</i>
        <span>MCP</span>
        <strong>已开启</strong>
      </button>
      {open && (
        <section className="mcp-popover" role="dialog" aria-label="连接 MCP">
          <header>
            <div>
              <strong>连接 Codex / 自定义 MCP</strong>
              <small>两种标准传输均可用</small>
            </div>
            <button onClick={() => setOpen(false)} aria-label="关闭 MCP 说明">
              ×
            </button>
          </header>
          <p>
            MCP 让 AI 在你的确认下管理
            AlemonJS：读取与修改项目、更新运行配置、启动机器人、构建、打包与发布。它不是网页远程控制；客户端只会连接本机服务。
          </p>
          <div
            className="mcp-transport-tabs"
            role="tablist"
            aria-label="MCP 接入类型"
          >
            <button
              className={!http ? 'active' : ''}
              role="tab"
              aria-selected={!http}
              onClick={() => setTransport('stdio')}
            >
              STDIO <small>推荐</small>
            </button>
            <button
              className={http ? 'active' : ''}
              role="tab"
              aria-selected={http}
              onClick={() => setTransport('http')}
            >
              流式 HTTP <small>本机</small>
            </button>
          </div>
          {http ? (
            <>
              <p>
                先在终端启动受 Token 保护的服务；随后在 Codex 的“连接至自定义
                MCP”中选择<strong> 流式 HTTP</strong>，填写地址与 Bearer Token。
              </p>
              <dl className="mcp-form-guide">
                <div>
                  <dt>名称</dt>
                  <dd>alemonjs-setup</dd>
                </div>
                <div>
                  <dt>类型</dt>
                  <dd>流式 HTTP</dd>
                </div>
                <div>
                  <dt>地址</dt>
                  <dd>
                    <code>http://127.0.0.1:17391/mcp</code>
                  </dd>
                </div>
                <div>
                  <dt>认证</dt>
                  <dd>
                    Bearer Token：<code>&lt;MCP_TOKEN&gt;</code>
                  </dd>
                </div>
                <div>
                  <dt>启动命令</dt>
                  <dd>
                    <code>{httpCommand}</code>
                  </dd>
                </div>
              </dl>
              <button
                className="mcp-copy-button"
                onClick={() => void copy(httpCommand)}
              >
                {copied ? '已复制启动命令' : '复制启动命令'}
              </button>
              <small className="mcp-note">
                服务仅绑定 127.0.0.1；不要把地址、Token
                或端口转发到局域网和公网。流式 HTTP 兼容 MCP 的 POST
                请求，服务不提供独立 SSE 推送流。
              </small>
            </>
          ) : (
            <>
              <p>
                在 Codex 的“连接至自定义 MCP”中选择<strong> STDIO</strong>
                ，把下列字段逐行填入。Codex 会直接启动本机 <code>albs</code>
                ，无需额外开启端口。
              </p>
              <dl className="mcp-form-guide">
                <div>
                  <dt>名称</dt>
                  <dd>alemonjs-setup</dd>
                </div>
                <div>
                  <dt>类型</dt>
                  <dd>STDIO</dd>
                </div>
                <div>
                  <dt>启动命令</dt>
                  <dd>
                    <code>albs</code>
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
  const label = (action: string) =>
    action.startsWith('setup:')
      ? `系统插件 · ${action.split(':').slice(-1)[0]}`
      : ({
          'install': '安装依赖',
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
    <div className="operation-tasks">
      <button
        className="operation-tasks-button"
        onClick={() => setOpen(value => !value)}
        aria-label="操作记录"
        title="当前目录操作记录"
      >
        <ClipboardList />
        {running > 0 && <b>{running}</b>}
      </button>
      {open && (
        <section className="operation-tasks-popover">
          <header>
            <div>
              <strong>操作记录</strong>
              <small>{root ? '当前机器人与系统操作' : '系统操作'}</small>
            </div>
            <button
              className="icon-button"
              onClick={() => setOpen(false)}
              aria-label="关闭操作记录"
            >
              <X />
            </button>
          </header>
          {isFetching && !tasks.length ? (
            <p>正在读取任务…</p>
          ) : !tasks.length ? (
            <p>暂无与当前位置相关的操作记录。</p>
          ) : (
            <>
              <div className="task-list">
                {tasks.slice(0, 12).map(item => (
                  <button
                    key={item.id}
                    className={current?.id === item.id ? 'active' : ''}
                    onClick={() => setSelected(item.id)}
                  >
                    <i className={item.status}>
                      {item.status === 'running'
                        ? '◌'
                        : item.status === 'completed'
                          ? '✓'
                          : '!'}
                    </i>
                    <span>
                      {label(item.action)}
                      <small>
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
                <pre className={current.status}>
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
      className="environment-panel"
      role="dialog"
      aria-label="全局环境详情"
    >
      <header>
        <strong>
          {checking
            ? '正在检查环境…'
            : checks.length
              ? `${readyCount}/${checks.length} 已就绪`
              : '等待检查'}
        </strong>
        <button onClick={onClose} aria-label="关闭环境详情">
          ×
        </button>
      </header>
      {checking && (
        <p className="environment-panel-state">
          正在读取 Node.js、Git 和系统工具状态。
        </p>
      )}
      {!checking && checks.length > 0 && (
        <div className="environment-check-list">
          {checks.map(check => (
            <article className={check.status} key={check.id}>
              <i>{check.status === 'ready' ? '✓' : '!'}</i>
              <div>
                <strong>{check.name}</strong>
                <span>{check.detail}</span>
                {check.status !== 'ready' && check.suggestion && (
                  <small>{check.suggestion}</small>
                )}
              </div>
              {check.status !== 'ready' && (
                <button className="text-button" onClick={() => onFix(check)}>
                  修复
                </button>
              )}
            </article>
          ))}
        </div>
      )}
      {!checking && !checks.length && (
        <p className="environment-panel-state">尚未获取检查结果。</p>
      )}
      <footer>
        <button
          className="secondary-button"
          disabled={checking}
          onClick={onRefresh}
        >
          重新检查
        </button>
      </footer>
    </aside>
  )
}
function EmptyWorkspace({ onAdd }: { onAdd: () => void }) {
  return (
    <section className="workspace-content empty-workspace">
      <span>◈</span>
      <strong>从左侧添加机器人目录</strong>
      <button className="primary-button" onClick={onAdd}>
        添加目录
      </button>
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
    <section className="workspace-content setup-plugin-manager">
      <header>
        <div>
          <h1>
            插件 <small>{plugins.filter(item => item.enabled).length}</small>
          </h1>
        </div>
      </header>
      {plugins.length ? (
        <section className="setup-plugin-cards">
          {plugins.map(plugin => (
            <article
              className={plugin.enabled ? '' : 'disabled'}
              key={plugin.id}
            >
              <button
                className="setup-plugin-open"
                onClick={() => plugin.enabled && onOpen(plugin.id)}
                disabled={!plugin.enabled}
              >
                <i>{setupPluginIcon(plugin.navigation.icon)}</i>
                <div>
                  <strong>{plugin.name}</strong>
                  <small>
                    v{plugin.version} · {plugin.enabled ? '已启用' : '已卸载'}
                  </small>
                </div>
                {plugin.enabled && <ChevronRight />}
              </button>
              <button
                className={
                  plugin.enabled
                    ? 'secondary-button setup-plugin-toggle'
                    : 'primary-button setup-plugin-toggle'
                }
                disabled={isLoading}
                onClick={() => void toggle(plugin)}
              >
                {plugin.enabled ? '卸载' : '启用'}
              </button>
            </article>
          ))}
        </section>
      ) : (
        <section className="setup-plugin-empty">
          <strong>暂未发现插件</strong>
          <span>将插件目录放入 plugins 后刷新即可。</span>
        </section>
      )}
      {message && <p className="setup-plugin-message">{message}</p>}
    </section>
  )
}
function SetupPluginCenter({ plugin }: { plugin: SetupPlugin }) {
  type SetupAction = NonNullable<SetupPlugin['actions']>[number]
  const [page, setPage] = useState(plugin.pages[0]?.id ?? 'overview')
  const [activeAction, setActiveAction] = useState('')
  const [message, setMessage] = useState('')
  const [values, setValues] = useState<Record<string, string>>({})
  const [startTask, { isLoading }] = useStartSetupPluginTaskMutation()
  const current = plugin.pages.find(item => item.id === page) ?? plugin.pages[0]
  const visibleActions = (plugin.actions ?? []).filter(
    action => !action.page || action.page === current?.id
  )

  useEffect(() => {
    setPage(plugin.pages[0]?.id ?? 'overview')
    setActiveAction('')
    setMessage('')
    setValues(
      Object.fromEntries(
        (plugin.actions ?? []).flatMap(action =>
          (action.fields ?? []).map(field => [
            `${action.id}:${field.key}`,
            field.default ?? ''
          ])
        )
      )
    )
  }, [plugin.actions, plugin.id, plugin.pages])

  const paramsFor = (action: SetupAction) =>
    Object.fromEntries(
      (action.fields ?? []).map(field => [
        field.key,
        values[`${action.id}:${field.key}`] ?? field.default ?? ''
      ])
    )
  const run = async (action: SetupAction) => {
    try {
      const task = await startTask({
        pluginID: plugin.id,
        action: action.id,
        confirm: action.confirm ?? false,
        params: paramsFor(action)
      }).unwrap()
      setActiveAction('')
      setMessage(`已开始“${action.label}”，可在右上角操作记录查看进度。`)
      void task
    } catch (reason) {
      setMessage(operationErrorMessage(reason, '插件操作未开始。'))
    }
  }

  return (
    <section className="workspace-content setup-plugin-page">
      <header>
        <div>
          <h1>{plugin.name}</h1>
        </div>
        <small>v{plugin.version}</small>
      </header>
      <div className="setup-plugin-layout">
        <nav aria-label={`${plugin.name} 功能页`}>
          {plugin.pages.map(item => (
            <button
              className={page === item.id ? 'active' : ''}
              key={item.id}
              onClick={() => {
                setPage(item.id)
                setActiveAction('')
              }}
            >
              {item.label}
              <b>›</b>
            </button>
          ))}
        </nav>
        <section className="setup-plugin-workspace">
          <header className="setup-plugin-context">
            <h2>{current?.label}</h2>
            {current?.description && <span>{current.description}</span>}
          </header>
          {!plugin.runnable && (
            <p className="setup-plugin-unavailable">
              当前系统缺少此插件的执行器。
            </p>
          )}
          {visibleActions.length > 0 && (
            <div className="setup-plugin-actions">
              {visibleActions.map(action => {
                const needsEditor = Boolean(
                  action.confirm || action.fields?.length
                )
                if (!needsEditor)
                  return (
                    <section className="setup-plugin-action" key={action.id}>
                      <div className="setup-plugin-action-row">
                        <span>
                          <strong>{action.label}</strong>
                          {action.description && (
                            <small>{action.description}</small>
                          )}
                        </span>
                        <button
                          className="primary-button"
                          disabled={!plugin.runnable || isLoading}
                          onClick={() => void run(action)}
                        >
                          {isLoading ? '启动中…' : action.label}
                        </button>
                      </div>
                    </section>
                  )
                return (
                  <section
                    className={
                      activeAction === action.id
                        ? 'setup-plugin-action active'
                        : 'setup-plugin-action'
                    }
                    key={action.id}
                  >
                    <button
                      className="setup-plugin-action-choice"
                      disabled={!plugin.runnable}
                      onClick={() =>
                        setActiveAction(
                          activeAction === action.id ? '' : action.id
                        )
                      }
                    >
                      <span>
                        <strong>{action.label}</strong>
                        {action.description && (
                          <small>{action.description}</small>
                        )}
                      </span>
                      <b>{activeAction === action.id ? '−' : '+'}</b>
                    </button>
                    {activeAction === action.id && (
                      <div className="setup-plugin-action-editor">
                        {action.fields?.length ? (
                          <div className="setup-plugin-fields">
                            {action.fields.map(field => (
                              <label key={field.key}>
                                {field.label}
                                {field.type === 'select' ? (
                                  <select
                                    value={
                                      values[`${action.id}:${field.key}`] ??
                                      field.default ??
                                      ''
                                    }
                                    onChange={event =>
                                      setValues({
                                        ...values,
                                        [`${action.id}:${field.key}`]:
                                          event.target.value
                                      })
                                    }
                                  >
                                    {(field.options ?? []).map(option => (
                                      <option
                                        key={option.value}
                                        value={option.value}
                                      >
                                        {option.label}
                                      </option>
                                    ))}
                                  </select>
                                ) : (
                                  <input
                                    type={field.type}
                                    value={
                                      values[`${action.id}:${field.key}`] ?? ''
                                    }
                                    onChange={event =>
                                      setValues({
                                        ...values,
                                        [`${action.id}:${field.key}`]:
                                          event.target.value
                                      })
                                    }
                                    placeholder={field.label}
                                  />
                                )}
                              </label>
                            ))}
                          </div>
                        ) : null}
                        <footer>
                          {action.confirm && (
                            <small>此操作会修改本机系统设置。</small>
                          )}
                          <button
                            className="secondary-button"
                            onClick={() => setActiveAction('')}
                          >
                            取消
                          </button>
                          <button
                            className="primary-button"
                            disabled={isLoading}
                            onClick={() => void run(action)}
                          >
                            {isLoading ? '启动中…' : '确认执行'}
                          </button>
                        </footer>
                      </div>
                    )}
                  </section>
                )
              })}
            </div>
          )}
          {message && <p className="setup-plugin-message">{message}</p>}
        </section>
      </div>
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
    <section className="backpack-panel">
      <header>
        <div>
          <p>背包</p>
          <small title={`${root}/packages`}>packages</small>
        </div>
        <div className="backpack-quick-actions">
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
        <p className="catalog-state">正在读取本地插件包…</p>
      ) : items.length ? (
        <div className="backpack-items">
          {items.map(item => (
            <article className={item.valid ? '' : 'invalid'} key={item.path}>
              <button
                type="button"
                className="backpack-open"
                onClick={() => setSelectedName(item.name)}
              >
                <div>
                  <strong>
                    {item.name}
                    {item.version && <em>v{item.version}</em>}
                  </strong>
                  <span>
                    {item.valid
                      ? item.description || '本地 AlemonJS 插件包'
                      : '缺少有效 package.json，暂不能作为插件运行。'}
                  </span>
                  <small title={item.path}>{item.path}</small>
                </div>
                <ChevronRight aria-hidden="true" />
              </button>
            </article>
          ))}
        </div>
      ) : (
        <section className="backpack-empty">
          <strong>暂无插件包</strong>
          <span>
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
  } = useLocalPackageVersionsQuery({ root, package: item.name }, {
    skip: !item.valid || tab !== 'version'
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
  useEffect(() => {
    if (versions?.latest) setVersion(versions.latest)
  }, [versions])
  return (
    <section className="backpack-manager">
      <header>
        <div>
          <button className="text-button backpack-back" onClick={onBack}>
            ‹ 返回背包
          </button>
          <h2>
            {item.name}
            {item.version && <em>v{item.version}</em>}
          </h2>
          <small title={item.path}>{item.path}</small>
        </div>
        <div className="backpack-detail-actions">
          <button
            className="secondary-button"
            onClick={onRefresh}
            title="刷新背包"
          >
            <RefreshCw />
          </button>
          <button
            className="danger-button"
            disabled={busy}
            onClick={() => void onRemove(item.name)}
          >
            卸载
          </button>
        </div>
      </header>
      <nav className="backpack-detail-tabs" aria-label="插件详情">
        <button
          className={tab === 'readme' ? 'selected' : ''}
          onClick={() => setTab('readme')}
        >
          文档
        </button>
        <button
          className={tab === 'config' ? 'selected' : ''}
          onClick={() => setTab('config')}
        >
          配置
        </button>
        <button
          className={tab === 'version' ? 'selected' : ''}
          onClick={() => setTab('version')}
        >
          版本
        </button>
      </nav>
      <div className="backpack-detail-content">
        {!item.valid ? (
          <p className="backpack-manager-note">
            这个目录没有有效的 package.json，因此只能从文件系统修复或移除。
          </p>
        ) : tab === 'readme' ? (
          isReadmeFetching ? (
            <p className="backpack-manager-note">正在读取 README.md…</p>
          ) : readmeError || !readme ? (
            <p className="backpack-manager-note">
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
            <div className="package-config-panel backpack-config-panel">
          <header>
            <div>
              <strong>插件配置</strong>
              <span>
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
          <div className="package-config-fields">
            {data.fields.map(field => (
              <label key={field.name}>
                {field.description || field.name}
                {field.required && <em>必填</em>}
                {field.type === 'boolean' || field.type === 'bool' ? (
                  <select
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
                    value={values[field.name] ?? ''}
                    type={
                      field.type === 'number' || field.type === 'integer'
                        ? 'number'
                        : 'text'
                    }
                    onChange={event =>
                      setValues({ ...values, [field.name]: event.target.value })
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
          <section className="backpack-version-panel">
            <div>
              <strong>{versions.source === 'git' ? 'Git 版本' : 'npm 版本'}</strong>
              <span>
                当前使用 {versions.current || item.version || '未知'}；
                {versions.source === 'git'
                  ? '此插件是 Git 工作区，版本以标签为准。'
                  : '未检测到 Git，使用 npm 已发布版本。'}
              </span>
            </div>
            <div className="backpack-version-controls">
              <select value={version} onChange={event => setVersion(event.target.value)}>
                {versions.versions.map(candidate => (
                  <option key={candidate} value={candidate}>{versions.source === 'npm' ? `v${candidate}` : candidate}</option>
                ))}
              </select>
              <button
                className="primary-button"
                disabled={busy || !version || version === versions.current || version.replace(/^v/, '') === item.version}
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
    <section className="catalog-detail">
      <header>
        <button className="text-button" onClick={onBack}>
          ‹ 返回目录
        </button>
        <span>{group}</span>
      </header>
      <section className="catalog-control">
        <div>
          <h1>{item.name}</h1>
          <p>{item.description || '在线生态目录条目'}</p>
        </div>
        <div className="catalog-control-actions">
          {packageName ? (
            <label>
              {repositoryInstall ? '插件版本' : '版本'}
              <select
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
            <span className="catalog-install-source">
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
        <p className="catalog-version-note">
          该插件仓库没有正式 Release，不能作为可复现的版本安装。
        </p>
      )}
      {repositoryInstall && versionsError && (
        <p className="catalog-version-note">
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
      <section className="catalog-document">
        <header>
          <strong>在线文档</strong>
          {item.url && (
            <a href={item.url} target="_blank" rel="noreferrer">
              在浏览器打开 ↗
            </a>
          )}
        </header>
        {isFetching && <p>正在读取 README.md…</p>}
        {error && <p>在线文档暂时无法读取，请使用右上角链接查看。</p>}
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
      <section className="package-config-panel">
        <p>正在读取包配置声明…</p>
      </section>
    )
  if (error || !data)
    return (
      <section className="package-config-panel">
        <p>该条目没有可读取的 alemonjs.config 声明。</p>
      </section>
    )
  return (
    <section className="package-config-panel">
      <header>
        <div>
          <strong>运行配置</strong>
          <span>保存至 alemon.config.yaml · {data.namespace}.*</span>
        </div>
        <button
          className="primary-button"
          disabled={busy}
          onClick={() => void onSave(data.package, values)}
        >
          保存配置
        </button>
      </header>
      <div className="package-config-fields">
        {data.fields.map(field => (
          <label key={field.name}>
            {field.description || field.name}
            {field.required && <em>必填</em>}
            {field.type === 'boolean' || field.type === 'bool' ? (
              <select
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
          <strong className="text-sm font-semibold text-slate-800">项目扩展配置</strong>
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
          <label className="grid gap-1 text-xs font-semibold text-slate-600" key={field.name}>
            {field.description || field.name}
            {field.required && <em className="not-italic text-amber-700">必填</em>}
            {field.type === 'boolean' || field.type === 'bool' ? (
                <select className="h-9 rounded-md border border-slate-300 bg-white px-2.5 text-sm font-normal text-slate-800 outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
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
                <input className="h-9 rounded-md border border-slate-300 bg-white px-2.5 text-sm font-normal text-slate-800 outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
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
  root,
  loading,
  busy,
  developmentRunning,
  foregroundRunning,
  onRefresh,
  onOpenConsole,
  onRun,
  onSaveLogin,
  onSavePackageConfig,
  developerMode
}: {
  overview?: RuntimeOverview
  root: string
  loading: boolean
  busy: boolean
  developmentRunning: boolean
  foregroundRunning: boolean
  onRefresh: () => void
  onOpenConsole: () => void
  onRun: (action: string, packageName?: string) => Promise<boolean>
  onSaveLogin: (login: string, packageName?: string) => Promise<boolean>
  onSavePackageConfig: (packageName: string, values: Record<string, string>) => Promise<boolean>
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
  const [loadPackageConfig] = useLazyPackageConfigQuery()
  const [loadRuntimePreflight] = useLazyRobotRuntimePreflightQuery()
  const [loginChoice, setLoginChoice] = useState<LoginChoice | null>(null)
  const [connectionConfig, setConnectionConfig] = useState<{
    package: string
    fields: Array<{ name: string; type: string; required: boolean; description: string }>
    values: Record<string, string>
  } | null>(null)
  const [connectionValues, setConnectionValues] = useState<Record<string, string>>({})
  const [loginDialogError, setLoginDialogError] = useState('')
  const [loginDialogBusy, setLoginDialogBusy] = useState(false)
  const [pm2LogsOpen, setPM2LogsOpen] = useState(false)
  const persistentReady = overview?.pm2Configured && overview.hasStartScript
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
  const askStart = async (action: string, label: string, note: string) => {
    try {
      const preflight = await loadRuntimePreflight(root, true).unwrap()
      const platform = (overview?.platforms ?? []).find(item => item.id === preflight.login)
      setCustomLogin(preflight.login)
      setSelectedPlatform(platform?.id ?? '')
      setCustomPackage(platform?.package ?? '')
      setConnectionConfig(null)
      setConnectionValues({})
      if (platform?.installed && platform.package) void loadConnectionConfig(platform.package)
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
      const config = await loadPackageConfig({ root, package: packageName }).unwrap()
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
  const choosePlatform = (id: string) => {
    setSelectedPlatform(id)
    const platform = (overview?.platforms ?? []).find(item => item.id === id)
    if (platform) {
      setCustomLogin(platform.id)
      setCustomPackage(platform.package)
      void loadConnectionConfig(platform.package)
    }
  }
  const saveLoginFromDialog = async () => {
    const login = customLogin.trim()
    if (!login) {
      setLoginDialogError('请选择或填写登录连接，也可以选择无 login 启动。')
      return
    }
    const missing = (connectionConfig?.fields ?? [])
      .filter(field => field.required && !connectionValues[field.name]?.trim())
      .map(field => field.description || field.name)
    if (missing.length) {
      setLoginDialogError(`请填写必填项：${missing.join('、')}`)
      return
    }
    setLoginDialogBusy(true)
    try {
      if (packageTarget && connectionConfig?.fields.length) {
        if (!(await onSavePackageConfig(packageTarget, connectionValues))) return
      }
      if (!(await onSaveLogin(login, packageTarget))) return
      const preflight = await loadRuntimePreflight(root, true).unwrap()
      setLoginChoice(current => current ? { ...current, preflight } : current)
      setLoginDialogError('登录连接已保存。请确认下方启动配置后继续。')
    } catch (reason) {
      setLoginDialogError(operationErrorMessage(reason, '登录连接未保存。'))
    } finally {
      setLoginDialogBusy(false)
    }
  }
  const installSelectedConnection = async () => {
    if (!packageTarget) return
    setLoginDialogBusy(true)
    try {
      if (await onRun('install-connection', packageTarget)) {
        await loadConnectionConfig(packageTarget)
        setLoginDialogError('连接包已安装。请填写下方配置后保存登录连接。')
      }
    } finally {
      setLoginDialogBusy(false)
    }
  }
  const continueStartFromDialog = async (withoutLogin = false) => {
    if (!loginChoice) return
    const preflight = loginChoice.preflight
    if (!withoutLogin && !preflight.login) {
      setLoginDialogError('请先保存登录连接，或明确选择“无 login 启动”。')
      return
    }
    if (!withoutLogin && preflight.missing.length) {
      setLoginDialogError(`启动前仍缺少：${preflight.missing.join('、')}。请在此弹窗完成连接配置后再启动。`)
      return
    }
    // This dialog is the final confirmation point.  Keeping the action here
    // avoids making people re-confirm the exact same startup choice in a
    // second, disconnected dialog.
    setLoginDialogBusy(true)
    try {
      if (await onRun(loginChoice.action)) closeLoginDialog()
    } catch (reason) {
      setLoginDialogError(operationErrorMessage(reason, '启动失败，请查看操作记录。'))
    } finally {
      setLoginDialogBusy(false)
    }
  }
  return (
    <section className="runtime-overview grid max-w-[760px] gap-4">
      <header className="flex items-start justify-between gap-4 border-b border-slate-200 pb-4">
        <div className="grid min-w-0 gap-1">
          <p className="m-0 text-xs font-semibold text-slate-500">运行</p>
          <h1 className="m-0 truncate text-xl font-semibold tracking-tight text-ink-950">{overview?.name || '正在读取项目…'}</h1>
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
        title="运行前配置不完整"
        subtitle="请先填写连接包声明的必填字段。"
        message={validationMessage}
        confirmLabel="知道了"
        cancelLabel="关闭"
        onCancel={() => setValidationMessage('')}
        onConfirm={() => setValidationMessage('')}
      />
      {loginChoice && createPortal(
        <div className="fixed inset-0 z-[96] flex items-center justify-center bg-slate-950/25 p-6" role="presentation">
          <section className="grid max-h-[min(720px,calc(100vh-48px))] w-full max-w-2xl grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_20px_58px_rgb(15_23_42/0.22)]" role="dialog" aria-modal="true" aria-label="启动前登录连接">
            <header className="flex items-center justify-between border-b border-slate-200 px-5 py-4"><div><strong className="text-sm text-ink-950">启动前登录连接</strong><p className="mt-1 text-xs text-slate-500">当前选择：{loginChoice.preflight.login || '未配置 login'}。可直接在这里完成连接配置。</p></div><button className="icon-button" onClick={closeLoginDialog} aria-label="关闭"><X /></button></header>
            <div className="grid min-h-0 gap-4 overflow-auto p-5">
              {loginDialogError && <p className="m-0 rounded-md border border-orange-200 bg-orange-50 px-3 py-2 text-xs leading-5 text-orange-800">{loginDialogError}</p>}
              <section className="rounded-lg border border-slate-200"><header className="border-b border-slate-200 bg-slate-50 px-3 py-2"><strong className="text-xs text-slate-700">选择登录平台</strong></header><div className="grid gap-3 p-3 sm:grid-cols-3"><label className="grid gap-1 text-xs font-semibold text-slate-600">已识别平台<select value={selectedPlatform} onChange={event => choosePlatform(event.target.value)}><option value="">不选择，直接输入</option>{(overview?.platforms ?? []).map(item => <option key={item.id} value={item.id}>{item.label}{item.installed ? ' · 已安装' : ' · 需安装'}</option>)}</select></label><label className="grid gap-1 text-xs font-semibold text-slate-600">登录连接<input value={customLogin} onChange={event => { setSelectedPlatform(''); setCustomLogin(event.target.value) }} placeholder="如 onebot" /></label><label className="grid gap-1 text-xs font-semibold text-slate-600">连接包（可选）<input value={customPackage} onChange={event => { setSelectedPlatform(''); setCustomPackage(event.target.value); setConnectionConfig(null) }} placeholder="如 @alemonjs/onebot" /></label></div>{packageTarget && (!knownPlatform || !knownPlatform.installed) && <footer className="flex items-center justify-between border-t border-slate-200 bg-slate-50 px-3 py-2"><small className="text-xs text-slate-500">{packageTarget} 尚未安装；安装后才能读取它的连接配置。</small><button className="secondary-button" disabled={loginDialogBusy || busy} onClick={() => void installSelectedConnection()}>安装连接包</button></footer>}</section>
              {connectionConfig?.fields.length ? <section className="rounded-lg border border-slate-200"><header className="border-b border-slate-200 bg-slate-50 px-3 py-2"><strong className="text-xs text-slate-700">连接配置</strong><small className="ml-2 text-[11px] text-slate-400">保存到 alemon.config.yaml</small></header><div className="grid gap-3 p-3 sm:grid-cols-2">{connectionConfig.fields.map(field => <label key={field.name} className="grid gap-1 text-xs font-semibold text-slate-600">{field.description || field.name}{field.required && <em className="not-italic text-orange-700">必填</em>}{field.type === 'boolean' || field.type === 'bool' ? <select value={connectionValues[field.name] ?? ''} onChange={event => setConnectionValues({ ...connectionValues, [field.name]: event.target.value })}><option value="">不设置</option><option value="true">开启</option><option value="false">关闭</option></select> : <input type={field.type === 'number' || field.type === 'integer' ? 'number' : 'text'} value={connectionValues[field.name] ?? ''} onChange={event => setConnectionValues({ ...connectionValues, [field.name]: event.target.value })} placeholder={field.name} />}</label>)}</div></section> : packageTarget && knownPlatform?.installed ? <p className="m-0 text-xs text-slate-500">该连接包没有声明可填写的 alemonjs.config，保存 login 后即可启动。</p> : null}
              <section className="rounded-lg border border-slate-200 bg-slate-50 p-3"><strong className="text-xs text-slate-700">本次启动检查</strong><ul className="mt-2 grid gap-1 pl-4 text-xs text-slate-500">{loginChoice.preflight.summary.map(item => <li key={item}>{item}</li>)}</ul></section>
            </div>
            <footer className="flex flex-wrap items-center justify-end gap-2 border-t border-slate-200 px-5 py-3"><button className="text-button" disabled={loginDialogBusy || busy} onClick={() => void continueStartFromDialog(true)}>无 login 启动</button><button className="secondary-button" disabled={loginDialogBusy || busy || !customLogin.trim()} onClick={() => void saveLoginFromDialog()}>保存登录连接</button><button className="primary-button" disabled={loginDialogBusy || busy} onClick={() => void continueStartFromDialog(false)}>确认启动</button></footer>
          </section>
        </div>, document.body)}
      <section className="grid gap-3">
        <section className="overflow-hidden rounded-xl border border-slate-200 bg-white">
          <header className="flex items-center justify-between gap-3 border-b border-slate-200 px-4 py-3">
            <div className="grid gap-1">
              <strong className="text-sm font-semibold text-slate-800">本机运行</strong>
              <span className="text-xs text-slate-500">适合调试。停止后，机器人就会下线。</span>
            </div>
            <button className="secondary-button" onClick={onOpenConsole}>
              日志
            </button>
          </header>
          <div className="divide-y divide-slate-200">
            <section className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
              <div>
                <strong className="block text-sm font-semibold text-slate-700">依赖</strong>
                <span className="block text-xs text-slate-500">运行前会自动检查；有问题时可重新安装。</span>
              </div>
              <button className="text-button" disabled={busy} onClick={() => onRun('dependency-status')}>检查</button>
              <button className="secondary-button" disabled={busy} onClick={() => ask('重新安装依赖', '会根据 package.json 重新安装当前机器人的全部依赖。', () => onRun('install'))}>重新安装</button>
            </section>
            {developerMode && (
              <section className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
                <div>
                  <strong className="block text-sm font-semibold text-slate-700">开发运行</strong>
                  <span className="block text-xs text-slate-500">
                    {developmentRunning
                      ? '正在运行，可随时停止。'
                      : foregroundRunning
                        ? '当前正在前台运行，请先停止前台进程。'
                      : overview?.hasDevScript
                        ? '适合改代码、排查问题。'
                        : '还没有开发命令。'}
                  </span>
                </div>
                {overview?.hasDevScript ? (
                  <button
                    className={developmentRunning ? 'secondary-button' : 'primary-button'}
                    disabled={busy || foregroundRunning}
                    title={foregroundRunning ? '当前目录正在前台运行，请先停止。' : ''}
                    onClick={() => developmentRunning
                      ? ask('停止开发', '会停止当前项目的开发运行。', () => onRun('dev-stop'))
                      : void askStart('dev', '启动开发', '会以开发模式启动，并打开运行日志。')}
                  >
                    {developmentRunning ? '停止开发' : '启动开发'}
                  </button>
                ) : (
                  <button className="secondary-button" disabled={busy} onClick={() => ask('修复开发命令', '会补齐开发所需的运行命令，并保留现有设置。', () => onRun('repair-dev'))}>修复</button>
                )}
              </section>
            )}
            <section className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
              <div>
                <strong className="block text-sm font-semibold text-slate-700">前台运行</strong>
                <span className="block text-xs text-slate-500">
                  {overview?.hasAppScript
                    ? foregroundRunning
                      ? '正在运行，可随时停止。'
                      : developmentRunning
                        ? '当前正在开发运行，请先停止开发进程。'
                        : '直接启动机器人，方便查看输出。'
                    : '还没有前台运行命令。'}
                </span>
              </div>
              {overview?.hasAppScript ? (
                <button
                  className={foregroundRunning ? 'secondary-button' : 'primary-button'}
                  disabled={busy || developmentRunning}
                  title={developmentRunning ? '当前目录正在开发运行，请先停止。' : ''}
                  onClick={() => foregroundRunning
                    ? ask('停止前台运行', '会停止当前项目的前台运行。', () => onRun('app-stop'))
                    : void askStart('app', '启动前台', '会直接启动机器人，并打开运行日志。')}
                >
                  {foregroundRunning ? '停止运行' : '启动前台'}
                </button>
              ) : developerMode ? (
                <button className="secondary-button" disabled={busy} onClick={() => ask('修复前台运行', '会补齐前台运行所需的命令。', () => onRun('repair-dev'))}>修复</button>
              ) : <small>还没有可直接运行的命令。</small>}
            </section>
          </div>
        </section>
        <section className="overflow-hidden rounded-xl border border-slate-200 bg-white">
          <header className="flex items-center justify-between gap-3 border-b border-slate-200 px-4 py-3">
            <div className="grid gap-1">
              <strong className="text-sm font-semibold text-slate-800">后台运行</strong>
              <span className="text-xs text-slate-500">{persistentReady ? '适合长期在线；关闭本窗口后仍会继续运行。' : '还未准备好，修复后可长期在线。'}</span>
            </div>
            <button
              className="primary-button"
              disabled={busy || !persistentReady}
              title={
                persistentReady ? '' : '补齐 start 脚本和 PM2 配置后可使用。'
              }
              onClick={() =>
                void askStart(
                  'pm2',
                  '启动服务',
                  '会在后台启动机器人；如已运行，将应用最新设置。'
                )
              }
              >
              启动服务
            </button>
          </header>
          <div className="flex flex-wrap items-center gap-2 px-4 py-3">
            <button
              className="secondary-button"
              disabled={busy || !overview?.pm2Configured}
              title={
                overview?.pm2Configured ? '' : '当前目录没有 pm2.config.cjs。'
              }
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
              disabled={busy || !overview?.pm2Configured}
              title={overview?.pm2Configured ? '' : '当前目录没有 pm2.config.cjs。'}
              onClick={() => ask('重启服务', '会停止并重新启动后台运行的机器人。', () => onRun('pm2-restart'))}
            >
              重启
            </button>
            <button
              className="secondary-button"
              disabled={busy || !overview?.pm2Configured}
              title={overview?.pm2Configured ? '' : '当前目录没有 pm2.config.cjs。'}
              onClick={() => ask('更新服务', '会尽量不中断服务地应用最新设置。', () => onRun('pm2-reload'))}
            >
              重载
            </button>
            {!persistentReady && (
              <button
                className="secondary-button"
                disabled={busy}
                onClick={() =>
                  ask(
                    '修复后台运行',
                    '会补齐后台运行所需的设置和依赖。',
                    () => onRun('repair-pm2')
                  )
                }
              >
                修复
              </button>
            )}
            <div className="runtime-persistent-utilities">
              <button className="text-button" disabled={busy} onClick={() => onRun('pm2-status')}>状态</button>
              <button className="text-button" disabled={busy || !overview?.pm2Configured} onClick={() => setPM2LogsOpen(true)}>日志</button>
            <button
              className="text-button danger-action"
              disabled={busy || !overview?.pm2Configured}
              onClick={() => ask('移除后台服务', '会移除后台运行记录；以后仍可再次启动。', () => onRun('pm2-delete'))}
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
    </section>
  )
}
function RobotPluginWebView({
  root,
  entries,
  tabs,
  activeTabKey,
  onActivate,
  onClose,
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
    <section className="workspace-content robot-plugin-webview">
      <header>
        <div>
          <div className="robot-plugin-webview-tabs">{tabs.map(tab => <button className={tab.key === activeTabKey ? 'active' : ''} key={tab.key} onClick={() => onActivate(tab.key)} title={tab.package}>{tab.title}<span onClick={event => { event.stopPropagation(); onClose(tab.key) }}>×</span></button>)}</div>
        </div>
        <div className="robot-plugin-webview-actions">
          <strong>{active?.title}</strong>
        </div>
      </header>
      <div className="robot-plugin-webview-frame">{tabs.map(tab => { const entry = entries.find(item => item.id === tab.entryID); return entry ? <PluginWebViewFrame key={tab.key} root={root} entry={entry} active={tab.key === activeTabKey} /> : null })}</div>
    </section>
  )
}

function PluginWebViewFrame({ root, entry, active }: { root: string; entry: RobotWebView; active: boolean }) {
  const [reloadKey, setReloadKey] = useState(0)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [apiError, setApiError] = useState('')
  const frameRef = useRef<HTMLIFrameElement>(null)
  const loadedRef = useRef(false)
  const apiErrorRef = useRef('')
  const rootToken = btoa(String.fromCharCode(...new TextEncoder().encode(root))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '')
  const pluginHost = `r-${rootToken.slice(0, 20).toLowerCase()}.localhost`
  const source = `${window.location.protocol}//${pluginHost}${window.location.port ? `:${window.location.port}` : ''}/api/v1/robot/webview/${rootToken}/${entry.id}/`
  useEffect(() => {
    const origin = new URL(source).origin
    const forward = (event: MessageEvent) => {
      if (event.origin !== origin || event.source !== frameRef.current?.contentWindow) return
      const message = event.data as { source?: string; type?: string; value?: { status?: number; message?: string } }
      if (message?.source !== 'albs-webview') return
      if (message.type === 'ready') {
        frameRef.current?.contentWindow?.postMessage({ source: 'albs-parent', value: { type: 'theme', data: document.documentElement.dataset.theme ?? 'light' } }, origin)
        return
      }
      if (message.type === 'api-error') {
        const status = message.value?.status
        const next = message.value?.message || (status === 502 || status === 503
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
  useEffect(() => { loadedRef.current = false; apiErrorRef.current = ''; setLoading(true); setLoadError(''); setApiError(''); const timer = window.setTimeout(() => { if (!loadedRef.current) { setLoading(false); setLoadError('页面加载超时。请确认插件正在正常安装，并检查插件的 Web 页面是否完整。') } }, 15_000); return () => window.clearTimeout(timer) }, [source, reloadKey])
  const reload = () => { setReloadKey(value => value + 1) }
  return <div className={`robot-plugin-webview-instance ${active ? 'active' : ''}`}>
    {loading && active && <span>正在加载 {entry.name}…</span>}
    {apiError && active && <div className="robot-plugin-webview-api-error" role="status"><span>{apiError}</span><button className="icon-button" onClick={() => { apiErrorRef.current = ''; setApiError('') }} aria-label="关闭接口错误提示" title="关闭"><X /></button></div>}
    {loadError && active && <div className="robot-plugin-webview-error"><strong>无法打开插件页面</strong><p>{loadError}</p><button className="secondary-button" onClick={reload}><RefreshCw />重新加载</button></div>}
    <button className="icon-button robot-plugin-webview-reload" onClick={reload} aria-label="重新加载插件页面" title="重新加载"><RefreshCw /></button>
    <iframe ref={frameRef} key={reloadKey} src={source} title={`${entry.name} 插件页面`} sandbox="allow-scripts allow-same-origin allow-forms allow-popups allow-downloads" referrerPolicy="no-referrer" onLoad={() => { loadedRef.current = true; setLoading(false); setLoadError('') }} onError={() => { loadedRef.current = true; setLoading(false); setLoadError('浏览器无法载入此插件页面。请重新加载，或确认插件的 dist 文件已完整安装。') }} />
  </div>
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
    <aside className="control-dock flex min-h-0 flex-col gap-3" aria-label="目录操作">
      <section className="control-card overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm">
        <header className="flex items-center justify-between gap-3 border-b border-slate-200 px-3.5 py-3">
          <div className="grid min-w-0 gap-1">
            <span className="text-[11px] font-medium text-slate-400">当前机器人</span>
            <strong className="truncate text-sm font-semibold text-slate-800">{project?.name ?? '未选择目录'}</strong>
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
        <div className="grid gap-1 p-2">
          {directoryActions
            .filter(item => developerMode || item.id !== 'build')
            .map(item => (
              <button
                className={cn('flex min-h-9 items-center gap-2 rounded-md px-2.5 text-left text-sm font-semibold transition', activePrimary === item.id ? 'bg-brand-50 text-brand-700' : 'text-slate-600 hover:bg-slate-50 hover:text-slate-900')}
                onClick={() => selectPrimary(item)}
                key={item.id}
              >
                <i className="inline-flex size-4 items-center justify-center not-italic">{item.icon}</i>
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
                  className={cn('flex min-h-8 items-center justify-between rounded-md px-2.5 text-xs font-semibold transition', activeSecondary === item.id ? 'bg-brand-50 text-brand-700' : 'text-slate-500 hover:bg-slate-50 hover:text-slate-800')}
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
          <footer className="flex gap-2 border-t border-slate-200 px-3 py-2" title={project.path}>
            <button
              className="icon-button size-8 p-0"
              onClick={onOpenConsole}
              aria-label="查看运行终端"
              title="查看运行终端"
            >
              <Terminal className="size-4" />
            </button>
            <button className="icon-button size-8 p-0" onClick={onOpenAI} aria-label="打开编程对话" title="编程对话"><Bot className="size-4" /></button>
          </footer>
        )}
      </section>
      {webViews.length > 0 && (
        <section
          className="grid gap-2"
          aria-label="机器人插件 Web 页面"
        >
          {webViews.map(item => (
            <button
              className={cn('flex min-h-10 items-center gap-2 rounded-lg border px-3 text-left text-xs font-semibold transition', item.id === activeWebViewID ? 'border-brand-200 bg-brand-50 text-brand-700' : 'border-slate-200 bg-white text-slate-600 hover:bg-slate-50')}
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
  useEffect(() => {
    if (!open || !root) return
    void load(root)
    const timer = window.setInterval(() => {
      void load(root, true)
    }, 900)
    return () => window.clearInterval(timer)
  }, [load, open, root])
  if (!open) return null
  const message = error
    ? operationErrorMessage(error, '无法读取当前目录的运行终端信息。')
    : (data?.output ?? '')
  return (
    <div className="readonly-console-backdrop" role="presentation">
      <section
        className="readonly-console"
        role="dialog"
        aria-modal="true"
        aria-label="运行终端"
      >
        <header>
          <div>
            <Terminal />
            <strong>运行终端</strong>
            <small>实时显示开发模式过程 · 不支持输入命令</small>
          </div>
          <div>
            <button
              className="icon-button"
              disabled={isFetching}
              onClick={() => void load(root)}
              aria-label="刷新运行终端"
              title="刷新"
            >
              <RefreshCw />
            </button>
            <button
              className="icon-button"
              onClick={onClose}
              aria-label="关闭运行终端"
              title="关闭"
            >
              <X />
            </button>
          </div>
        </header>
        <pre>{isFetching && !message ? '正在读取当前目录…' : message}</pre>
      </section>
    </div>
  )
}

function PM2LogsPanel({ open, root, onClose }: { open: boolean; root: string; onClose: () => void }) {
  const [page, setPage] = useState(1)
  const [data, setData] = useState<{ output: string; page: number; hasOlder: boolean } | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const load = useCallback(async (targetPage: number) => {
    setLoading(true)
    try {
      const response = await fetch(`/api/v1/robot/pm2-logs?${new URLSearchParams({ root, page: String(targetPage) })}`)
      const result = await response.json() as { output?: string; page?: number; hasOlder?: boolean; error?: string }
      if (!response.ok) throw new Error(result.error || '无法读取 PM2 日志。')
      setData({ output: result.output ?? 'PM2 暂无可读取的日志。', page: result.page ?? targetPage, hasOlder: Boolean(result.hasOlder) })
      setError('')
    } catch (reason) { setError(operationErrorMessage(reason, '无法读取 PM2 日志。')) } finally { setLoading(false) }
  }, [root])
  useEffect(() => { if (open) setPage(1) }, [open])
  useEffect(() => { if (open && root) void load(page) }, [load, open, page, root])
  if (!open) return null
  return <div className="readonly-console-backdrop" role="presentation"><section className="readonly-console pm2-log-panel" role="dialog" aria-modal="true" aria-label="PM2 日志"><header><div><Terminal /><strong>PM2 运行日志</strong><small>默认显示最新一页；每页 120 行，只能查看。</small></div><div><button className="icon-button" disabled={loading} onClick={() => void load(page)} aria-label="刷新 PM2 日志" title="刷新"><RefreshCw /></button><button className="icon-button" onClick={onClose} aria-label="关闭 PM2 日志" title="关闭"><X /></button></div></header><pre>{loading && !data ? '正在读取最新 PM2 日志…' : error || data?.output || '暂无日志。'}</pre><footer className="pm2-log-pagination"><button className="secondary-button" disabled={loading || page <= 1} onClick={() => setPage(current => current - 1)}>更新一页</button><span>第 {data?.page ?? page} 页{page === 1 ? ' · 最新' : ''}</span><button className="secondary-button" disabled={loading || !data?.hasOlder} onClick={() => setPage(current => current + 1)}>更早一页</button></footer></section></div>
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
    <div className="inline-flex rounded-md bg-slate-100 p-1" aria-label="配置编辑模式">
      <button
        className={cn('rounded px-3 py-1.5 text-xs font-semibold transition', active === 'visual' ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-500 hover:text-slate-800')}
        onClick={onVisual}
      >
        表单
      </button>
      <button className={cn('rounded px-3 py-1.5 text-xs font-semibold transition', active === 'text' ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-500 hover:text-slate-800')} onClick={onText}>
        文本
      </button>
    </div>
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
      <textarea className="min-h-[420px] w-full resize-y rounded-lg border border-slate-300 bg-white p-3 font-mono text-xs leading-5 text-slate-800 outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
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
  const needsPermission = failed && /没有权限|访问权限|permission denied|eacces/i.test(output)
  return (
    <aside
      className={`robot-output ${failed ? 'failed' : 'completed'}`}
      aria-live="polite"
      aria-label="最近操作结果"
    >
      <header>
        <div>
          <i>{failed ? '!' : '✓'}</i>
          <strong>{needsPermission ? '需要访问授权' : failed ? '操作未完成' : '操作已完成'}</strong>
        </div>
        <button onClick={onClose} aria-label="关闭操作结果">
          ×
        </button>
      </header>
      <pre>{output}</pre>
      <small>{needsPermission ? '授权完成后，请回到这里重新执行本次操作。' : '完整记录可在右上角的任务按钮中查看。'}</small>
    </aside>
  )
}
function GitReleasePanel({
  root,
  busy,
  version,
  confirmed,
  onVersionChange,
  onConfirm,
  onInitialize,
  onRun
}: {
  root: string
  busy: boolean
  version: string
  confirmed: boolean
  onVersionChange: (value: string) => void
  onConfirm: () => void
  onInitialize: (values: {
    authorName: string
    authorEmail: string
    repository: string
    message: string
  }) => Promise<boolean>
  onRun: () => void
}) {
  type GitStatus = {
    root?: string
    packageName?: string
    packageVersion?: string
    packageManager?: string
    repository?: string
    branch?: string
    remoteBranch?: string
    remoteReachable?: boolean
    remoteAdvice?: string
    suggestedVersion?: string
    tags?: string[]
    commits?: string[]
    gitReady?: boolean
    checks?: string[]
    issues?: string[]
  }
  const {
    data,
    isFetching: loading,
    error,
    refetch
  } = useGitStatusQuery(root, { skip: !root })
  const [initializing, setInitializing] = useState(false)
  const [gitInit, setGitInit] = useState({
    authorName: '',
    authorEmail: '',
    repository: '',
    message: 'chore: initialize project'
  })
  const status = error
    ? { issues: ['无法读取 Git 发布状态。'] }
    : (data as GitStatus | undefined)
  const refresh = () => {
    void refetch()
  }
  const issues = status?.issues ?? []
  const blockingIssues = issues.filter(item => !item.startsWith('尚未发现 lib'))
  const ready = !loading && blockingIssues.length === 0
  const tags = status?.tags ?? []
  const commits = status?.commits ?? []
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
  return (
    <section className="git-release-panel">
      <header className="release-toolbar">
        <span>
          {status?.packageName
            ? `${status.packageName}@${status.packageVersion || '未设置版本'} · ${status.packageManager}`
            : 'GIT 发布'}
        </span>
        <div className="release-toolbar-actions">
          <label className="release-version-field">
            <span>版本</span>
            <input
              value={version || status?.suggestedVersion || ''}
              onChange={event => onVersionChange(event.target.value)}
              placeholder="v0.0.1"
            />
          </label>
          {confirmed && (
            <button className="text-button" onClick={onConfirm}>
              取消
            </button>
          )}
          <button
            className="secondary-button"
            onClick={refresh}
            disabled={loading || busy}
          >
            <RefreshCw />
          </button>
          <button
            className="primary-button release-button"
            disabled={busy || !ready}
            onClick={confirmed ? onRun : onConfirm}
          >
            {busy ? '打包中…' : confirmed ? '确认打包' : '准备打包'}
          </button>
        </div>
      </header>
      {loading ? (
        <p className="publish-state">正在读取所选目录的 Git 状态…</p>
      ) : (
        <>
          <p className={`release-status ${ready ? 'ready' : 'blocked'}`}>
            {ready ? '✓ 发布条件已就绪' : '！ 发布前需要处理以下问题'}
          </p>
          {issues.length > 0 && (
            <section className="release-blockers">
              <ul>
                {issues.map(item => (
                  <li key={item}>{item}</li>
                ))}
              </ul>
              {needsInitialize && (
                <section className="git-init-form">
                  <strong>初始化当前项目仓库</strong>
                  <p>
                    将在所选目录创建独立 Git 仓库，不会修改父目录仓库或全局 Git
                    身份。
                  </p>
                  <div>
                    <label>
                      提交姓名
                      <input
                        value={gitInit.authorName}
                        onChange={event =>
                          setGitInit({
                            ...gitInit,
                            authorName: event.target.value
                          })
                        }
                        placeholder="你的姓名"
                      />
                    </label>
                    <label>
                      提交邮箱
                      <input
                        type="email"
                        value={gitInit.authorEmail}
                        onChange={event =>
                          setGitInit({
                            ...gitInit,
                            authorEmail: event.target.value
                          })
                        }
                        placeholder="name@example.com"
                      />
                    </label>
                    <label>
                      origin（可选）
                      <input
                        value={gitInit.repository}
                        onChange={event =>
                          setGitInit({
                            ...gitInit,
                            repository: event.target.value
                          })
                        }
                        placeholder="https://github.com/owner/repo.git"
                      />
                    </label>
                    <label>
                      首个提交
                      <input
                        value={gitInit.message}
                        onChange={event =>
                          setGitInit({
                            ...gitInit,
                            message: event.target.value
                          })
                        }
                      />
                    </label>
                  </div>
                  <button
                    className="primary-button"
                    disabled={
                      busy ||
                      initializing ||
                      !gitInit.authorName.trim() ||
                      !gitInit.authorEmail.trim()
                    }
                    onClick={() => void submitInitialize()}
                  >
                    {initializing ? '正在初始化…' : '确认初始化 Git'}
                  </button>
                </section>
              )}
            </section>
          )}
          <section className="release-records" hidden>
            <details className="release-history">
              <summary>
                release commits <small>{commits.length} 条</small>
              </summary>
              <div>
                <p>
                  这是已推送到 <code>release</code> 分支的发布提交；每次 Git
                  打包会新增一条。
                </p>
                {commits.length ? (
                  <ol>
                    {commits.map(item => (
                      <li key={item}>
                        <code>{item}</code>
                      </li>
                    ))}
                  </ol>
                ) : (
                  <p className="release-history-empty">
                    尚未创建 release 发布提交。
                  </p>
                )}
              </div>
            </details>
            <details className="release-history">
              <summary>
                Tags <small>{tags.length} 个</small>
              </summary>
              <div>
                <p>
                  Tag 是不可覆盖的发布版本标记，例如 <code>v0.0.666</code>
                  ；它用于定位、下载或回退到对应版本。
                </p>
                {tags.length ? (
                  <div className="release-tags">
                    {tags.map(item => (
                      <code key={item}>{item}</code>
                    ))}
                  </div>
                ) : (
                  <p className="release-history-empty">尚未创建发布 Tag。</p>
                )}
              </div>
            </details>
          </section>
        </>
      )}
    </section>
  )
}
void GitReleasePanel

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
  const [phase, setPhase] = useState<'source' | 'building' | 'artifacts' | 'confirm' | 'published'>('source')
  const [session, setSession] = useState<BuildSession | null>(null)
  const [artifacts, setArtifacts] = useState<string[]>([])
  const [expandedArtifacts, setExpandedArtifacts] = useState<string[]>([])
  const [requestError, setRequestError] = useState('')
  const [result, setResult] = useState<PublishResult | null>(null)
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
  const selectedBranch = branches.find(item => item.name === sourceBranch) ?? branches.find(item => item.name === status?.branch) ?? branches[0]
  const targetReleaseBranch = selectedBranch?.name === status?.remoteBranch ? 'release' : `${(selectedBranch?.name || 'source').replace(/[\s/]+/g, '-')}-release`
  const commits = selectedBranch?.commits ?? status?.sourceCommits ?? emptyGitCommits
  useEffect(() => {
    if (!branches.some(item => item.name === sourceBranch)) setSourceBranch(status?.branch || branches[0]?.name || '')
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
    const payload = await response.json().catch(() => ({})) as T & { error?: string }
    if (!response.ok) throw new Error(payload.error || '请求失败，请稍后重试。')
    return payload
  }
  const prepareBuild = async () => {
    if (!selectedBranch?.name || !sourceCommit) return
    setPhase('building')
    setRequestError('')
    setResult(null)
    try {
      const next = await post<BuildSession>('/api/v1/publish/git/prepare', { root, branch: selectedBranch.name, commit: sourceCommit })
      setSession(next)
      setArtifacts(['lib', 'dist', 'README.md'].filter(item => next.files.includes(item)))
      setPhase('artifacts')
    } catch (err) {
      setRequestError(err instanceof Error ? err.message : '构建失败，请重新构建。')
      setPhase('source')
    }
  }
  const publish = async () => {
    if (!session || !artifacts.length) return
    setRequestError('')
    try {
      const next = await post<PublishResult>('/api/v1/publish/git/publish', { sessionId: session.sessionId, version, artifacts, confirm: true })
      setResult(next)
      setPhase('published')
    } catch (err) {
      setRequestError(err instanceof Error ? err.message : '发布失败，请检查日志后重试。')
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
    return { directories, children, descendants, top: files.filter(path => !path.includes('/')) }
  }, [session])
  const selectedArtifacts = useMemo(() => new Set(artifacts), [artifacts])
  const descendantFiles = (item: string) => artifactIndex.descendants.get(item) ?? []
  const isDirectory = (item: string) => artifactIndex.directories.has(item)
  const artifactSelected = (item: string) => {
    const leaves = descendantFiles(item)
    return leaves.length > 0 && leaves.every(leaf => {
      const parts = leaf.split('/')
      return parts.some((_, index) => selectedArtifacts.has(parts.slice(0, index + 1).join('/')))
    })
  }
  const toggleArtifact = (item: string) => {
    setArtifacts(current => current.includes(item) ? current.filter(value => value !== item) : [...current, item])
  }
  return (
    <section className="git-release-panel">
      <header className="release-toolbar">
        <span>
          {status?.packageName
            ? `${status.packageName}@${status.packageVersion || '未设置版本'} · ${status.packageManager}`
            : 'GIT 发布'}
        </span>
        <div className="release-toolbar-actions">
          {(phase === 'artifacts' || phase === 'confirm') && <button className="secondary-button" onClick={() => setPhase(phase === 'confirm' ? 'artifacts' : 'source')}>上一步</button>}
          <button
            className="secondary-button"
            onClick={refresh}
            disabled={loading || busy}
          >
            <RefreshCw />
          </button>
          <button
            className="primary-button release-button"
            disabled={busy || loading || phase === 'building' || (phase === 'source' && !ready) || (phase === 'artifacts' && !artifacts.length)}
            onClick={() => {
              if (phase === 'source') void prepareBuild()
              else if (phase === 'artifacts') setPhase('confirm')
              else if (phase === 'confirm') void publish()
              else if (phase === 'published') refresh()
            }}
          >
            {busy || phase === 'building' ? '构建中…' : phase === 'source' ? '开始构建' : phase === 'artifacts' ? '继续确认' : phase === 'confirm' ? '确认发布' : '重新开始'}
          </button>
        </div>
      </header>
      {loading ? (
        <p className="publish-state">正在读取所选目录的 Git 状态…</p>
      ) : (
        <>
          {phase === 'source' && <section className="release-source-card">
            <div>
              <strong>1. 选择源码提交</strong>
              <p>只会构建这次已提交的代码，不会包含你还没提交的本地修改。</p>
            </div>
            <label className="release-field">
              源码分支{' '}
              <select value={selectedBranch?.name || ''} disabled={phase !== 'source'} onChange={event => setSourceBranch(event.target.value)}>{branches.map(item => <option key={item.name} value={item.name}>{item.name}</option>)}</select>
            </label>
            <label className="release-field">
              发布目标{' '}
              <input value={targetReleaseBranch} readOnly />
            </label>
            <label className="release-field release-commit-field">
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
          </section>}
          {phase === 'confirm' && <section className="release-source-card compact">
            <div>
              <strong>2. 设置发布版本</strong>
              <p>发布时会创建不可覆盖的 Git Tag，并推送到 {session?.target || targetReleaseBranch}。</p>
            </div>
            <label>
              版本{' '}
              <input
                value={version || status?.suggestedVersion || ''}
                onChange={event => onVersionChange(event.target.value)}
                placeholder="v0.0.1"
              />
            </label>
          </section>}
          {phase === 'building' && <section className="release-source-card"><div><strong>正在构建</strong><p>正在隔离目录中安装依赖并执行 build。完成前不能选择产物。</p></div></section>}
          {session && phase === 'artifacts' && (
            <section className="release-source-card release-artifact-card">
              <div><strong>3. 选择最终产物</strong><p>以下是本次构建实际生成的可发布文件。默认全选；依赖、隐藏文件和 package.json 不会显示。</p></div>
              <div className="release-artifacts">{artifactIndex.top.map(item => <div className="release-artifact-tree" key={item}><label className={artifactSelected(item) ? 'selected' : ''}><input type="checkbox" checked={artifactSelected(item)} onChange={() => toggleArtifact(item)} />{isDirectory(item) && <button type="button" className="artifact-expand" onClick={() => setExpandedArtifacts(current => current.includes(item) ? current.filter(value => value !== item) : [...current, item])}><ChevronRight className={expandedArtifacts.includes(item) ? 'expanded' : ''} /></button>}<span>{item}</span></label>{expandedArtifacts.includes(item) && (artifactIndex.children.get(item) ?? []).map(child => <label className={`artifact-child ${artifactSelected(child) ? 'selected' : ''}`} key={child}><input type="checkbox" checked={artifactSelected(child)} onChange={() => toggleArtifact(child)} /><span>{child.slice(item.length + 1)}</span></label>)}</div>)}</div>
              <p className="release-artifact-count">已选择 {artifacts.length} 项，将发布到 <code>{session.target}</code>。</p>
            </section>
          )}
          {phase === 'source' && <p className={`release-status ${ready ? 'ready' : 'blocked'}`}>
            {ready ? '✓ 可以从所选提交开始构建' : '！ 发布前需要处理以下问题'}
          </p>}
          {phase === 'source' && blockingIssues.length > 0 && (
            <section className="release-blockers">
              <ul>
                {blockingIssues.map(item => (
                  <li key={item}>{item}</li>
                ))}
              </ul>
              {needsInitialize && (
                <button className="primary-button" disabled={busy || initializing} onClick={() => setGitInitOpen(true)}>填写 Git 信息并初始化</button>
              )}
              {status?.repository && <p className="release-remote-hint">远程仓库：<code>{status.repository}</code>{status.remoteAdvice ? ` · ${status.remoteAdvice}` : ''}</p>}
            </section>
          )}
          {phase === 'confirm' && session && (
            <p className="release-confirmation">
              即将把 {artifacts.length} 项构建产物发布到 <code>{session.target}</code>，并创建标签 <code>{version || status?.suggestedVersion}</code>。
            </p>
          )}
          {requestError && <p className="release-status blocked">！ {requestError}</p>}
          {phase === 'published' && result?.output && <pre className="release-result">{result.output}</pre>}
        </>
      )}
      <GitInitializeDialog open={gitInitOpen} values={gitInit} busy={busy || initializing} onClose={() => setGitInitOpen(false)} onChange={setGitInit} onConfirm={async () => { await submitInitialize(); setGitInitOpen(false) }} />
    </section>
  )
}

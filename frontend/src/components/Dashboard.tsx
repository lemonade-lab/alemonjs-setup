import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import Markdown from 'markdown-to-jsx'
import { Archive, ArrowLeft, ArrowRight, Check, ChevronRight, ClipboardList, Eye, EyeOff, Folder, Link, Network, Package, Play, Plug, Plus, RefreshCw, Search, Send, Settings, Terminal, X } from 'lucide-react'
import { RobotConfigForm } from './RobotConfigForm'
import { NpmrcConfigForm } from './NpmrcConfigForm'
import { NpmPublishPanel } from './NpmPublishPanel'
import { PackageManifestPanel } from './PackageManifestPanel'
import { SetupUpdateButton } from './SetupUpdateButton'
import { ErrorNotice } from './ErrorNotice'
import { useCatalogDocumentQuery, useCatalogPackageConfigQuery, useCatalogQuery, useCatalogVersionsQuery, useGitStatusQuery, useInitializeGitMutation, useLazyRobotConsoleQuery, useLazyRobotFileQuery, useLazyRobotProjectQuery, useLocalPackagesQuery, useRobotTasksQuery, useSetSetupPluginEnabledMutation, useSetupPluginsQuery, useStartRobotTaskMutation, useStartSetupPluginTaskMutation, useWritePackageConfigMutation, useWriteRobotFileMutation, type SetupPlugin } from '../store/workspaceApi'
import { addProjects, removeProject as removeWorkspaceProject, selectProject, setDraft } from '../store/workspaceStore'
import { setProject as setGuideProject, type RootState } from '../store/guideStore'

type Check = { id: string; name: string; status: 'ready' | 'missing' | 'warning'; detail: string; suggestion: string }
type CatalogItem = { name: string; description: string; url: string; install: string }
type CatalogGroup = { title: string; items: CatalogItem[] }
type Page = 'robot' | 'build' | 'plugins' | 'connections'
type Section = 'backpack' | 'config' | 'npmrc' | 'actions'
type Project = { id: string; path: string; name: string }
type SystemFeature = string
type Props = { report: { checks: Check[] } | null; checking: boolean; error: string; defaultPage: string; onOpenGuide: () => void; onClearError: () => void; onCheck: () => void; onFix: (check: Check) => void; goals?: unknown; goal?: unknown; onSelect?: (id: string) => void }

const coreFeatureCatalog: Array<{ id: SystemFeature; label: string; icon: ReactNode; status?: string }> = [{ id: 'plugins', label: '插件', icon: <Plug /> }]
const directoryActions: Array<{ id: Section | Page; label: string; icon: ReactNode; kind: 'section' | 'page' }> = [{ id: 'config', label: '配置', icon: <Settings />, kind: 'section' }, { id: 'actions', label: '运行', icon: <Play />, kind: 'section' }, { id: 'connections', label: '连接', icon: <Link />, kind: 'page' }, { id: 'backpack', label: '背包', icon: <Archive />, kind: 'section' }, { id: 'plugins', label: '插件', icon: <Package />, kind: 'page' }, { id: 'build', label: '发布', icon: <Send />, kind: 'page' }]
const emptyGitCommits: Array<{ sha: string; shortSha: string; subject: string; createdAt: string }> = []

function setupPluginIcon(icon?: string) {
  return icon === 'network' ? <Network /> : <Plug />
}

function projectName(path: string) { return path.replace(/\/$/, '').split('/').pop() || path }

// RTK Query rejects with a serialised object rather than an Error. Keep the
// server's explanation intact so a permission problem is never shown as the
// unhelpful generic "操作未完成".
function operationErrorMessage(reason: unknown, fallback: string) {
  if (reason instanceof Error && reason.message) return reason.message
  if (typeof reason === 'string' && reason) return reason
  if (reason && typeof reason === 'object') {
    const value = reason as { data?: { error?: unknown; message?: unknown } | string; error?: unknown; message?: unknown }
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

export function DirectoryPicker({ open, multiple = true, onClose, onSelect }: { open: boolean; multiple?: boolean; onClose: () => void; onSelect: (paths: string[]) => void }) {
  type Directory = { name: string; path: string }
  type DirectoryData = { path: string; parent: string; roots: string[]; directories: Directory[] }
  const [path, setPath] = useState('')
  const [query, setQuery] = useState('')
  const [hidden, setHidden] = useState(false)
  const [data, setData] = useState<DirectoryData | null>(null)
  const [selected, setSelected] = useState<string[]>([])
  const [history, setHistory] = useState<string[]>([])
  const [historyIndex, setHistoryIndex] = useState(-1)

  const visit = (nextPath: string) => {
    if (!nextPath || nextPath === path) return
    setPath(nextPath)
    setSelected([])
    setHistory((entries) => {
      const next = [...entries.slice(0, historyIndex + 1), nextPath]
      setHistoryIndex(next.length - 1)
      return next
    })
  }
  useEffect(() => {
    if (!open) return
    const controller = new AbortController()
    const parameters = new URLSearchParams(path ? { path, hidden: String(hidden) } : { hidden: String(hidden) })
    void fetch(`/api/v1/directories?${parameters}`, { signal: controller.signal })
      .then(async (response) => {
        const body = await response.text()
        if (!response.ok) throw new Error(body || '目录无法读取。')
        return JSON.parse(body) as DirectoryData
      })
      .then((result) => {
        setData(result)
        if (!path) { setPath(result.path); setHistory([result.path]); setHistoryIndex(0) }
      })
      .catch((reason: unknown) => { if (!(reason instanceof DOMException && reason.name === 'AbortError')) setData(null) })
    return () => controller.abort()
  }, [hidden, open, path])
  if (!open) return null
  const items = (data?.directories ?? []).filter((item) => item.name.toLowerCase().includes(query.toLowerCase()))
  const toggleSelection = (itemPath: string) => setSelected((current) => multiple ? (current.includes(itemPath) ? current.filter((entry) => entry !== itemPath) : [...current, itemPath]) : [itemPath])
  const home = data?.roots[0] ?? ''
  const favorites = [{ name: '主目录', path: home }, ...['Desktop', 'Documents', 'Downloads', 'Pictures'].map((name) => ({ name, path: `${home}/${name}` }))]
  const otherRoots = (data?.roots ?? []).filter((root) => root !== home)
  const goHistory = (step: number) => { const target = history[historyIndex + step]; if (target) { setHistoryIndex(historyIndex + step); setPath(target); setSelected([]) } }
  return <div className="directory-picker-backdrop"><section className="directory-picker finder-picker" role="dialog" aria-label="选择目录"><header className="finder-toolbar"><div className="finder-tools"><nav className="finder-navigation" aria-label="目录导航"><button className="icon-button" disabled={historyIndex <= 0 && !data?.parent} onClick={() => historyIndex > 0 ? goHistory(-1) : visit(data?.parent ?? '')} title="后退"><ArrowLeft /></button><button className="icon-button" disabled={historyIndex >= history.length - 1} onClick={() => goHistory(1)} title="前进"><ArrowRight /></button><button className="icon-button" onClick={() => setHidden((value) => !value)} title={hidden ? '隐藏隐藏目录' : '显示隐藏目录'}>{hidden ? <EyeOff /> : <Eye />}</button></nav><small>单击选择，双击打开</small></div><strong>{data?.path?.split('/').filter(Boolean).pop() || '选择目录'}</strong><label className="finder-search"><Search /><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索当前目录" /></label></header><section className="finder-body"><aside className="finder-sidebar"><small>常用</small>{favorites.map((item) => <button className={item.path === data?.path ? 'active' : ''} key={item.path} onClick={() => visit(item.path)}><Folder />{item.name}</button>)}{otherRoots.length > 0 && <><small>位置</small>{otherRoots.map((root) => <button className={root === data?.path ? 'active' : ''} key={root} onClick={() => visit(root)}><Folder />{root.split('/').filter(Boolean).pop() || root}</button>)}</>}</aside><main className="finder-content"><header><span>名称</span><span>种类</span></header><div className="directory-picker-list">{items.map((item) => <button className={selected.includes(item.path) ? 'selected' : ''} key={item.path} onClick={() => toggleSelection(item.path)} onDoubleClick={() => visit(item.path)}><Folder /><span>{item.name}</span><small>文件夹</small></button>)}</div></main></section><footer><span title={data?.path ?? ''}>{data?.path ?? '正在读取目录…'}</span><div><button className="secondary-button" onClick={onClose}>取消</button><button className="primary-button" disabled={!selected.length} onClick={() => onSelect(selected)}>{multiple ? '添加' : '选择'}</button></div></footer></section></div>
}

export function Dashboard({ report, checking, error, defaultPage, onOpenGuide, onClearError, onCheck, onFix }: Props) {
  const [page, setPage] = useState<Page>('robot')
  const [systemFeature, setSystemFeature] = useState<SystemFeature | null>(null)
  const [section, setSection] = useState<Section>('config')
  const [file, setFile] = useState('.npmrc')
  const [output, setOutput] = useState('')
  const [outputFailed, setOutputFailed] = useState(false)
  const [consoleOpen, setConsoleOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [catalogTitle, setCatalogTitle] = useState('')
  const [catalogItem, setCatalogItem] = useState<CatalogItem | null>(null)
  const [configEditor, setConfigEditor] = useState<'visual' | 'text'>('visual')
  const [runMode, setRunMode] = useState<'dependencies' | 'development' | 'background'>('dependencies')
  const [buildMode, setBuildMode] = useState<'manifest' | 'npm' | 'git'>('git')
  const [releaseVersion, setReleaseVersion] = useState('')
  const [gitConfirm, setGitConfirm] = useState(false)
  const [environmentOpen, setEnvironmentOpen] = useState(false)
  const [directoryPickerOpen, setDirectoryPickerOpen] = useState(false)
  const [invalidDirectory, setInvalidDirectory] = useState('')
  const environmentChecked = useRef(false)
  const dispatch = useDispatch()
  const projects = useSelector((state: RootState) => state.workspace.projects) as Project[]
  const activeProjectID = useSelector((state: RootState) => state.workspace.activeProjectID)
  const activeProject = projects.find((item) => item.id === activeProjectID)
  const root = activeProject?.path ?? ''
  const draftKey = `${root}:${file}`
  const content = useSelector((state: RootState) => state.workspace.drafts[draftKey] ?? '')
  const configContent = useSelector((state: RootState) => state.workspace.drafts[`${root}:alemon.config.yaml`] ?? '')
  const setContent = (nextContent: string) => dispatch(setDraft({ key: draftKey, content: nextContent }))
  const catalogKind = page === 'plugins' ? 'apps' : 'environment'
  const { data: catalog = [], isFetching: catalogLoading, error: catalogQueryError } = useCatalogQuery(catalogKind, { skip: page !== 'plugins' && page !== 'connections', refetchOnMountOrArgChange: true })
  const { data: localPackages, isFetching: packagesLoading, error: packagesError, refetch: refetchPackages } = useLocalPackagesQuery(root, { skip: !root || section !== 'backpack' })
  const watchDevelopmentTask = page === 'robot' && section === 'actions' && runMode === 'development'
  const { data: operationTasks = [] } = useRobotTasksQuery(undefined, { skip: !watchDevelopmentTask, pollingInterval: watchDevelopmentTask ? 1200 : 0 })
  const [readRobotFile] = useLazyRobotFileQuery()
  const [validateRobot, { data: projectValidation }] = useLazyRobotProjectQuery()
  const [startRobotTask] = useStartRobotTaskMutation()
  const [writeRobotFile] = useWriteRobotFileMutation()
  const [writePackageConfig] = useWritePackageConfigMutation()
  const [initializeGit] = useInitializeGitMutation()
  const { data: setupPlugins = [] } = useSetupPluginsQuery()
  const catalogError = catalogQueryError ? '在线目录暂时无法读取。' : ''
  const showOutput = (message: string, failed = false) => { setOutput(message); setOutputFailed(failed) }

  useEffect(() => { if (defaultPage === 'robot') setPage('robot') }, [defaultPage])
  useEffect(() => {
    if (report || checking || environmentChecked.current) return
    environmentChecked.current = true
    onCheck()
  }, [checking, onCheck, page, report])
  useEffect(() => { if (!catalogTitle && catalog.length) setCatalogTitle(catalog[0].title) }, [catalog, catalogTitle])
  useEffect(() => { if (root) void validateRobot(root) }, [root, validateRobot])
  useEffect(() => {
    if (!root || section !== 'config') return
    void readRobotFile({ root, file: 'alemon.config.yaml' }, true).unwrap()
      .then((result) => dispatch(setDraft({ key: `${root}:alemon.config.yaml`, content: result.output ?? '' })))
      .catch(() => dispatch(setDraft({ key: `${root}:alemon.config.yaml`, content: '' })))
  }, [dispatch, readRobotFile, root, section])

  async function api(method: string, data: Record<string, string>): Promise<boolean> {
    if (!root) { showOutput('请先在左侧添加机器人目录。', true); return false }
    setBusy(true)
    try {
      if (method === 'GET') { const result = await readRobotFile({ root: data.root, file: data.file }, true).unwrap(); dispatch(setDraft({ key: `${data.root}:${data.file}`, content: result.output ?? '' })); return true }
      if (method === 'PUT') { const result = await writeRobotFile({ root: data.root, file: data.file, content: data.content }).unwrap(); showOutput(result.output ?? '操作完成。'); return true }
      const task = await startRobotTask(data).unwrap()
      if (data.action === 'dev') {
        setOutput('')
        setConsoleOpen(true)
        return true
      }
      showOutput('操作已开始，正在等待完成…')
      for (;;) {
        await new Promise((resolve) => window.setTimeout(resolve, 700))
        const response = await fetch(`/api/v1/robot/tasks?${new URLSearchParams({ id: task.id })}`)
        const current = await response.json() as { status: string; output?: string; error?: string }
        if (current.status === 'running') continue
        if (current.status === 'failed') throw new Error(current.error ?? '操作未完成。')
        showOutput(current.output ?? '操作完成。'); return true
      }
    } catch (reason) { showOutput(operationErrorMessage(reason, '操作未完成，请在右上角任务记录中查看详情。'), true); return false } finally { setBusy(false) }
  }

  async function savePackageConfig(packageName: string, values: Record<string, string>): Promise<boolean> {
    if (!root) return false
    setBusy(true)
    try {
      const result = await writePackageConfig({ root, package: packageName, values }).unwrap()
      showOutput(result.output ?? '机器人运行配置已保存。')
      return true
    } catch (reason) { showOutput(operationErrorMessage(reason, '配置未保存，请检查所选机器人目录。'), true); return false } finally { setBusy(false) }
  }

  async function initializeProjectGit(values: { authorName: string; authorEmail: string; repository: string; message: string }): Promise<boolean> {
    if (!root) return false
    setBusy(true)
    try {
      const result = await initializeGit({ root, ...values }).unwrap()
      showOutput(result.output ?? 'Git 仓库已初始化。')
      return true
    } catch (reason) { showOutput(operationErrorMessage(reason, 'Git 初始化未完成，请检查所选机器人目录。'), true); return false } finally { setBusy(false) }
  }

  async function chooseDirectories() {
    setDirectoryPickerOpen(true)
  }
  async function addSelectedDirectories(paths: string[]) {
    if (!paths.length) return
    const checks = await Promise.all(paths.map(async (path) => {
      try {
        const response = await fetch(`/api/v1/robot/validate?${new URLSearchParams({ root: path })}`)
        const data = await response.json() as { valid?: boolean }
        return { path, valid: response.ok && data.valid === true }
      } catch { return { path, valid: false } }
    }))
    const validPaths = checks.filter((item) => item.valid).map((item) => item.path)
    const invalid = checks.find((item) => !item.valid)?.path
    if (validPaths.length) dispatch(addProjects(validPaths.map((path) => ({ id: path, path, name: projectName(path) }))))
    setDirectoryPickerOpen(false)
    if (invalid) { setInvalidDirectory(invalid); return }
    setPage('robot'); setSection('config'); setOutput('')
  }

  function removeProject(id: string) {
    dispatch(removeWorkspaceProject(id))
    setOutput('')
  }

  function openSection(nextSection: Section) {
    setSection(nextSection); setOutput('')
    if (nextSection === 'npmrc') { setFile('.npmrc'); api('GET', { root, file: '.npmrc' }) }
  }
  function openTextConfig() { setConfigEditor('text'); setFile('alemon.config.yaml'); api('GET', { root, file: 'alemon.config.yaml' }) }
  function selectPage(nextPage: Page) { setSystemFeature(null); setPage(nextPage); setCatalogItem(null); setOutput('') }
  function selectSystemFeature(nextFeature: SystemFeature) { setSystemFeature(nextFeature); setOutput('') }

  const currentCatalog = catalog.find((group) => group.title === catalogTitle) ?? catalog[0]
  const readyCount = report?.checks.filter((item) => item.status === 'ready').length ?? 0
  const robotContent = <section className="workspace-content">
    {section === 'backpack' && <BackpackPanel root={root} items={localPackages?.items ?? []} loading={packagesLoading} failed={Boolean(packagesError)} onRefresh={() => void refetchPackages()} onOpenPlugins={() => selectPage('plugins')} />}
    {section === 'npmrc' && <NpmrcConfigForm content={content} busy={busy} onChange={setContent} onSave={(nextContent) => api('PUT', { root, file: '.npmrc', content: nextContent })} />}
    {section === 'config' && <section className="config-form">{configEditor === 'visual' ? <RobotConfigForm content={configContent} busy={busy} toolbar={<EditorMode active={configEditor} onVisual={() => setConfigEditor('visual')} onText={openTextConfig} />} onSave={(config) => api('PUT', { root, file: 'alemon.config.yaml', content: config })} /> : <FileEditor toolbar={<EditorMode active={configEditor} onVisual={() => setConfigEditor('visual')} onText={openTextConfig} />} content={content} busy={busy} placeholder="配置内容" onChange={setContent} onSave={() => api('PUT', { root, file, content })} />}</section>}
    {section === 'actions' && <RunPanel mode={runMode} busy={busy} developmentRunning={operationTasks.some((item) => item.root === root && item.action === 'dev' && item.status === 'running')} onRun={(action) => api('POST', { root, action })} />}
  </section>

  const catalogContent = <section className="workspace-content">{catalogLoading && <p className="catalog-state">正在读取目录…</p>}{catalogError && <p className="catalog-state">{catalogError}</p>}{!catalogLoading && !catalogError && currentCatalog && (catalogItem ? <CatalogDetail item={catalogItem} group={currentCatalog.title} busy={busy} onBack={() => setCatalogItem(null)} onRun={(action, packageName) => api('POST', { root, action, package: packageName })} onSaveConfig={savePackageConfig} /> : <section className="catalog-items">{currentCatalog.items.map((item) => <button className="catalog-item" key={`${currentCatalog.title}-${item.name}`} onClick={() => setCatalogItem(item)}><span className="catalog-item-copy"><strong>{item.name}</strong><small>{item.description || '查看包说明、安装与配置'}</small></span><ChevronRight /></button>)}</section>)}</section>
  const setupPlugin = setupPlugins.find((item) => systemFeature === `setup:${item.id}`)
  const invalidProject = Boolean(activeProject && projectValidation && !projectValidation.valid)
  const workspace = systemFeature === 'plugins' ? <SystemPluginCenter plugins={setupPlugins} onOpen={(id) => selectSystemFeature(`setup:${id}`)} /> : setupPlugin ? <SetupPluginCenter plugin={setupPlugin} /> : invalidProject ? <InvalidWorkspace project={activeProject!} reason={projectValidation?.error} onRemove={() => removeProject(activeProject!.id)} onChoose={chooseDirectories} /> : activeProject ? <>{page === 'robot' && robotContent}{page === 'build' && <section className="workspace-content build-page">{buildMode === 'manifest' ? <PackageManifestPanel root={root} busy={busy} onSaved={(message) => showOutput(message)} /> : buildMode === 'npm' ? <NpmPublishPanel root={root} busy={busy} onRun={(action, values) => api('POST', { root, action, ...values })} /> : <GitReleasePanelNext root={root} busy={busy} version={releaseVersion} confirmed={gitConfirm} onVersionChange={(value) => { setReleaseVersion(value); setGitConfirm(false) }} onConfirm={() => setGitConfirm((value) => !value)} onInitialize={initializeProjectGit} onRun={(sourceCommit) => api('POST', { root, action: 'git-release', version: releaseVersion, message: sourceCommit, confirm: 'true' })} />}{output && <OperationLog output={output} failed={outputFailed} onClose={() => { setOutput(''); setOutputFailed(false) }} />}</section>}{(page === 'plugins' || page === 'connections') && catalogContent}{page !== 'build' && output && <OperationLog output={output} failed={outputFailed} onClose={() => { setOutput(''); setOutputFailed(false) }} />}</> : <EmptyWorkspace onAdd={chooseDirectories} />

  const environmentReady = report ? `${readyCount}/${report.checks.length}` : '—'
  const environmentWarning = Boolean(report?.checks.some((item) => item.status !== 'ready'))

  return <><main className="guide-shell"><section className="guide-window dashboard-window"><header className="guide-bar dashboard-toolbar"><div className="workspace-title"><a className="workspace-name" href="https://alemonjs.com/" target="_blank" rel="noreferrer">ALEMONJS</a><SetupUpdateButton /></div><div className="header-global-actions"><McpControl /><OperationTasksButton root={root} /><button className={`environment-control ${environmentWarning ? 'warning' : ''}`} onClick={() => { setEnvironmentOpen(true); onCheck() }} disabled={checking} title="查看并检查全局环境"><i>{checking ? '◌' : environmentWarning ? '!' : '✓'}</i><span>环境</span><strong>{checking ? '检查中' : environmentReady}</strong></button><button className="guide-trigger" onClick={onOpenGuide} aria-label="打开引导" title="打开引导">?</button></div></header><EnvironmentPanel open={environmentOpen} report={report} checking={checking} onClose={() => setEnvironmentOpen(false)} onRefresh={onCheck} onFix={onFix} /><DirectoryPicker open={directoryPickerOpen} onClose={() => setDirectoryPickerOpen(false)} onSelect={(paths) => void addSelectedDirectories(paths)} /><section className="console-layout">
    <ProjectRail feature={systemFeature} setupPlugins={setupPlugins} projects={projects} activeID={activeProjectID} onFeature={selectSystemFeature} onAdd={chooseDirectories} onSelect={(id) => { dispatch(selectProject(id)); setSystemFeature(null); setPage('robot'); setSection('config'); setOutput('') }} onRemove={removeProject} />
    <section className="console-page">{workspace}{error && <ErrorNotice message={error} onClose={onClearError} />}{!systemFeature && activeProject && !invalidProject && <ControlCard page={page} section={section} runMode={runMode} project={activeProject} buildMode={buildMode} catalog={catalog} catalogTitle={catalogTitle} onOpenConsole={() => setConsoleOpen(true)} onPage={selectPage} onSection={openSection} onRunMode={(mode) => { setRunMode(mode); openSection('actions') }} onBuildMode={(mode) => { setBuildMode(mode); setGitConfirm(false); setOutput('') }} onCatalog={(title) => { setCatalogTitle(title); setCatalogItem(null) }} />}</section>
  </section></section></main><ReadonlyConsole open={consoleOpen} root={root} onClose={() => setConsoleOpen(false)} />{invalidDirectory && <InvalidDirectoryDialog path={invalidDirectory} onClose={() => setInvalidDirectory('')} onCreate={() => { dispatch(setGuideProject({ destinationMode: 'custom', destination: invalidDirectory })); setInvalidDirectory(''); onOpenGuide() }} />}</>
}

function ProjectRail({ feature, setupPlugins, projects, activeID, onFeature, onAdd, onSelect, onRemove }: { feature: SystemFeature | null; setupPlugins: SetupPlugin[]; projects: Project[]; activeID: string; onFeature: (feature: SystemFeature) => void; onAdd: () => void; onSelect: (id: string) => void; onRemove: (id: string) => void }) {
  const activePlugins = setupPlugins.filter((item) => item.enabled)
  return <aside className="project-rail"><section className="feature-catalog" aria-label="系统功能目录"><header><small>系统</small></header><nav>{coreFeatureCatalog.map((item) => <button className={feature === item.id ? 'active' : ''} key={item.id} onClick={() => onFeature(item.id)}><i>{item.icon}</i><span>{item.label}</span>{item.status && <small>{item.status}</small>}</button>)}</nav>{activePlugins.length > 0 && <><span className="setup-plugin-divider" /><nav>{activePlugins.map((item) => <button className={feature === `setup:${item.id}` ? 'active' : ''} key={item.id} onClick={() => onFeature(`setup:${item.id}`)}><i>{setupPluginIcon(item.navigation.icon)}</i><span>{item.navigation.label || item.name}</span><small>已加载</small></button>)}</nav></>}</section><section className="project-directory"><header><div><strong>机器人目录</strong><span>{projects.length}</span></div><button onClick={onAdd} aria-label="添加机器人目录" title="添加机器人目录"><Plus /></button></header><div className="project-list">{projects.map((project) => <ProjectItem active={project.id === activeID} key={project.id} project={project} onSelect={onSelect} onRemove={onRemove} />)}{!projects.length && <p>添加目录开始管理</p>}</div></section></aside>
}
function ProjectItem({ project, active, onSelect, onRemove }: { project: Project; active: boolean; onSelect: (id: string) => void; onRemove: (id: string) => void }) { const [validate, { data }] = useLazyRobotProjectQuery(); useEffect(() => { void validate(project.path) }, [project.path, validate]); const invalid = data?.valid === false; return <article className={`${active ? 'active ' : ''}${invalid ? 'invalid' : ''}`}><button className="project-select" onClick={() => onSelect(project.id)}><strong>{project.name}{invalid && <em>目录不可用</em>}</strong><small title={project.path}>{invalid ? data.error || project.path : project.path}</small></button><button className="project-remove" onClick={() => onRemove(project.id)} aria-label={`移除 ${project.name}`} title="移除目录"><X /></button></article> }
function McpControl() {
  const [open, setOpen] = useState(false)
  const [transport, setTransport] = useState<'stdio' | 'http'>('stdio')
  const [copied, setCopied] = useState(false)
  const stdioConfig = '{\n  "mcpServers": {\n    "alemonjs-setup": {\n      "command": "albs",\n      "args": ["mcp"]\n    }\n  }\n}'
  const httpCommand = "MCP_TOKEN='请生成高强度随机值' albs --mcp-port 17391 mcp-http"
  const copy = async (value: string) => { try { await navigator.clipboard.writeText(value); setCopied(true); window.setTimeout(() => setCopied(false), 1800) } catch { setCopied(false) } }
  const http = transport === 'http'
  return <div className="mcp-control"><button className="mcp-control-button" onClick={() => setOpen((value) => !value)} aria-expanded={open} title="连接 Codex 或其他本机 AI 客户端"><i>✓</i><span>MCP</span><strong>已开启</strong></button>{open && <section className="mcp-popover" role="dialog" aria-label="连接 MCP"><header><div><strong>连接 Codex / 自定义 MCP</strong><small>两种标准传输均可用</small></div><button onClick={() => setOpen(false)} aria-label="关闭 MCP 说明">×</button></header><p>MCP 让 AI 在你的确认下管理 AlemonJS：读取与修改项目、更新运行配置、启动机器人、构建、打包与发布。它不是网页远程控制；客户端只会连接本机服务。</p><div className="mcp-transport-tabs" role="tablist" aria-label="MCP 接入类型"><button className={!http ? 'active' : ''} role="tab" aria-selected={!http} onClick={() => setTransport('stdio')}>STDIO <small>推荐</small></button><button className={http ? 'active' : ''} role="tab" aria-selected={http} onClick={() => setTransport('http')}>流式 HTTP <small>本机</small></button></div>{http ? <><p>先在终端启动受 Token 保护的服务；随后在 Codex 的“连接至自定义 MCP”中选择<strong> 流式 HTTP</strong>，填写地址与 Bearer Token。</p><dl className="mcp-form-guide"><div><dt>名称</dt><dd>alemonjs-setup</dd></div><div><dt>类型</dt><dd>流式 HTTP</dd></div><div><dt>地址</dt><dd><code>http://127.0.0.1:17391/mcp</code></dd></div><div><dt>认证</dt><dd>Bearer Token：<code>&lt;MCP_TOKEN&gt;</code></dd></div><div><dt>启动命令</dt><dd><code>{httpCommand}</code></dd></div></dl><button className="mcp-copy-button" onClick={() => void copy(httpCommand)}>{copied ? '已复制启动命令' : '复制启动命令'}</button><small className="mcp-note">服务仅绑定 127.0.0.1；不要把地址、Token 或端口转发到局域网和公网。流式 HTTP 兼容 MCP 的 POST 请求，服务不提供独立 SSE 推送流。</small></> : <><p>在 Codex 的“连接至自定义 MCP”中选择<strong> STDIO</strong>，把下列字段逐行填入。Codex 会直接启动本机 <code>albs</code>，无需额外开启端口。</p><dl className="mcp-form-guide"><div><dt>名称</dt><dd>alemonjs-setup</dd></div><div><dt>类型</dt><dd>STDIO</dd></div><div><dt>启动命令</dt><dd><code>albs</code></dd></div><div><dt>参数</dt><dd><code>mcp</code></dd></div><div><dt>环境变量（可选）</dt><dd><code>MCP_ALLOWED_ROOTS=/你的/机器人目录</code></dd></div></dl><button className="mcp-copy-button" onClick={() => void copy(stdioConfig)}>{copied ? '已复制 JSON 配置' : '复制 JSON 配置'}</button><small className="mcp-note">涉及安装、构建、写入或执行脚本时，助手仍必须取得你的本次确认；密钥、.env、.npmrc、Git 元数据与依赖目录不开放。</small></>}</section>}</div>
}
function OperationTasksButton({ root }: { root: string }) { const [open, setOpen] = useState(false); const { data, isFetching } = useRobotTasksQuery(undefined, { skip: !open, pollingInterval: open ? 2000 : 0 }); const tasks = (Array.isArray(data) ? data : []).filter((item) => !root || !item.root || item.root === root); const [selected, setSelected] = useState<string>(''); const current = tasks.find((item) => item.id === selected) ?? tasks[0]; const running = tasks.filter((item) => item.status === 'running').length; const label = (action: string) => action.startsWith('setup:') ? `系统插件 · ${action.split(':').slice(-1)[0]}` : ({ install: '安装依赖', 'dependency-status': '检查依赖', dev: '开发启动', pm2: '后台启动', 'install-package': '安装插件', 'uninstall-package': '卸载插件', 'git-release': 'Git 打包', 'npm-publish': 'NPM 发布' }[action] ?? action); return <div className="operation-tasks"><button className="operation-tasks-button" onClick={() => setOpen((value) => !value)} aria-label="操作记录" title="当前目录操作记录"><ClipboardList />{running > 0 && <b>{running}</b>}</button>{open && <section className="operation-tasks-popover"><header><div><strong>操作记录</strong><small>{root ? '当前机器人与系统操作' : '系统操作'}</small></div><button className="icon-button" onClick={() => setOpen(false)} aria-label="关闭操作记录"><X /></button></header>{isFetching && !tasks.length ? <p>正在读取任务…</p> : !tasks.length ? <p>暂无与当前位置相关的操作记录。</p> : <><div className="task-list">{tasks.slice(0, 12).map((item) => <button key={item.id} className={current?.id === item.id ? 'active' : ''} onClick={() => setSelected(item.id)}><i className={item.status}>{item.status === 'running' ? '◌' : item.status === 'completed' ? '✓' : '!'}</i><span>{label(item.action)}<small>{item.status === 'running' ? '进行中' : item.status === 'failed' ? '需要处理' : '已完成'}</small></span></button>)}</div>{current && <pre className={current.status}>{current.status === 'failed' ? current.error : current.output || '正在执行…'}</pre>}</>}</section>}</div> }
function EnvironmentPanel({ open, report, checking, onClose, onRefresh, onFix }: { open: boolean; report: { checks: Check[] } | null; checking: boolean; onClose: () => void; onRefresh: () => void; onFix: (check: Check) => void }) { if (!open) return null; const checks = report?.checks ?? []; const readyCount = checks.filter((check) => check.status === 'ready').length; return <aside className="environment-panel" role="dialog" aria-label="全局环境详情"><header><strong>{checking ? '正在检查环境…' : checks.length ? `${readyCount}/${checks.length} 已就绪` : '等待检查'}</strong><button onClick={onClose} aria-label="关闭环境详情">×</button></header>{checking && <p className="environment-panel-state">正在读取 Node.js、Git 和系统工具状态。</p>}{!checking && checks.length > 0 && <div className="environment-check-list">{checks.map((check) => <article className={check.status} key={check.id}><i>{check.status === 'ready' ? '✓' : '!'}</i><div><strong>{check.name}</strong><span>{check.detail}</span>{check.status !== 'ready' && check.suggestion && <small>{check.suggestion}</small>}</div>{check.status !== 'ready' && <button className="text-button" onClick={() => onFix(check)}>修复</button>}</article>)}</div>}{!checking && !checks.length && <p className="environment-panel-state">尚未获取检查结果。</p>}<footer><button className="secondary-button" disabled={checking} onClick={onRefresh}>重新检查</button></footer></aside> }
function EmptyWorkspace({ onAdd }: { onAdd: () => void }) { return <section className="workspace-content empty-workspace"><span>◈</span><strong>从左侧添加机器人目录</strong><button className="primary-button" onClick={onAdd}>添加目录</button></section> }
function InvalidWorkspace({ project, reason, onRemove, onChoose }: { project: Project; reason?: string; onRemove: () => void; onChoose: () => void }) { return <section className="workspace-content invalid-workspace"><i>!</i><div><strong>机器人目录不可用</strong><span title={project.path}>{project.path}</span><small>{reason || '目录不存在或不再是可管理的机器人项目。'}</small></div><footer><button className="secondary-button" onClick={onRemove}>移除旧目录</button><button className="primary-button" onClick={onChoose}>重新选择目录</button></footer></section> }
function InvalidDirectoryDialog({ path, onClose, onCreate }: { path: string; onClose: () => void; onCreate: () => void }) { return <div className="invalid-directory-backdrop" role="presentation"><section className="invalid-directory-dialog" role="dialog" aria-modal="true" aria-label="目录不是合法机器人项目"><header><i>!</i><div><strong>所选目录不是合法机器人目录</strong><small title={path}>{path}</small></div><button className="icon-button" onClick={onClose} aria-label="关闭提示"><X /></button></header><p>这里缺少 <code>package.json</code>，因此不能作为已有机器人管理。你可以选择一个已有机器人目录，或在这里创建新机器人。</p><footer><button className="secondary-button" onClick={onClose}>重新选择</button><button className="primary-button" onClick={onCreate}>前往引导创建</button></footer></section></div> }
function SystemPluginCenter({ plugins, onOpen }: { plugins: SetupPlugin[]; onOpen: (id: string) => void }) {
  const [setEnabled, { isLoading }] = useSetSetupPluginEnabledMutation()
  const [message, setMessage] = useState('')
  const toggle = async (plugin: SetupPlugin) => {
    try {
      await setEnabled({ pluginID: plugin.id, enabled: !plugin.enabled }).unwrap()
      setMessage(plugin.enabled ? `已卸载“${plugin.name}”。` : `已启用“${plugin.name}”。`)
    } catch (reason) {
      setMessage(operationErrorMessage(reason, '插件状态未更新。'))
    }
  }
  return <section className="workspace-content setup-plugin-manager"><header><div><h1>插件 <small>{plugins.filter((item) => item.enabled).length}</small></h1></div></header>{plugins.length ? <section className="setup-plugin-cards">{plugins.map((plugin) => <article className={plugin.enabled ? '' : 'disabled'} key={plugin.id}><button className="setup-plugin-open" onClick={() => plugin.enabled && onOpen(plugin.id)} disabled={!plugin.enabled}><i>{setupPluginIcon(plugin.navigation.icon)}</i><div><strong>{plugin.name}</strong><small>v{plugin.version} · {plugin.enabled ? '已启用' : '已卸载'}</small></div>{plugin.enabled && <ChevronRight />}</button><button className={plugin.enabled ? 'secondary-button setup-plugin-toggle' : 'primary-button setup-plugin-toggle'} disabled={isLoading} onClick={() => void toggle(plugin)}>{plugin.enabled ? '卸载' : '启用'}</button></article>)}</section> : <section className="setup-plugin-empty"><strong>暂未发现插件</strong><span>将插件目录放入 plugins 后刷新即可。</span></section>}{message && <p className="setup-plugin-message">{message}</p>}</section>
}
function SetupPluginCenter({ plugin }: { plugin: SetupPlugin }) {
  type SetupAction = NonNullable<SetupPlugin['actions']>[number]
  const [page, setPage] = useState(plugin.pages[0]?.id ?? 'overview')
  const [activeAction, setActiveAction] = useState('')
  const [message, setMessage] = useState('')
  const [values, setValues] = useState<Record<string, string>>({})
  const [startTask, { isLoading }] = useStartSetupPluginTaskMutation()
  const current = plugin.pages.find((item) => item.id === page) ?? plugin.pages[0]
  const visibleActions = (plugin.actions ?? []).filter((action) => !action.page || action.page === current?.id)

  useEffect(() => {
    setPage(plugin.pages[0]?.id ?? 'overview')
    setActiveAction('')
    setMessage('')
    setValues(Object.fromEntries((plugin.actions ?? []).flatMap((action) => (action.fields ?? []).map((field) => [`${action.id}:${field.key}`, field.default ?? '']))))
  }, [plugin.actions, plugin.id, plugin.pages])

  const paramsFor = (action: SetupAction) => Object.fromEntries((action.fields ?? []).map((field) => [field.key, values[`${action.id}:${field.key}`] ?? field.default ?? '']))
  const run = async (action: SetupAction) => {
    try {
      const task = await startTask({ pluginID: plugin.id, action: action.id, confirm: action.confirm ?? false, params: paramsFor(action) }).unwrap()
      setActiveAction('')
      setMessage(`已开始“${action.label}”，可在右上角操作记录查看进度。`)
      void task
    } catch (reason) {
      setMessage(operationErrorMessage(reason, '插件操作未开始。'))
    }
  }

  return <section className="workspace-content setup-plugin-page">
    <header><div><h1>{plugin.name}</h1></div><small>v{plugin.version}</small></header>
    <div className="setup-plugin-layout">
      <nav aria-label={`${plugin.name} 功能页`}>{plugin.pages.map((item) => <button className={page === item.id ? 'active' : ''} key={item.id} onClick={() => { setPage(item.id); setActiveAction('') }}>{item.label}<b>›</b></button>)}</nav>
      <section className="setup-plugin-workspace">
        <header className="setup-plugin-context"><h2>{current?.label}</h2>{current?.description && <span>{current.description}</span>}</header>
        {!plugin.runnable && <p className="setup-plugin-unavailable">当前系统缺少此插件的执行器。</p>}
        {visibleActions.length > 0 && <div className="setup-plugin-actions">
          {visibleActions.map((action) => {
            const needsEditor = Boolean(action.confirm || action.fields?.length)
            if (!needsEditor) return <section className="setup-plugin-action" key={action.id}><div className="setup-plugin-action-row"><span><strong>{action.label}</strong>{action.description && <small>{action.description}</small>}</span><button className="primary-button" disabled={!plugin.runnable || isLoading} onClick={() => void run(action)}>{isLoading ? '启动中…' : action.label}</button></div></section>
            return <section className={activeAction === action.id ? 'setup-plugin-action active' : 'setup-plugin-action'} key={action.id}>
              <button className="setup-plugin-action-choice" disabled={!plugin.runnable} onClick={() => setActiveAction(activeAction === action.id ? '' : action.id)}>
                <span><strong>{action.label}</strong>{action.description && <small>{action.description}</small>}</span><b>{activeAction === action.id ? '−' : '+'}</b>
              </button>
              {activeAction === action.id && <div className="setup-plugin-action-editor">
                {action.fields?.length ? <div className="setup-plugin-fields">{action.fields.map((field) => <label key={field.key}>{field.label}{field.type === 'select' ? <select value={values[`${action.id}:${field.key}`] ?? field.default ?? ''} onChange={(event) => setValues({ ...values, [`${action.id}:${field.key}`]: event.target.value })}>{(field.options ?? []).map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}</select> : <input type={field.type} value={values[`${action.id}:${field.key}`] ?? ''} onChange={(event) => setValues({ ...values, [`${action.id}:${field.key}`]: event.target.value })} placeholder={field.label} />}</label>)}</div> : null}
                <footer>{action.confirm && <small>此操作会修改本机系统设置。</small>}<button className="secondary-button" onClick={() => setActiveAction('')}>取消</button><button className="primary-button" disabled={isLoading} onClick={() => void run(action)}>{isLoading ? '启动中…' : '确认执行'}</button></footer>
              </div>}
            </section>
          })}
        </div>}
        {message && <p className="setup-plugin-message">{message}</p>}
      </section>
    </div>
  </section>
}
function BackpackPanel({ root, items, loading, failed, onRefresh, onOpenPlugins }: { root: string; items: Array<{ name: string; version?: string; description?: string; path: string; valid: boolean }>; loading: boolean; failed: boolean; onRefresh: () => void; onOpenPlugins: () => void }) { return <section className="backpack-panel"><header><div><p>本地插件包</p><h1>背包</h1><small title={`${root}/packages`}>packages</small></div><button className="secondary-button" disabled={loading} onClick={onRefresh}>{loading ? '读取中…' : <RefreshCw />}</button></header>{loading ? <p className="catalog-state">正在读取本地插件包…</p> : items.length ? <div className="backpack-items">{items.map((item) => <article className={item.valid ? '' : 'invalid'} key={item.path}><i>{item.valid ? '▣' : '!'}</i><div><strong>{item.name}{item.version && <em>v{item.version}</em>}</strong><span>{item.valid ? item.description || '本地 AlemonJS 插件包' : '缺少有效 package.json，暂不能作为插件运行。'}</span><small title={item.path}>{item.path}</small></div></article>)}</div> : <section className="backpack-empty"><strong>暂无插件包</strong><span>{failed ? '暂未能读取本地 packages 目录，你仍可从插件页安装。' : '安装后的本地插件包会显示在这里。'}</span><button className="secondary-button" onClick={onOpenPlugins}>前往插件</button></section>}</section> }
function CatalogDetail({ item, group, busy, onBack, onRun, onSaveConfig }: { item: CatalogItem; group: string; busy: boolean; onBack: () => void; onRun: (action: string, packageName: string) => void; onSaveConfig: (packageName: string, values: Record<string, string>) => Promise<boolean> }) {
  const [version, setVersion] = useState('')
  const [configOpen, setConfigOpen] = useState(false)
  const { data: document, isFetching, error } = useCatalogDocumentQuery(item.url, { skip: !item.url })
  const packageName = item.install || (item.name === 'alemonjs' || item.name.startsWith('@alemonjs/') ? item.name : '')
  const repositoryInstall = packageName.startsWith('git+')
  const npmPackage = Boolean(packageName && !repositoryInstall)
  const { data: packageVersions, isFetching: versionsLoading } = useCatalogVersionsQuery(packageName, { skip: !npmPackage })
  const installTarget = version.trim() && npmPackage ? `${packageName}@${version.trim()}` : packageName
  return <section className="catalog-detail"><header><button className="text-button" onClick={onBack}>‹ 返回目录</button><span>{group}</span></header><section className="catalog-control"><div><h1>{item.name}</h1><p>{item.description || '在线生态目录条目'}</p></div><div className="catalog-control-actions">{npmPackage ? <label>版本<select value={version} onChange={(event) => setVersion(event.target.value)} disabled={versionsLoading}><option value="">{versionsLoading ? '读取版本…' : `latest${packageVersions?.latest ? ` · ${packageVersions.latest}` : ''}`}</option>{packageVersions?.versions.map((itemVersion) => <option key={itemVersion} value={itemVersion}>{itemVersion}{itemVersion === packageVersions.latest ? ' · latest' : ''}</option>)}</select></label> : <span className="catalog-install-source">{repositoryInstall ? 'Git安装' : '暂不可安装'}</span>}<button className="primary-button" disabled={busy || !packageName} onClick={() => onRun('install-package', installTarget)}>{busy ? '处理中…' : '安装'}</button><button className="secondary-button" disabled={busy || !packageName || repositoryInstall} title={repositoryInstall ? '仓库安装请按文档卸载' : '卸载当前包'} onClick={() => onRun('uninstall-package', packageName)}>卸载</button><button className="secondary-button" disabled={busy || !item.url} onClick={() => setConfigOpen((open) => !open)}>配置</button></div></section>{configOpen && <PackageConfigPanel source={item.url} busy={busy} onSave={onSaveConfig} />}<section className="catalog-document"><header><strong>在线文档</strong>{item.url && <a href={item.url} target="_blank" rel="noreferrer">在浏览器打开 ↗</a>}</header>{isFetching && <p>正在读取 README.md…</p>}{error && <p>在线文档暂时无法读取，请使用右上角链接查看。</p>}{document && <MarkdownPage markdown={document.markdown} />}</section></section>
}
function PackageConfigPanel({ source, busy, onSave }: { source: string; busy: boolean; onSave: (packageName: string, values: Record<string, string>) => Promise<boolean> }) {
  const { data, isFetching, error } = useCatalogPackageConfigQuery(source, { skip: !source })
  const [values, setValues] = useState<Record<string, string>>({})
  useEffect(() => { if (data) setValues(Object.fromEntries(data.fields.map((field) => [field.name, data.values[field.name] ?? '']))) }, [data])
  if (isFetching) return <section className="package-config-panel"><p>正在读取包配置声明…</p></section>
  if (error || !data) return <section className="package-config-panel"><p>该条目没有可读取的 alemonjs.config 声明。</p></section>
  return <section className="package-config-panel"><header><div><strong>运行配置</strong><span>保存至 alemon.config.yaml · {data.namespace}.*</span></div><button className="primary-button" disabled={busy} onClick={() => void onSave(data.package, values)}>保存配置</button></header><div className="package-config-fields">{data.fields.map((field) => <label key={field.name}>{field.description || field.name}{field.required && <em>必填</em>}{field.type === 'boolean' || field.type === 'bool' ? <select value={values[field.name] ?? ''} onChange={(event) => setValues({ ...values, [field.name]: event.target.value })}><option value="">不设置</option><option value="true">开启</option><option value="false">关闭</option></select> : <input value={values[field.name] ?? ''} type={field.type === 'number' || field.type === 'integer' ? 'number' : 'text'} onChange={(event) => setValues({ ...values, [field.name]: event.target.value })} placeholder={field.name} />}</label>)}</div></section>
}
function MarkdownPage({ markdown }: { markdown: string }) {
  return <article className="markdown-page"><Markdown options={{ forceBlock: true, overrides: { a: { component: ({ href, children, ...props }: { href?: string; children?: ReactNode }) => <a href={href} target="_blank" rel="noreferrer" {...props}>{children}</a> } } }}>{markdown}</Markdown></article>
}
function RunPanel({ mode, busy, developmentRunning, onRun }: { mode: 'dependencies' | 'development' | 'background'; busy: boolean; developmentRunning: boolean; onRun: (action: string) => void }) { const views = { dependencies: { title: '依赖管理', note: '检查当前目录是否已安装依赖；缺失时再执行安装或修复。', primary: '检查依赖', action: 'dependency-status', secondary: '安装或修复依赖', secondaryAction: 'install' }, development: { title: '开发模式', note: '启动后立即返回；过程与后续日志会实时显示在运行终端。', primary: '启动开发模式', action: 'dev' }, background: { title: '后台运行', note: '构建后交由 PM2 守护运行，适合持续在线的机器人。', primary: '使用 PM2 启动', action: 'pm2' } }[mode]; const running = mode === 'development' && developmentRunning; return <section className="run-panel"><header><div><h1>{views.title}</h1><small>{running ? '开发模式正在运行；可在运行终端查看日志，停止后可再次启动。' : views.note}</small></div></header><div className="run-panel-actions"><button className={running ? 'secondary-button' : 'primary-button'} disabled={busy} onClick={() => onRun(running ? 'dev-stop' : views.action)}>{busy ? '处理中…' : running ? '停止开发模式' : views.primary}</button>{views.secondary && <button className="secondary-button" disabled={busy} onClick={() => onRun(views.secondaryAction!)}>{views.secondary}</button>}</div></section> }
function ControlCard({ page, section, runMode, project, buildMode, catalog, catalogTitle, onOpenConsole, onPage, onSection, onRunMode, onBuildMode, onCatalog }: { page: Page; section: Section; runMode: 'dependencies' | 'development' | 'background'; project?: Project; buildMode: 'manifest' | 'npm' | 'git'; catalog: CatalogGroup[]; catalogTitle: string; onOpenConsole: () => void; onPage: (page: Page) => void; onSection: (section: Section) => void; onRunMode: (mode: 'dependencies' | 'development' | 'background') => void; onBuildMode: (mode: 'manifest' | 'npm' | 'git') => void; onCatalog: (title: string) => void }) {
  const activePrimary = page === 'robot' ? section === 'actions' ? 'actions' : section === 'backpack' ? 'backpack' : 'config' : page
  const subitems = activePrimary === 'config' ? [{ id: 'npmrc', label: 'npm 源' }] : activePrimary === 'actions' ? [{ id: 'dependencies', label: '依赖' }, { id: 'development', label: '开发' }, { id: 'background', label: '后台' }] : activePrimary === 'build' ? [{ id: 'manifest', label: '包配置' }, { id: 'git', label: 'Git 打包' }, { id: 'npm', label: 'NPM 发布' }] : activePrimary === 'backpack' ? [] : catalog.map((item) => ({ id: item.title, label: item.title }))
  const activeSecondary = activePrimary === 'config' ? section : activePrimary === 'actions' ? runMode : activePrimary === 'build' ? buildMode : catalogTitle
  function selectPrimary(item: typeof directoryActions[number]) { if (item.kind === 'section') { onPage('robot'); onSection(item.id as Section); return }; onPage(item.id as Page) }
  function selectSecondary(id: string) { if (activePrimary === 'config') { onSection(id as Section); return }; if (activePrimary === 'actions') { onRunMode(id as 'dependencies' | 'development' | 'background'); return }; if (activePrimary === 'build') { onBuildMode(id as 'manifest' | 'npm' | 'git'); return }; onCatalog(id) }
  return <aside className="control-dock" aria-label="目录操作"><section className="control-card"><header><div><span>当前机器人</span><strong>{project?.name ?? '未选择目录'}</strong></div><i>◈</i></header><div className="control-list">{directoryActions.map((item) => <button className={activePrimary === item.id ? 'active' : ''} onClick={() => selectPrimary(item)} key={item.id}><i>{item.icon}</i><span>{item.label}</span><ChevronRight /></button>)}</div>{subitems.length > 0 && <><span className="control-divider" /><div className="control-sublist">{subitems.map((item) => <button className={activeSecondary === item.id ? 'active' : ''} onClick={() => selectSecondary(item.id)} key={item.id}>{item.label}<ChevronRight /></button>)}</div></>}{project && <footer title={project.path}><div><span>当前目录</span><strong>{project.path}</strong></div><button className="icon-button" onClick={onOpenConsole} aria-label="查看运行终端" title="查看运行终端"><Terminal /></button></footer>}</section></aside> }
function ReadonlyConsole({ open, root, onClose }: { open: boolean; root: string; onClose: () => void }) { const [load, { data, error, isFetching }] = useLazyRobotConsoleQuery(); useEffect(() => { if (!open || !root) return; void load(root); const timer = window.setInterval(() => { void load(root, true) }, 900); return () => window.clearInterval(timer) }, [load, open, root]); if (!open) return null; const message = error ? operationErrorMessage(error, '无法读取当前目录的运行终端信息。') : data?.output ?? ''; return <div className="readonly-console-backdrop" role="presentation"><section className="readonly-console" role="dialog" aria-modal="true" aria-label="运行终端"><header><div><Terminal /><strong>运行终端</strong><small>实时显示开发模式过程 · 不支持输入命令</small></div><div><button className="icon-button" disabled={isFetching} onClick={() => void load(root)} aria-label="刷新运行终端" title="刷新"><RefreshCw /></button><button className="icon-button" onClick={onClose} aria-label="关闭运行终端" title="关闭"><X /></button></div></header><pre>{isFetching && !message ? '正在读取当前目录…' : message}</pre></section></div> }
function EditorMode({ active, onVisual, onText }: { active: 'visual' | 'text'; onVisual: () => void; onText: () => void }) { return <div className="editor-mode" aria-label="配置编辑模式"><button className={active === 'visual' ? 'active' : ''} onClick={onVisual}>表单</button><button className={active === 'text' ? 'active' : ''} onClick={onText}>文本</button></div> }
function FileEditor({ toolbar, content, busy, placeholder, onChange, onSave }: { toolbar?: ReactNode; content: string; busy: boolean; placeholder: string; onChange: (value: string) => void; onSave: () => void }) { return <section className="file-editor"><header>{toolbar}<button className="primary-button" disabled={busy} onClick={onSave}>保存</button></header><textarea value={content} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} /></section> }
function OperationLog({ output, failed, onClose }: { output: string; failed: boolean; onClose: () => void }) {
  return <aside className={`robot-output ${failed ? 'failed' : 'completed'}`} aria-live="polite" aria-label="最近操作结果">
    <header><div><i>{failed ? '!' : '✓'}</i><strong>{failed ? '操作未完成' : '操作已完成'}</strong></div><button onClick={onClose} aria-label="关闭操作结果">×</button></header>
    <pre>{output}</pre>
    <small>完整记录可在右上角的任务按钮中查看。</small>
  </aside>
}
function GitReleasePanel({ root, busy, version, confirmed, onVersionChange, onConfirm, onInitialize, onRun }: { root: string; busy: boolean; version: string; confirmed: boolean; onVersionChange: (value: string) => void; onConfirm: () => void; onInitialize: (values: { authorName: string; authorEmail: string; repository: string; message: string }) => Promise<boolean>; onRun: () => void }) {
  type GitStatus = { root?: string; packageName?: string; packageVersion?: string; packageManager?: string; suggestedVersion?: string; tags?: string[]; commits?: string[]; gitReady?: boolean; checks?: string[]; issues?: string[] }
  const { data, isFetching: loading, error, refetch } = useGitStatusQuery(root, { skip: !root })
  const [initializing, setInitializing] = useState(false)
  const [gitInit, setGitInit] = useState({ authorName: '', authorEmail: '', repository: '', message: 'chore: initialize project' })
  const status = error ? { issues: ['无法读取 Git 发布状态。'] } : data as GitStatus | undefined
  const refresh = () => { void refetch() }
  const issues = status?.issues ?? []; const blockingIssues = issues.filter((item) => !item.startsWith('尚未发现 lib')); const ready = !loading && blockingIssues.length === 0; const tags = status?.tags ?? []; const commits = status?.commits ?? []
  const needsInitialize = !status?.gitReady || issues.some((item) => item.includes('不是 Git 仓库根目录'))
  const submitInitialize = async () => { setInitializing(true); try { if (await onInitialize(gitInit)) await refetch() } finally { setInitializing(false) } }
  return <section className="git-release-panel"><header className="release-toolbar"><span>{status?.packageName ? `${status.packageName}@${status.packageVersion || '未设置版本'} · ${status.packageManager}` : 'Git 打包'}</span><div className="release-toolbar-actions"><label className="release-version-field"><span>版本</span><input value={version || status?.suggestedVersion || ''} onChange={(event) => onVersionChange(event.target.value)} placeholder="v0.0.1" /></label>{confirmed && <button className="text-button" onClick={onConfirm}>取消</button>}<button className="secondary-button" onClick={refresh} disabled={loading || busy}><RefreshCw /></button><button className="primary-button release-button" disabled={busy || !ready} onClick={confirmed ? onRun : onConfirm}>{busy ? '打包中…' : confirmed ? '确认打包' : '准备打包'}</button></div></header>{loading ? <p className="publish-state">正在读取所选目录的 Git 状态…</p> : <><p className={`release-status ${ready ? 'ready' : 'blocked'}`}>{ready ? '✓ 发布条件已就绪' : '！ 发布前需要处理以下问题'}</p>{issues.length > 0 && <section className="release-blockers"><ul>{issues.map((item) => <li key={item}>{item}</li>)}</ul>{needsInitialize && <section className="git-init-form"><strong>初始化当前项目仓库</strong><p>将在所选目录创建独立 Git 仓库，不会修改父目录仓库或全局 Git 身份。</p><div><label>提交姓名<input value={gitInit.authorName} onChange={(event) => setGitInit({ ...gitInit, authorName: event.target.value })} placeholder="你的姓名" /></label><label>提交邮箱<input type="email" value={gitInit.authorEmail} onChange={(event) => setGitInit({ ...gitInit, authorEmail: event.target.value })} placeholder="name@example.com" /></label><label>origin（可选）<input value={gitInit.repository} onChange={(event) => setGitInit({ ...gitInit, repository: event.target.value })} placeholder="https://github.com/owner/repo.git" /></label><label>首个提交<input value={gitInit.message} onChange={(event) => setGitInit({ ...gitInit, message: event.target.value })} /></label></div><button className="primary-button" disabled={busy || initializing || !gitInit.authorName.trim() || !gitInit.authorEmail.trim()} onClick={() => void submitInitialize()}>{initializing ? '正在初始化…' : '确认初始化 Git'}</button></section>}</section>}<section className="release-records"><details className="release-history"><summary>release commits <small>{commits.length} 条</small></summary><div><p>这是已推送到 <code>release</code> 分支的发布提交；每次 Git 打包会新增一条。</p>{commits.length ? <ol>{commits.map((item) => <li key={item}><code>{item}</code></li>)}</ol> : <p className="release-history-empty">尚未创建 release 发布提交。</p>}</div></details><details className="release-history"><summary>Tags <small>{tags.length} 个</small></summary><div><p>Tag 是不可覆盖的发布版本标记，例如 <code>v0.0.666</code>；它用于定位、下载或回退到对应版本。</p>{tags.length ? <div className="release-tags">{tags.map((item) => <code key={item}>{item}</code>)}</div> : <p className="release-history-empty">尚未创建发布 Tag。</p>}</div></details></section></>}</section>
}
void GitReleasePanel

function GitReleasePanelNext({ root, busy, version, confirmed, onVersionChange, onConfirm, onInitialize, onRun }: { root: string; busy: boolean; version: string; confirmed: boolean; onVersionChange: (value: string) => void; onConfirm: () => void; onInitialize: (values: { authorName: string; authorEmail: string; repository: string; message: string }) => Promise<boolean>; onRun: (sourceCommit: string) => void }) {
  type SourceCommit = { sha: string; shortSha: string; subject: string; createdAt: string }
  type ReleaseMapping = { version: string; sourceBranch: string; sourceCommit: string; releaseCommit: string }
  type GitStatus = { packageName?: string; packageVersion?: string; packageManager?: string; suggestedVersion?: string; tags?: string[]; commits?: string[]; sourceCommits?: SourceCommit[]; releaseMappings?: ReleaseMapping[]; gitReady?: boolean; checks?: string[]; issues?: string[] }
  const { data, isFetching: loading, error, refetch } = useGitStatusQuery(root, { skip: !root })
  const [initializing, setInitializing] = useState(false)
  const [sourceCommit, setSourceCommit] = useState('')
  const [gitInit, setGitInit] = useState({ authorName: '', authorEmail: '', repository: '', message: 'chore: initialize project' })
  const status = error ? { issues: ['无法读取 Git 发布状态。'] } : data as GitStatus | undefined
  const commits = status?.sourceCommits ?? emptyGitCommits
  useEffect(() => { if (!commits.some((item) => item.sha === sourceCommit)) setSourceCommit(commits[0]?.sha ?? '') }, [commits, sourceCommit])
  const issues = status?.issues ?? []
  const blockingIssues = issues.filter((item) => !item.startsWith('尚未发现 lib'))
  const buildNotes = issues.filter((item) => item.startsWith('尚未发现 lib'))
  const ready = !loading && blockingIssues.length === 0 && !!sourceCommit
  const needsInitialize = !status?.gitReady || issues.some((item) => item.includes('不是 Git 仓库根目录'))
  const submitInitialize = async () => { setInitializing(true); try { if (await onInitialize(gitInit)) await refetch() } finally { setInitializing(false) } }
  const refresh = () => { if (confirmed) onConfirm(); void refetch() }
  return <section className="git-release-panel">
    <header className="release-toolbar"><span>{status?.packageName ? `${status.packageName}@${status.packageVersion || '未设置版本'} · ${status.packageManager}` : 'Git 打包'}</span><div className="release-toolbar-actions"><button className="secondary-button" onClick={refresh} disabled={loading || busy}><RefreshCw /></button><button className="primary-button release-button" disabled={busy || !ready} onClick={confirmed ? () => onRun(sourceCommit) : onConfirm}>{busy ? '打包中…' : confirmed ? '确认打包' : '准备打包'}</button></div></header>
    {loading ? <p className="publish-state">正在读取所选目录的 Git 状态…</p> : <>
      <section className="release-source-card">
        <div><strong>1. 选择源码提交</strong><p>只会构建这次已提交的代码，不会包含你还没提交的本地修改。</p></div>
        <label>源码分支 <input value={(status as GitStatus & { branch?: string })?.branch || 'main'} readOnly /></label>
        <label>提交 <select value={sourceCommit} onChange={(event) => { setSourceCommit(event.target.value); if (confirmed) onConfirm() }} disabled={!commits.length}>{commits.length ? commits.map((item) => <option key={item.sha} value={item.sha}>{item.shortSha} · {item.subject} · {item.createdAt}</option>) : <option value="">暂无可选提交</option>}</select></label>
      </section>
      <section className="release-source-card compact"><div><strong>2. 设置发布版本</strong><p>会创建不可覆盖的 Git Tag，并同步到 release 分支。</p></div><label>版本 <input value={version || status?.suggestedVersion || ''} onChange={(event) => { onVersionChange(event.target.value); if (confirmed) onConfirm() }} placeholder="v0.0.1" /></label></section>
      <p className={`release-status ${ready ? 'ready' : 'blocked'}`}>{ready ? '✓ 可以从所选提交开始打包' : '！ 发布前需要处理以下问题'}</p>
      {blockingIssues.length > 0 && <section className="release-blockers"><ul>{blockingIssues.map((item) => <li key={item}>{item}</li>)}</ul>{needsInitialize && <section className="git-init-form"><strong>初始化当前项目仓库</strong><p>只在所选目录创建独立 Git 仓库，不会修改父目录仓库或全局 Git 身份。</p><div><label>提交姓名<input value={gitInit.authorName} onChange={(event) => setGitInit({ ...gitInit, authorName: event.target.value })} placeholder="你的姓名" /></label><label>提交邮箱<input type="email" value={gitInit.authorEmail} onChange={(event) => setGitInit({ ...gitInit, authorEmail: event.target.value })} placeholder="name@example.com" /></label><label>origin（可选）<input value={gitInit.repository} onChange={(event) => setGitInit({ ...gitInit, repository: event.target.value })} placeholder="https://github.com/owner/repo.git" /></label><label>首个提交<input value={gitInit.message} onChange={(event) => setGitInit({ ...gitInit, message: event.target.value })} /></label></div><button className="primary-button" disabled={busy || initializing || !gitInit.authorName.trim() || !gitInit.authorEmail.trim()} onClick={() => void submitInitialize()}>{initializing ? '正在初始化…' : '确认初始化 Git'}</button></section>}</section>}
      {buildNotes.length > 0 && <p className="release-build-note">构建产物会在打包时自动生成，无需手动处理。</p>}
      {confirmed && <p className="release-confirmation">即将从 <code>{commits.find((item) => item.sha === sourceCommit)?.shortSha}</code> 构建；确认后会安装依赖、构建、写入源码映射并推送 release。</p>}
      <section className="release-records"><details className="release-history"><summary>源码映射 <small>{status?.releaseMappings?.length ?? 0} 条</small></summary><div>{status?.releaseMappings?.length ? <ol>{status.releaseMappings.map((item) => <li key={item.releaseCommit}><code>{item.version}</code> ← <code>{item.sourceBranch}@{item.sourceCommit.slice(0, 8)}</code></li>)}</ol> : <p className="release-history-empty">后续每次 Git 打包都会在 release 中记录源码分支与 commit。</p>}</div></details><details className="release-history"><summary>Tags <small>{status?.tags?.length ?? 0} 个</small></summary><div>{status?.tags?.length ? <div className="release-tags">{status.tags.map((item) => <code key={item}>{item}</code>)}</div> : <p className="release-history-empty">尚未创建发布 Tag。</p>}</div></details></section>
    </>}
  </section>
}

import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { Archive, ArrowLeft, ArrowRight, Check, Eye, EyeOff, Folder, Link, Network, Package, Play, Plug, Plus, Search, Send, Settings, X } from 'lucide-react'
import { RobotConfigForm } from './RobotConfigForm'
import { NpmrcConfigForm } from './NpmrcConfigForm'
import { NpmPublishPanel } from './NpmPublishPanel'
import { PackageManifestPanel } from './PackageManifestPanel'
import { SetupUpdateButton } from './SetupUpdateButton'
import { useCatalogDocumentQuery, useCatalogPackageConfigQuery, useCatalogQuery, useGitStatusQuery, useInitializeGitMutation, useLazyRobotFileQuery, useLocalPackagesQuery, useRobotTasksQuery, useSetSetupPluginEnabledMutation, useSetupPluginsQuery, useStartRobotTaskMutation, useStartSetupPluginTaskMutation, useWritePackageConfigMutation, useWriteRobotFileMutation, type SetupPlugin } from '../store/workspaceApi'
import { addProjects, removeProject as removeWorkspaceProject, selectProject, setDraft } from '../store/workspaceStore'
import type { RootState } from '../store/guideStore'

type Check = { id: string; name: string; status: 'ready' | 'missing' | 'warning'; detail: string; suggestion: string }
type CatalogItem = { name: string; description: string; url: string; install: string }
type CatalogGroup = { title: string; items: CatalogItem[] }
type Page = 'robot' | 'build' | 'plugins' | 'connections'
type Section = 'backpack' | 'config' | 'npmrc' | 'actions'
type Project = { id: string; path: string; name: string }
type SystemFeature = string
type Props = { report: { checks: Check[] } | null; checking: boolean; error: string; defaultPage: string; onOpenGuide: () => void; onCheck: () => void; onFix: (check: Check) => void; goals?: unknown; goal?: unknown; onSelect?: (id: string) => void }

const coreFeatureCatalog: Array<{ id: SystemFeature; label: string; icon: ReactNode; status?: string }> = [{ id: 'plugins', label: '插件', icon: <Plug /> }]
const directoryActions: Array<{ id: Section | Page; label: string; icon: ReactNode; kind: 'section' | 'page' }> = [{ id: 'config', label: '配置', icon: <Settings />, kind: 'section' }, { id: 'actions', label: '运行', icon: <Play />, kind: 'section' }, { id: 'connections', label: '连接', icon: <Link />, kind: 'page' }, { id: 'backpack', label: '背包', icon: <Archive />, kind: 'section' }, { id: 'plugins', label: '插件', icon: <Package />, kind: 'page' }, { id: 'build', label: '发布', icon: <Send />, kind: 'page' }]

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

export function Dashboard({ report, checking, error, defaultPage, onOpenGuide, onCheck, onFix }: Props) {
  const [page, setPage] = useState<Page>('robot')
  const [systemFeature, setSystemFeature] = useState<SystemFeature | null>(null)
  const [section, setSection] = useState<Section>('config')
  const [file, setFile] = useState('.npmrc')
  const [output, setOutput] = useState('')
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
  const { data: catalog = [], isFetching: catalogLoading, error: catalogQueryError } = useCatalogQuery(catalogKind, { skip: page !== 'plugins' && page !== 'connections' })
  const { data: localPackages, isFetching: packagesLoading, error: packagesError, refetch: refetchPackages } = useLocalPackagesQuery(root, { skip: !root || section !== 'backpack' })
  const [readRobotFile] = useLazyRobotFileQuery()
  const [startRobotTask] = useStartRobotTaskMutation()
  const [writeRobotFile] = useWriteRobotFileMutation()
  const [writePackageConfig] = useWritePackageConfigMutation()
  const [initializeGit] = useInitializeGitMutation()
  const { data: setupPlugins = [] } = useSetupPluginsQuery()
  const catalogError = catalogQueryError ? '在线目录暂时无法读取。' : ''

  useEffect(() => { if (defaultPage === 'robot') setPage('robot') }, [defaultPage])
  useEffect(() => {
    if (report || checking || environmentChecked.current) return
    environmentChecked.current = true
    onCheck()
  }, [checking, onCheck, page, report])
  useEffect(() => { if (!catalogTitle && catalog.length) setCatalogTitle(catalog[0].title) }, [catalog, catalogTitle])
  useEffect(() => {
    if (!root || section !== 'config') return
    void readRobotFile({ root, file: 'alemon.config.yaml' }, true).unwrap()
      .then((result) => dispatch(setDraft({ key: `${root}:alemon.config.yaml`, content: result.output ?? '' })))
      .catch(() => dispatch(setDraft({ key: `${root}:alemon.config.yaml`, content: '' })))
  }, [dispatch, readRobotFile, root, section])

  async function api(method: string, data: Record<string, string>): Promise<boolean> {
    if (!root) { setOutput('请先在左侧添加机器人目录。'); return false }
    setBusy(true)
    try {
      if (method === 'GET') { const result = await readRobotFile({ root: data.root, file: data.file }, true).unwrap(); dispatch(setDraft({ key: `${data.root}:${data.file}`, content: result.output ?? '' })); return true }
      if (method === 'PUT') { const result = await writeRobotFile({ root: data.root, file: data.file, content: data.content }).unwrap(); setOutput(result.output ?? '操作完成。'); return true }
      const task = await startRobotTask(data).unwrap()
      setOutput('操作已开始，正在等待完成…')
      for (;;) {
        await new Promise((resolve) => window.setTimeout(resolve, 700))
        const response = await fetch(`/api/v1/robot/tasks?${new URLSearchParams({ id: task.id })}`)
        const current = await response.json() as { status: string; output?: string; error?: string }
        if (current.status === 'running') continue
        if (current.status === 'failed') throw new Error(current.error ?? '操作未完成。')
        setOutput(current.output ?? '操作完成。'); return true
      }
    } catch (reason) { setOutput(operationErrorMessage(reason, '操作未完成，请在右上角任务记录中查看详情。')); return false } finally { setBusy(false) }
  }

  async function savePackageConfig(packageName: string, values: Record<string, string>): Promise<boolean> {
    if (!root) return false
    setBusy(true)
    try {
      const result = await writePackageConfig({ root, package: packageName, values }).unwrap()
      setOutput(result.output ?? '机器人运行配置已保存。')
      return true
    } catch (reason) { setOutput(operationErrorMessage(reason, '配置未保存，请检查所选机器人目录。')); return false } finally { setBusy(false) }
  }

  async function initializeProjectGit(values: { authorName: string; authorEmail: string; repository: string; message: string }): Promise<boolean> {
    if (!root) return false
    setBusy(true)
    try {
      const result = await initializeGit({ root, ...values }).unwrap()
      setOutput(result.output ?? 'Git 仓库已初始化。')
      return true
    } catch (reason) { setOutput(operationErrorMessage(reason, 'Git 初始化未完成，请检查所选机器人目录。')); return false } finally { setBusy(false) }
  }

  async function chooseDirectories() {
    setDirectoryPickerOpen(true)
  }
  function addSelectedDirectories(paths: string[]) {
    if (!paths.length) return
    dispatch(addProjects(paths.map((path) => ({ id: path, path, name: projectName(path) }))))
    setDirectoryPickerOpen(false)
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
    {section === 'actions' && <RunPanel mode={runMode} busy={busy} onRun={(action) => api('POST', { root, action })} />}
  </section>

  const catalogContent = <section className="workspace-content">{catalogLoading && <p className="catalog-state">正在读取目录…</p>}{catalogError && <p className="catalog-state">{catalogError}</p>}{!catalogLoading && !catalogError && currentCatalog && (catalogItem ? <CatalogDetail item={catalogItem} group={currentCatalog.title} busy={busy} onBack={() => setCatalogItem(null)} onRun={(action, packageName) => api('POST', { root, action, package: packageName })} onSaveConfig={savePackageConfig} /> : <section className="catalog-items">{currentCatalog.items.map((item) => <button className="catalog-item" key={`${currentCatalog.title}-${item.name}`} onClick={() => setCatalogItem(item)}><strong>{item.name}</strong><span>›</span></button>)}</section>)}</section>
  const setupPlugin = setupPlugins.find((item) => systemFeature === `setup:${item.id}`)
  const workspace = systemFeature === 'plugins' ? <SystemPluginCenter plugins={setupPlugins} onOpen={(id) => selectSystemFeature(`setup:${id}`)} /> : setupPlugin ? <SetupPluginCenter plugin={setupPlugin} /> : activeProject ? <>{page === 'robot' && robotContent}{page === 'build' && <section className="workspace-content build-page">{buildMode === 'manifest' ? <PackageManifestPanel root={root} busy={busy} onSaved={setOutput} /> : buildMode === 'npm' ? <NpmPublishPanel root={root} busy={busy} onRun={(action, values) => api('POST', { root, action, ...values })} /> : <GitReleasePanel root={root} busy={busy} version={releaseVersion} confirmed={gitConfirm} onVersionChange={(value) => { setReleaseVersion(value); setGitConfirm(false) }} onConfirm={() => setGitConfirm((value) => !value)} onInitialize={initializeProjectGit} onRun={() => api('POST', { root, action: 'git-release', version: releaseVersion, confirm: 'true' })} />}{output && <OperationLog output={output} onClose={() => setOutput('')} />}</section>}{(page === 'plugins' || page === 'connections') && catalogContent}{page !== 'build' && output && <OperationLog output={output} onClose={() => setOutput('')} />}</> : <EmptyWorkspace onAdd={chooseDirectories} />

  const environmentReady = report ? `${readyCount}/${report.checks.length}` : '—'
  const environmentWarning = Boolean(report?.checks.some((item) => item.status !== 'ready'))

  return <main className="guide-shell"><section className="guide-window dashboard-window"><header className="guide-bar dashboard-toolbar"><div className="workspace-title"><a className="workspace-name" href="https://alemonjs.com/" target="_blank" rel="noreferrer">ALEMONJS</a><SetupUpdateButton /></div><div className="header-global-actions"><McpControl /><OperationTasksButton /><button className={`environment-control ${environmentWarning ? 'warning' : ''}`} onClick={() => { setEnvironmentOpen(true); onCheck() }} disabled={checking} title="查看并检查全局环境"><i>{checking ? '◌' : environmentWarning ? '!' : '✓'}</i><span>环境</span><strong>{checking ? '检查中' : environmentReady}</strong></button><button className="guide-trigger" onClick={onOpenGuide} aria-label="打开引导" title="打开引导">?</button></div></header><EnvironmentPanel open={environmentOpen} report={report} checking={checking} onClose={() => setEnvironmentOpen(false)} onRefresh={onCheck} onFix={onFix} /><DirectoryPicker open={directoryPickerOpen} onClose={() => setDirectoryPickerOpen(false)} onSelect={addSelectedDirectories} /><section className="console-layout">
    <ProjectRail feature={systemFeature} setupPlugins={setupPlugins} projects={projects} activeID={activeProjectID} onFeature={selectSystemFeature} onAdd={chooseDirectories} onSelect={(id) => { dispatch(selectProject(id)); setSystemFeature(null); setPage('robot'); setSection('config'); setOutput('') }} onRemove={removeProject} />
    <section className="console-page">{workspace}{error && <p className="error">{error}</p>}{!systemFeature && activeProject && <ControlCard page={page} section={section} runMode={runMode} project={activeProject} buildMode={buildMode} catalog={catalog} catalogTitle={catalogTitle} onPage={selectPage} onSection={openSection} onRunMode={(mode) => { setRunMode(mode); openSection('actions') }} onBuildMode={(mode) => { setBuildMode(mode); setGitConfirm(false); setOutput('') }} onCatalog={(title) => { setCatalogTitle(title); setCatalogItem(null) }} />}</section>
  </section></section></main>
}

function ProjectRail({ feature, setupPlugins, projects, activeID, onFeature, onAdd, onSelect, onRemove }: { feature: SystemFeature | null; setupPlugins: SetupPlugin[]; projects: Project[]; activeID: string; onFeature: (feature: SystemFeature) => void; onAdd: () => void; onSelect: (id: string) => void; onRemove: (id: string) => void }) {
  const activePlugins = setupPlugins.filter((item) => item.enabled)
  return <aside className="project-rail"><section className="feature-catalog" aria-label="系统功能目录"><header><span>功能目录</span><small>系统</small></header><nav>{coreFeatureCatalog.map((item) => <button className={feature === item.id ? 'active' : ''} key={item.id} onClick={() => onFeature(item.id)}><i>{item.icon}</i><span>{item.label}</span>{item.status && <small>{item.status}</small>}</button>)}</nav>{activePlugins.length > 0 && <><span className="setup-plugin-divider" /><p className="setup-plugin-heading">已安装能力</p><nav>{activePlugins.map((item) => <button className={feature === `setup:${item.id}` ? 'active' : ''} key={item.id} onClick={() => onFeature(`setup:${item.id}`)}><i>{setupPluginIcon(item.navigation.icon)}</i><span>{item.navigation.label || item.name}</span><small>已加载</small></button>)}</nav></>}</section><section className="project-directory"><header><div><strong>机器人目录</strong><span>{projects.length}</span></div><button onClick={onAdd} aria-label="添加机器人目录" title="添加机器人目录"><Plus /></button></header><div className="project-list">{projects.map((project) => <article className={project.id === activeID ? 'active' : ''} key={project.id}><button className="project-select" onClick={() => onSelect(project.id)}><strong>{project.name}</strong><small title={project.path}>{project.path}</small></button><button className="project-remove" onClick={() => onRemove(project.id)} aria-label={`移除 ${project.name}`} title="移除目录"><X /></button></article>)}{!projects.length && <p>添加目录开始管理</p>}</div></section></aside>
}
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
function OperationTasksButton() { const [open, setOpen] = useState(false); const { data, isFetching } = useRobotTasksQuery(undefined, { skip: !open, pollingInterval: open ? 1500 : 0 }); const tasks = Array.isArray(data) ? data : []; const [selected, setSelected] = useState<string>(''); const current = tasks.find((item) => item.id === selected) ?? tasks[0]; const running = tasks.filter((item) => item.status === 'running').length; const labels: Record<string, string> = { install: '安装依赖', 'dependency-status': '检查依赖', dev: '开发启动', pm2: '后台启动', 'install-package': '安装插件', 'uninstall-package': '卸载插件', 'git-release': 'Git 打包', 'npm-publish': 'NPM 发布' }; return <div className="operation-tasks"><button className="operation-tasks-button" onClick={() => setOpen((value) => !value)} title="操作记录">▤{running > 0 && <b>{running}</b>}</button>{open && <section className="operation-tasks-popover"><header><strong>操作记录</strong><button onClick={() => setOpen(false)} aria-label="关闭操作记录">×</button></header>{isFetching && !tasks.length ? <p>正在读取任务…</p> : !tasks.length ? <p>暂无操作记录。</p> : <><div className="task-list">{tasks.slice(0, 8).map((item) => <button key={item.id} className={current?.id === item.id ? 'active' : ''} onClick={() => setSelected(item.id)}><i className={item.status}>{item.status === 'running' ? '◌' : item.status === 'completed' ? '✓' : '!'}</i><span>{labels[item.action] ?? item.action}</span></button>)}</div>{current && <pre className={current.status}>{current.status === 'failed' ? current.error : current.output || '正在执行…'}</pre>}</>}</section>}</div> }
function EnvironmentPanel({ open, report, checking, onClose, onRefresh, onFix }: { open: boolean; report: { checks: Check[] } | null; checking: boolean; onClose: () => void; onRefresh: () => void; onFix: (check: Check) => void }) { if (!open) return null; const checks = report?.checks ?? []; const readyCount = checks.filter((check) => check.status === 'ready').length; return <aside className="environment-panel" role="dialog" aria-label="全局环境详情"><header><strong>{checking ? '正在检查环境…' : checks.length ? `${readyCount}/${checks.length} 已就绪` : '等待检查'}</strong><button onClick={onClose} aria-label="关闭环境详情">×</button></header>{checking && <p className="environment-panel-state">正在读取 Node.js、Git 和系统工具状态。</p>}{!checking && checks.length > 0 && <div className="environment-check-list">{checks.map((check) => <article className={check.status} key={check.id}><i>{check.status === 'ready' ? '✓' : '!'}</i><div><strong>{check.name}</strong><span>{check.detail}</span>{check.status !== 'ready' && check.suggestion && <small>{check.suggestion}</small>}</div>{check.status !== 'ready' && <button className="text-button" onClick={() => onFix(check)}>修复</button>}</article>)}</div>}{!checking && !checks.length && <p className="environment-panel-state">尚未获取检查结果。</p>}<footer><button className="secondary-button" disabled={checking} onClick={onRefresh}>重新检查</button></footer></aside> }
function EmptyWorkspace({ onAdd }: { onAdd: () => void }) { return <section className="workspace-content empty-workspace"><span>◈</span><strong>从左侧添加机器人目录</strong><button className="primary-button" onClick={onAdd}>添加目录</button></section> }
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
  return <section className="workspace-content setup-plugin-manager"><header><h1>插件 <small>{plugins.filter((item) => item.enabled).length}</small></h1><span>丢入插件目录后自动加载</span></header>{plugins.length ? <section className="setup-plugin-cards">{plugins.map((plugin) => <article className={plugin.enabled ? '' : 'disabled'} key={plugin.id}><button onClick={() => plugin.enabled && onOpen(plugin.id)} disabled={!plugin.enabled}><i>{setupPluginIcon(plugin.navigation.icon)}</i><div><strong>{plugin.name}</strong><small>v{plugin.version} · {plugin.enabled ? '已启用' : '已卸载'}</small></div><b>{plugin.enabled ? '›' : '—'}</b></button><button className="setup-plugin-toggle" disabled={isLoading} onClick={() => void toggle(plugin)}>{plugin.enabled ? '卸载' : '启用'}</button></article>)}</section> : <section className="setup-plugin-empty"><strong>暂未发现插件</strong><span>将插件目录放入 plugins 后刷新即可。</span></section>}{message && <p className="setup-plugin-message">{message}</p>}</section>
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
          {visibleActions.map((action) => <section className={activeAction === action.id ? 'setup-plugin-action active' : 'setup-plugin-action'} key={action.id}>
            <button className="setup-plugin-action-choice" disabled={!plugin.runnable} onClick={() => setActiveAction(activeAction === action.id ? '' : action.id)}>
              <span><strong>{action.label}</strong>{action.description && <small>{action.description}</small>}</span><b>{activeAction === action.id ? '−' : '+'}</b>
            </button>
            {activeAction === action.id && <div className="setup-plugin-action-editor">
              {action.fields?.length ? <div className="setup-plugin-fields">{action.fields.map((field) => <label key={field.key}>{field.label}{field.type === 'select' ? <select value={values[`${action.id}:${field.key}`] ?? field.default ?? ''} onChange={(event) => setValues({ ...values, [`${action.id}:${field.key}`]: event.target.value })}>{(field.options ?? []).map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}</select> : <input type={field.type} value={values[`${action.id}:${field.key}`] ?? ''} onChange={(event) => setValues({ ...values, [`${action.id}:${field.key}`]: event.target.value })} placeholder={field.label} />}</label>)}</div> : null}
              <footer>{action.confirm && <small>此操作会修改本机系统设置。</small>}<button className="secondary-button" onClick={() => setActiveAction('')}>取消</button><button className="primary-button" disabled={isLoading} onClick={() => void run(action)}>{isLoading ? '启动中…' : action.confirm ? '确认执行' : '执行'}</button></footer>
            </div>}
          </section>)}
        </div>}
        {message && <p className="setup-plugin-message">{message}</p>}
      </section>
    </div>
  </section>
}
function BackpackPanel({ root, items, loading, failed, onRefresh, onOpenPlugins }: { root: string; items: Array<{ name: string; version?: string; description?: string; path: string; valid: boolean }>; loading: boolean; failed: boolean; onRefresh: () => void; onOpenPlugins: () => void }) { return <section className="backpack-panel"><header><div><p>本地插件包</p><h1>背包</h1><small title={`${root}/packages`}>packages</small></div><button className="secondary-button" disabled={loading} onClick={onRefresh}>{loading ? '读取中…' : '刷新'}</button></header>{loading ? <p className="catalog-state">正在读取本地插件包…</p> : items.length ? <div className="backpack-items">{items.map((item) => <article className={item.valid ? '' : 'invalid'} key={item.path}><i>{item.valid ? '▣' : '!'}</i><div><strong>{item.name}{item.version && <em>v{item.version}</em>}</strong><span>{item.valid ? item.description || '本地 AlemonJS 插件包' : '缺少有效 package.json，暂不能作为插件运行。'}</span><small title={item.path}>{item.path}</small></div></article>)}</div> : <section className="backpack-empty"><strong>暂无插件包</strong><span>{failed ? '暂未能读取本地 packages 目录，你仍可从插件页安装。' : '安装后的本地插件包会显示在这里。'}</span><button className="secondary-button" onClick={onOpenPlugins}>前往插件</button></section>}</section> }
function CatalogDetail({ item, group, busy, onBack, onRun, onSaveConfig }: { item: CatalogItem; group: string; busy: boolean; onBack: () => void; onRun: (action: string, packageName: string) => void; onSaveConfig: (packageName: string, values: Record<string, string>) => Promise<boolean> }) {
  const [version, setVersion] = useState('')
  const [configOpen, setConfigOpen] = useState(false)
  const { data: document, isFetching, error } = useCatalogDocumentQuery(item.url, { skip: !item.url })
  const repositoryInstall = item.install.startsWith('git+')
  const installTarget = version.trim() && !repositoryInstall ? `${item.install}@${version.trim()}` : item.install
  return <section className="catalog-detail"><header><button className="text-button" onClick={onBack}>‹ 返回目录</button><span>{group}</span></header><section className="catalog-control"><div><h1>{item.name}</h1><p>{item.description || '在线生态目录条目'}</p></div><div className="catalog-control-actions"><label>版本<input value={version} onChange={(event) => setVersion(event.target.value)} disabled={!item.install || repositoryInstall} placeholder={repositoryInstall ? '仓库安装' : 'latest'} /></label><button className="primary-button" disabled={busy || !item.install} onClick={() => onRun('install-package', installTarget)}>{busy ? '处理中…' : '安装'}</button><button className="secondary-button" disabled={busy || !item.install || repositoryInstall} title={repositoryInstall ? '仓库安装请按文档卸载' : '卸载当前包'} onClick={() => onRun('uninstall-package', item.install)}>卸载</button><button className="secondary-button" disabled={busy || !item.url} onClick={() => setConfigOpen((open) => !open)}>配置</button></div></section>{configOpen && <PackageConfigPanel source={item.url} busy={busy} onSave={onSaveConfig} />}<section className="catalog-document"><header><strong>在线文档</strong>{item.url && <a href={item.url} target="_blank" rel="noreferrer">在浏览器打开 ↗</a>}</header>{isFetching && <p>正在读取 README.md…</p>}{error && <p>在线文档暂时无法读取，请使用右上角链接查看。</p>}{document && <MarkdownPage markdown={document.markdown} />}</section></section>
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
  const blocks = markdown.replace(/\r/g, '').split(/\n{2,}/).filter(Boolean)
  return <article className="markdown-page">{blocks.map((block, index) => {
    const text = block.trim()
    if (text.startsWith('```')) return <pre key={index}><code>{text.replace(/^```[^\n]*\n?/, '').replace(/```$/, '')}</code></pre>
    const heading = text.match(/^(#{1,3})\s+(.+)$/)
    if (heading) { const Tag = (`h${heading[1].length}` as 'h1' | 'h2' | 'h3'); return <Tag key={index}>{heading[2]}</Tag> }
    if (text.split('\n').every((line) => /^[-*+]\s+/.test(line))) return <ul key={index}>{text.split('\n').map((line) => <li key={line}>{line.replace(/^[-*+]\s+/, '')}</li>)}</ul>
    return <p key={index}>{text}</p>
  })}</article>
}
function RunPanel({ mode, busy, onRun }: { mode: 'dependencies' | 'development' | 'background'; busy: boolean; onRun: (action: string) => void }) { const views = { dependencies: { title: '依赖管理', note: '检查当前目录是否已安装依赖；缺失时再执行安装或修复。', primary: '检查依赖', action: 'dependency-status', secondary: '安装或修复依赖', secondaryAction: 'install' }, development: { title: '开发模式', note: '以开发模式启动机器人，操作输出会显示在右上角操作记录中。', primary: '启动开发模式', action: 'dev' }, background: { title: '后台运行', note: '构建后交由 PM2 守护运行，适合持续在线的机器人。', primary: '使用 PM2 启动', action: 'pm2' } }[mode]; return <section className="run-panel"><header><div><h1>{views.title}</h1><small>{views.note}</small></div></header><div className="run-panel-actions"><button className="primary-button" disabled={busy} onClick={() => onRun(views.action)}>{busy ? '处理中…' : views.primary}</button>{views.secondary && <button className="secondary-button" disabled={busy} onClick={() => onRun(views.secondaryAction!)}>{views.secondary}</button>}</div></section> }
function ControlCard({ page, section, runMode, project, buildMode, catalog, catalogTitle, onPage, onSection, onRunMode, onBuildMode, onCatalog }: { page: Page; section: Section; runMode: 'dependencies' | 'development' | 'background'; project?: Project; buildMode: 'manifest' | 'npm' | 'git'; catalog: CatalogGroup[]; catalogTitle: string; onPage: (page: Page) => void; onSection: (section: Section) => void; onRunMode: (mode: 'dependencies' | 'development' | 'background') => void; onBuildMode: (mode: 'manifest' | 'npm' | 'git') => void; onCatalog: (title: string) => void }) {
  const activePrimary = page === 'robot' ? section === 'actions' ? 'actions' : section === 'backpack' ? 'backpack' : 'config' : page
  const subitems = activePrimary === 'config' ? [{ id: 'npmrc', label: 'npm 源' }] : activePrimary === 'actions' ? [{ id: 'dependencies', label: '依赖' }, { id: 'development', label: '开发' }, { id: 'background', label: '后台' }] : activePrimary === 'build' ? [{ id: 'manifest', label: '包配置' }, { id: 'git', label: 'Git 打包' }, { id: 'npm', label: 'NPM 发布' }] : activePrimary === 'backpack' ? [] : catalog.map((item) => ({ id: item.title, label: item.title }))
  const activeSecondary = activePrimary === 'config' ? section : activePrimary === 'actions' ? runMode : activePrimary === 'build' ? buildMode : catalogTitle
  function selectPrimary(item: typeof directoryActions[number]) { if (item.kind === 'section') { onPage('robot'); onSection(item.id as Section); return }; onPage(item.id as Page) }
  function selectSecondary(id: string) { if (activePrimary === 'config') { onSection(id as Section); return }; if (activePrimary === 'actions') { onRunMode(id as 'dependencies' | 'development' | 'background'); return }; if (activePrimary === 'build') { onBuildMode(id as 'manifest' | 'npm' | 'git'); return }; onCatalog(id) }
  return <aside className="control-dock" aria-label="目录操作"><section className="control-card"><header><div><span>当前机器人</span><strong>{project?.name ?? '未选择目录'}</strong></div><i>◈</i></header><div className="control-list">{directoryActions.map((item) => <button className={activePrimary === item.id ? 'active' : ''} onClick={() => selectPrimary(item)} key={item.id}><i>{item.icon}</i><span>{item.label}</span><b>›</b></button>)}</div>{subitems.length > 0 && <><span className="control-divider" /><div className="control-sublist">{subitems.map((item) => <button className={activeSecondary === item.id ? 'active' : ''} onClick={() => selectSecondary(item.id)} key={item.id}>{item.label}<b>›</b></button>)}</div></>}{project && <footer title={project.path}><span>当前目录</span><strong>{project.path}</strong></footer>}</section></aside> }
function EditorMode({ active, onVisual, onText }: { active: 'visual' | 'text'; onVisual: () => void; onText: () => void }) { return <div className="editor-mode" aria-label="配置编辑模式"><button className={active === 'visual' ? 'active' : ''} onClick={onVisual}>表单</button><button className={active === 'text' ? 'active' : ''} onClick={onText}>文本</button></div> }
function FileEditor({ toolbar, content, busy, placeholder, onChange, onSave }: { toolbar?: ReactNode; content: string; busy: boolean; placeholder: string; onChange: (value: string) => void; onSave: () => void }) { return <section className="file-editor"><header>{toolbar}<button className="primary-button" disabled={busy} onClick={onSave}>保存</button></header><textarea value={content} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} /></section> }
function OperationLog({ output, onClose }: { output: string; onClose: () => void }) {
  const isFailure = /失败|错误|error|failed|exit status/i.test(output)
  return <aside className={`robot-output ${isFailure ? 'failed' : 'completed'}`} aria-live="polite" aria-label="最近操作结果">
    <header><div><i>{isFailure ? '!' : '✓'}</i><strong>{isFailure ? '操作未完成' : '操作已完成'}</strong></div><button onClick={onClose} aria-label="关闭操作结果">×</button></header>
    <pre>{output}</pre>
    <small>完整记录可在右上角的任务按钮中查看。</small>
  </aside>
}
function GitReleasePanel({ root, busy, version, confirmed, onVersionChange, onConfirm, onInitialize, onRun }: { root: string; busy: boolean; version: string; confirmed: boolean; onVersionChange: (value: string) => void; onConfirm: () => void; onInitialize: (values: { authorName: string; authorEmail: string; repository: string; message: string }) => Promise<boolean>; onRun: () => void }) {
  type GitStatus = { root?: string; repository?: string; packageName?: string; packageVersion?: string; packageManager?: string; gitHubActionsUrl?: string; workflowConfigured?: boolean; latestVersion?: string; suggestedVersion?: string; tags?: string[]; commits?: string[]; artifacts?: string[]; gitReady?: boolean; releaseBranch?: boolean; checks?: string[]; issues?: string[] }
  const { data, isFetching: loading, error, refetch } = useGitStatusQuery(root, { skip: !root })
  const [initializing, setInitializing] = useState(false)
  const [gitInit, setGitInit] = useState({ authorName: '', authorEmail: '', repository: '', message: 'chore: initialize project' })
  const status = error ? { issues: ['无法读取 Git 发布状态。'] } : data as GitStatus | undefined
  const refresh = () => { void refetch() }
  const checks = status?.checks ?? []; const issues = status?.issues ?? []; const blockingIssues = issues.filter((item) => !item.startsWith('尚未发现 lib')); const ready = !loading && blockingIssues.length === 0
  const needsInitialize = !status?.gitReady || issues.some((item) => item.includes('不是 Git 仓库根目录'))
  const submitInitialize = async () => { setInitializing(true); try { if (await onInitialize(gitInit)) await refetch() } finally { setInitializing(false) } }
  return <section className="git-release-panel"><header className="release-toolbar"><span title={status?.root || root}>{status?.packageName ? `${status.packageName}@${status.packageVersion || '未设置版本'} · ${status.packageManager}` : 'Git 打包'}</span><div className="release-toolbar-actions"><label className="release-version-field"><span>版本</span><input value={version || status?.suggestedVersion || ''} onChange={(event) => onVersionChange(event.target.value)} placeholder="v0.0.1" /></label>{confirmed && <button className="text-button" onClick={onConfirm}>取消</button>}<button className="secondary-button" onClick={refresh} disabled={loading || busy}>刷新</button><button className="primary-button release-button" disabled={busy || !ready} onClick={confirmed ? onRun : onConfirm}>{busy ? '打包中…' : confirmed ? '确认打包' : '准备打包'}</button></div></header>{loading ? <p className="publish-state">正在读取所选目录的 Git 状态…</p> : <><div className="release-safety">{checks.map((item) => <span key={item}>✓ {item}</span>)}</div>{issues.length > 0 && <section className="release-blockers"><ul>{issues.map((item) => <li key={item}>{item}</li>)}</ul>{needsInitialize && <section className="git-init-form"><strong>初始化当前项目仓库</strong><p>将在所选目录创建独立 Git 仓库，不会修改父目录仓库或全局 Git 身份。</p><div><label>提交姓名<input value={gitInit.authorName} onChange={(event) => setGitInit({ ...gitInit, authorName: event.target.value })} placeholder="你的姓名" /></label><label>提交邮箱<input type="email" value={gitInit.authorEmail} onChange={(event) => setGitInit({ ...gitInit, authorEmail: event.target.value })} placeholder="name@example.com" /></label><label>origin（可选）<input value={gitInit.repository} onChange={(event) => setGitInit({ ...gitInit, repository: event.target.value })} placeholder="https://github.com/owner/repo.git" /></label><label>首个提交<input value={gitInit.message} onChange={(event) => setGitInit({ ...gitInit, message: event.target.value })} /></label></div><button className="primary-button" disabled={busy || initializing || !gitInit.authorName.trim() || !gitInit.authorEmail.trim()} onClick={() => void submitInitialize()}>{initializing ? '正在初始化…' : '确认初始化 Git'}</button></section>}</section>}<details className="release-details"><summary>发布详情</summary><div><span title={status?.root || root}>目录：{status?.root || root}</span><span>建议 {status?.suggestedVersion || '—'}</span><span>{status?.releaseBranch ? 'release 分支已就绪' : '首次打包会创建 release 分支'}</span><span>{(status?.artifacts ?? []).length ? (status?.artifacts ?? []).join(' · ') : '构建后生成发布文件'}</span>{(status?.tags ?? []).slice(0, 3).map((item) => <span key={item}>{item}</span>)}{status?.gitHubActionsUrl && <a href={status.gitHubActionsUrl} target="_blank" rel="noreferrer">Actions</a>}</div></details></>}</section>
}

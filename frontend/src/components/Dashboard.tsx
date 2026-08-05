import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { RobotConfigForm } from './RobotConfigForm'
import { NpmrcConfigForm } from './NpmrcConfigForm'
import { NpmPublishPanel } from './NpmPublishPanel'
import { useCatalogDocumentQuery, useCatalogPackageConfigQuery, useCatalogQuery, useGitStatusQuery, useInitializeGitMutation, useLazyRobotFileQuery, useLocalPackagesQuery, useRobotOperationMutation, useWritePackageConfigMutation, useWriteRobotFileMutation } from '../store/workspaceApi'
import { addProjects, removeProject as removeWorkspaceProject, selectProject, setDraft } from '../store/workspaceStore'
import type { RootState } from '../store/guideStore'

type Check = { id: string; name: string; status: 'ready' | 'missing' | 'warning'; detail: string; suggestion: string }
type CatalogItem = { name: string; description: string; url: string; install: string }
type CatalogGroup = { title: string; items: CatalogItem[] }
type Page = 'robot' | 'build' | 'plugins' | 'connections'
type Section = 'backpack' | 'config' | 'npmrc' | 'actions'
type Project = { id: string; path: string; name: string }
type SystemFeature = 'plugins'
type Props = { report: { checks: Check[] } | null; checking: boolean; error: string; defaultPage: string; onOpenGuide: () => void; onCheck: () => void; onFix: (check: Check) => void; goals?: unknown; goal?: unknown; onSelect?: (id: string) => void }

const featureCatalog: Array<{ id: SystemFeature; label: string; icon: string; status?: string }> = [{ id: 'plugins', label: '插件', icon: '▦', status: '即将支持' }]
const directoryActions: Array<{ id: Section | Page; label: string; icon: string; kind: 'section' | 'page' }> = [{ id: 'config', label: '配置', icon: '⚙', kind: 'section' }, { id: 'actions', label: '运行', icon: '▶', kind: 'section' }, { id: 'connections', label: '连接', icon: '⌁', kind: 'page' }, { id: 'backpack', label: '背包', icon: '▣', kind: 'section' }, { id: 'plugins', label: '插件', icon: '▦', kind: 'page' }, { id: 'build', label: '发布', icon: '⌗', kind: 'page' }]

function projectName(path: string) { return path.replace(/\/$/, '').split('/').pop() || path }

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
  const [buildMode, setBuildMode] = useState<'npm' | 'git'>('git')
  const [releaseVersion, setReleaseVersion] = useState('')
  const [gitConfirm, setGitConfirm] = useState(false)
  const [environmentOpen, setEnvironmentOpen] = useState(false)
  const environmentChecked = useRef(false)
  const dispatch = useDispatch()
  const projects = useSelector((state: RootState) => state.workspace.projects) as Project[]
  const activeProjectID = useSelector((state: RootState) => state.workspace.activeProjectID)
  const activeProject = projects.find((item) => item.id === activeProjectID)
  const root = activeProject?.path ?? ''
  const draftKey = `${root}:${file}`
  const content = useSelector((state: RootState) => state.workspace.drafts[draftKey] ?? '')
  const setContent = (nextContent: string) => dispatch(setDraft({ key: draftKey, content: nextContent }))
  const catalogKind = page === 'plugins' ? 'apps' : 'environment'
  const { data: catalog = [], isFetching: catalogLoading, error: catalogQueryError } = useCatalogQuery(catalogKind, { skip: page !== 'plugins' && page !== 'connections' })
  const { data: localPackages, isFetching: packagesLoading, error: packagesError, refetch: refetchPackages } = useLocalPackagesQuery(root, { skip: !root || section !== 'backpack' })
  const [readRobotFile] = useLazyRobotFileQuery()
  const [runRobotOperation] = useRobotOperationMutation()
  const [writeRobotFile] = useWriteRobotFileMutation()
  const [writePackageConfig] = useWritePackageConfigMutation()
  const [initializeGit] = useInitializeGitMutation()
  const catalogError = catalogQueryError ? '在线目录暂时无法读取。' : ''

  useEffect(() => { if (defaultPage === 'robot') setPage('robot') }, [defaultPage])
  useEffect(() => {
    if (report || checking || environmentChecked.current) return
    environmentChecked.current = true
    onCheck()
  }, [checking, onCheck, page, report])
  useEffect(() => { if (!catalogTitle && catalog.length) setCatalogTitle(catalog[0].title) }, [catalog, catalogTitle])

  async function api(method: string, data: Record<string, string>): Promise<boolean> {
    if (!root) { setOutput('请先在左侧添加机器人目录。'); return false }
    setBusy(true)
    try {
      if (method === 'GET') { const result = await readRobotFile({ root: data.root, file: data.file }, true).unwrap(); dispatch(setDraft({ key: `${data.root}:${data.file}`, content: result.output ?? '' })); return true }
      const result = method === 'PUT' ? await writeRobotFile({ root: data.root, file: data.file, content: data.content }).unwrap() : await runRobotOperation(data).unwrap()
      setOutput(result.output ?? '操作完成。'); return true
    } catch (reason) { setOutput(reason instanceof Error ? reason.message : '操作未完成。'); return false } finally { setBusy(false) }
  }

  async function savePackageConfig(packageName: string, values: Record<string, string>): Promise<boolean> {
    if (!root) return false
    setBusy(true)
    try {
      const result = await writePackageConfig({ root, package: packageName, values }).unwrap()
      setOutput(result.output ?? '机器人运行配置已保存。')
      return true
    } catch (reason) { setOutput(reason instanceof Error ? reason.message : '配置未保存。'); return false } finally { setBusy(false) }
  }

  async function initializeProjectGit(values: { authorName: string; authorEmail: string; repository: string; message: string }): Promise<boolean> {
    if (!root) return false
    setBusy(true)
    try {
      const result = await initializeGit({ root, ...values }).unwrap()
      setOutput(result.output ?? 'Git 仓库已初始化。')
      return true
    } catch (reason) { setOutput(reason instanceof Error ? reason.message : 'Git 初始化未完成。'); return false } finally { setBusy(false) }
  }

  async function chooseDirectories() {
    const response = await fetch('/api/v1/directories/select', { method: 'POST' })
    const data = await response.json() as { paths?: string[]; path?: string }
    const paths = data.paths ?? (data.path ? [data.path] : [])
    if (!response.ok || !paths.length) return
    dispatch(addProjects(paths.map((path) => ({ id: path, path, name: projectName(path) }))))
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
    {section === 'config' && <section className="config-form">{configEditor === 'visual' ? <RobotConfigForm busy={busy} toolbar={<EditorMode active={configEditor} onVisual={() => setConfigEditor('visual')} onText={openTextConfig} />} onSave={(config) => api('PUT', { root, file: 'alemon.config.yaml', content: config })} /> : <FileEditor toolbar={<EditorMode active={configEditor} onVisual={() => setConfigEditor('visual')} onText={openTextConfig} />} content={content} busy={busy} placeholder="配置内容" onChange={setContent} onSave={() => api('PUT', { root, file, content })} />}</section>}
    {section === 'actions' && <RunPanel mode={runMode} busy={busy} onRun={(action) => api('POST', { root, action })} />}
  </section>

  const catalogContent = <section className="workspace-content">{catalogLoading && <p className="catalog-state">正在读取目录…</p>}{catalogError && <p className="catalog-state">{catalogError}</p>}{!catalogLoading && !catalogError && currentCatalog && (catalogItem ? <CatalogDetail item={catalogItem} group={currentCatalog.title} busy={busy} onBack={() => setCatalogItem(null)} onRun={(action, packageName) => api('POST', { root, action, package: packageName })} onSaveConfig={savePackageConfig} /> : <section className="catalog-items">{currentCatalog.items.map((item) => <button className="catalog-item" key={`${currentCatalog.title}-${item.name}`} onClick={() => setCatalogItem(item)}><strong>{item.name}</strong><span>›</span></button>)}</section>)}</section>
  const workspace = systemFeature === 'plugins' ? <SystemPluginCenter /> : activeProject ? <>{page === 'robot' && robotContent}{page === 'build' && <section className="workspace-content build-page">{buildMode === 'npm' ? <NpmPublishPanel root={root} busy={busy} onRun={(action, values) => api('POST', { root, action, ...values })} /> : <GitReleasePanel root={root} busy={busy} version={releaseVersion} confirmed={gitConfirm} onVersionChange={(value) => { setReleaseVersion(value); setGitConfirm(false) }} onConfirm={() => setGitConfirm((value) => !value)} onInitialize={initializeProjectGit} onRun={() => api('POST', { root, action: 'git-release', version: releaseVersion, confirm: 'true' })} />}{output && <OperationLog output={output} onClose={() => setOutput('')} />}</section>}{(page === 'plugins' || page === 'connections') && catalogContent}{page !== 'build' && output && <OperationLog output={output} onClose={() => setOutput('')} />}</> : <EmptyWorkspace onAdd={chooseDirectories} />

  const environmentReady = report ? `${readyCount}/${report.checks.length}` : '—'
  const environmentWarning = Boolean(report?.checks.some((item) => item.status !== 'ready'))

  return <main className="guide-shell"><section className="guide-window dashboard-window"><header className="guide-bar dashboard-toolbar"><a className="workspace-name" href="https://alemonjs.com/" target="_blank" rel="noreferrer">ALEMONJS</a><div className="header-global-actions"><button className={`environment-control ${environmentWarning ? 'warning' : ''}`} onClick={() => { setEnvironmentOpen(true); onCheck() }} disabled={checking} title="查看并检查全局环境"><i>{checking ? '◌' : environmentWarning ? '!' : '✓'}</i><span>环境</span><strong>{checking ? '检查中' : environmentReady}</strong></button><button className="guide-trigger" onClick={onOpenGuide} aria-label="打开引导" title="打开引导">?</button></div></header><EnvironmentPanel open={environmentOpen} report={report} checking={checking} onClose={() => setEnvironmentOpen(false)} onRefresh={onCheck} onFix={onFix} /><section className="console-layout">
    <ProjectRail feature={systemFeature} projects={projects} activeID={activeProjectID} onFeature={selectSystemFeature} onAdd={chooseDirectories} onSelect={(id) => { dispatch(selectProject(id)); setSystemFeature(null); setPage('robot'); setSection('config'); setOutput('') }} onRemove={removeProject} />
    <section className="console-page">{workspace}{error && <p className="error">{error}</p>}{!systemFeature && <ControlCard page={page} section={section} runMode={runMode} project={activeProject} buildMode={buildMode} catalog={catalog} catalogTitle={catalogTitle} onPage={selectPage} onSection={openSection} onRunMode={(mode) => { setRunMode(mode); openSection('actions') }} onBuildMode={(mode) => { setBuildMode(mode); setGitConfirm(false); setOutput('') }} onCatalog={(title) => { setCatalogTitle(title); setCatalogItem(null) }} />}</section>
  </section></section></main>
}

function ProjectRail({ feature, projects, activeID, onFeature, onAdd, onSelect, onRemove }: { feature: SystemFeature | null; projects: Project[]; activeID: string; onFeature: (feature: SystemFeature) => void; onAdd: () => void; onSelect: (id: string) => void; onRemove: (id: string) => void }) { return <aside className="project-rail"><section className="feature-catalog" aria-label="系统功能目录"><header><span>功能目录</span><small>系统</small></header><nav>{featureCatalog.map((item) => <button className={feature === item.id ? 'active' : ''} key={item.id} onClick={() => onFeature(item.id)}><i>{item.icon}</i><span>{item.label}</span>{item.status && <small>{item.status}</small>}</button>)}</nav></section><section className="project-directory"><header><div><strong>机器人目录</strong><span>{projects.length}</span></div><button onClick={onAdd} aria-label="添加机器人目录" title="添加机器人目录">＋</button></header><div className="project-list">{projects.map((project) => <article className={project.id === activeID ? 'active' : ''} key={project.id}><button className="project-select" onClick={() => onSelect(project.id)}><strong>{project.name}</strong><small title={project.path}>{project.path}</small></button><button className="project-remove" onClick={() => onRemove(project.id)} aria-label={`移除 ${project.name}`} title="移除目录">×</button></article>)}{!projects.length && <p>添加目录开始管理</p>}</div></section></aside> }
function EnvironmentPanel({ open, report, checking, onClose, onRefresh, onFix }: { open: boolean; report: { checks: Check[] } | null; checking: boolean; onClose: () => void; onRefresh: () => void; onFix: (check: Check) => void }) { if (!open) return null; const checks = report?.checks ?? []; const readyCount = checks.filter((check) => check.status === 'ready').length; return <aside className="environment-panel" role="dialog" aria-label="全局环境详情"><header><strong>{checking ? '正在检查环境…' : checks.length ? `${readyCount}/${checks.length} 已就绪` : '等待检查'}</strong><button onClick={onClose} aria-label="关闭环境详情">×</button></header>{checking && <p className="environment-panel-state">正在读取 Node.js、Git 和系统工具状态。</p>}{!checking && checks.length > 0 && <div className="environment-check-list">{checks.map((check) => <article className={check.status} key={check.id}><i>{check.status === 'ready' ? '✓' : '!'}</i><div><strong>{check.name}</strong><span>{check.detail}</span>{check.status !== 'ready' && check.suggestion && <small>{check.suggestion}</small>}</div>{check.status !== 'ready' && <button className="text-button" onClick={() => onFix(check)}>修复</button>}</article>)}</div>}{!checking && !checks.length && <p className="environment-panel-state">尚未获取检查结果。</p>}<footer><button className="secondary-button" disabled={checking} onClick={onRefresh}>重新检查</button></footer></aside> }
function EmptyWorkspace({ onAdd }: { onAdd: () => void }) { return <section className="workspace-content empty-workspace"><span>◈</span><strong>从左侧添加机器人目录</strong><button className="primary-button" onClick={onAdd}>添加目录</button></section> }
function SystemPluginCenter() { return <section className="workspace-content system-feature-placeholder"><span>▦</span><p>系统功能</p><h1>系统插件</h1><strong>即将支持</strong></section> }
function BackpackPanel({ root, items, loading, failed, onRefresh, onOpenPlugins }: { root: string; items: Array<{ name: string; path: string }>; loading: boolean; failed: boolean; onRefresh: () => void; onOpenPlugins: () => void }) { return <section className="backpack-panel"><header><div><p>本地插件包</p><h1>背包</h1><small title={`${root}/packages`}>packages</small></div><button className="secondary-button" disabled={loading} onClick={onRefresh}>{loading ? '读取中…' : '刷新'}</button></header>{loading ? <p className="catalog-state">正在读取本地插件包…</p> : items.length ? <div className="backpack-items">{items.map((item) => <article key={item.path}><i>▣</i><div><strong>{item.name}</strong><small title={item.path}>{item.path}</small></div></article>)}</div> : <section className="backpack-empty"><strong>暂无插件包</strong><span>{failed ? '暂未能读取本地 packages 目录，你仍可从插件页安装。' : '安装后的本地插件包会显示在这里。'}</span><button className="secondary-button" onClick={onOpenPlugins}>前往插件</button></section>}</section> }
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
function RunPanel({ mode, busy, onRun }: { mode: 'dependencies' | 'development' | 'background'; busy: boolean; onRun: (action: string) => void }) { const views = { dependencies: { title: '依赖管理', note: '检查当前目录是否已安装依赖；缺失时再执行安装或修复。', primary: '检查依赖', action: 'dependency-status', secondary: '安装或修复依赖', secondaryAction: 'install' }, development: { title: '开发模式', note: '以开发模式启动机器人，操作输出会显示在控制台中。', primary: '启动开发模式', action: 'dev' }, background: { title: '后台运行', note: '构建后交由 PM2 守护运行，适合持续在线的机器人。', primary: '使用 PM2 启动', action: 'pm2' } }[mode]; return <section className="run-panel"><header><div><p>运行</p><h1>{views.title}</h1><small>{views.note}</small></div></header><div className="run-panel-actions"><button className="primary-button" disabled={busy} onClick={() => onRun(views.action)}>{busy ? '处理中…' : views.primary}</button>{views.secondary && <button className="secondary-button" disabled={busy} onClick={() => onRun(views.secondaryAction!)}>{views.secondary}</button>}</div>{mode === 'development' && <p className="run-console-note">启动后的输出会显示在右下角“操作日志”中。</p>}</section> }
function ControlCard({ page, section, runMode, project, buildMode, catalog, catalogTitle, onPage, onSection, onRunMode, onBuildMode, onCatalog }: { page: Page; section: Section; runMode: 'dependencies' | 'development' | 'background'; project?: Project; buildMode: 'npm' | 'git'; catalog: CatalogGroup[]; catalogTitle: string; onPage: (page: Page) => void; onSection: (section: Section) => void; onRunMode: (mode: 'dependencies' | 'development' | 'background') => void; onBuildMode: (mode: 'npm' | 'git') => void; onCatalog: (title: string) => void }) {
  const activePrimary = page === 'robot' ? section === 'actions' ? 'actions' : section === 'backpack' ? 'backpack' : 'config' : page
  const subitems = activePrimary === 'config' ? [{ id: 'config', label: '配置' }, { id: 'npmrc', label: '镜像' }] : activePrimary === 'actions' ? [{ id: 'dependencies', label: '依赖' }, { id: 'development', label: '开发' }, { id: 'background', label: '后台' }] : activePrimary === 'build' ? [{ id: 'git', label: 'Git 打包' }, { id: 'npm', label: 'NPM 发布' }] : activePrimary === 'backpack' ? [] : catalog.map((item) => ({ id: item.title, label: item.title }))
  const activeSecondary = activePrimary === 'config' ? section : activePrimary === 'actions' ? runMode : activePrimary === 'build' ? buildMode : catalogTitle
  function selectPrimary(item: typeof directoryActions[number]) { if (item.kind === 'section') { onPage('robot'); onSection(item.id as Section); return }; onPage(item.id as Page) }
  function selectSecondary(id: string) { if (activePrimary === 'config') { onSection(id as Section); return }; if (activePrimary === 'actions') { onRunMode(id as 'dependencies' | 'development' | 'background'); return }; if (activePrimary === 'build') { onBuildMode(id as 'npm' | 'git'); return }; onCatalog(id) }
  return <aside className="control-dock" aria-label="目录操作"><section className="control-card"><header><div><span>当前机器人</span><strong>{project?.name ?? '未选择目录'}</strong></div><i>◈</i></header><div className="control-list">{directoryActions.map((item) => <button className={activePrimary === item.id ? 'active' : ''} onClick={() => selectPrimary(item)} key={item.id}><i>{item.icon}</i><span>{item.label}</span><b>›</b></button>)}</div>{subitems.length > 0 && <><span className="control-divider" /><div className="control-sublist">{subitems.map((item) => <button className={activeSecondary === item.id ? 'active' : ''} onClick={() => selectSecondary(item.id)} key={item.id}>{item.label}<b>›</b></button>)}</div></>}{project && <footer title={project.path}><span>当前目录</span><strong>{project.path}</strong></footer>}</section></aside> }
function EditorMode({ active, onVisual, onText }: { active: 'visual' | 'text'; onVisual: () => void; onText: () => void }) { return <div className="editor-mode" aria-label="配置编辑模式"><button className={active === 'visual' ? 'active' : ''} onClick={onVisual}>表单</button><button className={active === 'text' ? 'active' : ''} onClick={onText}>文本</button></div> }
function FileEditor({ toolbar, content, busy, placeholder, onChange, onSave }: { toolbar?: ReactNode; content: string; busy: boolean; placeholder: string; onChange: (value: string) => void; onSave: () => void }) { return <section className="file-editor"><header>{toolbar}<button className="primary-button" disabled={busy} onClick={onSave}>保存</button></header><textarea value={content} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} /></section> }
function OperationLog({ output, onClose }: { output: string; onClose: () => void }) { return <aside className="robot-output" aria-label="操作日志"><header><strong>操作日志</strong><button onClick={onClose} aria-label="关闭操作日志">×</button></header><pre>{output}</pre></aside> }
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
  return <section className="git-release-panel"><header className="release-toolbar"><span title={status?.root || root}>{status?.packageName ? `${status.packageName}@${status.packageVersion || '未设置版本'} · ${status.packageManager}` : 'Git 打包'}</span><button className="secondary-button" onClick={refresh} disabled={loading || busy}>刷新</button></header>{loading ? <p className="publish-state">正在读取所选目录的 Git 状态…</p> : <><div className="release-safety">{checks.map((item) => <span key={item}>✓ {item}</span>)}</div>{issues.length > 0 && <section className="release-blockers"><ul>{issues.map((item) => <li key={item}>{item}</li>)}</ul>{needsInitialize && <section className="git-init-form"><strong>初始化当前项目仓库</strong><p>将在所选目录创建独立 Git 仓库，不会修改父目录仓库或全局 Git 身份。</p><div><label>提交姓名<input value={gitInit.authorName} onChange={(event) => setGitInit({ ...gitInit, authorName: event.target.value })} placeholder="你的姓名" /></label><label>提交邮箱<input type="email" value={gitInit.authorEmail} onChange={(event) => setGitInit({ ...gitInit, authorEmail: event.target.value })} placeholder="name@example.com" /></label><label>origin（可选）<input value={gitInit.repository} onChange={(event) => setGitInit({ ...gitInit, repository: event.target.value })} placeholder="https://github.com/owner/repo.git" /></label><label>首个提交<input value={gitInit.message} onChange={(event) => setGitInit({ ...gitInit, message: event.target.value })} /></label></div><button className="primary-button" disabled={busy || initializing || !gitInit.authorName.trim() || !gitInit.authorEmail.trim()} onClick={() => void submitInitialize()}>{initializing ? '正在初始化…' : '确认初始化 Git'}</button></section>}</section>}<section className="release-action-row"><label>版本<input value={version || status?.suggestedVersion || ''} onChange={(event) => onVersionChange(event.target.value)} placeholder="v0.0.1" /></label><button className="primary-button release-button" disabled={busy || !ready} onClick={confirmed ? onRun : onConfirm}>{busy ? '打包中…' : confirmed ? '确认打包' : '准备打包'}</button>{confirmed && <button className="text-button" onClick={onConfirm}>取消</button>}</section><details className="release-details"><summary>发布详情</summary><div><span title={status?.root || root}>目录：{status?.root || root}</span><span>建议 {status?.suggestedVersion || '—'}</span><span>{status?.releaseBranch ? 'release 分支已就绪' : '首次打包会创建 release 分支'}</span><span>{(status?.artifacts ?? []).length ? (status?.artifacts ?? []).join(' · ') : '构建后生成发布文件'}</span>{(status?.tags ?? []).slice(0, 3).map((item) => <span key={item}>{item}</span>)}{status?.gitHubActionsUrl && <a href={status.gitHubActionsUrl} target="_blank" rel="noreferrer">Actions</a>}</div></details></>}</section>
}

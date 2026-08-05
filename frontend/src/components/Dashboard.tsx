import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { RobotConfigForm } from './RobotConfigForm'
import { NpmrcConfigForm } from './NpmrcConfigForm'
import { NpmPublishPanel } from './NpmPublishPanel'

type Check = { id: string; name: string; status: 'ready' | 'missing' | 'warning'; detail: string; suggestion: string }
type CatalogGroup = { title: string; items: Array<{ name: string; description: string; url: string; install: string }> }
type Page = 'robot' | 'build' | 'plugins' | 'connections'
type Section = 'config' | 'npmrc' | 'actions'
type Project = { id: string; path: string; name: string }
type SystemFeature = 'plugins'
type Props = { report: { checks: Check[] } | null; checking: boolean; error: string; defaultPage: string; onOpenGuide: () => void; onCheck: () => void; onFix: (check: Check) => void; goals?: unknown; goal?: unknown; onSelect?: (id: string) => void }

const projectStorageKey = 'alemonjs-setup-projects'
const featureCatalog: Array<{ id: SystemFeature; label: string; icon: string; status?: string }> = [{ id: 'plugins', label: '插件', icon: '▦', status: '即将支持' }]
const directoryActions: Array<{ id: Section | Page; label: string; icon: string; kind: 'section' | 'page' }> = [{ id: 'config', label: '配置', icon: '⚙', kind: 'section' }, { id: 'actions', label: '运行', icon: '▶', kind: 'section' }, { id: 'connections', label: '连接', icon: '⌁', kind: 'page' }, { id: 'plugins', label: '插件', icon: '▦', kind: 'page' }, { id: 'build', label: '发布', icon: '⌗', kind: 'page' }]

function projectName(path: string) { return path.replace(/\/$/, '').split('/').pop() || path }
function savedProjects() { try { const value = window.localStorage.getItem(projectStorageKey); return value ? JSON.parse(value) as Project[] : [] } catch { return [] } }

export function Dashboard({ report, checking, error, defaultPage, onOpenGuide, onCheck, onFix }: Props) {
  const [page, setPage] = useState<Page>('robot')
  const [systemFeature, setSystemFeature] = useState<SystemFeature | null>(null)
  const [section, setSection] = useState<Section>('config')
  const [projects, setProjects] = useState<Project[]>(savedProjects)
  const [activeProjectID, setActiveProjectID] = useState(() => savedProjects()[0]?.id ?? '')
  const [file, setFile] = useState('.npmrc')
  const [content, setContent] = useState('')
  const [output, setOutput] = useState('')
  const [busy, setBusy] = useState(false)
  const [catalog, setCatalog] = useState<CatalogGroup[]>([])
  const [catalogLoading, setCatalogLoading] = useState(false)
  const [catalogError, setCatalogError] = useState('')
  const [catalogTitle, setCatalogTitle] = useState('')
  const [configEditor, setConfigEditor] = useState<'visual' | 'text'>('visual')
  const [runMode, setRunMode] = useState<'dependencies' | 'development' | 'background'>('dependencies')
  const [buildMode, setBuildMode] = useState<'npm' | 'git'>('git')
  const [releaseVersion, setReleaseVersion] = useState('')
  const [gitConfirm, setGitConfirm] = useState(false)
  const [environmentOpen, setEnvironmentOpen] = useState(false)
  const environmentChecked = useRef(false)
  const activeProject = projects.find((item) => item.id === activeProjectID)
  const root = activeProject?.path ?? ''

  useEffect(() => { if (defaultPage === 'robot') setPage('robot') }, [defaultPage])
  useEffect(() => { window.localStorage.setItem(projectStorageKey, JSON.stringify(projects)) }, [projects])
  useEffect(() => {
    if (report || checking || environmentChecked.current) return
    environmentChecked.current = true
    onCheck()
  }, [checking, onCheck, page, report])
  useEffect(() => {
    if (page !== 'plugins' && page !== 'connections') return
    setCatalogLoading(true); setCatalogError('')
    fetch(`/api/v1/catalog?kind=${page === 'plugins' ? 'apps' : 'environment'}`)
      .then(async (response) => { if (!response.ok) { const data = await response.json() as { error?: string }; throw new Error(data.error ?? '在线目录暂时无法读取。') }; return response.json() as Promise<CatalogGroup[]> })
      .then((data) => { setCatalog(data); setCatalogTitle(data[0]?.title ?? '') })
      .catch((reason) => { setCatalog([]); setCatalogTitle(''); setCatalogError(reason instanceof Error ? reason.message : '在线目录暂时无法读取。') })
      .finally(() => setCatalogLoading(false))
  }, [page])

  async function api(method: string, data: Record<string, string>): Promise<boolean> {
    if (!root) { setOutput('请先在左侧添加机器人目录。'); return false }
    setBusy(true)
    try {
      const query = method === 'GET' ? `?${new URLSearchParams(data)}` : ''
      const response = await fetch(`/api/v1/robot${query}`, method === 'GET' ? {} : { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(data) })
      const json = await response.json() as { output?: string; error?: string }
      if (!response.ok) throw new Error(json.error)
      if (method === 'GET') { setContent(json.output ?? ''); return true }
      setOutput(json.output ?? '操作完成。'); return true
    } catch (reason) { setOutput(reason instanceof Error ? reason.message : '操作未完成。'); return false } finally { setBusy(false) }
  }

  async function chooseDirectories() {
    const response = await fetch('/api/v1/directories/select', { method: 'POST' })
    const data = await response.json() as { paths?: string[]; path?: string }
    const paths = data.paths ?? (data.path ? [data.path] : [])
    if (!response.ok || !paths.length) return
    const knownPaths = new Set(projects.map((item) => item.path))
    const additions = paths.filter((path) => !knownPaths.has(path)).map((path) => ({ id: path, path, name: projectName(path) }))
    const nextProjects = [...projects, ...additions]
    setProjects(nextProjects)
    setActiveProjectID(additions[0]?.id ?? nextProjects[0]?.id ?? '')
    setPage('robot'); setSection('config'); setOutput('')
  }

  function removeProject(id: string) {
    const nextProjects = projects.filter((item) => item.id !== id)
    setProjects(nextProjects)
    if (id === activeProjectID) setActiveProjectID(nextProjects[0]?.id ?? '')
    setOutput('')
  }

  function openSection(nextSection: Section) {
    setSection(nextSection); setOutput('')
    if (nextSection === 'npmrc') { setFile('.npmrc'); api('GET', { root, file: '.npmrc' }) }
  }
  function openTextConfig() { setConfigEditor('text'); setFile('alemon.config.yaml'); api('GET', { root, file: 'alemon.config.yaml' }) }
  function selectPage(nextPage: Page) { setSystemFeature(null); setPage(nextPage); setOutput('') }
  function selectSystemFeature(nextFeature: SystemFeature) { setSystemFeature(nextFeature); setOutput('') }

  const currentCatalog = catalog.find((group) => group.title === catalogTitle) ?? catalog[0]
  const readyCount = report?.checks.filter((item) => item.status === 'ready').length ?? 0
  const robotContent = <section className="workspace-content">
    {section === 'npmrc' && <NpmrcConfigForm content={content} busy={busy} onChange={setContent} onSave={(nextContent) => api('PUT', { root, file: '.npmrc', content: nextContent })} />}
    {section === 'config' && <section className="config-form">{configEditor === 'visual' ? <RobotConfigForm busy={busy} toolbar={<EditorMode active={configEditor} onVisual={() => setConfigEditor('visual')} onText={openTextConfig} />} onSave={(config) => api('PUT', { root, file: 'alemon.config.yaml', content: config })} /> : <FileEditor toolbar={<EditorMode active={configEditor} onVisual={() => setConfigEditor('visual')} onText={openTextConfig} />} content={content} busy={busy} placeholder="配置内容" onChange={setContent} onSave={() => api('PUT', { root, file, content })} />}</section>}
    {section === 'actions' && <RunPanel mode={runMode} busy={busy} onRun={(action) => api('POST', { root, action })} />}
  </section>

  const catalogContent = <section className="workspace-content">{catalogLoading && <p className="catalog-state">正在读取目录…</p>}{catalogError && <p className="catalog-state">{catalogError}</p>}{!catalogLoading && !catalogError && currentCatalog && <section className="catalog-items">{currentCatalog.items.map((item) => <article className="catalog-item" key={`${currentCatalog.title}-${item.name}`}><div><strong>{item.name}</strong>{item.description && <p>{item.description}</p>}</div><div className="catalog-actions">{item.url && <a href={item.url} target="_blank" rel="noreferrer">详情</a>}<button className="primary-button" disabled={busy || !item.install} onClick={() => api('POST', { root, action: 'install-package', package: item.install })}>{item.install ? '安装' : '不可用'}</button></div></article>)}</section>}</section>
  const workspace = systemFeature === 'plugins' ? <SystemPluginCenter /> : activeProject ? <>{page === 'robot' && robotContent}{page === 'build' && <section className="workspace-content build-page">{buildMode === 'npm' ? <NpmPublishPanel root={root} busy={busy} onRun={(action, values) => api('POST', { root, action, ...values })} /> : <GitReleasePanel root={root} busy={busy} version={releaseVersion} confirmed={gitConfirm} onVersionChange={(value) => { setReleaseVersion(value); setGitConfirm(false) }} onConfirm={() => setGitConfirm((value) => !value)} onInit={() => api('POST', { root, action: 'git-init' })} onRun={() => api('POST', { root, action: 'git-release', version: releaseVersion, confirm: 'true' })} />}{output && <OperationLog output={output} onClose={() => setOutput('')} />}</section>}{(page === 'plugins' || page === 'connections') && catalogContent}{page !== 'build' && output && <OperationLog output={output} onClose={() => setOutput('')} />}</> : <EmptyWorkspace onAdd={chooseDirectories} />

  const environmentReady = report ? `${readyCount}/${report.checks.length}` : '—'
  const environmentWarning = Boolean(report?.checks.some((item) => item.status !== 'ready'))

  return <main className="guide-shell"><section className="guide-window dashboard-window"><header className="guide-bar dashboard-toolbar"><a className="workspace-name" href="https://alemonjs.com/" target="_blank" rel="noreferrer">ALEMONJS</a><div className="header-global-actions"><button className={`environment-control ${environmentWarning ? 'warning' : ''}`} onClick={() => { setEnvironmentOpen(true); onCheck() }} disabled={checking} title="查看并检查全局环境"><i>{checking ? '◌' : environmentWarning ? '!' : '✓'}</i><span>环境</span><strong>{checking ? '检查中' : environmentReady}</strong></button><button className="guide-trigger" onClick={onOpenGuide} aria-label="打开引导" title="打开引导">?</button></div></header><EnvironmentPanel open={environmentOpen} report={report} checking={checking} onClose={() => setEnvironmentOpen(false)} onRefresh={onCheck} onFix={onFix} /><section className="console-layout">
    <ProjectRail feature={systemFeature} projects={projects} activeID={activeProjectID} onFeature={selectSystemFeature} onAdd={chooseDirectories} onSelect={(id) => { setActiveProjectID(id); setSystemFeature(null); setPage('robot'); setSection('config'); setOutput('') }} onRemove={removeProject} />
    <section className="console-page">{workspace}{error && <p className="error">{error}</p>}{!systemFeature && <ControlCard page={page} section={section} runMode={runMode} project={activeProject} buildMode={buildMode} catalog={catalog} catalogTitle={catalogTitle} onPage={selectPage} onSection={openSection} onRunMode={(mode) => { setRunMode(mode); openSection('actions') }} onBuildMode={(mode) => { setBuildMode(mode); setGitConfirm(false); setOutput('') }} onCatalog={setCatalogTitle} />}</section>
  </section></section></main>
}

function ProjectRail({ feature, projects, activeID, onFeature, onAdd, onSelect, onRemove }: { feature: SystemFeature | null; projects: Project[]; activeID: string; onFeature: (feature: SystemFeature) => void; onAdd: () => void; onSelect: (id: string) => void; onRemove: (id: string) => void }) { return <aside className="project-rail"><section className="feature-catalog" aria-label="系统功能目录"><header><span>功能目录</span><small>系统</small></header><nav>{featureCatalog.map((item) => <button className={feature === item.id ? 'active' : ''} key={item.id} onClick={() => onFeature(item.id)}><i>{item.icon}</i><span>{item.label}</span>{item.status && <small>{item.status}</small>}</button>)}</nav></section><section className="project-directory"><header><div><strong>机器人目录</strong><span>{projects.length}</span></div><button onClick={onAdd} aria-label="添加机器人目录" title="添加机器人目录">＋</button></header><div className="project-list">{projects.map((project) => <article className={project.id === activeID ? 'active' : ''} key={project.id}><button className="project-select" onClick={() => onSelect(project.id)}><strong>{project.name}</strong><small title={project.path}>{project.path}</small></button><button className="project-remove" onClick={() => onRemove(project.id)} aria-label={`移除 ${project.name}`} title="移除目录">×</button></article>)}{!projects.length && <p>添加目录开始管理</p>}</div></section></aside> }
function EnvironmentPanel({ open, report, checking, onClose, onRefresh, onFix }: { open: boolean; report: { checks: Check[] } | null; checking: boolean; onClose: () => void; onRefresh: () => void; onFix: (check: Check) => void }) { if (!open) return null; const checks = report?.checks ?? []; const readyCount = checks.filter((check) => check.status === 'ready').length; return <aside className="environment-panel" role="dialog" aria-label="全局环境详情"><header><strong>{checking ? '正在检查环境…' : checks.length ? `${readyCount}/${checks.length} 已就绪` : '等待检查'}</strong><button onClick={onClose} aria-label="关闭环境详情">×</button></header>{checking && <p className="environment-panel-state">正在读取 Node.js、Git 和系统工具状态。</p>}{!checking && checks.length > 0 && <div className="environment-check-list">{checks.map((check) => <article className={check.status} key={check.id}><i>{check.status === 'ready' ? '✓' : '!'}</i><div><strong>{check.name}</strong><span>{check.detail}</span>{check.status !== 'ready' && check.suggestion && <small>{check.suggestion}</small>}</div>{check.status !== 'ready' && <button className="text-button" onClick={() => onFix(check)}>修复</button>}</article>)}</div>}{!checking && !checks.length && <p className="environment-panel-state">尚未获取检查结果。</p>}<footer><button className="secondary-button" disabled={checking} onClick={onRefresh}>重新检查</button></footer></aside> }
function EmptyWorkspace({ onAdd }: { onAdd: () => void }) { return <section className="workspace-content empty-workspace"><span>◈</span><strong>从左侧添加机器人目录</strong><button className="primary-button" onClick={onAdd}>添加目录</button></section> }
function SystemPluginCenter() { return <section className="workspace-content system-feature-placeholder"><span>▦</span><p>系统功能</p><h1>系统插件</h1><strong>即将支持</strong><small>这里将用于安装和管理 AlemonJS Setup 自身的扩展，不会写入任何机器人目录。</small></section> }
function RunPanel({ mode, busy, onRun }: { mode: 'dependencies' | 'development' | 'background'; busy: boolean; onRun: (action: string) => void }) { const views = { dependencies: { title: '依赖管理', note: '检查当前目录是否已安装依赖；缺失时再执行安装或修复。', primary: '检查依赖', action: 'dependency-status', secondary: '安装或修复依赖', secondaryAction: 'install' }, development: { title: '开发模式', note: '以开发模式启动机器人，操作输出会显示在控制台中。', primary: '启动开发模式', action: 'dev' }, background: { title: '后台运行', note: '构建后交由 PM2 守护运行，适合持续在线的机器人。', primary: '使用 PM2 启动', action: 'pm2' } }[mode]; return <section className="run-panel"><header><div><p>运行</p><h1>{views.title}</h1><small>{views.note}</small></div></header><div className="run-panel-actions"><button className="primary-button" disabled={busy} onClick={() => onRun(views.action)}>{busy ? '处理中…' : views.primary}</button>{views.secondary && <button className="secondary-button" disabled={busy} onClick={() => onRun(views.secondaryAction!)}>{views.secondary}</button>}</div>{mode === 'development' && <p className="run-console-note">启动后的输出会显示在右下角“操作日志”中。</p>}</section> }
function ControlCard({ page, section, runMode, project, buildMode, catalog, catalogTitle, onPage, onSection, onRunMode, onBuildMode, onCatalog }: { page: Page; section: Section; runMode: 'dependencies' | 'development' | 'background'; project?: Project; buildMode: 'npm' | 'git'; catalog: CatalogGroup[]; catalogTitle: string; onPage: (page: Page) => void; onSection: (section: Section) => void; onRunMode: (mode: 'dependencies' | 'development' | 'background') => void; onBuildMode: (mode: 'npm' | 'git') => void; onCatalog: (title: string) => void }) {
  const activePrimary = page === 'robot' ? section === 'actions' ? 'actions' : 'config' : page
  const subitems = activePrimary === 'config' ? [{ id: 'config', label: '配置' }, { id: 'npmrc', label: '镜像' }] : activePrimary === 'actions' ? [{ id: 'dependencies', label: '依赖' }, { id: 'development', label: '开发' }, { id: 'background', label: '后台' }] : activePrimary === 'build' ? [{ id: 'git', label: 'Git 打包' }, { id: 'npm', label: 'NPM 发布' }] : catalog.map((item) => ({ id: item.title, label: item.title }))
  const activeSecondary = activePrimary === 'config' ? section : activePrimary === 'actions' ? runMode : activePrimary === 'build' ? buildMode : catalogTitle
  function selectPrimary(item: typeof directoryActions[number]) { if (item.kind === 'section') { onPage('robot'); onSection(item.id as Section); return }; onPage(item.id as Page) }
  function selectSecondary(id: string) { if (activePrimary === 'config') { onSection(id as Section); return }; if (activePrimary === 'actions') { onRunMode(id as 'dependencies' | 'development' | 'background'); return }; if (activePrimary === 'build') { onBuildMode(id as 'npm' | 'git'); return }; onCatalog(id) }
  return <aside className="control-dock" aria-label="目录操作"><section className="control-card"><header><div><span>当前机器人</span><strong>{project?.name ?? '未选择目录'}</strong></div><i>◈</i></header><div className="control-list">{directoryActions.map((item) => <button className={activePrimary === item.id ? 'active' : ''} onClick={() => selectPrimary(item)} key={item.id}><i>{item.icon}</i><span>{item.label}</span><b>›</b></button>)}</div><span className="control-divider" /><div className="control-sublist">{subitems.map((item) => <button className={activeSecondary === item.id ? 'active' : ''} onClick={() => selectSecondary(item.id)} key={item.id}>{item.label}<b>›</b></button>)}</div>{project && <footer title={project.path}><span>当前目录</span><strong>{project.path}</strong></footer>}</section></aside> }
function EditorMode({ active, onVisual, onText }: { active: 'visual' | 'text'; onVisual: () => void; onText: () => void }) { return <div className="editor-mode" aria-label="配置编辑模式"><button className={active === 'visual' ? 'active' : ''} onClick={onVisual}>表单</button><button className={active === 'text' ? 'active' : ''} onClick={onText}>文本</button></div> }
function FileEditor({ toolbar, content, busy, placeholder, onChange, onSave }: { toolbar?: ReactNode; content: string; busy: boolean; placeholder: string; onChange: (value: string) => void; onSave: () => void }) { return <section className="file-editor"><header>{toolbar}<button className="primary-button" disabled={busy} onClick={onSave}>保存</button></header><textarea value={content} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} /></section> }
function OperationLog({ output, onClose }: { output: string; onClose: () => void }) { return <aside className="robot-output" aria-label="操作日志"><header><strong>操作日志</strong><button onClick={onClose} aria-label="关闭操作日志">×</button></header><pre>{output}</pre></aside> }
function GitReleasePanel({ root, busy, version, confirmed, onVersionChange, onConfirm, onInit, onRun }: { root: string; busy: boolean; version: string; confirmed: boolean; onVersionChange: (value: string) => void; onConfirm: () => void; onInit: () => void; onRun: () => void }) {
  type GitStatus = { repository?: string; packageName?: string; packageVersion?: string; packageManager?: string; gitHubActionsUrl?: string; workflowConfigured?: boolean; latestVersion?: string; suggestedVersion?: string; tags?: string[]; commits?: string[]; artifacts?: string[]; gitReady?: boolean; releaseBranch?: boolean; checks?: string[]; issues?: string[] }
  const [status, setStatus] = useState<GitStatus | null>(null); const [loading, setLoading] = useState(true)
  const refresh = useCallback(() => { if (!root) { setStatus(null); setLoading(false); return }; setLoading(true); fetch(`/api/v1/publish/git/status?${new URLSearchParams({ root })}`).then(async (response) => { const data = await response.json(); if (!response.ok) throw new Error(data.error); return data }).then(setStatus).catch(() => setStatus({ issues: ['无法读取 Git 发布状态。'] })).finally(() => setLoading(false)) }, [root])
  useEffect(() => { refresh() }, [refresh])
  const checks = status?.checks ?? []; const issues = status?.issues ?? []; const blockingIssues = issues.filter((item) => !item.startsWith('尚未发现 lib')); const ready = !loading && blockingIssues.length === 0
  return <section className="git-release-panel"><header className="release-toolbar"><span>{status?.packageName ? `${status.packageName}@${status.packageVersion || '未设置版本'} · ${status.packageManager}` : 'Git 打包'}</span><button className="secondary-button" onClick={refresh} disabled={loading || busy}>刷新</button></header>{loading ? <p className="publish-state">正在读取 Git 状态…</p> : <><div className="release-safety">{checks.map((item) => <span key={item}>✓ {item}</span>)}</div>{issues.length > 0 && <section className="release-blockers"><ul>{issues.map((item) => <li key={item}>{item}</li>)}</ul>{!status?.gitReady && <button className="secondary-button" disabled={busy} onClick={onInit}>初始化 Git</button>}</section>}<section className="release-action-row"><label>版本<input value={version || status?.suggestedVersion || ''} onChange={(event) => onVersionChange(event.target.value)} placeholder="v0.0.1" /></label><button className="primary-button release-button" disabled={busy || !ready} onClick={confirmed ? onRun : onConfirm}>{busy ? '打包中…' : confirmed ? '确认打包' : '准备打包'}</button>{confirmed && <button className="text-button" onClick={onConfirm}>取消</button>}</section><details className="release-details"><summary>发布详情</summary><div><span>建议 {status?.suggestedVersion || '—'}</span><span>{status?.releaseBranch ? 'release 分支已就绪' : '首次打包会创建 release 分支'}</span><span>{(status?.artifacts ?? []).length ? (status?.artifacts ?? []).join(' · ') : '构建后生成发布文件'}</span>{(status?.tags ?? []).slice(0, 3).map((item) => <span key={item}>{item}</span>)}{status?.gitHubActionsUrl && <a href={status.gitHubActionsUrl} target="_blank" rel="noreferrer">Actions</a>}</div></details></>}</section>
}

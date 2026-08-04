import { useEffect, useRef, useState, type ReactNode } from 'react'
import { HeaderLinks } from './GuideHeader'
import { RobotConfigForm } from './RobotConfigForm'
import { NpmrcConfigForm } from './NpmrcConfigForm'

type Check = { id: string; name: string; status: 'ready' | 'missing' | 'warning'; detail: string }
type CatalogGroup = { title: string; items: Array<{ name: string; description: string; url: string; install: string }> }
type Page = 'environment' | 'robot' | 'build' | 'plugins' | 'connections'
type Section = 'overview' | 'npmrc' | 'config' | 'readme' | 'actions'
type Props = { report: { checks: Check[] } | null; checking: boolean; error: string; defaultPage: string; onOpenGuide: () => void; onCheck: () => void; goals?: unknown; goal?: unknown; onSelect?: (id: string) => void }

const pages: Array<{ id: Page; label: string; icon: string }> = [
  { id: 'environment', label: '环境', icon: '⌘' },
  { id: 'robot', label: '机器人', icon: '◈' },
  { id: 'build', label: '构建', icon: '⌗' },
  { id: 'plugins', label: '插件', icon: '▦' },
  { id: 'connections', label: '连接', icon: '⌁' },
]

const robotSections: Array<{ id: Section; label: string }> = [
  { id: 'overview', label: '概览' },
  { id: 'npmrc', label: '镜像' },
  { id: 'config', label: '配置' },
  { id: 'readme', label: 'README' },
  { id: 'actions', label: '运行' },
]

export function Dashboard({ report, checking, error, defaultPage, onOpenGuide, onCheck }: Props) {
  const [page, setPage] = useState<Page>(defaultPage === 'robot' ? 'robot' : 'environment')
  const [section, setSection] = useState<Section>('overview')
  const [root, setRoot] = useState(() => new URLSearchParams(window.location.search).get('root') ?? '.')
  const [file, setFile] = useState('.npmrc')
  const [content, setContent] = useState('')
  const [output, setOutput] = useState('')
  const [busy, setBusy] = useState(false)
  const [catalog, setCatalog] = useState<CatalogGroup[]>([])
  const [catalogLoading, setCatalogLoading] = useState(false)
  const [catalogError, setCatalogError] = useState('')
  const [catalogTitle, setCatalogTitle] = useState('')
  const [configEditor, setConfigEditor] = useState<'visual' | 'text'>('visual')
  const [buildMode, setBuildMode] = useState<'npm' | 'git'>('npm')
  const [releaseVersion, setReleaseVersion] = useState('')
  const [npmTag, setNpmTag] = useState('latest')
  const environmentChecked = useRef(false)

  useEffect(() => setPage(defaultPage === 'robot' ? 'robot' : 'environment'), [defaultPage])
  useEffect(() => {
    if (page !== 'environment' || report || checking || environmentChecked.current) return
    environmentChecked.current = true
    onCheck()
  }, [checking, onCheck, page, report])
  useEffect(() => {
    if (page !== 'plugins' && page !== 'connections') return
    setCatalogLoading(true)
    setCatalogError('')
    fetch(`/api/v1/catalog?kind=${page === 'plugins' ? 'apps' : 'environment'}`)
      .then(async (response) => {
        if (!response.ok) {
          const data = await response.json() as { error?: string }
          throw new Error(data.error ?? '在线目录暂时无法读取。')
        }
        return response.json() as Promise<CatalogGroup[]>
      })
      .then((data) => { setCatalog(data); setCatalogTitle(data[0]?.title ?? '') })
      .catch((reason) => { setCatalog([]); setCatalogTitle(''); setCatalogError(reason instanceof Error ? reason.message : '在线目录暂时无法读取。') })
      .finally(() => setCatalogLoading(false))
  }, [page])

  async function api(method: string, data: Record<string, string>) {
    setBusy(true)
    try {
      const query = method === 'GET' ? `?${new URLSearchParams(data)}` : ''
      const response = await fetch(`/api/v1/robot${query}`, method === 'GET' ? {} : { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(data) })
      const json = await response.json() as { output?: string; error?: string }
      if (!response.ok) throw new Error(json.error)
      if (method === 'GET') {
        setContent(json.output ?? '')
        return
      }
      setOutput(json.output ?? '操作完成。')
    } catch (reason) { setOutput(reason instanceof Error ? reason.message : '操作未完成。') } finally { setBusy(false) }
  }

  async function chooseDirectory() {
    const response = await fetch('/api/v1/directories/select', { method: 'POST' })
    const data = await response.json() as { path?: string }
    if (response.ok && data.path) setRoot(data.path)
  }

  function openSection(nextSection: Section) {
    setSection(nextSection)
    if (nextSection === 'npmrc') { setFile('.npmrc'); api('GET', { root, file: '.npmrc' }) }
    if (nextSection === 'readme') { setFile('README.md'); api('GET', { root, file: 'README.md' }) }
  }

  function openTextConfig() {
    setConfigEditor('text')
    setFile('alemon.config.yaml')
    api('GET', { root, file: 'alemon.config.yaml' })
  }

  function selectPage(nextPage: Page) {
    setPage(nextPage)
    setOutput('')
  }

  const currentCatalog = catalog.find((group) => group.title === catalogTitle) ?? catalog[0]
  const readyCount = report?.checks.filter((item) => item.status === 'ready').length ?? 0
  const robotTitle = robotSections.find((item) => item.id === section)?.label ?? '机器人'
  const catalogPage = page === 'plugins' ? '插件' : '连接'

  const robotContent = <>
    <DashboardSubnav items={robotSections} active={section} onSelect={openSection} />
    <section className="workspace-content">
      <PageHeader title={robotTitle} />
      {section === 'overview' && <section className="overview-grid">
        <article><span>项目目录</span><strong title={root}>{root === '.' ? '当前目录' : root}</strong></article>
        <article><span>运行环境</span><strong>未设置</strong></article>
        <article><span>入口</span><strong>未设置</strong></article>
      </section>}
      {section === 'npmrc' && <NpmrcConfigForm content={content} busy={busy} onChange={setContent} onSave={(nextContent) => api('PUT', { root, file: '.npmrc', content: nextContent })} />}
      {section === 'config' && <section className="config-form">
        <div className="editor-mode" aria-label="编辑模式"><button className={configEditor === 'visual' ? 'active' : ''} onClick={() => setConfigEditor('visual')}>表单</button><button className={configEditor === 'text' ? 'active' : ''} onClick={openTextConfig}>文本</button></div>
        {configEditor === 'visual' ? <RobotConfigForm busy={busy} onSave={(config) => api('PUT', { root, file: 'alemon.config.yaml', content: config })} /> : <FileEditor title="alemon.config.yaml" content={content} busy={busy} placeholder="配置内容" onChange={setContent} onSave={() => api('PUT', { root, file, content })} />}
      </section>}
      {section === 'readme' && <FileEditor title="README.md" content={content} busy={busy} placeholder="项目说明" onChange={setContent} onSave={() => api('PUT', { root, file, content })} />}
      {section === 'actions' && <section className="robot-actions">{[['install', '重载依赖', '重新安装 package.json 中声明的所有依赖包。'], ['dev', '开发启动', '以开发模式运行机器人，适合边改代码边调试。'], ['pm2', 'PM2 启动', '构建后交由 PM2 在后台持续运行。']].map(([action, label, note]) => <button key={action} disabled={busy} onClick={() => api('POST', { root, action })}><div><strong>{label}</strong><small>{note}</small></div><span>›</span></button>)}</section>}
    </section>
  </>

  const catalogContent = <>
    <DashboardSubnav items={catalog.map((group) => ({ id: group.title, label: group.title }))} active={currentCatalog?.title ?? ''} onSelect={setCatalogTitle} />
    <section className="workspace-content">
      <PageHeader title={catalogPage} />
      {catalogLoading && <p className="catalog-state">正在读取目录…</p>}
      {catalogError && <p className="catalog-state">{catalogError}</p>}
      {!catalogLoading && !catalogError && currentCatalog && <section className="catalog-items">{currentCatalog.items.map((item) => <article className="catalog-item" key={`${currentCatalog.title}-${item.name}`}><div><strong>{item.name}</strong>{item.description && <p>{item.description}</p>}</div><div className="catalog-actions">{item.url && <a href={item.url} target="_blank" rel="noreferrer">详情</a>}<button className="primary-button" disabled={busy || !item.install} onClick={() => api('POST', { root, action: 'install-package', package: item.install })}>{item.install ? '安装' : '不可用'}</button></div></article>)}</section>}
    </section>
  </>

  return <main className="guide-shell"><section className="guide-window dashboard-window">
    <header className="guide-bar dashboard-toolbar"><div className="window-project-picker"><button className={root === '.' ? 'active' : ''} onClick={() => setRoot('.')} aria-label="使用当前目录" title="当前目录">⌂</button><button onClick={chooseDirectory} aria-label="选择项目目录" title="选择项目目录">▰</button><span title={root}>{root === '.' ? '当前目录' : root}</span></div><HeaderLinks /><button className="guide-trigger" onClick={onOpenGuide} aria-label="打开引导" title="打开引导">?</button></header>
    <section className="console-layout"><aside className="console-nav" aria-label="主导航">{pages.map((item) => <button className={page === item.id ? 'active' : ''} onClick={() => selectPage(item.id)} key={item.id}><i>{item.icon}</i>{item.label}</button>)}</aside><section className="console-page">
      {page === 'environment' && <section className="workspace-content environment-page"><PageHeader title="环境" action={<button className="primary-button" onClick={onCheck} disabled={checking}>{checking ? '检查中…' : '重新检查'}</button>} /><div className="environment-summary"><strong>{report ? `${readyCount} / ${report.checks.length}` : '—'}</strong><span>已就绪</span></div><div className="compact-checks">{report?.checks.map((item) => <article className={item.status} key={item.id}><i>{item.status === 'ready' ? '✓' : '!'}</i><div><strong>{item.name}</strong><span>{item.detail}</span></div></article>)}</div></section>}
      {page === 'robot' && robotContent}
      {page === 'build' && <section className="workspace-content build-page"><PageHeader title="发布与打包" /><div className="build-mode" role="tablist" aria-label="发布方式"><button className={buildMode === 'npm' ? 'active' : ''} onClick={() => { setBuildMode('npm'); setOutput('') }} role="tab" aria-selected={buildMode === 'npm'}>NPM 发布</button><button className={buildMode === 'git' ? 'active' : ''} onClick={() => { setBuildMode('git'); setOutput('') }} role="tab" aria-selected={buildMode === 'git'}>Git 打包</button></div>{buildMode === 'npm' ? <section className="release-workflow"><div className="workflow-heading"><div><strong>NPM 发布</strong><span>执行 npm publish</span></div><label>发布标签<input value={npmTag} onChange={(event) => setNpmTag(event.target.value)} /></label></div><ol className="workflow-steps"><li>读取当前项目的 package.json</li><li>使用标签 <code>{npmTag || 'latest'}</code> 发布到 npm</li></ol><button className="primary-button" disabled={busy} onClick={() => api('POST', { root, action: 'npm-publish', tag: npmTag })}>{busy ? '发布中…' : '发布到 npm'}</button></section> : <section className="release-workflow"><div className="workflow-heading"><div><strong>Git 打包发布</strong><span>执行 local-release.sh</span></div><label>版本号<input value={releaseVersion} onChange={(event) => setReleaseVersion(event.target.value)} placeholder="留空则自动递增 patch" /></label></div><ol className="workflow-steps"><li>检查 Git、Node.js、jq 和当前分支</li><li>同步 main 与 release 分支</li><li>安装依赖并构建发布产物</li><li>推送 release 分支与版本 tag</li><li>恢复原分支与依赖</li></ol><button className="primary-button release-button" disabled={busy} onClick={() => api('POST', { root, action: 'git-release', version: releaseVersion })}>{busy ? '发布中…' : '执行 Git 打包发布'}</button></section>}{output && <pre className="robot-output build-output">{output}</pre>}</section>}
      {(page === 'plugins' || page === 'connections') && catalogContent}
      {page !== 'build' && output && <aside className="robot-output" aria-label="操作日志"><header><strong>操作日志</strong><button onClick={() => setOutput('')} aria-label="关闭操作日志">×</button></header><pre>{output}</pre></aside>}{error && <p className="error">{error}</p>}
    </section></section>
  </section></main>
}

function PageHeader({ title, action }: { title: string; action?: ReactNode }) { return <header className="page-header"><h1>{title}</h1>{action}</header> }

function DashboardSubnav<T extends string>({ items, active, onSelect }: { items: Array<{ id: T; label: string }>; active: T; onSelect: (id: T) => void }) {
  if (!items.length) return null
  return <nav className="subnav" aria-label="页面导航">{items.map((item) => <button key={item.id} className={active === item.id ? 'active' : ''} onClick={() => onSelect(item.id)}>{item.label}</button>)}</nav>
}

function FileEditor({ title, content, busy, placeholder, onChange, onSave }: { title: string; content: string; busy: boolean; placeholder: string; onChange: (value: string) => void; onSave: () => void }) {
  return <section className="file-editor"><header><h2>{title}</h2><button className="primary-button" disabled={busy} onClick={onSave}>保存</button></header><textarea value={content} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} /></section>
}

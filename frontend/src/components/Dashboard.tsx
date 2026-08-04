import { useEffect, useState } from 'react'

type Check = { id: string; name: string; status: 'ready' | 'missing' | 'warning'; detail: string }
type CatalogGroup = { title: string; items: Array<{ name: string; description: string; url: string }> }
type Props = { report: { checks: Check[] } | null; checking: boolean; error: string; defaultPage: string; onOpenGuide: () => void; onCheck: () => void; goals?: unknown; goal?: unknown; onSelect?: (id: string) => void }

export function Dashboard({ report, checking, error, defaultPage, onOpenGuide, onCheck }: Props) {
  const [page, setPage] = useState(defaultPage)
  const [section, setSection] = useState<'overview' | 'npmrc' | 'config' | 'readme' | 'actions'>('overview')
  const [root, setRoot] = useState(() => new URLSearchParams(window.location.search).get('root') ?? '.')
  const [file, setFile] = useState('.npmrc')
  const [content, setContent] = useState('')
  const [output, setOutput] = useState('')
  const [busy, setBusy] = useState(false)
  const [catalog, setCatalog] = useState<CatalogGroup[]>([])
  const [catalogLoading, setCatalogLoading] = useState(false)
  const [catalogError, setCatalogError] = useState('')
  const [catalogTitle, setCatalogTitle] = useState('')
  const [runtimeConfig, setRuntimeConfig] = useState({ name: 'alemonjs-app', script: 'node index.js', environment: 'production' })
  const [configEditor, setConfigEditor] = useState<'visual' | 'text'>('visual')
  const [namePreset, setNamePreset] = useState('alemonjs-app')
  const [scriptPreset, setScriptPreset] = useState('node index.js')
  useEffect(() => setPage(defaultPage), [defaultPage])
  useEffect(() => {
    if (page !== 'plugins' && page !== 'connections') return
    setCatalogLoading(true)
    setCatalogError('')
    fetch(`/api/v1/catalog?kind=${page === 'plugins' ? 'apps' : 'open'}`)
      .then(async (response) => {
        if (!response.ok) {
          const data = await response.json() as { error?: string }
          throw new Error(data.error ?? '在线目录暂时无法读取。')
        }
        return response.json() as Promise<CatalogGroup[]>
      })
      .then((data) => {
        setCatalog(data)
        setCatalogTitle(data[0]?.title ?? '')
      })
      .catch((reason) => {
        setCatalog([])
        setCatalogTitle('')
        setCatalogError(reason instanceof Error ? reason.message : '在线目录暂时无法读取。')
      })
      .finally(() => setCatalogLoading(false))
  }, [page])

  async function api(method: string, data: Record<string, string>) {
    setBusy(true)
    try {
      const query = method === 'GET' ? `?${new URLSearchParams(data)}` : ''
      const response = await fetch(`/api/v1/robot${query}`, method === 'GET' ? {} : { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(data) })
      const json = await response.json() as { output?: string; error?: string }
      if (!response.ok) throw new Error(json.error)
      setOutput(json.output ?? '操作完成。')
      if (method === 'GET') setContent(json.output ?? '')
    } catch (reason) { setOutput(reason instanceof Error ? reason.message : '操作未完成。') } finally { setBusy(false) }
  }
  async function chooseDirectory() {
    const response = await fetch('/api/v1/directories/select', { method: 'POST' })
    const data = await response.json() as { path?: string; error?: string }
    if (response.ok && data.path) setRoot(data.path); else setOutput(data.error ?? '未选择文件夹。')
  }
  const actions = [['install', '重载依赖'], ['dev', '开发模式启动'], ['build', '构建应用'], ['pm2', 'PM2 后台启动']]
  const currentCatalog = catalog.find((group) => group.title === catalogTitle) ?? catalog[0]
  const robot = <>
    <div className="project-context"><span>{root === '.' ? '当前运行目录' : root}</span><button onClick={chooseDirectory}>选择机器人文件夹</button></div>
    <nav className="subnav"><button className={section === 'overview' ? 'active' : ''} onClick={() => setSection('overview')}>概览</button><button className={section === 'npmrc' ? 'active' : ''} onClick={() => { setSection('npmrc'); setFile('.npmrc'); api('GET', { root, file: '.npmrc' }) }}>镜像设置</button><button className={section === 'config' ? 'active' : ''} onClick={() => setSection('config')}>AlemonJS 配置</button><button className={section === 'readme' ? 'active' : ''} onClick={() => { setSection('readme'); setFile('README.md'); api('GET', { root, file: 'README.md' }) }}>README</button><button className={section === 'actions' ? 'active' : ''} onClick={() => setSection('actions')}>运行与发布</button></nav>
    {section === 'overview' && <div className="panel-copy"><h2>从这里开始</h2><p>选择项目后，可以编辑配置、安装依赖、启动开发模式或构建并交给 PM2 后台运行。</p></div>}
    {section === 'npmrc' && <div className="file-editor"><h2>下载镜像设置</h2><textarea value={content} onChange={(event) => setContent(event.target.value)} placeholder="点击镜像设置后读取内容" /><button className="primary-button" disabled={busy} onClick={() => api('PUT', { root, file, content })}>保存镜像设置</button></div>}
    {section === 'config' && <section className="config-form"><div className="editor-mode"><button className={configEditor === 'visual' ? 'active' : ''} onClick={() => setConfigEditor('visual')}>可视化配置</button><button className={configEditor === 'text' ? 'active' : ''} onClick={() => { setConfigEditor('text'); setFile('alemon.config.yaml'); api('GET', { root, file: 'alemon.config.yaml' }) }}>纯文本模式</button></div>{configEditor === 'visual' ? <><h2>机器人运行配置</h2><p>常用选项已预设；只有选择“自定义”时才需要输入内容。</p><label>应用名称<select value={namePreset} onChange={(event) => { setNamePreset(event.target.value); if (event.target.value !== 'custom') setRuntimeConfig({ ...runtimeConfig, name: event.target.value }) }}><option value="alemonjs-app">AlemonJS 应用</option><option value="alemonjs-bot">AlemonJS 机器人</option><option value="custom">自定义名称</option></select></label>{namePreset === 'custom' && <label>自定义应用名称<input value={runtimeConfig.name} onChange={(event) => setRuntimeConfig({ ...runtimeConfig, name: event.target.value })} /></label>}<label>启动入口<select value={scriptPreset} onChange={(event) => { setScriptPreset(event.target.value); if (event.target.value !== 'custom') setRuntimeConfig({ ...runtimeConfig, script: event.target.value }) }}><option value="node index.js">node index.js（默认）</option><option value="node app.js">node app.js</option><option value="custom">自定义启动命令</option></select></label>{scriptPreset === 'custom' && <label>自定义启动入口<input value={runtimeConfig.script} onChange={(event) => setRuntimeConfig({ ...runtimeConfig, script: event.target.value })} /></label>}<label>运行环境<select value={runtimeConfig.environment} onChange={(event) => setRuntimeConfig({ ...runtimeConfig, environment: event.target.value })}><option value="development">开发环境</option><option value="production">生产环境（推荐）</option></select></label><button className="primary-button" disabled={busy} onClick={() => api('PUT', { root, file: 'alemon.config.yaml', content: `pm2:\n  apps:\n    - name: '${runtimeConfig.name}'\n      script: '${runtimeConfig.script}'\n      env:\n        NODE_ENV: '${runtimeConfig.environment}'\n` })}>保存运行配置</button></> : <div className="file-editor"><h2>alemon.config.yaml</h2><textarea value={content} onChange={(event) => setContent(event.target.value)} placeholder="正在读取配置文件…" /><button className="primary-button" disabled={busy} onClick={() => api('PUT', { root, file: 'alemon.config.yaml', content })}>保存纯文本配置</button></div>}</section>}
    {section === 'readme' && <div className="file-editor"><h2>项目说明</h2><textarea value={content} onChange={(event) => setContent(event.target.value)} placeholder="点击 README 后读取内容" /><button className="primary-button" disabled={busy} onClick={() => api('PUT', { root, file, content })}>保存 README</button></div>}
    {section === 'actions' && <section className="robot-actions">{actions.map(([action, label]) => <button key={action} disabled={busy} onClick={() => api('POST', { root, action })}>{label}</button>)}<button disabled={busy} onClick={() => api('POST', { root, action: 'commit', message: 'chore: update robot' })}>提交代码</button></section>}
  </>
  const onlineCatalog = <>
    <p className="eyebrow">{page === 'plugins' ? '插件管理' : '连接管理'}</p>
    <h1>{page === 'plugins' ? '在线插件目录' : '在线连接目录'}</h1>
    <p>内容来自 AlemonJS 官方文档，点击项目即可查看对应说明或仓库。</p>
    {catalog.length > 0 && <nav className="subnav catalog-subnav" aria-label="目录分类">{catalog.map((group) => <button key={group.title} className={currentCatalog?.title === group.title ? 'active' : ''} onClick={() => setCatalogTitle(group.title)}>{group.title}</button>)}</nav>}
    {catalogLoading && <p className="catalog-state">正在读取官方在线目录…</p>}
    {catalogError && <p className="error">{catalogError}</p>}
    {!catalogLoading && !catalogError && currentCatalog && <section className="catalog-group"><h2>{currentCatalog.title}</h2><div className="catalog-items">{currentCatalog.items.map((item) => <article className="catalog-item" key={`${currentCatalog.title}-${item.name}`}><div><strong>{item.name}</strong><p>{item.description || '查看该项目的详细说明。'}</p></div>{item.url && <a href={item.url} target="_blank" rel="noreferrer">查看项目</a>}</article>)}</div></section>}
  </>
  return <main className="guide-shell"><section className="guide-window dashboard-window"><header className="guide-bar"><span className="console-title">后台中心</span><button className="primary-button" onClick={onOpenGuide}>打开引导</button></header><section className="console-layout"><aside className="console-nav"><button onClick={onOpenGuide}>引导首页</button>{[['environment', '环境管理'], ['robot', '机器人管理'], ['plugins', '插件管理'], ['connections', '连接管理']].map(([id, label]) => <button className={page === id ? 'active' : ''} onClick={() => setPage(id)} key={id}>{label}</button>)}</aside><section className="console-page">{page === 'environment' ? <><p className="eyebrow">环境管理</p><h1>当前电脑的准备状态</h1><button className="primary-button" onClick={onCheck} disabled={checking}>{checking ? '检查中…' : '开始检查'}</button><div className="compact-checks">{report?.checks.map((item) => <span className={item.status} key={item.id}>{item.status === 'ready' ? '✓' : '!'} {item.name}：{item.detail}</span>)}</div></> : page === 'robot' ? robot : onlineCatalog}{output && <pre className="robot-output">{output}</pre>}{error && <p className="error">{error}</p>}</section></section></section></main>
}

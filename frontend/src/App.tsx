import { useEffect, useRef, useState } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { useLocation, useNavigate } from 'react-router-dom'
import { setDeveloper, setProject, type RootState } from './store/guideStore'
import { GuideHeader } from './components/GuideHeader'
import { Dashboard } from './components/Dashboard'
import { EnvironmentFixDialog } from './components/EnvironmentFixDialog'
import { useGoalsQuery, useLazyEnvironmentReportQuery, useReleasesQuery } from './store/workspaceApi'

type Mirror = { name: string; url: string }
type Goal = { id: string; title: string; description: string; steps: string[]; downloadUrl?: string; mirrors?: Mirror[] }
type Check = { id: string; name: string; status: 'ready' | 'missing' | 'warning'; detail: string; suggestion: string }
type Report = { ready: boolean; platform: string; checks: Check[]; checkedAt: string }
type ProjectConfig = { template: 'bot' | 'dev'; name: string; destinationMode: 'current' | 'custom'; destination: string; language: string; packageManager: string; eslint: boolean; initializeGit: boolean; usePM2: boolean; imageMode: string; styleMode: string; downloadSkills: boolean }
type Creation = { path?: string; status?: string; logs?: string[] }
type ReleaseAsset = { name: string; url: string; size: number }
type Release = { tag: string; name: string; url: string; publishedAt: string; assets: ReleaseAsset[] }
const icons: Record<string, string> = { install: '↓', manage: '⚙', develop: '⌘', desktop: '▣', mobile: '▤', web: '◎' }

export default function App() {
  const navigate = useNavigate()
  const location = useLocation()
  const [error, setError] = useState('')
  const [creating, setCreating] = useState(false)
  const [creation, setCreation] = useState<Creation | null>(null)
  const [repairCheck, setRepairCheck] = useState<Check | null>(null)
  const { data: goalData = [], isLoading: loading } = useGoalsQuery()
  const [loadEnvironmentReport, { data: environmentData, isFetching: checking }] = useLazyEnvironmentReportQuery()
  const report = environmentData as Report | undefined ?? null
  const goals = goalData as Goal[]
  const routeGoal = location.pathname.match(/^\/guide\/([^/]+)\/step\/\d+$/)?.[1]
  const guideGroup = location.pathname.match(/^\/guide\/group\/([^/]+)$/)?.[1]
  const selectedID = routeGoal ?? null
  const guideOpen = !location.pathname.startsWith('/dashboard')
  const activeID = selectedID ?? 'install'
  const activeGoal = goals.find((goal) => goal.id === activeID)

  useEffect(() => { if (location.pathname === '/') navigate('/guide', { replace: true }) }, [location.pathname, navigate])
  function openGuide() { navigate('/guide'); setCreation(null); setError('') }
  async function checkEnvironment(variant?: string) { const selectedVariant = typeof variant === 'string' ? variant : ''; try { setError(''); await loadEnvironmentReport({ goalId: activeID, variant: selectedVariant }, true).unwrap() } catch (reason) { setError(reason instanceof Error ? reason.message : '环境检查未完成，请稍后重试。') } }
  async function createProject(config: ProjectConfig) { try { setCreating(true); setError(''); setCreation(null); const response = await fetch('/api/v1/projects', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(config) }); const data = await response.json() as Creation & { error?: string; result?: Creation }; setCreation(data.result ?? data); if (!response.ok) setError(data.error ?? '项目创建未完成，请检查下方日志。') } catch { setError('项目创建请求未完成，请稍后重试。') } finally { setCreating(false) } }

  return <div className="app-shell">
    {guideOpen ? <GuideHome loading={loading} group={guideGroup} goal={selectedID ? activeGoal : undefined} report={report} checking={checking} error={error} creating={creating} creation={creation} onSelect={(id) => { if (id === 'manage') { navigate('/dashboard/robot'); return }; navigate(id ? `/guide/${id}/step/1` : '/guide'); setCreation(null); setError('') }} onClose={() => navigate('/dashboard')} onCheck={checkEnvironment} onCreate={createProject} onFix={setRepairCheck} /> : <Dashboard goals={goals} goal={activeGoal} report={report} checking={checking} error={error} defaultPage={location.pathname.endsWith('/robot') ? 'robot' : 'environment'} onSelect={(id) => { if (id === 'manage') { navigate('/dashboard/robot'); return }; navigate(`/guide/${id}/step/1`); setError('') }} onOpenGuide={openGuide} onCheck={checkEnvironment} onFix={setRepairCheck} />}
    {repairCheck && <EnvironmentFixDialog check={repairCheck} onClose={() => setRepairCheck(null)} />}
  </div>
}

function GuideHome({ loading, group, goal, report, checking, error, creating, creation, onSelect, onClose, onCheck, onCreate, onFix }: { loading: boolean; group?: string; goal?: Goal; report: Report | null; checking: boolean; error: string; creating: boolean; creation: Creation | null; onSelect: (id: string | null) => void; onClose: () => void; onCheck: (variant?: string) => void; onCreate: (config: ProjectConfig) => void; onFix: (check: Check) => void }) {
  const backAction = useRef<() => void>(() => {})
  const location = useLocation()
  const navigate = useNavigate()
  const currentStep = Number(location.pathname.match(/\/step\/(\d+)/)?.[1] ?? 0)
  const missingChecks = report?.checks.filter((check) => check.status !== 'ready' && ['node', 'git', 'docker'].includes(check.id)) ?? []
  const isCheckStep = (goal?.id === 'install' || goal?.id === 'develop') ? currentStep === 1 : goal?.id === 'web' && currentStep === 2
  return <main className="guide-shell"><section className="guide-window"><GuideHeader onBack={() => group ? navigate('/guide') : backAction.current()} onClose={onClose} showBack={Boolean(goal || group)} />{error && <p className="error" role="alert">{error}</p>}{group ? <PurposeGroup group={group} onSelect={onSelect} /> : <FlowView loading={loading} goal={goal} report={report} checking={checking} creating={creating} creation={creation} onSelect={onSelect} onCheck={onCheck} onCreate={onCreate} registerBack={(handler) => { backAction.current = handler }} />}{isCheckStep && missingChecks.length > 0 && <div className="environment-repair">{missingChecks.map((check) => <button key={check.id} onClick={() => onFix(check)}>安装 / 下载 {check.name}</button>)}</div>}</section></main>
}

function PurposeGroup({ group, onSelect }: { group: string; onSelect: (id: string) => void }) {
  const options = group === 'deploy'
    ? [['install', '源码版', '下载并安装一个可运行的 AlemonJS 机器人源码项目。'], ['desktop', '桌面版', '下载适合当前电脑的 AlemonDesk 安装包。'], ['mobile', '手机版', '下载 Android 通用 APK 安装包。'], ['web', 'Web 版', '下载 AlemonGo，或使用 Docker 部署。']]
    : [['develop', '开发机器人', '创建一个可自由选择语言、依赖和工具的开发项目。']]
  return <section className="wizard purpose-group"><section className="wizard-page"><div className="wizard-content"><div className="guide-question"><p className="question-kicker">部署向导</p><h1>{group === 'deploy' ? '你要部署哪一种版本？' : '开始开发机器人'}</h1><p className="question-lead">选择一种方式，接下来只会展示与它有关的步骤。</p><div className="question-options">{options.map(([id, title, note]) => <button key={id} onClick={() => onSelect(id)}><i>{icons[id] ?? '·'}</i><span><strong>{title}</strong><small>{note}</small></span><b>→</b></button>)}</div></div></div></section></section>
}

function EnvironmentCheckPanel({ title, report, checking, onCheck }: { title: string; report: Report | null; checking: boolean; onCheck: () => void }) {
  const ready = Boolean(report?.ready)
  return <section className="mx-auto grid w-full max-w-[640px] gap-5 pt-8"><header className="flex items-start justify-between gap-5 rounded-2xl border border-slate-200 bg-white p-5 shadow-[0_12px_30px_rgb(15_23_42/0.06)]"><div className="flex items-start gap-3"><i className={`mt-0.5 inline-flex h-9 w-9 items-center justify-center rounded-xl text-base font-extrabold not-italic ${ready ? 'bg-emerald-100 text-emerald-700' : 'bg-amber-100 text-amber-700'}`}>{ready ? '✓' : '!'}</i><div><h1 className="m-0 text-xl font-bold tracking-tight text-slate-800">{title}</h1><p className="mt-1.5 text-sm text-slate-500">{checking || !report ? '正在检查所需工具…' : ready ? '环境已就绪，可以继续。' : '有项目需要先处理。'}</p></div></div><button className="rounded-lg border border-slate-200 bg-white px-3 py-2 text-xs font-bold text-slate-600 transition hover:border-teal-300 hover:text-teal-700 disabled:cursor-wait disabled:opacity-50" onClick={onCheck} disabled={checking}>{checking ? '检查中' : '重新检查'}</button></header>{checking || !report ? <div className="flex min-h-32 items-center justify-center rounded-2xl border border-dashed border-slate-200 bg-slate-50"><span className="checking-indicator" /></div> : <div className="grid gap-2 sm:grid-cols-2">{report.checks.map((check) => <article className={`flex min-h-20 items-start gap-3 rounded-xl border p-4 ${check.status === 'ready' ? 'border-emerald-100 bg-emerald-50/45' : 'border-amber-200 bg-amber-50'}`} key={check.id}><i className={`inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-xs font-extrabold not-italic ${check.status === 'ready' ? 'bg-emerald-600 text-white' : 'bg-amber-500 text-white'}`}>{check.status === 'ready' ? '✓' : '!'}</i><div className="min-w-0"><strong className="block text-sm font-bold text-slate-700">{check.name}</strong><span className="mt-1 block break-words text-xs leading-5 text-slate-500">{check.detail}</span>{check.status !== 'ready' && check.suggestion && <small className="mt-1 block text-xs leading-5 text-amber-700">{check.suggestion}</small>}</div></article>)}</div>}</section>
}

function FlowView({ loading, goal, report, checking, creating, creation, onSelect, onCheck, onCreate, registerBack }: { loading: boolean; goal?: Goal; report: Report | null; checking: boolean; creating: boolean; creation: Creation | null; onSelect: (id: string | null) => void; onCheck: (variant?: string) => void; onCreate: (config: ProjectConfig) => void; registerBack: (handler: () => void) => void }) {
  const navigate = useNavigate()
  const location = useLocation()
  const dispatch = useDispatch()
  const config = useSelector((state: RootState) => state.guide.developer)
  const project = useSelector((state: RootState) => state.guide.project)
  const routedStep = Number(location.pathname.match(/\/step\/(\d+)/)?.[1] ?? 0)
  const [step, setStep] = useState(routedStep)
  const [webEdition, setWebEdition] = useState<'clean' | 'docker' | null>(null)
  const [buildMode, setBuildMode] = useState<'npm' | 'git' | null>(null)
  const [selectedMirror, setSelectedMirror] = useState<Mirror | null>(null)
  const [releaseURL, setReleaseURL] = useState<string | null>(null)
  const [selectedAssetURL, setSelectedAssetURL] = useState<string | null>(null)
  const [selectingFolder, setSelectingFolder] = useState(false)
  const [folderError, setFolderError] = useState('')
  const automaticCheck = useRef<string | null>(null)
  const currentStepElement = useRef<HTMLDivElement | null>(null)
  const isDeveloper = goal?.id === 'develop'
  const isInstaller = goal?.id === 'install'
  const releaseApp = goal?.id === 'desktop' ? 'alemondesk' : goal?.id === 'web' && webEdition === 'clean' ? 'alemongo' : null
  const { data: releaseData = [] } = useReleasesQuery(releaseApp ?? '', { skip: !releaseApp })
  const releases = releaseData as Release[]
  const webSteps = webEdition === 'clean' ? ['选择部署方式', '检查 Node.js 与 Git', '选择下载镜像', '选择版本', '选择安装包'] : webEdition === 'docker' ? ['选择部署方式', '检查 Docker', 'Docker Compose 快速启动'] : ['选择部署方式']
  const buildSteps = buildMode === 'npm' ? ['选择构建方式', '检查 npm 环境', 'NPM 构建'] : buildMode === 'git' ? ['选择构建方式', '检查 Git 环境', 'Git 化构建'] : ['选择构建方式']
  const downloadSteps = goal?.id === 'mobile' ? ['下载 Android 安装包'] : selectedMirror ? ['选择镜像', '选择版本', '选择安装包'] : ['选择镜像']
  const totalSteps = goal ? ['选择目的', ...(goal.id === 'web' ? webSteps : goal.id === 'build' ? buildSteps : (goal.id === 'desktop' || goal.id === 'mobile') ? downloadSteps : goal.steps)] : ['选择目的']
  const flowStep = step - 1
  const setFlowStep = (value: number) => { const nextStep = Math.max(0, Math.min(value, totalSteps.length - 1)); setStep(nextStep); if (goal) navigate(`/guide/${goal.id}/step/${nextStep}`) }
  const next = () => setFlowStep(step + 1)
  const back = () => setFlowStep(step - 1)
  const choose = (key: keyof typeof config, value: string) => dispatch(setDeveloper({ [key]: value }))
  const chooseDestination = async () => {
    setSelectingFolder(true)
    setFolderError('')
    try {
      const response = await fetch('/api/v1/directories/select', { method: 'POST' })
      const data = await response.json() as { path?: string; error?: string }
      if (!response.ok || !data.path) throw new Error(data.error ?? '未选择文件夹。')
      dispatch(setProject({ destinationMode: 'custom', destination: data.path }))
    } catch (reason) {
      setFolderError(reason instanceof Error ? reason.message : '未选择文件夹。')
    } finally {
      setSelectingFolder(false)
    }
  }
  useEffect(() => { if (routedStep !== step) setStep(routedStep) }, [routedStep, step])
  useEffect(() => {
    const variant = goal?.id === 'web' ? webEdition : goal?.id === 'build' ? buildMode : undefined
    const isCheckStep = (goal?.id === 'develop' || goal?.id === 'install') ? flowStep === 0 : (goal?.id === 'web' || goal?.id === 'build') && Boolean(variant) && flowStep === 1
    if ((goal?.id === 'develop' || goal?.id === 'install' || variant) && isCheckStep && automaticCheck.current !== `${goal?.id}:${variant ?? ''}`) {
      automaticCheck.current = `${goal?.id}:${variant ?? ''}`
      onCheck(variant ?? undefined)
    }
  }, [buildMode, flowStep, goal?.id, onCheck, webEdition])
  useEffect(() => {
    currentStepElement.current?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  }, [step])
  useEffect(() => { if (!releaseApp) return; setReleaseURL(releases[0]?.url ?? null); setSelectedAssetURL(null) }, [releaseApp, releases])
  const mirrorURL = (mirror: Mirror | null, url: string) => { if (!mirror) return url; const index = mirror.url.indexOf('https://github.com'); return index === -1 ? url : mirror.url.slice(0, index) + url }
  const releasePicker = () => <label className="release-picker">选择版本<select value={releaseURL ?? ''} onChange={(event) => { setReleaseURL(event.target.value); setSelectedAssetURL(null) }}>{releases.map((item) => <option value={item.url} key={item.tag}>{item.tag} · {item.name}</option>)}</select>{!releases.length && <small>正在获取正式版本列表…</small>}</label>
  const selectedRelease = releases.find((item) => item.url === releaseURL)
  const platform = navigator.userAgent.toLowerCase().includes('win') ? 'windows' : navigator.userAgent.toLowerCase().includes('mac') ? 'macos' : 'linux'
  const architecture = /arm64|aarch64/.test(navigator.userAgent.toLowerCase()) ? 'arm64' : 'x64'
  const matchesSystem = (asset: ReleaseAsset) => { const name = asset.name.toLowerCase(); return (platform === 'windows' && /windows|win/.test(name)) || (platform === 'macos' && /macos|mac|darwin|osx/.test(name)) || (platform === 'linux' && /linux|appimage|\.deb|\.rpm/.test(name)) }
  const matchesArchitecture = (asset: ReleaseAsset) => { const name = asset.name.toLowerCase(); return (architecture === 'arm64' && /arm64|aarch64/.test(name)) || (architecture === 'x64' && /x64|amd64|x86_64/.test(name)) }
  const isMetadataAsset = (asset: ReleaseAsset) => /\.sha\d*|\.sig|checksums?|latest\.yml/.test(asset.name.toLowerCase())
  const releaseAssets = (selectedRelease?.assets ?? []).filter((asset) => !isMetadataAsset(asset))
  const hasArchitectureRecommendation = releaseAssets.some((asset) => matchesSystem(asset) && matchesArchitecture(asset))
  const assetPicker = () => <><h1>选择安装包</h1><div className="choice-list asset-list">{releaseAssets.slice().sort((a, b) => Number(matchesSystem(b)) - Number(matchesSystem(a)) || Number(matchesArchitecture(b)) - Number(matchesArchitecture(a))).map((asset) => <button className={selectedAssetURL === asset.url ? 'choice selected' : 'choice'} key={asset.url} onClick={() => setSelectedAssetURL(asset.url)}><strong>{asset.name}{hasArchitectureRecommendation && matchesSystem(asset) && matchesArchitecture(asset) && <em>推荐</em>}</strong><small>{asset.size ? `${(asset.size / 1024 / 1024).toFixed(1)} MB` : 'GitHub 安装包'}</small></button>)}</div>{selectedRelease && releaseAssets.length === 0 && <p>该版本没有可直接下载的安装包，请返回选择其他版本。</p>}</>
  const developerPage = () => {
    const choices = (title: string, items: Array<[string, string, string]>, key: keyof typeof config) => <><h1>{title}</h1><div className="choice-list">{items.map(([value, label, note]) => <button className={String(config[key]) === value ? 'choice selected' : 'choice'} key={value} onClick={() => choose(key, value)}><strong>{label}</strong><small>{note}</small></button>)}</div></>
    switch (flowStep) {
      case 0: return <EnvironmentCheckPanel title="检查开发环境" report={report} checking={checking} onCheck={() => onCheck()} />
      case 1: return <><h1>给项目起个名字</h1><p>会在你选择的保存位置中新建一个同名文件夹。</p><div className="project-fields"><label>项目名称<input value={project.name} onChange={(event) => dispatch(setProject({ name: event.target.value }))} placeholder="alemonb" /></label><div className="location-options"><button className={project.destinationMode === 'current' ? 'choice selected' : 'choice'} onClick={() => dispatch(setProject({ destinationMode: 'current' }))}><strong>当前运行目录（推荐）</strong><small>直接在启动 alemonjs-setup 的文件夹里创建。</small></button><button className={project.destinationMode === 'custom' ? 'choice selected' : 'choice'} onClick={chooseDestination} disabled={selectingFolder}><strong>{selectingFolder ? '正在打开选择器…' : '选择指定文件夹'}</strong><small>{project.destinationMode === 'custom' && project.destination ? `已选择：${project.destination}` : '点击后在系统窗口中选择保存位置。'}</small></button></div>{folderError && <p className="error">{folderError}</p>}</div></>
      case 2: return choices('你想用哪种语言？', [['js', 'JavaScript（推荐新手）', '写法更简单，先把机器人跑起来。'], ['ts', 'TypeScript', '会在写代码时提前提醒常见错误。']], 'language')
      case 3: return choices('需要代码小助手吗？', [['yes', '需要', 'ESLint 像拼写检查，会提醒容易写错的地方。'], ['no', '暂时不要（默认）', '项目更简单，以后随时可以加。']], 'eslint')
      case 4: return choices('要给项目留存档吗？', [['yes', '要（推荐）', 'Git 会记录每次修改，写错了也方便回退。'], ['no', '暂时不要', '不会创建版本记录。']], 'git')
      case 5: return choices('要让机器人在后台运行吗？', [['yes', '要，使用 PM2', 'PM2 是帮你守着机器人的小管家：关掉终端后，它仍会继续运行。'], ['no', '暂时不要（默认）', '开发时在终端里直接运行，更容易看懂。']], 'pm2')
      case 6: return choices('用什么安装项目依赖？', [['yarn', 'Yarn（推荐）', '没有 Yarn 时会临时使用，不会修改电脑的全局安装。'], ['npm', 'npm', 'Node.js 自带，不需要额外安装。'], ['pnpm', 'pnpm', '更省磁盘空间，需要电脑已经安装。']], 'manager')
      case 7: return <><h1>需要做图片功能吗？</h1><div className="choice-list">{[['none', '不需要', '机器人只发送文字、按钮和普通消息。'], ['html', '纯 HTML', '用简单的网页标签制作图片内容。'], ['react', 'React / JSX', '把图片拆成小组件，适合复杂画面。']].map(([value, label, note]) => <button className={config.image === value ? 'choice selected' : 'choice'} key={value} onClick={() => choose('image', value)}><strong>{label}</strong><small>{note}</small></button>)}</div></>
      case 8: return config.image === 'react' ? choices('图片用什么方式做样式？', [['css', '原生 CSS（推荐）', '最容易理解，不需要再学额外工具。'], ['tailwind', 'Tailwind CSS', '通过组合短类名快速调整外观。'], ['sass', 'Sass / SCSS', '在 CSS 上增加更方便的写法。'], ['less', 'Less', '另一种增强 CSS 的写法。']], 'style') : <><h1>不需要样式工具</h1><p>你没有选择 React / JSX 图片开发，所以这一步不需要额外配置。</p></>
      case 9: return <><h1>下载开发技能吗？</h1><p>开发技能像一本 AlemonJS 的使用说明。安装后，Codex 等工具更容易按推荐方式帮你写代码。</p><div className="choice-list"><button className={config.skills === 'yes' ? 'choice selected' : 'choice'} onClick={() => choose('skills', 'yes')}><strong>下载（推荐）</strong><small>下载 alemonjs-dev-skill，后续可随时更新。</small></button><button className={config.skills === 'no' ? 'choice selected' : 'choice'} onClick={() => choose('skills', 'no')}><strong>暂时不下载</strong><small>不会影响机器人运行，以后也可以安装。</small></button></div><a className="download-link" href="https://github.com/lemonade-lab/alemonjs-dev-skill" target="_blank" rel="noreferrer">查看开发技能说明</a></>
      case 10: return <><h1>{creation?.status === 'ready' ? '项目已创建' : '确认创建项目'}</h1>{creation?.status === 'ready' ? <><p>项目已保存至：{creation.path}</p>{creation.path && <a className="primary-button" href={`/dashboard/robot?root=${encodeURIComponent(creation.path)}`}>前往管理机器人</a>}</> : <div className="config-summary"><span>位置：{project.destinationMode === 'current' ? `当前运行目录/${project.name}` : project.destination ? `${project.destination}/${project.name}` : '请返回填写保存位置'}</span><span>语言：{config.language === 'ts' ? 'TypeScript' : 'JavaScript'}</span><span>包管理器：{config.manager}</span><span>代码小助手：{config.eslint === 'yes' ? '启用' : '不启用'}</span><span>项目存档：{config.git === 'yes' ? '初始化 Git' : '跳过'}</span><span>后台运行：{config.pm2 === 'yes' ? '使用 PM2' : '不使用'}</span><span>开发技能：{config.skills === 'yes' ? '下载' : '不下载'}</span></div>}{creation?.logs && <div className="creation-logs">{creation.logs.map((log, index) => <p key={index}>{log}</p>)}</div>}{creation?.status !== 'ready' && <button className="primary-button" onClick={() => onCreate(createConfig())} disabled={creating}>{creating ? '正在创建…' : '确认创建'}</button>}</>
      default: return null
    }
  }
  const installerPage = () => {
    if (flowStep === 0) return <EnvironmentCheckPanel title="检查安装环境" report={report} checking={checking} onCheck={() => onCheck()} />
    if (flowStep === 1) return <><h1>机器人放在哪里？</h1><p>只需给机器人起名并选一个保存位置；其余均使用适合新手的默认设置。</p><div className="project-fields"><label>机器人名称<input value={project.name} onChange={(event) => dispatch(setProject({ name: event.target.value }))} placeholder="my-alemonjs-bot" /></label><div className="location-options"><button className={project.destinationMode === 'current' ? 'choice selected' : 'choice'} onClick={() => dispatch(setProject({ destinationMode: 'current' }))}><strong>当前运行目录（推荐）</strong><small>直接在启动 alemonjs-setup 的文件夹里安装。</small></button><button className={project.destinationMode === 'custom' ? 'choice selected' : 'choice'} onClick={chooseDestination} disabled={selectingFolder}><strong>{selectingFolder ? '正在打开选择器…' : '选择指定文件夹'}</strong><small>{project.destinationMode === 'custom' && project.destination ? `已选择：${project.destination}` : '点击后在系统窗口中选择保存位置。'}</small></button></div>{folderError && <p className="error">{folderError}</p>}</div></>
    return <><h1>{creation?.status === 'ready' ? '机器人已安装' : '确认安装机器人'}</h1>{creation?.status === 'ready' ? <><p>机器人已安装至：{creation.path}</p>{creation.path && <a className="primary-button" href={`/dashboard/robot?root=${encodeURIComponent(creation.path)}`}>前往管理机器人</a>}</> : <><p>将使用默认 JavaScript 模板、Yarn、Git 存档，不附加开发技能、图片工具或 PM2。</p><div className="config-summary"><span>位置：{project.destinationMode === 'current' ? `当前运行目录/${project.name}` : project.destination ? `${project.destination}/${project.name}` : '请返回填写保存位置'}</span><span>默认环境：JavaScript + Yarn</span><span>项目存档：初始化 Git</span></div></>}{creation?.logs && <div className="creation-logs">{creation.logs.map((log, index) => <p key={index}>{log}</p>)}</div>}</>
  }
  const webPage = () => {
    if (flowStep === 0) return <><h1>选择部署方式</h1><div className="choice-list"><button className={webEdition === 'clean' ? 'choice selected' : 'choice'} onClick={() => { setWebEdition('clean'); automaticCheck.current = null }}><strong>纯净版</strong><small>检查 Node.js 与 Git 后启动 AlemonGo</small></button><button className={webEdition === 'docker' ? 'choice selected' : 'choice'} onClick={() => { setWebEdition('docker'); automaticCheck.current = null }}><strong>Docker 版</strong><small>检查 Docker 后使用 Docker Compose 快速启动</small></button></div></>
    if (flowStep === 1) return <EnvironmentCheckPanel title={webEdition === 'clean' ? '检查运行环境' : '检查 Docker'} report={report} checking={checking} onCheck={() => onCheck(webEdition ?? undefined)} />
    if (webEdition === 'clean' && flowStep === 2) return <><h1>选择下载镜像</h1><p>选择下载来源后继续；随后选择版本与安装包。</p><div className="choice-list">{goal?.mirrors?.map((mirror) => <button className={selectedMirror?.url === mirror.url ? 'choice selected' : 'choice'} key={mirror.url} onClick={() => { setSelectedMirror(mirror); setSelectedAssetURL(null) }}><strong>{mirror.name}</strong><small>{mirror.name === 'GitHub 官方' ? '从 GitHub 官方下载' : '下载速度可能更快'}</small></button>)}</div></>
    if (webEdition === 'clean' && flowStep === 3) return <><h1>选择版本</h1><p>默认已选择最新正式版本。确认后继续选择适合电脑的安装包。</p>{releasePicker()}</>
    if (webEdition === 'clean' && flowStep === 4) return assetPicker()
    return <><h1>Docker Compose 快速启动</h1><p>下一步会生成 docker-compose.yml 并启动服务。</p></>
  }
  const buildPage = () => {
    if (flowStep === 0) return <><h1>选择构建方式</h1><div className="choice-list"><button className={buildMode === 'npm' ? 'choice selected' : 'choice'} onClick={() => { setBuildMode('npm'); automaticCheck.current = null }}><strong>NPM 构建</strong><small>使用 npm 生成应用构建产物</small></button><button className={buildMode === 'git' ? 'choice selected' : 'choice'} onClick={() => { setBuildMode('git'); automaticCheck.current = null }}><strong>Git 化构建</strong><small>遵循 main、release 分支与版本标签标准</small></button></div></>
    if (flowStep === 1) return <EnvironmentCheckPanel title="检查构建环境" report={report} checking={checking} onCheck={() => onCheck(buildMode ?? undefined)} />
    return <><h1>{buildMode === 'git' ? 'Git 化构建' : 'NPM 构建'}</h1><p>{buildMode === 'git' ? '构建产物将按标准整理至 release 分支，并创建对应版本标签。' : '将构建应用并生成可分发产物。'}</p></>
  }
  const downloadPage = () => {
    if (goal?.id === 'mobile') return <><h1>下载 Android 安装包</h1><p>手机版目前仅提供 Android 通用 APK，不经过 GitHub。</p>{goal.downloadUrl && <a className="primary-button" href={goal.downloadUrl} target="_blank" rel="noreferrer">下载 Android APK</a>}</>
    if (flowStep === 0) return <><h1>选择下载镜像</h1><p>选择一个下载来源后继续；下一页再选要下载的版本。</p><div className="choice-list">{goal?.mirrors?.map((mirror) => <button className={selectedMirror?.url === mirror.url ? 'choice selected' : 'choice'} key={mirror.url} onClick={() => { setSelectedMirror(mirror); setSelectedAssetURL(null) }}><strong>{mirror.name}</strong><small>{mirror.name === 'GitHub 官方' ? '从 GitHub 官方下载' : '下载速度可能更快'}</small></button>)}</div></>
    if (flowStep === 1) return <><h1>选择下载版本</h1><p>默认已选择最新正式版本。确认后继续选择安装包。</p>{releasePicker()}<p className="selected-download">下载镜像：{selectedMirror?.name ?? 'GitHub 官方'}</p></>
    return assetPicker()
  }
  const selectPurpose = (id: string) => { automaticCheck.current = null; setSelectedMirror(null); setSelectedAssetURL(null); onSelect(id) }
  const goBack = () => { if (step === 1) { onSelect(null); setStep(0); return }; back() }
  const resetPurpose = () => { onSelect(null); setStep(0) }
  registerBack(step > 0 ? goBack : () => {})
  const createConfig = (): ProjectConfig => isInstaller ? { template: 'bot', name: project.name, destinationMode: project.destinationMode, destination: project.destination, language: 'js', packageManager: 'yarn', eslint: false, initializeGit: true, usePM2: false, imageMode: 'none', styleMode: 'css', downloadSkills: false } : ({ template: 'dev', name: project.name, destinationMode: project.destinationMode, destination: project.destination, language: config.language, packageManager: config.manager, eslint: config.eslint === 'yes', initializeGit: config.git === 'yes', usePM2: config.pm2 === 'yes', imageMode: config.image, styleMode: config.image === 'react' ? config.style : 'css', downloadSkills: config.skills === 'yes' })
  const isDownloadFlow = goal?.id === 'desktop' || goal?.id === 'mobile'
  const isWeb = goal?.id === 'web'
  const isBuild = goal?.id === 'build'
  const purposeOptions = [['develop', '开发', '创建一个可按需配置的 AlemonJS 开发项目。'], ['deploy', '部署', '部署源码版、桌面版、手机版或 Web 版。'], ['manage', '管理', '进入后台，管理已有机器人项目。']]
  return <section className="wizard"><aside className="wizard-steps"><p>{goal?.title ?? '开始'}</p>{totalSteps.map((label, index) => <div ref={index === step ? currentStepElement : null} key={label} className={index < step ? 'done' : index === step ? 'current' : ''} onClick={index <= step ? () => index === 0 ? resetPurpose() : setFlowStep(index) : undefined}><span>{index < step ? '✓' : index + 1}</span>{label}</div>)}</aside><section className="wizard-page"><div className="wizard-content">{!goal || step === 0 ? <div className="guide-question"><p className="question-kicker">AlemonJS Setup</p><h1>你现在想做什么？</h1><p className="question-lead">从一个目标开始，剩下的步骤交给引导。</p><div className="question-options">{loading ? <p>正在准备选项…</p> : purposeOptions.map(([id, title, note]) => <button key={id} onClick={() => id === 'deploy' ? navigate('/guide/group/deploy') : selectPurpose(id)}><i>{icons[id] ?? '·'}</i><span><strong>{title}</strong><small>{note}</small></span><b>→</b></button>)}</div></div> : isInstaller ? installerPage() : isDeveloper ? developerPage() : isWeb ? webPage() : isBuild ? buildPage() : isDownloadFlow ? downloadPage() : <><h1>{totalSteps[step]}</h1><p>{goal.description}</p></>} </div><footer className="wizard-actions">{isInstaller && flowStep === 0 && report?.ready && <button className="next-button" onClick={next}>继续</button>}{isInstaller && flowStep === 1 && <button className="next-button" onClick={next}>继续</button>}{isInstaller && flowStep === 2 && creation?.status !== 'ready' && <button className="next-button" onClick={() => onCreate(createConfig())} disabled={creating}>{creating ? '正在安装…' : '确认安装'}</button>}{isDeveloper && flowStep === 0 && report?.ready && <button className="next-button" onClick={next}>继续</button>}{isDeveloper && flowStep > 0 && flowStep < 10 && <button className="next-button" onClick={next}>继续</button>}{isDeveloper && flowStep === 10 && creation?.status !== 'ready' && <button className="next-button" onClick={() => onCreate(createConfig())} disabled={creating}>{creating ? '正在创建…' : '确认创建'}</button>}{isWeb && flowStep === 0 && webEdition && <button className="next-button" onClick={next}>继续</button>}{isWeb && flowStep === 1 && report?.ready && <button className="next-button" onClick={next}>继续</button>}{isWeb && flowStep === 2 && webEdition === 'clean' && selectedMirror && <button className="next-button" onClick={next}>继续</button>}{isWeb && flowStep === 3 && webEdition === 'clean' && releaseURL && <button className="next-button" onClick={next}>继续</button>}{isWeb && flowStep === 4 && webEdition === 'clean' && selectedAssetURL && <a className="next-button" href={mirrorURL(selectedMirror, selectedAssetURL)} target="_blank" rel="noreferrer">开始下载</a>}{isWeb && flowStep === 2 && webEdition === 'docker' && <button className="next-button" disabled>生成并启动</button>}{isBuild && flowStep === 0 && buildMode && <button className="next-button" onClick={next}>继续</button>}{isBuild && flowStep === 1 && report?.ready && <button className="next-button" onClick={next}>继续</button>}{isBuild && flowStep === 2 && <button className="next-button" disabled>开始构建</button>}{!isInstaller && !isDeveloper && !isWeb && !isBuild && !isDownloadFlow && goal && <button className="next-button" onClick={next}>继续</button>}{isDownloadFlow && flowStep === 0 && selectedMirror && <button className="next-button" onClick={next}>继续</button>}{isDownloadFlow && flowStep === 1 && releaseURL && <button className="next-button" onClick={next}>继续</button>}{isDownloadFlow && flowStep === 2 && selectedAssetURL && <a className="next-button" href={mirrorURL(selectedMirror, selectedAssetURL)} target="_blank" rel="noreferrer">开始下载</a>}</footer></section></section>
}

function LegacyDashboard({ goals, goal, report, checking, error, onSelect, onOpenGuide, onCheck }: { goals: Goal[]; goal?: Goal; report: Report | null; checking: boolean; error: string; onSelect: (id: string) => void; onOpenGuide: () => void; onCheck: () => void }) {
  const readyCount = report?.checks.filter((check) => check.status === 'ready').length ?? 0
  return <main className="dashboard-shell">
    <header className="dashboard-bar"><div className="brand dark"><span>λ</span><div><strong>AlemonJS Setup</strong><small>后台中心</small></div></div><button className="primary-button" onClick={onOpenGuide}>？</button></header>
    <section className="dashboard-heading"><div><p className="eyebrow">后台中心</p><h1>管理已创建的流程</h1><p>在这里手动检查环境、查看准备状态；需要新建或继续引导时，随时重新打开引导。</p></div></section>
    {error && <p className="error" role="alert">{error}</p>}
    <section className="console-goals">{goals.map((item) => <button className={item.id === goal?.id ? 'console-goal active' : 'console-goal'} key={item.id} onClick={() => onSelect(item.id)}><i>{icons[item.id] ?? '·'}</i><div><strong>{item.title}</strong><small>{item.description}</small></div></button>)}</section>
    {goal && <section className="dashboard-detail">
      <div className="detail-heading"><div><p className="eyebrow">当前功能</p><h2>{goal.title}</h2><p>{goal.description}</p></div><button className="primary-button" onClick={onCheck} disabled={checking}>{checking ? '检查中…' : '手动检查环境'}</button></div>
      <section className="summary-grid">
        <article><span>当前状态</span><strong className={report?.ready ? 'success' : ''}>{report ? (report.ready ? '已准备' : '需要处理') : '未检查'}</strong><small>{report ? `检测于 ${new Date(report.checkedAt).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })}` : '可在此手动检查'}</small></article>
        <article><span>环境通过项</span><strong>{report ? `${readyCount} / ${report.checks.length}` : '—'}</strong><small>{report?.platform ?? '等待检测本机信息'}</small></article>
        <article><span>引导状态</span><strong>可重新打开</strong><small>关闭引导不会影响已有项目或环境</small></article>
      </section>
      <section className="content-card"><h2>环境准备情况</h2>{report ? <div className="check-list">{report.checks.map((check) => <article className={`check-row ${check.status}`} key={check.id}><span className="check-state">{check.status === 'ready' ? '✓' : '!'}</span><div><strong>{check.name}</strong><p>{check.detail}</p></div>{check.suggestion && <small>{check.suggestion}</small>}</article>)}</div> : <div className="empty-state"><span>◌</span><p>尚未进行环境检查</p><small>点击“手动检查环境”即可查看本机准备状态。</small></div>}</section>
    </section>}
  </main>
}

void LegacyDashboard

void DashboardDeprecated
function DashboardDeprecated({ report, checking, error, defaultPage, onOpenGuide, onCheck }: { goals: Goal[]; goal?: Goal; report: Report | null; checking: boolean; error: string; defaultPage: string; onSelect: (id: string) => void; onOpenGuide: () => void; onCheck: () => void }) {
  const [page, setPage] = useState<string>(defaultPage)
  const [root, setRoot] = useState(() => new URLSearchParams(window.location.search).get('root') ?? '.')
  const [file, setFile] = useState('.npmrc')
  const [content, setContent] = useState('')
  const [output, setOutput] = useState('')
  const [message, setMessage] = useState('chore: update robot')
  const [busy, setBusy] = useState(false)
  useEffect(() => setPage(defaultPage), [defaultPage])
  useEffect(() => {
    const input = document.querySelector<HTMLInputElement>('input[placeholder="/完整/机器人/目录"]')
    const chooseDirectory = async () => { const response = await fetch('/api/v1/directories/select', { method: 'POST' }); const data = await response.json() as { path?: string }; if (response.ok && data.path) setRoot(data.path) }
    input?.addEventListener('focus', chooseDirectory)
    return () => input?.removeEventListener('focus', chooseDirectory)
  }, [])
  const api = async (method: string, data: Record<string, string>) => { setBusy(true); try { const query = method === 'GET' ? `?${new URLSearchParams(data)}` : ''; const response = await fetch(`/api/v1/robot${query}`, method === 'GET' ? {} : { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(data) }); const json = await response.json() as { output?: string; error?: string }; if (!response.ok) throw new Error(json.error); setOutput(json.output ?? '操作完成。'); if (method === 'GET') setContent(json.output ?? '') } catch (reason) { setOutput(reason instanceof Error ? reason.message : '操作未完成。') } finally { setBusy(false) } }
  const editorFile = (next: string) => { setFile(next); api('GET', { root, file: next }) }
  const operations: Array<[string, string, string]> = [['install', '重载依赖', '重新安装项目需要的包。'], ['dev', '开发模式启动', '在开发模式运行机器人。'], ['build', '构建应用', '生成可发布的构建产物。'], ['pm2', '后台模式启动', '先构建，再交给 PM2 在后台守护运行。']]
  const catalogs = page === 'plugins' ? [['alemonjs', 'AlemonJS 核心', '机器人框架核心包。'], ['@alemonjs/db', '数据存储', '为机器人增加数据库能力。'], ['@alemonjs/bubble', '气泡服务', '接入气泡相关能力。']] : [['@alemonjs/onebot', 'OneBot 连接', '连接支持 OneBot 协议的平台。'], ['@alemonjs/qq-bot', 'QQ Bot 连接', '连接 QQ 官方机器人平台。'], ['@alemonjs/discord', 'Discord 连接', '连接 Discord 平台。']]
  return <main className="guide-shell"><section className="guide-window dashboard-window"><header className="guide-bar"><span className="console-title">后台中心</span><button className="primary-button" onClick={onOpenGuide}>？</button></header><section className="console-layout"><aside className="console-nav"><button className={page === 'environment' ? 'active' : ''} onClick={() => setPage('environment')}>环境管理</button><button className={page === 'robot' ? 'active' : ''} onClick={() => setPage('robot')}>机器人管理</button><button className={page === 'plugins' ? 'active' : ''} onClick={() => setPage('plugins')}>插件管理</button><button className={page === 'connections' ? 'active' : ''} onClick={() => setPage('connections')}>连接管理</button></aside><section className="console-page">{page === 'environment' ? <><p className="eyebrow">环境管理</p><h1>检查当前电脑的开发环境</h1><p>这里会检查 Node.js、Git 等机器人需要的工具。</p><button className="primary-button" onClick={onCheck} disabled={checking}>{checking ? '检查中…' : '开始检查'}</button>{report && <div className="compact-checks">{report.checks.map((check) => <span className={check.status} key={check.id}>{check.status === 'ready' ? '✓' : '!'} {check.name}：{check.detail}</span>)}</div>}</> : <><p className="eyebrow">{page === 'plugins' ? '插件管理' : page === 'connections' ? '连接管理' : '机器人管理'}</p><h1>{page === 'plugins' ? '安装机器人能力' : page === 'connections' ? '安装平台连接包' : '管理你的机器人项目'}</h1>{page !== 'robot' && page !== 'environment' && <section className="package-catalog">{catalogs.map(([pkg, title, note]) => <article key={pkg}><strong>{title}</strong><small>{note}</small><code>{pkg}</code><button className="primary-button" disabled={busy} onClick={() => api('POST', { root, action: 'install-package', package: pkg })}>安装</button></article>)}</section>}<div className="robot-root"><button className={root === '.' ? 'choice selected' : 'choice'} onClick={() => setRoot('.')}><strong>当前运行目录</strong><small>管理启动本工具所在的机器人项目。</small></button><button className={root !== '.' ? 'choice selected' : 'choice'} onClick={() => setRoot('')}><strong>指定机器人目录</strong><small>输入包含 package.json 的机器人文件夹。</small></button>{root !== '.' && <input value={root} onChange={(event) => setRoot(event.target.value)} placeholder="/完整/机器人/目录" />}</div>{page === 'robot' && <><div className="robot-tabs">{[['.npmrc', '镜像设置'], ['alemon.config.yaml', 'AlemonJS 配置'], ['README.md', 'README']].map(([name, label]) => <button className={file === name ? 'active' : ''} key={name} onClick={() => editorFile(name)}>{label}</button>)}</div><div className="file-editor"><textarea value={content} onChange={(event) => setContent(event.target.value)} placeholder="点击上方标签读取文件内容" /><button className="primary-button" disabled={busy} onClick={() => api('PUT', { root, file, content })}>保存 {file}</button></div><section className="robot-actions">{operations.map(([action, title, note]) => <button key={action} disabled={busy} onClick={() => api('POST', { root, action })}><strong>{title}</strong><small>{note}</small></button>)}<div className="commit-action"><input value={message} onChange={(event) => setMessage(event.target.value)} placeholder="提交说明" /><button disabled={busy} onClick={() => api('POST', { root, action: 'commit', message })}>提交代码</button></div></section></>}{(output || error) && <pre className="robot-output">{output || error}</pre>}</>}</section></section></section></main>
}

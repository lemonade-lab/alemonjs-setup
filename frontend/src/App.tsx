import { useStoreState } from './store/guideStore'
import { useEffect, useMemo, useRef } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { useLocation, useNavigate } from 'react-router-dom'
import {
  setDeveloper,
  setProject,
  toggleCapability as toggleCapabilityAction,
  type RootState
} from './store/guideStore'
import { Dashboard } from './components/Dashboard'
import { EnvironmentFixDialog } from './components/EnvironmentFixDialog'
import { ErrorNotice } from './components/ErrorNotice'
import {
  workspaceApi,
  useGoalsQuery,
  useLazyEnvironmentReportQuery,
  useReleasesQuery
} from './store/workspaceApi'
import { GuideHome } from './features/guide/GuideHome'
import { EnvironmentCheckPanel } from './features/guide/EnvironmentCheckPanel'
import { guideIcons as icons } from './features/guide/icons'
import { recommendReleaseAssets } from './features/guide/releaseAssets'
import type {
  Check,
  Creation,
  Goal,
  Mirror,
  ProjectConfig,
  Release,
  Report
} from './features/guide/types'

const capabilityLabels: Record<string, string> = {
  bubble: '气泡服务',
  database: '数据存储',
  discord: 'Discord',
  onebot: 'OneBot',
  qqbot: 'QQ Bot'
}

export default function App() {
  const navigate = useNavigate()
  const location = useLocation()
  const [error, setError] = useStoreState('')
  const [creating, setCreating] = useStoreState(false)
  const [creation, setCreation] = useStoreState<Creation | null>(null)
  const [repairCheck, setRepairCheck] = useStoreState<Check | null>(null)
  const { data: goalData, isLoading: loading } = useGoalsQuery()
  const [
    loadEnvironmentReport,
    { data: environmentData, isFetching: checking }
  ] = useLazyEnvironmentReportQuery()
  const report = (environmentData as Report | undefined) ?? null
  const goals = (goalData as Goal[] | null | undefined) ?? []
  const routeGoal = location.pathname.match(
    /^\/guide\/([^/]+)\/step\/\d+$/
  )?.[1]
  const guideGroup = location.pathname.match(/^\/guide\/group\/([^/]+)$/)?.[1]
  const selectedID = routeGoal ?? null
  const guideOpen = !location.pathname.startsWith('/dashboard')
  const activeID = selectedID ?? 'install'
  const activeGoal = goals.find(goal => goal.id === activeID)

  useEffect(() => {
    if (location.pathname === '/') navigate('/guide', { replace: true })
  }, [location.pathname, navigate])
  function openGuide() {
    navigate('/guide')
    setCreation(null)
    setError('')
  }
  async function checkEnvironment(variant?: string) {
    const selectedVariant = typeof variant === 'string' ? variant : ''
    try {
      setError('')
      // Invalidate the cached result first so "重新检查" always performs a
      // fresh server-side environment check instead of returning the cached
      // 5-minute report with no visible change.
      workspaceApi.util.invalidateTags([
        { type: 'EnvironmentReport', id: `${activeID}:${selectedVariant}` }
      ])
      await loadEnvironmentReport(
        { goalId: activeID, variant: selectedVariant },
        false
      ).unwrap()
    } catch (reason) {
      setError(
        reason instanceof Error
          ? reason.message
          : '环境检查未完成，请稍后重试。'
      )
    }
  }
  async function createProject(config: ProjectConfig) {
    try {
      setCreating(true)
      setError('')
      setCreation(null)
      const response = await fetch('/api/v1/projects', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(config)
      })
      const data = (await response.json()) as Creation & {
        error?: string
        result?: Creation
      }
      setCreation(data.result ?? data)
      if (!response.ok)
        setError(data.error ?? '项目创建未完成，请检查下方日志。')
    } catch {
      setError('项目创建请求未完成，请稍后重试。')
    } finally {
      setCreating(false)
    }
  }

  return (
    <div className="app-shell">
      {guideOpen ? (
        <GuideHome
          loading={loading}
          group={guideGroup}
          goal={selectedID ? activeGoal : undefined}
          report={report}
          checking={checking}
          error={error}
          creating={creating}
          creation={creation}
          onSelect={id => {
            if (id === 'manage') {
              navigate('/dashboard/robot')
              return
            }
            navigate(id ? `/guide/${id}/step/1` : '/guide')
            setCreation(null)
            setError('')
          }}
          onClose={() => navigate('/dashboard')}
          onClearError={() => setError('')}
          onCheck={checkEnvironment}
          onCreate={createProject}
          onFix={setRepairCheck}
          renderFlow={registerBack => (
            <FlowView
              loading={loading}
              goal={selectedID ? activeGoal : undefined}
              report={report}
              checking={checking}
              creating={creating}
              creation={creation}
              onSelect={id => {
                if (id === 'manage') {
                  navigate('/dashboard/robot')
                  return
                }
                navigate(id ? `/guide/${id}/step/1` : '/guide')
                setCreation(null)
                setError('')
              }}
              onCheck={checkEnvironment}
              onCreate={createProject}
              registerBack={registerBack}
            />
          )}
        />
      ) : (
        <Dashboard
          goals={goals}
          goal={activeGoal}
          report={report}
          checking={checking}
          error={error}
          onClearError={() => setError('')}
          defaultPage={
            location.pathname.endsWith('/robot') ? 'robot' : 'environment'
          }
          onSelect={id => {
            if (id === 'manage') {
              navigate('/dashboard/robot')
              return
            }
            navigate(`/guide/${id}/step/1`)
            setError('')
          }}
          onOpenGuide={openGuide}
          onCheck={checkEnvironment}
          onFix={setRepairCheck}
        />
      )}
      {repairCheck && (
        <EnvironmentFixDialog
          check={repairCheck}
          onClose={() => setRepairCheck(null)}
        />
      )}
    </div>
  )
}

function FlowView({
  loading,
  goal,
  report,
  checking,
  creating,
  creation,
  onSelect,
  onCheck,
  onCreate,
  registerBack
}: {
  loading: boolean
  goal?: Goal
  report: Report | null
  checking: boolean
  creating: boolean
  creation: Creation | null
  onSelect: (id: string | null) => void
  onCheck: (variant?: string) => void
  onCreate: (config: ProjectConfig) => void
  registerBack: (handler: () => void) => void
}) {
  const navigate = useNavigate()
  const location = useLocation()
  const dispatch = useDispatch()
  const config = useSelector((state: RootState) => state.guide.developer)
  const project = useSelector((state: RootState) => state.guide.project)
  const routedStep = Number(location.pathname.match(/\/step\/(\d+)/)?.[1] ?? 0)
  const [step, setStep] = useStoreState(routedStep)
  const [webEdition, setWebEdition] = useStoreState<'clean' | 'docker' | null>(null)
  const [buildMode, setBuildMode] = useStoreState<'npm' | 'git' | null>(null)
  const [selectedMirror, setSelectedMirror] = useStoreState<Mirror | null>(null)
  const [releaseURL, setReleaseURL] = useStoreState<string | null>(null)
  const [selectedAssetURL, setSelectedAssetURL] = useStoreState<string | null>(null)
  const [folderError, setFolderError] = useStoreState('')
  const automaticCheck = useRef<string | null>(null)
  const currentStepElement = useRef<HTMLButtonElement | null>(null)
  const capabilities = config.capabilities ?? []
  const isDeveloper = goal?.id === 'develop'
  const isInstaller = goal?.id === 'install'
  const releaseApp =
    goal?.id === 'desktop'
      ? 'alemondesk'
      : goal?.id === 'web' && webEdition === 'clean'
        ? 'alx'
        : null
  const {
    data: releaseData,
    isError: releaseError,
    isFetching: releaseFetching,
    refetch: refetchReleases
  } = useReleasesQuery(releaseApp ?? '', {
    skip: !releaseApp
  })
  const releases = useMemo(
    () => (releaseData as Release[] | null | undefined) ?? [],
    [releaseData]
  )
  const webSteps =
    webEdition === 'clean'
      ? [
          '选择部署方式',
          '检查 Node.js 与 Git',
          '选择下载镜像',
          '选择版本',
          '选择安装包'
        ]
      : webEdition === 'docker'
        ? ['选择部署方式', '检查 Docker', 'Docker Compose 快速启动']
        : ['选择部署方式']
  const buildSteps =
    buildMode === 'npm'
      ? ['选择构建方式', '检查 npm 环境', 'NPM 构建']
      : buildMode === 'git'
        ? ['选择构建方式', '检查 Git 环境', 'Git 化构建']
        : ['选择构建方式']
  const downloadSteps =
    goal?.id === 'mobile'
      ? ['下载 Android 安装包']
      : selectedMirror
        ? ['选择镜像', '选择版本', '选择安装包']
        : ['选择镜像']
  const totalSteps = goal
    ? [
        '选择目的',
        ...(goal.id === 'web'
          ? webSteps
          : goal.id === 'build'
            ? buildSteps
            : goal.id === 'desktop' || goal.id === 'mobile'
              ? downloadSteps
              : goal.steps)
      ]
    : ['选择目的']
  const flowStep = step - 1
  const setFlowStep = (value: number) => {
    const nextStep = Math.max(0, Math.min(value, totalSteps.length - 1))
    setStep(nextStep)
    if (goal) navigate(`/guide/${goal.id}/step/${nextStep}`)
  }
  const next = () => setFlowStep(step + 1)
  const back = () => setFlowStep(step - 1)
  const choose = (key: keyof typeof config, value: string) =>
    dispatch(setDeveloper({ [key]: value }))
  const toggleCapability = (value: string) =>
    dispatch(toggleCapabilityAction(value))
  const chooseDestination = () => {
    setFolderError('')
    window.dispatchEvent(new Event('alemon:choose-directory'))
  }
  useEffect(() => {
    if (routedStep !== step) setStep(routedStep)
  }, [routedStep, step, setStep])
  useEffect(() => {
    const variant =
      goal?.id === 'web'
        ? webEdition
        : goal?.id === 'build'
          ? buildMode
          : undefined
    const isCheckStep =
      goal?.id === 'develop' || goal?.id === 'install'
        ? flowStep === 0
        : (goal?.id === 'web' || goal?.id === 'build') &&
          Boolean(variant) &&
          flowStep === 1
    if (
      (goal?.id === 'develop' || goal?.id === 'install' || variant) &&
      isCheckStep &&
      automaticCheck.current !== `${goal?.id}:${variant ?? ''}`
    ) {
      automaticCheck.current = `${goal?.id}:${variant ?? ''}`
      onCheck(variant ?? undefined)
    }
  }, [buildMode, flowStep, goal?.id, onCheck, webEdition])
  useEffect(() => {
    currentStepElement.current?.scrollIntoView({
      behavior: 'smooth',
      block: 'center'
    })
  }, [step, setStep])
  useEffect(() => {
    if (!releaseApp) return
    setReleaseURL(releases[0]?.url ?? null)
    setSelectedAssetURL(null)
  }, [releaseApp, releases, setReleaseURL, setSelectedAssetURL])
  const mirrorURL = (mirror: Mirror | null, url: string) => {
    if (!mirror) return url
    const index = mirror.url.indexOf('https://github.com')
    return index === -1 ? url : mirror.url.slice(0, index) + url
  }
  const releasePicker = () => (
    <div className="release-picker">
      <label>
        选择版本
        <select
          value={releaseURL ?? ''}
          onChange={event => {
            setReleaseURL(event.target.value)
            setSelectedAssetURL(null)
          }}
        >
          {releases.map(item => (
            <option value={item.url} key={item.tag}>
              {item.tag} · {item.name}
            </option>
          ))}
        </select>
      </label>
      {!releases.length && !releaseError && releaseFetching && (
        <small>正在获取正式版本列表…</small>
      )}
      {!releases.length && releaseError && (
        <small className="text-(--theme-danger-text)">
          正式版本列表获取失败，可能是网络或代理问题。
          <button type="button" onClick={() => void refetchReleases()}>
            重试
          </button>
        </small>
      )}
    </div>
  )
  const selectedRelease = releases.find(item => item.url === releaseURL)
  const releaseAssets = recommendReleaseAssets(
    selectedRelease?.assets ?? [],
    navigator.userAgent.toLowerCase()
  )
  const assetPicker = () => (
    <>
      <h1>选择安装包</h1>
      <div className="grid gap-2.5 my-5 asset-list">
        {releaseAssets.assets.map(asset => (
          <button
            className={
              selectedAssetURL === asset.url ? 'choice selected' : 'choice'
            }
            key={asset.url}
            onClick={() => setSelectedAssetURL(asset.url)}
          >
            <strong>
              {asset.name}
              {releaseAssets.isRecommended(asset) && <em>推荐</em>}
            </strong>
            <small>
              {asset.size
                ? `${(asset.size / 1024 / 1024).toFixed(1)} MB`
                : 'GitHub 安装包'}
            </small>
          </button>
        ))}
      </div>
      {selectedRelease && releaseAssets.assets.length === 0 && (
        <p>该版本没有可直接下载的安装包，请返回选择其他版本。</p>
      )}
    </>
  )
  const developerPage = () => {
    const choices = (
      title: string,
      items: Array<[string, string, string]>,
      key: keyof typeof config
    ) => (
      <>
        <h1>{title}</h1>
        <div className="grid gap-2.5 my-5">
          {items.map(([value, label, note]) => (
            <button
              className={
                String(config[key]) === value ? 'choice selected' : 'choice'
              }
              key={value}
              onClick={() => choose(key, value)}
            >
              <strong>{label}</strong>
              <small>{note}</small>
            </button>
          ))}
        </div>
      </>
    )
    switch (flowStep) {
      case 0:
        return (
          <EnvironmentCheckPanel
            title="检查开发环境"
            report={report}
            checking={checking}
            onCheck={() => onCheck()}
          />
        )
      case 1:
        return (
          <>
            <h1>给项目起个名字</h1>
            <p>会在你选择的保存位置中新建一个同名文件夹。</p>
            <div className="project-fields">
              <label>
                项目名称
                <input
                  value={project.name}
                  onChange={event =>
                    dispatch(setProject({ name: event.target.value }))
                  }
                  placeholder="alemonb"
                />
              </label>
              <div className="location-options">
                <button
                  className={
                    project.destinationMode === 'current'
                      ? 'choice selected'
                      : 'choice'
                  }
                  onClick={() =>
                    dispatch(setProject({ destinationMode: 'current' }))
                  }
                >
                  <strong>当前运行目录（推荐）</strong>
                  <small>直接在启动 alemonx 的文件夹里创建。</small>
                </button>
                <button
                  className={
                    project.destinationMode === 'custom'
                      ? 'choice selected'
                      : 'choice'
                  }
                  onClick={chooseDestination}
                >
                  <strong>选择指定文件夹</strong>
                  <small>
                    {project.destinationMode === 'custom' && project.destination
                      ? `已选择：${project.destination}`
                      : '在目录选择器中选择保存位置。'}
                  </small>
                </button>
              </div>
              {folderError && (
                <ErrorNotice
                  message={folderError}
                  onClose={() => setFolderError('')}
                />
              )}
            </div>
          </>
        )
      case 2:
        return choices(
          '你想用哪种语言？',
          [
            ['js', 'JavaScript（推荐新手）', '写法更简单，先把机器人跑起来。'],
            ['ts', 'TypeScript', '会在写代码时提前提醒常见错误。']
          ],
          'language'
        )
      case 3:
        return choices(
          '需要代码小助手吗？',
          [
            ['yes', '需要', 'ESLint 像拼写检查，会提醒容易写错的地方。'],
            ['no', '暂时不要（默认）', '项目更简单，以后随时可以加。']
          ],
          'eslint'
        )
      case 4:
        return choices(
          '要给项目留存档吗？',
          [
            ['yes', '要（推荐）', 'Git 会记录每次修改，写错了也方便回退。'],
            ['no', '暂时不要', '不会创建版本记录。']
          ],
          'git'
        )
      case 5:
        return choices(
          '要让机器人在后台运行吗？',
          [
            [
              'yes',
              '要，使用 PM2（默认）',
              'PM2 是帮你守着机器人的小管家：关掉终端后，它仍会继续运行。'
            ],
            ['no', '暂时不要', '开发时在终端里直接运行，更容易看懂。']
          ],
          'pm2'
        )
      case 6:
        return choices(
          '用什么安装项目依赖？',
          [
            [
              'yarn',
              'Yarn（推荐）',
              '没有 Yarn 时会临时使用，不会修改电脑的全局安装。'
            ],
            ['npm', 'npm', 'Node.js 自带，不需要额外安装。'],
            ['pnpm', 'pnpm', '更省磁盘空间，需要电脑已经安装。']
          ],
          'manager'
        )
      case 7:
        return (
          <>
            <h1>选择扩展包</h1>
            <div className="grid gap-2.5 my-5">
              {[
                ['database', '数据存储', '@alemonjs/db'],
                ['qqbot', 'QQ Bot 连接', '@alemonjs/qq-bot'],
                ['onebot', 'OneBot 连接', '@alemonjs/onebot'],
                ['bubble', 'bubble服务', '@alemonjs/bubble'],
                ['discord', 'Discord 连接', '@alemonjs/discord'],
              ].map(([value, label, note]) => (
                <button
                  className={
                    capabilities.includes(value) ? 'choice selected' : 'choice'
                  }
                  key={value}
                  onClick={() => toggleCapability(value)}
                >
                  <strong>{label}</strong>
                  <small>
                    {note} ·{' '}
                    {capabilities.includes(value) ? '已选择' : '点击添加'}
                  </small>
                </button>
              ))}
            </div>
          </>
        )
      case 8:
        return (
          <>
            <h1>需要做图片功能吗？</h1>
            <div className="grid gap-2.5 my-5">
              {[
                ['none', '不需要', '生成文字、按钮和普通消息模板。'],
                [
                  'html',
                  '纯 HTML',
                  '保留文本帮助模板，后续可按需加入 HTML 渲染方案。'
                ],
                [
                  'react',
                  'React / JSX',
                  '安装 jsxp，并生成可拆分组件的图片模板。'
                ]
              ].map(([value, label, note]) => (
                <button
                  className={
                    config.image === value ? 'choice selected' : 'choice'
                  }
                  key={value}
                  onClick={() => choose('image', value)}
                >
                  <strong>{label}</strong>
                  <small>{note}</small>
                </button>
              ))}
            </div>
          </>
        )
      case 9:
        return config.image === 'react' ? (
          choices(
            '图片用什么方式做样式？',
            [
              ['css', '原生 CSS（推荐）', '最容易理解，不需要再学额外工具。'],
              ['tailwind', 'Tailwind CSS', '安装 Tailwind 与 CSS 压缩工具。'],
              ['sass', 'Sass / SCSS', '安装 Sass。'],
              ['less', 'Less', '安装 Less。']
            ],
            'style'
          )
        ) : (
          <>
            <h1>不需要样式工具</h1>
            <p>未选择 React / JSX 图片模板，不会安装额外样式依赖。</p>
          </>
        )
      case 10:
        return (
          <>
            <h1>下载开发技能吗？</h1>
            <p>
              开发技能像一本 AlemonJS 的使用说明。安装后，Codex
              等工具更容易按推荐方式帮你写代码。
            </p>
            <div className="grid gap-2.5 my-5">
              <button
                className={
                  config.skills === 'yes' ? 'choice selected' : 'choice'
                }
                onClick={() => choose('skills', 'yes')}
              >
                <strong>下载（推荐）</strong>
                <small>下载 alemonjs-dev-skill，后续可随时更新。</small>
              </button>
              <button
                className={
                  config.skills === 'no' ? 'choice selected' : 'choice'
                }
                onClick={() => choose('skills', 'no')}
              >
                <strong>暂时不下载</strong>
                <small>不会影响机器人运行，以后也可以安装。</small>
              </button>
            </div>
            <a
              className="download-link"
              href="https://github.com/lemonade-lab/alemonjs-dev-skill"
              target="_blank"
              rel="noreferrer"
            >
              查看开发技能说明
            </a>
          </>
        )
      case 11:
        return (
          <>
            <h1>
              {creation?.status === 'ready' ? '项目已创建' : '确认创建项目'}
            </h1>
            {creation?.status === 'ready' ? (
              <>
                <p>{creation.path}</p>
                {creation.path && (
                  <a
                    className="primary-button"
                    href={`/dashboard/robot?root=${encodeURIComponent(creation.path)}`}
                  >
                    前往管理机器人
                  </a>
                )}
              </>
            ) : (
              <div className="config-summary">
                <span>
                  位置：
                  {project.destinationMode === 'current'
                    ? `当前运行目录/${project.name}`
                    : project.destination
                      ? `${project.destination}/${project.name}`
                      : '请返回填写保存位置'}
                </span>
                <span>
                  语言：{config.language === 'ts' ? 'TypeScript' : 'JavaScript'}
                </span>
                <span>包管理器：{config.manager}</span>
                <span>
                  开发能力包：
                  {capabilities.length
                    ? capabilities
                        .map(item => capabilityLabels[item] ?? item)
                        .join('、')
                    : '仅基础框架'}
                </span>
                <span>
                  图片模板：
                  {config.image === 'react'
                    ? `React / JSX · ${config.style}`
                    : config.image === 'html'
                      ? '纯 HTML'
                      : '不生成'}
                </span>
                <span>
                  代码小助手：{config.eslint === 'yes' ? '启用' : '不启用'}
                </span>
                <span>
                  项目存档：{config.git === 'yes' ? '初始化 Git' : '跳过'}
                </span>
                <span>
                  后台运行：{config.pm2 === 'yes' ? '使用 PM2' : '不使用'}
                </span>
                <span>
                  开发技能：{config.skills === 'yes' ? '下载' : '不下载'}
                </span>
              </div>
            )}
          </>
        )
      default:
        return null
    }
  }
  const installerPage = () => {
    if (flowStep === 0)
      return (
        <EnvironmentCheckPanel
          title="检查安装环境"
          report={report}
          checking={checking}
          onCheck={() => onCheck()}
        />
      )
    if (flowStep === 1)
      return (
        <>
          <h1>机器人放在哪里？</h1>
          <p>
            只需给机器人起名并选一个保存位置；其余均使用适合新手的默认设置。
          </p>
          <div className="project-fields">
            <label>
              机器人名称
              <input
                value={project.name}
                onChange={event =>
                  dispatch(setProject({ name: event.target.value }))
                }
                placeholder="my-alemonjs-bot"
              />
            </label>
            <div className="location-options">
              <button
                className={
                  project.destinationMode === 'current'
                    ? 'choice selected'
                    : 'choice'
                }
                onClick={() =>
                  dispatch(setProject({ destinationMode: 'current' }))
                }
              >
                <strong>当前运行目录（推荐）</strong>
                <small>直接在启动 alemonx 的文件夹里安装。</small>
              </button>
              <button
                className={
                  project.destinationMode === 'custom'
                    ? 'choice selected'
                    : 'choice'
                }
                onClick={chooseDestination}
              >
                <strong>选择指定文件夹</strong>
                <small>
                  {project.destinationMode === 'custom' && project.destination
                    ? `已选择：${project.destination}`
                    : '在目录选择器中选择保存位置。'}
                </small>
              </button>
            </div>
            {folderError && (
              <ErrorNotice
                message={folderError}
                onClose={() => setFolderError('')}
              />
            )}
          </div>
        </>
      )
    return (
      <>
        <h1>
          {creation?.status === 'ready' ? '机器人已安装' : '确认安装机器人'}
        </h1>
        {creation?.status === 'ready' ? (
          <>
            <p>机器人已安装至：{creation.path}</p>
            {creation.path && (
              <a
                className="primary-button"
                href={`/dashboard/robot?root=${encodeURIComponent(creation.path)}`}
              >
                前往管理机器人
              </a>
            )}
          </>
        ) : (
          <>
            <p>
              将使用默认 JavaScript 模板、Yarn、Git
              存档，不附加开发技能、图片工具或 PM2。
            </p>
            <div className="config-summary">
              <span>
                位置：
                {project.destinationMode === 'current'
                  ? `当前运行目录/${project.name}`
                  : project.destination
                    ? `${project.destination}/${project.name}`
                    : '请返回填写保存位置'}
              </span>
              <span>默认环境：JavaScript + Yarn</span>
              <span>项目存档：初始化 Git</span>
            </div>
          </>
        )}
        {creation?.logs && (
          <div className="creation-logs">
            {creation.logs.map((log, index) => (
              <p key={index}>{log}</p>
            ))}
          </div>
        )}
      </>
    )
  }
  const webPage = () => {
    if (flowStep === 0)
      return (
        <>
          <h1>选择部署方式</h1>
          <div className="grid gap-2.5 my-5">
            <button
              className={webEdition === 'clean' ? 'choice selected' : 'choice'}
              onClick={() => {
                setWebEdition('clean')
                automaticCheck.current = null
              }}
            >
              <strong>纯净版</strong>
              <small>检查 Node.js 与 Git 后启动 alx</small>
            </button>
            <button
              className={webEdition === 'docker' ? 'choice selected' : 'choice'}
              onClick={() => {
                setWebEdition('docker')
                automaticCheck.current = null
              }}
            >
              <strong>Docker 版</strong>
              <small>检查 Docker 后使用 Docker Compose 快速启动</small>
            </button>
          </div>
        </>
      )
    if (flowStep === 1)
      return (
        <EnvironmentCheckPanel
          title={webEdition === 'clean' ? '检查运行环境' : '检查 Docker'}
          report={report}
          checking={checking}
          onCheck={() => onCheck(webEdition ?? undefined)}
        />
      )
    if (webEdition === 'clean' && flowStep === 2)
      return (
        <>
          <h1>选择下载镜像</h1>
          <p>选择下载来源后继续；随后选择版本与安装包。</p>
          <div className="grid gap-2.5 my-5">
            {goal?.mirrors?.map(mirror => (
              <button
                className={
                  selectedMirror?.url === mirror.url
                    ? 'choice selected'
                    : 'choice'
                }
                key={mirror.url}
                onClick={() => {
                  setSelectedMirror(mirror)
                  setSelectedAssetURL(null)
                }}
              >
                <strong>{mirror.name}</strong>
                <small>
                  {mirror.name === 'GitHub 官方'
                    ? '从 GitHub 官方下载'
                    : '下载速度可能更快'}
                </small>
              </button>
            ))}
          </div>
        </>
      )
    if (webEdition === 'clean' && flowStep === 3)
      return (
        <>
          <h1>选择版本</h1>
          <p>默认已选择最新正式版本。确认后继续选择适合电脑的安装包。</p>
          {releasePicker()}
        </>
      )
    if (webEdition === 'clean' && flowStep === 4) return assetPicker()
    return (
      <>
        <h1>Docker Compose 快速启动</h1>
        <p>下一步会生成 docker-compose.yml 并启动服务。</p>
      </>
    )
  }
  const buildPage = () => {
    if (flowStep === 0)
      return (
        <>
          <h1>选择构建方式</h1>
          <div className="grid gap-2.5 my-5">
            <button
              className={buildMode === 'npm' ? 'choice selected' : 'choice'}
              onClick={() => {
                setBuildMode('npm')
                automaticCheck.current = null
              }}
            >
              <strong>NPM 构建</strong>
              <small>使用 npm 生成应用构建产物</small>
            </button>
            <button
              className={buildMode === 'git' ? 'choice selected' : 'choice'}
              onClick={() => {
                setBuildMode('git')
                automaticCheck.current = null
              }}
            >
              <strong>Git 化构建</strong>
              <small>遵循 main、release 分支与版本标签标准</small>
            </button>
          </div>
        </>
      )
    if (flowStep === 1)
      return (
        <EnvironmentCheckPanel
          title="检查构建环境"
          report={report}
          checking={checking}
          onCheck={() => onCheck(buildMode ?? undefined)}
        />
      )
    return (
      <>
        <h1>{buildMode === 'git' ? 'Git 化构建' : 'NPM 构建'}</h1>
        <p>
          {buildMode === 'git'
            ? '构建产物将按标准整理至 release 分支，并创建对应版本标签。'
            : '将构建应用并生成可分发产物。'}
        </p>
      </>
    )
  }
  const downloadPage = () => {
    if (goal?.id === 'mobile')
      return (
        <>
          <h1>下载 Android 安装包</h1>
          <p>手机版目前仅提供 Android 通用 APK，不经过 GitHub。</p>
          {goal.downloadUrl && (
            <a
              className="primary-button"
              href={goal.downloadUrl}
              target="_blank"
              rel="noreferrer"
            >
              下载 Android APK
            </a>
          )}
        </>
      )
    if (flowStep === 0)
      return (
        <>
          <h1>选择下载镜像</h1>
          <p>选择一个下载来源后继续；下一页再选要下载的版本。</p>
          <div className="grid gap-2.5 my-5">
            {goal?.mirrors?.map(mirror => (
              <button
                className={
                  selectedMirror?.url === mirror.url
                    ? 'choice selected'
                    : 'choice'
                }
                key={mirror.url}
                onClick={() => {
                  setSelectedMirror(mirror)
                  setSelectedAssetURL(null)
                }}
              >
                <strong>{mirror.name}</strong>
                <small>
                  {mirror.name === 'GitHub 官方'
                    ? '从 GitHub 官方下载'
                    : '下载速度可能更快'}
                </small>
              </button>
            ))}
          </div>
        </>
      )
    if (flowStep === 1)
      return (
        <>
          <h1>选择下载版本</h1>
          <p>默认已选择最新正式版本。确认后继续选择安装包。</p>
          {releasePicker()}
          <p className="selected-download">
            下载镜像：{selectedMirror?.name ?? 'GitHub 官方'}
          </p>
        </>
      )
    return assetPicker()
  }
  const selectPurpose = (id: string) => {
    automaticCheck.current = null
    setSelectedMirror(null)
    setSelectedAssetURL(null)
    onSelect(id)
  }
  const goBack = () => {
    if (step === 1) {
      onSelect(null)
      setStep(0)
      return
    }
    back()
  }
  const resetPurpose = () => {
    onSelect(null)
    setStep(0)
  }
  registerBack(step > 0 ? goBack : () => {})
  const createConfig = (): ProjectConfig =>
    isInstaller
      ? {
          template: 'bot',
          name: project.name,
          destinationMode: project.destinationMode,
          destination: project.destination,
          language: 'js',
          packageManager: 'yarn',
          eslint: false,
          initializeGit: true,
          usePM2: false,
          imageMode: 'none',
          styleMode: 'css',
          downloadSkills: false,
          developmentPackages: []
        }
      : {
          template: 'dev',
          name: project.name,
          destinationMode: project.destinationMode,
          destination: project.destination,
          language: config.language,
          packageManager: config.manager,
          eslint: config.eslint === 'yes',
          initializeGit: config.git === 'yes',
          usePM2: config.pm2 === 'yes',
          imageMode: config.image,
          styleMode: config.image === 'react' ? config.style : 'css',
          downloadSkills: config.skills === 'yes',
          developmentPackages: capabilities
        }
  const isDownloadFlow = goal?.id === 'desktop' || goal?.id === 'mobile'
  const isWeb = goal?.id === 'web'
  const isBuild = goal?.id === 'build'
  const purposeOptions = [
    ['develop', '开发', '创建一个可按需配置的 AlemonJS 开发项目。'],
    ['deploy', '部署', '部署源码版、桌面版、手机版或 Web 版。'],
    ['manage', '管理', '进入后台，管理已有机器人项目。']
  ]
  return (
    <section className="wizard">
      <aside className="wizard-steps">
        <p>{goal?.title ?? '开始'}</p>
        {totalSteps.map((label, index) => (
          <button
            type="button"
            ref={index === step ? currentStepElement : null}
            key={label}
            className={index < step ? 'done' : index === step ? 'current' : ''}
            disabled={index > step}
            aria-current={index === step ? 'step' : undefined}
            onClick={
              index <= step
                ? () => (index === 0 ? resetPurpose() : setFlowStep(index))
                : undefined
            }
          >
            <span>{index < step ? '✓' : index + 1}</span>
            {label}
          </button>
        ))}
      </aside>
      <section className="wizard-page">
        {goal && (
          <p className="wizard-progress" aria-live="polite">
            第 {step + 1} / {totalSteps.length} 步 · {totalSteps[step]}
          </p>
        )}
        <div className="wizard-content">
          {!goal || step === 0 ? (
            <div className="guide-question guide-choice-screen mx-auto max-w-140 text-center">
              <p className="guide-choice-eyebrow">
                ALemonX
              </p>
              <h1>你现在想做什么？</h1>
              <p className="guide-choice-description">
                选择一个目标；引导会只展示必要步骤，并在需要时检查你的环境。
              </p>
              <div className="question-options" role="list">
                {loading ? (
                  <p>正在准备选项…</p>
                ) : (
                  purposeOptions.map(([id, title, note]) => (
                    <button
                      type="button"
                      className="guide-choice-card"
                      key={id}
                      onClick={() =>
                        id === 'deploy'
                          ? navigate('/guide/group/deploy')
                          : selectPurpose(id)
                      }
                    >
                      <i>{icons[id] ?? '·'}</i>
                      <span>
                        <strong>{title}</strong>
                        <small>{note}</small>
                      </span>
                      <b aria-hidden="true">→</b>
                    </button>
                  ))
                )}
              </div>
            </div>
          ) : isInstaller ? (
            installerPage()
          ) : isDeveloper ? (
            developerPage()
          ) : isWeb ? (
            webPage()
          ) : isBuild ? (
            buildPage()
          ) : isDownloadFlow ? (
            downloadPage()
          ) : (
            <>
              <h1>{totalSteps[step]}</h1>
              <p>{goal.description}</p>
            </>
          )}{' '}
        </div>
        <footer className="wizard-actions">
          {isInstaller && flowStep === 0 && report?.ready && (
            <button className="next-button" onClick={next}>
              继续
            </button>
          )}
          {isInstaller && flowStep === 1 && (
            <button className="next-button" onClick={next}>
              继续
            </button>
          )}
          {isInstaller && flowStep === 2 && creation?.status !== 'ready' && (
            <button
              className="next-button"
              onClick={() => onCreate(createConfig())}
              disabled={creating}
            >
              {creating ? '正在安装…' : '确认安装'}
            </button>
          )}
          {isDeveloper && flowStep === 0 && report?.ready && (
            <button className="next-button" onClick={next}>
              继续
            </button>
          )}
          {isDeveloper && flowStep > 0 && flowStep < 11 && (
            <button className="next-button" onClick={next}>
              继续
            </button>
          )}
          {isDeveloper && flowStep === 11 && creation?.status !== 'ready' && (
            <button
              className="next-button"
              onClick={() => onCreate(createConfig())}
              disabled={creating}
            >
              {creating ? '正在创建…' : '确认创建'}
            </button>
          )}
          {isWeb && flowStep === 0 && webEdition && (
            <button className="next-button" onClick={next}>
              继续
            </button>
          )}
          {isWeb && flowStep === 1 && report?.ready && (
            <button className="next-button" onClick={next}>
              继续
            </button>
          )}
          {isWeb &&
            flowStep === 2 &&
            webEdition === 'clean' &&
            selectedMirror && (
              <button className="next-button" onClick={next}>
                继续
              </button>
            )}
          {isWeb && flowStep === 3 && webEdition === 'clean' && releaseURL && (
            <button className="next-button" onClick={next}>
              继续
            </button>
          )}
          {isWeb &&
            flowStep === 4 &&
            webEdition === 'clean' &&
            selectedAssetURL && (
              <a
                className="next-button"
                href={mirrorURL(selectedMirror, selectedAssetURL)}
                target="_blank"
                rel="noreferrer"
              >
                开始下载
              </a>
            )}
          {isWeb && flowStep === 2 && webEdition === 'docker' && (
            <button className="next-button" disabled>
              生成并启动
            </button>
          )}
          {isBuild && flowStep === 0 && buildMode && (
            <button className="next-button" onClick={next}>
              继续
            </button>
          )}
          {isBuild && flowStep === 1 && report?.ready && (
            <button className="next-button" onClick={next}>
              继续
            </button>
          )}
          {isBuild && flowStep === 2 && (
            <button className="next-button" disabled>
              开始构建
            </button>
          )}
          {!isInstaller &&
            !isDeveloper &&
            !isWeb &&
            !isBuild &&
            !isDownloadFlow &&
            goal && (
              <button className="next-button" onClick={next}>
                继续
              </button>
            )}
          {isDownloadFlow && flowStep === 0 && selectedMirror && (
            <button className="next-button" onClick={next}>
              继续
            </button>
          )}
          {isDownloadFlow && flowStep === 1 && releaseURL && (
            <button className="next-button" onClick={next}>
              继续
            </button>
          )}
          {isDownloadFlow && flowStep === 2 && selectedAssetURL && (
            <a
              className="next-button"
              href={mirrorURL(selectedMirror, selectedAssetURL)}
              target="_blank"
              rel="noreferrer"
            >
              开始下载
            </a>
          )}
        </footer>
      </section>
    </section>
  )
}

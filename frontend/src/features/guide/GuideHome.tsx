import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useDispatch } from 'react-redux'
import { useLocation, useNavigate } from 'react-router-dom'
import { ArrowRight, Code2 } from 'lucide-react'
import { setProject } from '../../store/guideStore'
import { DirectoryPicker } from '../../components/Dashboard'
import { ErrorNotice } from '../../components/ErrorNotice'
import { GuideHeader } from '../../components/GuideHeader'
import type { Check, Creation, Goal, ProjectConfig, Report } from './types'
import { guideIcons } from './icons'

type Props = {
  loading: boolean
  group?: string
  goal?: Goal
  report: Report | null
  checking: boolean
  error: string
  creating: boolean
  creation: Creation | null
  onSelect: (id: string | null) => void
  onClose: () => void
  onClearError: () => void
  onCheck: (variant?: string) => void
  onCreate: (config: ProjectConfig) => void
  onFix: (check: Check) => void
  renderFlow: (registerBack: (handler: () => void) => void) => ReactNode
}

export function GuideHome({
  group,
  goal,
  report,
  error,
  onSelect,
  onClose,
  onClearError,
  onFix,
  renderFlow
}: Props) {
  const backAction = useRef<() => void>(() => {})
  const dispatch = useDispatch()
  const [directoryPickerOpen, setDirectoryPickerOpen] = useState(false)
  const location = useLocation()
  const navigate = useNavigate()
  const currentStep = Number(location.pathname.match(/\/step\/(\d+)/)?.[1] ?? 0)
  const missingChecks =
    report?.checks.filter(
      check =>
        check.status !== 'ready' && ['node', 'git', 'docker'].includes(check.id)
    ) ?? []
  const isCheckStep =
    goal?.id === 'install' || goal?.id === 'develop'
      ? currentStep === 1
      : goal?.id === 'web' && currentStep === 2

  useEffect(() => {
    const open = () => setDirectoryPickerOpen(true)
    window.addEventListener('alemon:choose-directory', open)
    return () => window.removeEventListener('alemon:choose-directory', open)
  }, [])

  return (
    <main className="guide-shell">
      <section className="guide-window">
        <GuideHeader
          onBack={() => (group ? navigate('/guide') : backAction.current())}
          onClose={onClose}
          showBack={Boolean(goal || group)}
        />
        {error && <ErrorNotice message={error} onClose={onClearError} />}
        {group ? (
          <PurposeGroup group={group} onSelect={onSelect} />
        ) : (
          renderFlow(handler => {
            backAction.current = handler
          })
        )}
        {isCheckStep && missingChecks.length > 0 && (
          <div className="environment-repair">
            {missingChecks.map(check => (
              <button key={check.id} onClick={() => onFix(check)}>
                安装 / 下载 {check.name}
              </button>
            ))}
          </div>
        )}
        <DirectoryPicker
          open={directoryPickerOpen}
          multiple={false}
          onClose={() => setDirectoryPickerOpen(false)}
          onSelect={paths => {
            dispatch(
              setProject({ destinationMode: 'custom', destination: paths[0] })
            )
            setDirectoryPickerOpen(false)
          }}
        />
      </section>
    </main>
  )
}

function PurposeGroup({
  group,
  onSelect
}: {
  group: string
  onSelect: (id: string) => void
}) {
  const options =
    group === 'deploy'
      ? [
          [
            'install',
            '源码版',
            '下载并安装一个可运行的 AlemonJS 机器人源码项目。'
          ],
          ['desktop', '桌面版', '下载适合当前电脑的 AlemonDesk 安装包。'],
          ['mobile', '手机版', '下载 Android 通用 APK 安装包。'],
          ['web', 'Web 版', '下载 alx，或使用 Docker 部署。']
        ]
      : [
          [
            'develop',
            '开发机器人',
            '创建一个可自由选择语言、依赖和工具的开发项目。'
          ]
        ]

  return (
    <section className="wizard purpose-group">
      <section className="wizard-page">
        <div className="wizard-content">
          <div className="guide-question mx-auto max-w-140 text-center">
            <p className="text-(--theme-accent-text) text-xs font-extrabold uppercase tracking-[0.13em] mb-3.5">
              部署向导
            </p>
            <h1>
              {group === 'deploy' ? '你要部署哪一种版本？' : '开始开发机器人'}
            </h1>
            <p className="text-(--theme-text-muted) text-[0.95rem] leading-[1.65] mt-3.5">
              选择一种方式，接下来只会展示与它有关的步骤。
            </p>
            <div className="question-options">
              {options.map(([id, title, note]) => (
                <button key={id} onClick={() => onSelect(id)}>
                  <i>{guideIcons[id] ?? <Code2 />}</i>
                  <span>
                    <strong>{title}</strong>
                    <small>{note}</small>
                  </span>
                  <b>
                    <ArrowRight />
                  </b>
                </button>
              ))}
            </div>
          </div>
        </div>
      </section>
    </section>
  )
}

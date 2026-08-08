import { useStoreState } from '../store/guideStore'
import { useEffect } from 'react'
import { useAutoSave } from '../hooks/useAutoSave'
import {
  usePackageManifestQuery,
  useWritePackageManifestMutation
} from '../store/workspaceApi'
import { BotWorkspace } from './BotWorkspace'

type Manifest = {
  name: string
  version: string
  description: string
  homepage: string
  repository: string
  license: string
  private: boolean
  access: string
}
const blank: Manifest = {
  name: '',
  version: '',
  description: '',
  homepage: '',
  repository: '',
  license: '',
  private: false,
  access: 'public'
}

function sameManifest(left: Manifest, right: Manifest) {
  return (
    left.name === right.name &&
    left.version === right.version &&
    left.description === right.description &&
    left.homepage === right.homepage &&
    left.repository === right.repository &&
    left.license === right.license &&
    left.private === right.private &&
    left.access === right.access
  )
}

function saveErrorMessage(reason: unknown) {
  if (reason instanceof Error && reason.message) return reason.message
  if (reason && typeof reason === 'object') {
    const value = reason as {
      data?: { error?: unknown; message?: unknown }
      error?: unknown
      message?: unknown
    }
    if (typeof value.data?.error === 'string') return value.data.error
    if (typeof value.data?.message === 'string') return value.data.message
    if (typeof value.error === 'string') return value.error
    if (typeof value.message === 'string') return value.message
  }
  return '发布信息未保存，请检查目录权限。'
}

export function PackageManifestPanel({
  root,
  onSaveError
}: {
  root: string
  onSaveError: (message: string) => void
}) {
  const { data, isLoading: isInitialLoading, error } = usePackageManifestQuery(
    root,
    { skip: !root }
  )
  const [save, { isLoading }] = useWritePackageManifestMutation()
  const [values, setValues] = useStoreState<Manifest>(blank)
  useEffect(() => {
    if (!data) return
    const next = { ...blank, ...data, access: data.access || 'public' }
    setValues(current => (sameManifest(current, next) ? current : next))
  }, [data])
  const saveManifest = async (next: Manifest) => {
    try {
      const result = await save({
        root,
        ...next,
        access: next.private ? next.access : ''
      }).unwrap()
      return result
    } catch (reason) {
      onSaveError(saveErrorMessage(reason))
    }
  }
  const scheduleSave = useAutoSave(saveManifest)
  const set = <K extends keyof Manifest>(key: K, value: Manifest[K]) => {
    const next = { ...values, [key]: value }
    setValues(next)
    scheduleSave(next)
  }
  const fieldClass =
    'min-h-9 w-full rounded-md border border-slate-300 bg-white px-2.5 text-sm font-normal text-slate-800 outline-none transition focus:border-brand-600 focus:ring-2 focus:ring-brand-100'
  if (isInitialLoading)
    return (
      <section className="grid max-w-180 rounded-xl border border-slate-200 bg-white p-4 text-xs text-slate-500">
        <p>正在读取 package.json…</p>
      </section>
    )
  if (error)
    return (
      <section className="grid max-w-180 rounded-xl border border-slate-200 bg-white p-4 text-xs text-slate-500">
        <p>无法读取 package.json。</p>
      </section>
    )
  return (
    <BotWorkspace
      className="package-manifest-panel max-w-180"
      header={<header className="bot-page-header flex items-center justify-between gap-4">
        <div className="bot-page-header-meta">
          <strong className="text-sm text-ink-950">包信息</strong>
          <small>
            Git 与 npm 发布共用 ·{' '}
            {isLoading ? '正在自动保存…' : '修改后自动保存'}
          </small>
        </div>
      </header>}
    >
      <div className="grid grid-cols-2 gap-3 rounded-xl border border-slate-200 bg-white p-4 shadow-[0_7px_18px_rgb(28_26_23/0.035)]">
        <label className="grid gap-1 text-xs font-semibold text-slate-600">
          包名
          <input
            className={fieldClass}
            value={values.name}
            onChange={event => set('name', event.target.value)}
            placeholder="@scope/package-name"
          />
        </label>
        <label className="grid gap-1 text-xs font-semibold text-slate-600">
          版本
          <input
            className={fieldClass}
            value={values.version}
            onChange={event => set('version', event.target.value)}
            placeholder="1.2.3"
          />
        </label>
        <label className="col-span-2 grid gap-1 text-xs font-semibold text-slate-600">
          描述
          <input
            className={fieldClass}
            value={values.description}
            onChange={event => set('description', event.target.value)}
            placeholder="一句话说明这个包的用途"
          />
        </label>
        <label className="grid gap-1 text-xs font-semibold text-slate-600">
          主页
          <input
            className={fieldClass}
            value={values.homepage}
            onChange={event => set('homepage', event.target.value)}
            placeholder="https://example.com"
          />
        </label>
        <label className="grid gap-1 text-xs font-semibold text-slate-600">
          仓库
          <input
            className={fieldClass}
            value={values.repository}
            onChange={event => set('repository', event.target.value)}
            placeholder="https://github.com/owner/repo"
          />
        </label>
        <label className="grid gap-1 text-xs font-semibold text-slate-600">
          许可证
          <input
            className={fieldClass}
            value={values.license}
            onChange={event => set('license', event.target.value)}
            placeholder="MIT"
          />
        </label>
        <label className="grid gap-1 text-xs font-semibold text-slate-600">
          发布权限
          <select
            className={fieldClass}
            value={values.access}
            disabled={values.private}
            onChange={event => set('access', event.target.value)}
          >
            <option value="public">公开（public）</option>
            <option value="restricted">受限（restricted）</option>
          </select>
        </label>
        <label className="col-span-2 flex items-center gap-2 text-xs font-semibold text-slate-500">
          <input
            className="size-4"
            type="checkbox"
            checked={values.private}
            onChange={event => set('private', event.target.checked)}
          />
          仅本地使用，不发布到 npm
        </label>
      </div>
    </BotWorkspace>
  )
}

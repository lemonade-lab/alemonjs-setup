import { useEffect, useState } from 'react'
import {
  usePackageManifestQuery,
  useWritePackageManifestMutation
} from '../store/workspaceApi'

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
  busy,
  onSaved
}: {
  root: string
  busy: boolean
  onSaved: (message: string) => void
}) {
  const { data, isFetching, error } = usePackageManifestQuery(root, {
    skip: !root
  })
  const [save, { isLoading }] = useWritePackageManifestMutation()
  const [values, setValues] = useState<Manifest>(blank)
  useEffect(() => {
    if (data) setValues({ ...blank, ...data, access: data.access || 'public' })
  }, [data])
  const set = <K extends keyof Manifest>(key: K, value: Manifest[K]) =>
    setValues(current => ({ ...current, [key]: value }))
  const submit = async () => {
    try {
      const result = await save({
        root,
        ...values,
        access: values.private ? values.access : ''
      }).unwrap()
      onSaved(result.output || '发布信息已保存。')
    } catch (reason) {
      onSaved(saveErrorMessage(reason))
    }
  }
  const fieldClass =
    'min-h-9 w-full rounded-md border border-slate-300 bg-white px-2.5 text-sm font-normal text-slate-800 outline-none transition focus:border-brand-600 focus:ring-2 focus:ring-brand-100'
  if (isFetching)
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
    <section className="grid max-w-180 gap-4 rounded-xl border border-slate-200 bg-white p-4 shadow-[0_7px_18px_rgb(28_26_23/0.035)]">
      <header className="flex items-start justify-between gap-4 border-b border-slate-100 pb-3">
        <div className="grid gap-0.5">
          <strong className="text-sm text-ink-950">包信息</strong>
          <span className="text-xs text-slate-500">Git 与 npm 发布共用。</span>
        </div>
        <button
          className="inline-flex min-h-9 items-center justify-center rounded-md bg-brand-600 px-3.5 text-xs font-semibold text-white hover:bg-brand-700 disabled:cursor-not-allowed disabled:opacity-50"
          disabled={busy || isLoading}
          onClick={() => void submit()}
        >
          {isLoading ? '保存中' : '保存'}
        </button>
      </header>
      <div className="grid grid-cols-2 gap-3">
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
    </section>
  )
}

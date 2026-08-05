import { useEffect, useState } from 'react'
import { usePackageManifestQuery, useWritePackageManifestMutation } from '../store/workspaceApi'

type Manifest = { name: string; version: string; description: string; homepage: string; repository: string; license: string; private: boolean; access: string }
const blank: Manifest = { name: '', version: '', description: '', homepage: '', repository: '', license: '', private: false, access: 'public' }

function saveErrorMessage(reason: unknown) {
  if (reason instanceof Error && reason.message) return reason.message
  if (reason && typeof reason === 'object') {
    const value = reason as { data?: { error?: unknown; message?: unknown }; error?: unknown; message?: unknown }
    if (typeof value.data?.error === 'string') return value.data.error
    if (typeof value.data?.message === 'string') return value.data.message
    if (typeof value.error === 'string') return value.error
    if (typeof value.message === 'string') return value.message
  }
  return '发布信息未保存，请检查目录权限。'
}

export function PackageManifestPanel({ root, busy, onSaved }: { root: string; busy: boolean; onSaved: (message: string) => void }) {
  const { data, isFetching, error } = usePackageManifestQuery(root, { skip: !root })
  const [save, { isLoading }] = useWritePackageManifestMutation()
  const [values, setValues] = useState<Manifest>(blank)
  useEffect(() => { if (data) setValues({ ...blank, ...data, access: data.access || 'public' }) }, [data])
  const set = <K extends keyof Manifest>(key: K, value: Manifest[K]) => setValues((current) => ({ ...current, [key]: value }))
  const submit = async () => { try { const result = await save({ root, ...values, access: values.private ? values.access : '' }).unwrap(); onSaved(result.output || '发布信息已保存。') } catch (reason) { onSaved(saveErrorMessage(reason)) } }
  if (isFetching) return <section className="manifest-panel"><p>正在读取 package.json…</p></section>
  if (error) return <section className="manifest-panel"><p>无法读取 package.json。</p></section>
  return <section className="manifest-panel"><header><div><p>发布信息</p><strong>package.json</strong><span>Git 打包与 npm 发布共用这些字段。</span></div><button className="primary-button" disabled={busy || isLoading} onClick={() => void submit()}>{isLoading ? '保存中…' : '保存'}</button></header><div className="manifest-fields"><label>包名<input value={values.name} onChange={(event) => set('name', event.target.value)} placeholder="@scope/package-name" /></label><label>版本<input value={values.version} onChange={(event) => set('version', event.target.value)} placeholder="1.2.3" /></label><label className="wide">描述<input value={values.description} onChange={(event) => set('description', event.target.value)} placeholder="一句话说明这个包的用途" /></label><label>主页<input value={values.homepage} onChange={(event) => set('homepage', event.target.value)} placeholder="https://example.com" /></label><label>仓库<input value={values.repository} onChange={(event) => set('repository', event.target.value)} placeholder="https://github.com/owner/repo" /></label><label>许可证<input value={values.license} onChange={(event) => set('license', event.target.value)} placeholder="MIT" /></label><label>发布权限<select value={values.access} disabled={values.private} onChange={(event) => set('access', event.target.value)}><option value="public">公开（public）</option><option value="restricted">受限（restricted）</option></select></label><label className="manifest-private"><input type="checkbox" checked={values.private} onChange={(event) => set('private', event.target.checked)} />仅本地使用，不发布到 npm</label></div></section>
}

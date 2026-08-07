import { useEffect, useState } from 'react'
import { Tabs } from './Tabs'

type Props = {
  content: string
  busy: boolean
  onChange: (content: string) => void
  onSave: (content: string) => void
}

const officialRegistry = 'https://registry.npmjs.org/'
const mirrorRegistry = 'https://registry.npmmirror.com/'

function registryFrom(content: string) {
  return content.match(/^\s*registry\s*=\s*(.+?)\s*$/m)?.[1] ?? officialRegistry
}

function withRegistry(content: string, registry: string) {
  const lines = content
    .split(/\r?\n/)
    .filter(line => !/^\s*registry\s*=/.test(line) && line.trim())
  return [...lines, `registry=${registry}`].join('\n') + '\n'
}

export function NpmrcConfigForm({ content, busy, onChange, onSave }: Props) {
  const [editor, setEditor] = useState<'visual' | 'text'>('visual')
  const [preset, setPreset] = useState(officialRegistry)
  const [customRegistry, setCustomRegistry] = useState('')

  useEffect(() => {
    const registry = registryFrom(content)
    if (registry === officialRegistry || registry === mirrorRegistry) {
      setPreset(registry)
    } else {
      setPreset('custom')
      setCustomRegistry(registry)
    }
  }, [content])

  const saveVisual = () => {
    const registry = (preset === 'custom' ? customRegistry : preset).trim()
    if (!registry) return
    onSave(withRegistry(content, registry))
  }

  const mode = (
    <Tabs
      ariaLabel="编辑模式"
      value={editor}
      onChange={setEditor}
      variant="segmented"
      items={[
        { id: 'visual', label: '表单' },
        { id: 'text', label: '文本' }
      ]}
    />
  )
  const fieldClass =
    'min-h-9 w-full rounded-md border border-slate-300 bg-white px-2.5 text-sm font-normal text-slate-800 outline-none transition focus:border-brand-600 focus:ring-2 focus:ring-brand-100'
  const saveClass =
    'inline-flex min-h-9 items-center justify-center rounded-md bg-brand-600 px-3.5 text-xs font-semibold text-white transition hover:bg-brand-700 disabled:cursor-not-allowed disabled:opacity-50'
  return (
    <section className="grid max-w-[620px] gap-4">
      {editor === 'visual' ? (
        <>
          <header className="flex items-center justify-between">
            {mode}
            <button
              className={saveClass}
              disabled={busy || (preset === 'custom' && !customRegistry.trim())}
              onClick={saveVisual}
            >
              保存
            </button>
          </header>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <label className="grid gap-1 text-xs font-semibold text-slate-600">
              Registry
              <select
                className={fieldClass}
                value={preset}
                onChange={event => setPreset(event.target.value)}
              >
                <option value={officialRegistry}>npm 官方源</option>
                <option value={mirrorRegistry}>npmmirror 镜像</option>
                <option value="custom">自定义地址</option>
              </select>
            </label>
            {preset === 'custom' && (
              <label className="grid gap-1 text-xs font-semibold text-slate-600">
                自定义地址
                <input
                  className={fieldClass}
                  value={customRegistry}
                  onChange={event => setCustomRegistry(event.target.value)}
                  placeholder="https://registry.example.com/"
                />
              </label>
            )}
          </div>
        </>
      ) : (
        <section className="grid overflow-hidden rounded-xl border border-slate-200 bg-white">
          <header className="flex items-center justify-between border-b border-slate-100 px-3 py-2.5">
            {mode}
            <button
              className={saveClass}
              disabled={busy}
              onClick={() => onSave(content)}
            >
              保存
            </button>
          </header>
          <textarea
            className="min-h-72 w-full resize-y border-0 p-3 font-mono text-sm text-slate-700 outline-none"
            value={content}
            onChange={event => onChange(event.target.value)}
            placeholder="npm 配置"
          />
        </section>
      )}
    </section>
  )
}

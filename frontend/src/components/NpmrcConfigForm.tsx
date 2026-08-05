import { useEffect, useState } from 'react'

type Props = { content: string; busy: boolean; onChange: (content: string) => void; onSave: (content: string) => void }

const officialRegistry = 'https://registry.npmjs.org/'
const mirrorRegistry = 'https://registry.npmmirror.com/'

function registryFrom(content: string) {
  return content.match(/^\s*registry\s*=\s*(.+?)\s*$/m)?.[1] ?? officialRegistry
}

function withRegistry(content: string, registry: string) {
  const lines = content.split(/\r?\n/).filter((line) => !/^\s*registry\s*=/.test(line) && line.trim())
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

  const mode = <div className="editor-mode" aria-label="编辑模式"><button className={editor === 'visual' ? 'active' : ''} onClick={() => setEditor('visual')}>表单</button><button className={editor === 'text' ? 'active' : ''} onClick={() => setEditor('text')}>文本</button></div>
  return <section className="config-form npmrc-config-form">{editor === 'visual' ? <><header className="config-form-header">{mode}<button className="primary-button" disabled={busy || (preset === 'custom' && !customRegistry.trim())} onClick={saveVisual}>保存</button></header><div className="form-grid"><label>Registry<select value={preset} onChange={(event) => setPreset(event.target.value)}><option value={officialRegistry}>npm 官方源</option><option value={mirrorRegistry}>npmmirror 镜像</option><option value="custom">自定义地址</option></select></label>{preset === 'custom' && <label>自定义地址<input value={customRegistry} onChange={(event) => setCustomRegistry(event.target.value)} placeholder="https://registry.example.com/" /></label>}</div></> : <section className="file-editor"><header>{mode}<button className="primary-button" disabled={busy} onClick={() => onSave(content)}>保存</button></header><textarea value={content} onChange={(event) => onChange(event.target.value)} placeholder="npm 配置" /></section>}</section>
}

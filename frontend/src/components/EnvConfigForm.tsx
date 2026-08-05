import { useEffect, useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'

type Entry = { key: string; value: string }
type Props = { content: string; busy: boolean; onChange: (content: string) => void; onSave: (content: string) => void }

function parse(content: string): Entry[] {
  return content.replace(/\r/g, '').split('\n').flatMap((line) => {
    const match = line.match(/^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)\s*$/)
    return match ? [{ key: match[1], value: match[2] }] : []
  })
}

function serialize(entries: Entry[]) {
  return entries.filter((item) => item.key.trim()).map((item) => `${item.key.trim()}=${item.value}`).join('\n') + (entries.some((item) => item.key.trim()) ? '\n' : '')
}

export function EnvConfigForm({ content, busy, onChange, onSave }: Props) {
  const [mode, setMode] = useState<'visual' | 'text'>('visual')
  const [entries, setEntries] = useState<Entry[]>([])
  useEffect(() => setEntries(parse(content)), [content])
  const update = (index: number, field: keyof Entry, value: string) => setEntries((current) => current.map((item, position) => position === index ? { ...item, [field]: value } : item))
  const editor = <div className="editor-mode" aria-label=".env 编辑模式"><button className={mode === 'visual' ? 'active' : ''} onClick={() => setMode('visual')}>表单</button><button className={mode === 'text' ? 'active' : ''} onClick={() => setMode('text')}>文本</button></div>
  if (mode === 'text') return <section className="file-editor"><header>{editor}<button className="primary-button" disabled={busy} onClick={() => onSave(content)}>保存</button></header><textarea value={content} onChange={(event) => onChange(event.target.value)} placeholder={'BOT_TOKEN=\nPORT=17117'} /></section>
  return <section className="env-config-form"><header className="config-form-header"><div>{editor}<small>环境变量常用于密钥、端口和第三方服务地址；请勿截图或公开提交。</small></div><button className="primary-button" disabled={busy} onClick={() => onSave(serialize(entries))}>保存</button></header><div className="env-entries">{entries.map((entry, index) => <div className="env-entry" key={`${index}-${entry.key}`}><input value={entry.key} onChange={(event) => update(index, 'key', event.target.value)} placeholder="变量名，例如 BOT_TOKEN" /><span>=</span><input value={entry.value} onChange={(event) => update(index, 'value', event.target.value)} placeholder="变量值" type="text" autoComplete="off" /><button className="icon-button" onClick={() => setEntries((current) => current.filter((_, position) => position !== index))} aria-label="移除此变量"><Trash2 /></button></div>)}</div><button className="text-button env-add" onClick={() => setEntries((current) => [...current, { key: '', value: '' }])}><Plus /> 添加环境变量</button></section>
}

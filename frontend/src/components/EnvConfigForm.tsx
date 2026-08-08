import { useStoreState } from '../store/guideStore'
import { useEffect } from 'react'
import { KeyRound, Plus, Trash2 } from 'lucide-react'
import { Tabs } from './Tabs'
import { BotWorkspace } from './BotWorkspace'

type Entry = { key: string; value: string }
type Props = {
  content: string
  onChange: (content: string) => void
}

function parse(content: string): Entry[] {
  return content
    .replace(/\r/g, '')
    .split('\n')
    .flatMap(line => {
      const match = line.match(
        /^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)\s*$/
      )
      return match ? [{ key: match[1], value: match[2] }] : []
    })
}

function serialize(entries: Entry[]) {
  return (
    entries
      .filter(item => item.key.trim())
      .map(item => `${item.key.trim()}=${item.value}`)
      .join('\n') + (entries.some(item => item.key.trim()) ? '\n' : '')
  )
}

function sameEntries(left: Entry[], right: Entry[]) {
  return (
    left.length === right.length &&
    left.every(
      (entry, index) =>
        entry.key === right[index]?.key && entry.value === right[index]?.value
    )
  )
}

export function EnvConfigForm({ content, onChange }: Props) {
  const [mode, setMode] = useStoreState<'visual' | 'text'>('visual')
  const [entries, setEntries] = useStoreState<Entry[]>([])
  useEffect(() => {
    const next = parse(content)
    setEntries(current => (sameEntries(current, next) ? current : next))
  }, [content])
  const update = (index: number, field: keyof Entry, value: string) => {
    const next = entries.map((item, position) =>
      position === index ? { ...item, [field]: value } : item
    )
    setEntries(next)
    onChange(serialize(next))
  }
  const editor = (
    <Tabs
      ariaLabel=".env 编辑模式"
      value={mode}
      onChange={setMode}
      variant="segmented"
      items={[
        { id: 'visual', label: '表单' },
        { id: 'text', label: '文本' }
      ]}
    />
  )
  const inputClass =
    'min-h-9 min-w-0 rounded-md border border-slate-300 bg-white px-2.5 font-mono text-sm text-slate-700 outline-none transition focus:border-brand-600 focus:ring-2 focus:ring-brand-100'
  if (mode === 'text')
    return (
      <section className="grid overflow-hidden rounded-xl border border-slate-200 bg-white">
        <header className="flex items-center justify-between border-b border-slate-100 px-3 py-2.5">
          {editor}
          <small className="text-xs text-slate-400">修改后自动保存</small>
        </header>
        <textarea
          className="min-h-72 w-full resize-y border-0 p-3 font-mono text-sm text-slate-700 outline-none"
          value={content}
          onChange={event => onChange(event.target.value)}
          placeholder={'BOT_TOKEN=\nPORT=17117'}
        />
      </section>
    )
  return (
    <BotWorkspace
      className="max-w-190"
      header={
        <header className="bot-page-header flex items-start justify-between gap-4">
          <div className="flex min-w-0 flex-1 items-center gap-3">
            <span className="bot-page-header-icon">
              <KeyRound className="size-4" />
            </span>
            <div className="flex min-w-0 flex-1 items-center gap-3">
              <div className="bot-page-header-meta">
                <strong>环境变量</strong>
                <small>管理密钥、端口和第三方服务地址 · 修改后自动保存</small>
              </div>
            </div>
              {editor}
          </div>
        </header>
      }
    >
      <div className="grid gap-2">
        {entries.map((entry, index) => (
          <div
            className="grid grid-cols-1 items-center gap-2 sm:grid-cols-[minmax(150px,.85fr)_auto_minmax(180px,1.4fr)_auto]"
            key={`${index}-${entry.key}`}
          >
            <input
              className={inputClass}
              value={entry.key}
              onChange={event => update(index, 'key', event.target.value)}
              placeholder="变量名，例如 BOT_TOKEN"
            />
            <span className="hidden justify-self-center font-mono text-slate-400 sm:inline">
              =
            </span>
            <input
              className={inputClass}
              value={entry.value}
              onChange={event => update(index, 'value', event.target.value)}
              placeholder="变量值"
              type="text"
              autoComplete="off"
            />
            <button
              className="inline-flex size-8 items-center justify-center justify-self-end rounded-md border border-slate-300 bg-white text-slate-400 hover:bg-slate-50 hover:text-red-700 sm:justify-self-auto"
              onClick={() => {
                const next = entries.filter((_, position) => position !== index)
                setEntries(next)
                onChange(serialize(next))
              }}
              aria-label="移除此变量"
            >
              <Trash2 className="size-4" />
            </button>
          </div>
        ))}
      </div>
      <button
        className="inline-flex min-h-8 items-center gap-1.5 justify-self-start rounded-md px-2.5 text-xs font-semibold text-slate-500 hover:bg-slate-50 hover:text-brand-600"
        onClick={() =>
          setEntries(current => [...current, { key: '', value: '' }])
        }
      >
        <Plus className="size-4" />
        添加环境变量
      </button>
    </BotWorkspace>
  )
}

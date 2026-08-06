import {
  ArrowUp,
  Bot,
  Mic,
  Paperclip,
  Send,
  Settings2,
  SlidersHorizontal,
  X
} from 'lucide-react'
import cn from 'classnames'
import { useCallback, useEffect, useRef, useState } from 'react'
import { Tabs } from './Tabs'

type Provider = {
  id: string
  name: string
  baseURL: string
  model: string
  hasKey: boolean
}
type Message = { role: 'user' | 'assistant'; content: string }

// The embedded page is intentionally only the conversation surface. Provider
// settings are kept out of this first interaction so opening AI never becomes
// a configuration flow.
export function AIChatPage({ root }: { root: string }) {
  const [prompt, setPrompt] = useState('')
  const [messages, setMessages] = useState<Message[]>([])
  const [attachments, setAttachments] = useState<File[]>([])
  const [access, setAccess] = useState<'ask' | 'read' | 'full'>('ask')
  const [accessOpen, setAccessOpen] = useState(false)
  const [notice, setNotice] = useState('')
  const fileInput = useRef<HTMLInputElement>(null)
  const [settings, setSettings] = useState(false)
  const [providers, setProviders] = useState<Provider[]>([])
  const [provider, setProvider] = useState('openai')
  const [baseURL, setBaseURL] = useState('')
  const [apiKey, setAPIKey] = useState('')
  const [models, setModels] = useState<string[]>([])
  const [model, setModel] = useState('')
  const current = providers.find(item => item.id === provider)
  const loadProviders = useCallback(async () => {
    const response = await fetch('/api/v1/ai/providers')
    const data = (await response.json()) as Provider[]
    setProviders(data)
    const item = data.find(value => value.id === provider) ?? data[0]
    if (item) {
      setProvider(item.id)
      setBaseURL(item.baseURL)
      setModel(item.model)
    }
  }, [provider])
  const loadModels = async (id: string) => {
    const response = await fetch(
      `/api/v1/ai/models?provider=${encodeURIComponent(id)}`
    )
    const data = (await response.json()) as {
      models?: string[]
      error?: string
    }
    if (!response.ok) {
      setNotice(data.error || '无法读取模型列表。')
      return
    }
    setModels(data.models ?? [])
    setModel(value =>
      data.models?.includes(value) ? value : (data.models?.[0] ?? value)
    )
  }
  useEffect(() => {
    void loadProviders()
  }, [loadProviders])
  useEffect(() => {
    if (current?.hasKey) void loadModels(provider)
  }, [current?.hasKey, provider])
  const accessLabel = { ask: '操作前询问', read: '仅查看', full: '完全访问' }[
    access
  ]
  const attach = (files: FileList | null) => {
    const next = Array.from(files ?? [])
      .filter(file => file.size <= 5 * 1024 * 1024)
      .slice(0, 3)
    if (!next.length) {
      setNotice('请选择不超过 5 MB 的文件。')
      return
    }
    setAttachments(value => [...value, ...next].slice(0, 3))
    setNotice(`已附加 ${next.length} 个文件。`)
  }
  const submit = async () => {
    if (!prompt.trim()) return
    if (!current?.hasKey) {
      setSettings(true)
      setNotice('请先配置 AI 接口与 API Key。')
      return
    }
    const next = [
      ...messages,
      {
        role: 'user' as const,
        content:
          prompt.trim() +
          (attachments.length
            ? `\n\n附件：${attachments.map(file => file.name).join('、')}`
            : '')
      }
    ]
    setMessages(next)
    setPrompt('')
    setAttachments([])
    setNotice('正在生成回复…')
    try {
      const response = await fetch('/api/v1/ai/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ provider, model, messages: next })
      })
      const data = (await response.json()) as {
        answer?: string
        error?: string
      }
      if (!response.ok) throw new Error(data.error)
      setMessages(value => [
        ...value,
        { role: 'assistant', content: data.answer || '没有返回内容。' }
      ])
      setNotice('')
    } catch (reason) {
      setNotice(reason instanceof Error ? reason.message : 'AI 请求失败。')
    }
  }
  const voice = () => {
    const Recognition =
      (
        window as Window & {
          SpeechRecognition?: new () => {
            lang: string
            onresult:
              | ((event: {
                  results: ArrayLike<ArrayLike<{ transcript: string }>>
                }) => void)
              | null
            onerror: (() => void) | null
            start: () => void
          }
          webkitSpeechRecognition?: new () => {
            lang: string
            onresult:
              | ((event: {
                  results: ArrayLike<ArrayLike<{ transcript: string }>>
                }) => void)
              | null
            onerror: (() => void) | null
            start: () => void
          }
        }
      ).SpeechRecognition ??
      (
        window as Window & {
          webkitSpeechRecognition?: new () => {
            lang: string
            onresult:
              | ((event: {
                  results: ArrayLike<ArrayLike<{ transcript: string }>>
                }) => void)
              | null
            onerror: (() => void) | null
            start: () => void
          }
        }
      ).webkitSpeechRecognition
    if (!Recognition) {
      setNotice('当前浏览器不支持语音输入。')
      return
    }
    const recognition = new Recognition()
    recognition.lang = 'zh-CN'
    recognition.onresult = event =>
      setPrompt(
        value => `${value}${value ? ' ' : ''}${event.results[0][0].transcript}`
      )
    recognition.onerror = () => setNotice('语音识别未完成，请检查麦克风权限。')
    recognition.start()
    setNotice('正在听…')
  }
  const save = async () => {
    const response = await fetch('/api/v1/ai/providers', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        provider,
        baseURL,
        model: current?.model || model,
        apiKey
      })
    })
    const data = (await response.json()) as { error?: string }
    if (!response.ok) {
      setNotice(data.error || '保存失败。')
      return
    }
    setAPIKey('')
    await loadProviders()
    await loadModels(provider)
    setSettings(false)
    setNotice('AI 接口已保存。')
  }
  return (
    <section className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] bg-white">
      <header className="flex min-h-14 items-center justify-between border-b border-slate-200 px-5">
        <div className="flex min-w-0 items-center gap-2.5">
          <Bot className="size-5 shrink-0 text-brand-600" />
          <small className="truncate text-xs text-slate-500">{root}</small>
        </div>
        <button
          className="icon-button size-8 p-0"
          onClick={() => setSettings(true)}
          title="AI 设置"
        >
          <Settings2 className="size-4" />
        </button>
      </header>
      {settings ? (
        <section className="mx-auto grid w-full max-w-[520px] content-start gap-4 p-6">
          <h2 className="text-lg font-semibold text-ink-950">AI 接口配置</h2>
          <p className="text-sm leading-6 text-slate-500">
            密钥仅保存在本机；选择接口后会读取该地址提供的模型。
          </p>
          <div className="flex flex-wrap gap-2">
            {providers.map(item => (
              <button
                className={cn(
                  'rounded-md border px-3 py-2 text-sm font-medium transition',
                  provider === item.id
                    ? 'border-brand-200 bg-brand-50 text-brand-700'
                    : 'border-slate-200 text-slate-600 hover:bg-slate-50'
                )}
                key={item.id}
                onClick={() => {
                  setProvider(item.id)
                  setBaseURL(item.baseURL)
                  setAPIKey('')
                }}
              >
                {item.name}
              </button>
            ))}
          </div>
          <label className="grid gap-1.5 text-xs font-semibold text-slate-600">
            接口地址
            <input
              className="h-9 rounded-md border border-slate-300 px-2.5 text-sm font-normal outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
              value={baseURL}
              onChange={event => setBaseURL(event.target.value)}
            />
          </label>
          <label className="grid gap-1.5 text-xs font-semibold text-slate-600">
            API Key
            <input
              className="h-9 rounded-md border border-slate-300 px-2.5 text-sm font-normal outline-none focus:border-brand-600 focus:ring-2 focus:ring-brand-100"
              type="password"
              value={apiKey}
              onChange={event => setAPIKey(event.target.value)}
              placeholder={
                current?.hasKey ? '重新填写以更新密钥' : '填写 API Key'
              }
            />
          </label>
          <footer className="flex justify-end gap-2 border-t border-slate-100 pt-4">
            <button
              className="secondary-button"
              onClick={() => setSettings(false)}
            >
              返回对话
            </button>
            <button
              className="primary-button"
              disabled={!apiKey}
              onClick={() => void save()}
            >
              保存并读取模型
            </button>
          </footer>
        </section>
      ) : (
        <>
          <section className="grid min-h-0 content-start gap-3 overflow-auto p-6">
            {messages.length === 0 && (
              <div className="m-auto grid justify-items-center text-center text-slate-500">
                <Bot className="size-8 text-brand-600" />
                <h2 className="mt-3 text-lg font-semibold text-slate-800">
                  从这里开始处理项目
                </h2>
                <p className="mt-1 text-sm">
                  描述修改目标、贴入报错，或让我先查看当前机器人目录。
                </p>
              </div>
            )}
            {messages.map((item, index) => (
              <article
                className={cn(
                  'max-w-[80%] whitespace-pre-wrap rounded-xl px-3.5 py-3 text-sm leading-6',
                  item.role === 'user'
                    ? 'justify-self-end bg-brand-600 text-white'
                    : 'bg-slate-100 text-slate-700'
                )}
                key={index}
              >
                {item.content}
              </article>
            ))}
          </section>
          <footer className="border-t border-slate-200 p-4">
            <div className="rounded-xl border border-slate-300 bg-white p-2 shadow-sm">
              <textarea
                className="min-h-20 w-full resize-none border-0 bg-transparent px-2 py-1.5 text-sm text-slate-800 outline-none placeholder:text-slate-400"
                value={prompt}
                onChange={event => setPrompt(event.target.value)}
                onKeyDown={event => {
                  if (event.key === 'Enter' && !event.shiftKey) {
                    event.preventDefault()
                    void submit()
                  }
                }}
                placeholder="输入问题，Enter 发送"
              />
              <input
                ref={fileInput}
                className="hidden"
                type="file"
                multiple
                onChange={event => attach(event.target.files)}
              />
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="flex items-center gap-2">
                  <button
                    className="icon-button size-8 p-0"
                    onClick={() => fileInput.current?.click()}
                    title="附加文件"
                  >
                    <Paperclip className="size-4" />
                  </button>
                  <div className="relative">
                    <button
                      className="inline-flex h-8 items-center gap-1 rounded-md border border-slate-200 px-2 text-xs font-medium text-slate-600 hover:bg-slate-50"
                      onClick={() => setAccessOpen(value => !value)}
                    >
                      <SlidersHorizontal className="size-3.5" /> {accessLabel}
                    </button>
                    {accessOpen && (
                      <div className="absolute bottom-10 left-0 z-10 grid min-w-32 rounded-lg border border-slate-200 bg-white p-1 shadow-lg">
                        {(
                          [
                            ['ask', '操作前询问'],
                            ['read', '仅查看'],
                            ['full', '完全访问']
                          ] as const
                        ).map(([id, label]) => (
                          <button
                            className={cn(
                              'rounded px-2 py-1.5 text-left text-xs transition',
                              access === id
                                ? 'bg-brand-50 text-brand-700'
                                : 'text-slate-600 hover:bg-slate-50'
                            )}
                            key={id}
                            onClick={() => {
                              setAccess(id)
                              setAccessOpen(false)
                              setNotice(`权限已切换为：${label}。`)
                            }}
                          >
                            {label}
                          </button>
                        ))}
                      </div>
                    )}
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <select
                    className="h-8 max-w-48 rounded-md border border-slate-200 bg-white px-2 text-xs text-slate-600"
                    value={model}
                    onChange={event => setModel(event.target.value)}
                    disabled={!models.length}
                  >
                    {models.length ? (
                      models.map(item => <option key={item}>{item}</option>)
                    ) : (
                      <option>未配置模型</option>
                    )}
                  </select>
                  <button
                    className="icon-button size-8 p-0"
                    onClick={voice}
                    title="语音输入"
                  >
                    <Mic className="size-4" />
                  </button>
                  <button
                    className="inline-flex size-8 items-center justify-center rounded-md bg-ink-950 text-white transition hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-40"
                    disabled={!prompt.trim()}
                    onClick={() => void submit()}
                    aria-label="发送"
                  >
                    <ArrowUp className="size-4" />
                  </button>
                </div>
              </div>
              {attachments.length > 0 && (
                <div className="mt-2 flex flex-wrap gap-1.5">
                  {attachments.map(file => (
                    <button
                      className="rounded bg-brand-50 px-2 py-1 text-xs text-brand-700 hover:bg-brand-100"
                      key={file.name}
                      onClick={() =>
                        setAttachments(value =>
                          value.filter(item => item !== file)
                        )
                      }
                    >
                      {file.name} ×
                    </button>
                  ))}
                </div>
              )}
            </div>
            {notice && (
              <small className="mt-2 block text-xs text-slate-500">
                {notice}
              </small>
            )}
          </footer>
        </>
      )}
    </section>
  )
}

export function AIControl({ root }: { root?: string }) {
  const [open, setOpen] = useState(false)
  const [providers, setProviders] = useState<Provider[]>([])
  const [provider, setProvider] = useState('openai')
  const [settings, setSettings] = useState(false)
  const [model, setModel] = useState('')
  const [baseURL, setBaseURL] = useState('')
  const [apiKey, setAPIKey] = useState('')
  const [messages, setMessages] = useState<Message[]>([])
  const [prompt, setPrompt] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const current = providers.find(item => item.id === provider)
  const load = useCallback(async () => {
    const res = await fetch('/api/v1/ai/providers')
    const data = (await res.json()) as Provider[]
    setProviders(data)
    const item = data.find(value => value.id === provider) ?? data[0]
    if (item) {
      setProvider(item.id)
      setModel(item.model)
      setBaseURL(item.baseURL)
    }
  }, [provider])
  useEffect(() => {
    if (open) void load()
  }, [open, load])
  useEffect(() => {
    if (current) {
      setModel(current.model)
      setBaseURL(current.baseURL)
      setAPIKey('')
    }
  }, [current])
  const save = async () => {
    setBusy(true)
    setError('')
    try {
      const res = await fetch('/api/v1/ai/providers', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ provider, model, baseURL, apiKey })
      })
      const data = (await res.json()) as { error?: string }
      if (!res.ok) throw new Error(data.error)
      await load()
      setSettings(false)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '配置保存失败。')
    } finally {
      setBusy(false)
    }
  }
  const send = async () => {
    if (!prompt.trim() || !current?.hasKey) return
    const next = [
      ...messages,
      { role: 'user' as const, content: prompt.trim() }
    ]
    setMessages(next)
    setPrompt('')
    setBusy(true)
    setError('')
    try {
      const res = await fetch('/api/v1/ai/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ provider, messages: next })
      })
      const data = (await res.json()) as { answer?: string; error?: string }
      if (!res.ok) throw new Error(data.error)
      setMessages([
        ...next,
        { role: 'assistant', content: data.answer || '没有返回内容。' }
      ])
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'AI 请求失败。')
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="ai-control">
      <button
        className="ai-control-button"
        onClick={() => setOpen(true)}
        aria-label="打开 AI 对话"
        title="AI 对话"
      >
        <Bot />
      </button>
      {open && (
        <section className="ai-workspace">
          <header>
            <div>
              <Bot />
              <span>
                <strong>AI 助手</strong>
                <small>{root || '当前运行目录'}</small>
              </span>
            </div>
            <div>
              <button
                className="icon-button"
                onClick={() => setSettings(value => !value)}
                title="AI 设置"
              >
                <Settings2 />
              </button>
              <button
                className="icon-button"
                onClick={() => setOpen(false)}
                title="关闭 AI"
              >
                <X />
              </button>
            </div>
          </header>
          <main>
            {settings || !current?.hasKey ? (
              <section className="ai-settings">
                <Tabs
                  ariaLabel="AI 服务商"
                  className="ai-config-tabs"
                  items={providers.map(item => ({
                    id: item.id,
                    label: item.name,
                    meta: item.hasKey ? '已配置' : '未配置'
                  }))}
                  onChange={setProvider}
                  value={provider}
                  variant="segmented"
                />
                <h2>接口配置</h2>
                <p>
                  每个接口独立保存地址与 API
                  Key；模型会在开始对话时根据所选接口获取。
                </p>
                <label>
                  接口地址
                  <input
                    value={baseURL}
                    onChange={e => setBaseURL(e.target.value)}
                  />
                </label>
                <label>
                  API Key
                  <input
                    type="password"
                    value={apiKey}
                    onChange={e => setAPIKey(e.target.value)}
                    placeholder={
                      current?.hasKey ? '重新填写以更新密钥' : '填写 API Key'
                    }
                  />
                </label>
                <button
                  className="primary-button"
                  disabled={busy || !apiKey}
                  onClick={() => void save()}
                >
                  {busy ? '保存中…' : '保存配置'}
                </button>
              </section>
            ) : (
              <>
                <section className="ai-chat-head">
                  <select
                    value={provider}
                    onChange={e => setProvider(e.target.value)}
                  >
                    {providers
                      .filter(item => item.hasKey)
                      .map(item => (
                        <option key={item.id} value={item.id}>
                          {item.name}
                        </option>
                      ))}
                  </select>
                  <span>{current?.model}</span>
                </section>
                <section className="ai-messages">
                  {messages.length === 0 && (
                    <div className="ai-empty">
                      <Bot />
                      <h2>有什么可以帮你？</h2>
                      <p>当前目录：{root || '当前运行目录'}</p>
                    </div>
                  )}
                  {messages.map((item, index) => (
                    <article className={item.role} key={index}>
                      {item.content}
                    </article>
                  ))}
                  {busy && <article className="assistant">正在思考…</article>}
                </section>
                <footer>
                  <textarea
                    value={prompt}
                    onChange={e => setPrompt(e.target.value)}
                    onKeyDown={e => {
                      if (e.key === 'Enter' && !e.shiftKey) {
                        e.preventDefault()
                        void send()
                      }
                    }}
                    placeholder="询问当前目录中的项目，Enter 发送"
                  />
                  <button
                    className="primary-button"
                    disabled={busy || !prompt.trim()}
                    onClick={() => void send()}
                  >
                    <Send />
                  </button>
                </footer>
              </>
            )}
            {error && <p className="ai-error">{error}</p>}
          </main>
        </section>
      )}
    </div>
  )
}

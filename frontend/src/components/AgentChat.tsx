import {
  ArrowUp,
  Check,
  ChevronDown,
  CircleX,
  Clock3,
  FileSearch,
  FileText,
  Loader2,
  Pencil,
  Plus,
  Settings2,
  ShieldCheck,
  ShieldQuestion,
  Sparkles,
  Square,
  Terminal,
  Trash2,
  Unlock,
  X
} from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import Markdown from 'markdown-to-jsx'

type Provider = {
  ID: string
  Name: string
  BaseURL: string
  Model: string
  HasKey: boolean
}
type ChatMessage = { role: 'user' | 'assistant'; content: string }
type Activity = {
  id: number
  tool: string
  args: string
  status: 'running' | 'done' | 'error'
  output?: string
}
type SessionMeta = {
  id: string
  title: string
  root: string
  provider: string
  model: string
  updated: string
}

const TOOL_LABEL: Record<string, string> = {
  read_project_file: '读取文件',
  list_project_files: '列出文件',
  agent_search: '搜索代码',
  agent_edit_file: '编辑文件',
  agent_run_command: '运行命令',
  agent_verify: '自动验证'
}

const TOOL_DESCRIPTION: Record<string, string> = {
  read_project_file: '读取项目文件',
  list_project_files: '列出项目文件',
  agent_search: '在项目中搜索代码',
  agent_edit_file: '精确编辑项目文件',
  agent_run_command: '运行白名单命令',
  agent_verify: '检查改动是否通过验证'
}

const PROMPT_EXAMPLES: Array<[string, string]> = [
  ['介绍一下这个项目', '读取项目结构，帮我介绍这个机器人项目。'],
  ['加一个新命令', '给机器人新增一个打招呼的命令并验证。'],
  ['找功能实现位置', '搜索某个功能的实现位置并解释。'],
  ['修复最近的报错', '查看最近改动，修复导致的报错。']
]

// formatUpdated renders an ISO timestamp as a short relative time in Chinese.
function formatUpdated(iso: string): string {
  if (!iso) return ''
  const updated = new Date(iso).getTime()
  if (Number.isNaN(updated)) return ''
  const diff = Date.now() - updated
  const minute = 60 * 1000
  const hour = 60 * minute
  const day = 24 * hour
  if (diff < minute) return '刚刚'
  if (diff < hour) return `${Math.floor(diff / minute)} 分钟前`
  if (diff < day) return `${Math.floor(diff / hour)} 小时前`
  return `${Math.floor(diff / day)} 天前`
}

function ToolIcon({ name }: { name: string }) {
  switch (name) {
    case 'agent_edit_file':
      return <Pencil className="size-3.5" />
    case 'agent_search':
      return <FileSearch className="size-3.5" />
    case 'agent_run_command':
      return <Terminal className="size-3.5" />
    case 'agent_verify':
      return <Check className="size-3.5" />
    default:
      return <FileText className="size-3.5" />
  }
}

function StepStatus({ status }: { status: Activity['status'] }) {
  if (status === 'running') {
    return (
      <>
        <Loader2 className="spinner size-3.5 animate-spin" />
        <span>执行中</span>
      </>
    )
  }
  if (status === 'done') {
    return (
      <>
        <Check className="size-3.5" />
        <span>完成</span>
      </>
    )
  }
  return (
    <>
      <CircleX className="size-3.5" />
      <span>失败</span>
    </>
  )
}

// AgentChatPage is the built-in coding-agent workspace. It streams the agent
// loop's progress over SSE and renders each tool call as a timeline step while
// the final answer streams in. Provider settings live behind the gear button.
export function AgentChatPage({
  root,
  initialSessionId
}: {
  root: string
  initialSessionId?: string
}) {
  const [providers, setProviders] = useState<Provider[]>([])
  const [provider, setProvider] = useState('')
  const [model, setModel] = useState('')
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [activity, setActivity] = useState<Activity[]>([])
  const [prompt, setPrompt] = useState('')
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const [settings, setSettings] = useState(false)
  const [baseURL, setBaseURL] = useState('')
  const [apiKey, setAPIKey] = useState('')
  const streamRef = useRef<AbortController | null>(null)
  const activityId = useRef(0)
  const lastToolId = useRef(-1)
  const promptRef = useRef<HTMLTextAreaElement | null>(null)
  const threadRef = useRef<HTMLElement | null>(null)
  const [access, setAccess] = useState<'ask' | 'auto' | 'full'>('ask')
  const [pendingConfirm, setPendingConfirm] = useState<{
    id: string
    tool: string
    args: string
  } | null>(null)
  const [sessions, setSessions] = useState<SessionMeta[]>([])
  const [sessionId, setSessionId] = useState('')
  const [sessionOpen, setSessionOpen] = useState(false)
  const [models, setModels] = useState<string[]>([])
  const [modelCardOpen, setModelCardOpen] = useState(false)
  const [moreOpen, setMoreOpen] = useState(false)
  const [accessOpen, setAccessOpen] = useState(false)
  const [editingIndex, setEditingIndex] = useState(-1)

  const current = providers.find(item => item.ID === provider)

  const loadModels = useCallback(
    async (id: string) => {
      try {
        const response = await fetch(
          `/api/v1/ai/models?provider=${encodeURIComponent(id)}`
        )
        if (!response.ok) return
        const data = (await response.json()) as { models?: string[] }
        setModels(data.models ?? [])
      } catch {
        // 模型列表加载失败不阻塞
      }
    },
    []
  )

  const loadProviders = useCallback(async () => {
    const response = await fetch('/api/v1/ai/providers')
    const data = (await response.json()) as Provider[]
    setProviders(data)
    const item = data.find(value => value.HasKey) ?? data[0]
    if (item) {
      setProvider(item.ID)
      setModel(item.Model)
      setBaseURL(item.BaseURL)
      void loadModels(item.ID)
    }
  }, [loadModels])
  useEffect(() => {
    void loadProviders()
  }, [loadProviders])

  useEffect(() => {
    threadRef.current?.scrollTo({ top: threadRef.current.scrollHeight })
  }, [messages, activity])

  const loadSessions = useCallback(async () => {
    try {
      const response = await fetch('/api/v1/agent/sessions')
      if (!response.ok) return
      const data = (await response.json()) as SessionMeta[]
      setSessions(data)
    } catch {
      // 列表加载失败不阻塞对话
    }
  }, [])
  useEffect(() => {
    void loadSessions()
  }, [loadSessions])

  // When opened from the directory session tree, load that conversation.
  const initialLoaded = useRef(false)
  useEffect(() => {
    if (!initialSessionId || initialLoaded.current) return
    initialLoaded.current = true
    void (async () => {
      try {
        const response = await fetch(`/api/v1/agent/sessions/${initialSessionId}`)
        if (!response.ok) return
        const data = (await response.json()) as {
          session: SessionMeta
          messages: Array<{ role: string; content: string }>
        }
        setSessionId(data.session.id)
        setMessages(
          data.messages.filter(
            message => message.content && message.role !== 'system'
          ) as ChatMessage[]
        )
        setActivity([])
      } catch {
        setNotice('加载会话失败。')
      }
    })()
  }, [initialSessionId])

  const newSession = () => {
    if (busy) return
    setSessionId('')
    setMessages([])
    setActivity([])
    setSessionOpen(false)
  }

  const openSession = async (id: string) => {
    if (busy || id === sessionId) return
    try {
      const response = await fetch(`/api/v1/agent/sessions/${id}`)
      if (!response.ok) return
      const data = (await response.json()) as {
        session: SessionMeta
        messages: Array<{ role: string; content: string }>
      }
      setSessionId(data.session.id)
      setMessages(
        data.messages.filter(
          message => message.content && message.role !== 'system'
        ) as ChatMessage[]
      )
      setActivity([])
      setSessionOpen(false)
    } catch {
      setNotice('加载会话失败。')
    }
  }

  const deleteSession = async (id: string) => {
    if (busy) return
    try {
      await fetch(`/api/v1/agent/sessions/${id}`, { method: 'DELETE' })
      if (id === sessionId) newSession()
      await loadSessions()
    } catch {
      setNotice('删除会话失败。')
    }
  }

  const handleEvent = useCallback(
    (event: {
      type: string
      text?: string
      tool?: string
      output?: string
    }) => {
      switch (event.type) {
        case 'tool':
          lastToolId.current = activityId.current
          setActivity(value => [
            ...value,
            {
              id: activityId.current++,
              tool: event.tool || '',
              args: event.text || '',
              status: 'running'
            }
          ])
          break
        case 'result':
          setActivity(value =>
            value.map(item =>
              item.id === lastToolId.current && item.status === 'running'
                ? {
                    ...item,
                    status: event.output?.startsWith('错误') ? 'error' : 'done',
                    output: event.output
                  }
                : item
            )
          )
          break
        case 'text':
          setMessages(value => [
            ...value,
            { role: 'assistant', content: event.text || '' }
          ])
          break
        case 'done':
          setActivity([])
          setMessages(value => [
            ...value,
            { role: 'assistant', content: event.text || '完成。' }
          ])
          setNotice('')
          break
        case 'error':
          setNotice(event.text || 'Agent 执行出错。')
          break
        case 'confirm':
          setPendingConfirm({
            id: event.tool || '',
            tool: event.text || '',
            args: event.output || ''
          })
          break
        case 'session':
          setSessionId(event.tool || '')
          void loadSessions()
          break
      }
    },
    [loadSessions]
  )

  const send = async () => {
    if (!prompt.trim() || busy) return
    if (!current?.HasKey) {
      setSettings(true)
      setNotice('请先配置 AI 接口与 API Key。')
      return
    }
    // Editing replaces the last user message and drops its prior assistant
    // reply, so the corrected prompt is re-sent in full.
    let next: ChatMessage[]
    if (editingIndex >= 0 && editingIndex < messages.length) {
      const prefix = messages.slice(0, editingIndex)
      const suffix = messages
        .slice(editingIndex + 1)
        .filter(item => item.role !== 'assistant')
      next = [...prefix, { role: 'user', content: prompt.trim() }, ...suffix]
    } else {
      next = [...messages, { role: 'user', content: prompt.trim() }]
    }
    setMessages(next)
    setPrompt('')
    setEditingIndex(-1)
    setActivity([])
    setNotice('Agent 正在执行…')
    setBusy(true)
    const controller = new AbortController()
    streamRef.current = controller
    try {
      const response = await fetch('/api/v1/agent/chat?stream=1', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          provider,
          model,
          root,
          access,
          sessionId,
          messages: next
        }),
        signal: controller.signal
      })
      if (!response.ok) {
        const data = (await response.json().catch(() => ({}))) as {
          error?: string
        }
        throw new Error(data.error || 'Agent 请求失败。')
      }
      if (!response.body) throw new Error('当前浏览器不支持流式读取。')
      const reader = response.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split('\n')
        buffer = lines.pop() ?? ''
        for (const line of lines) {
          const trimmed = line.trim()
          if (!trimmed.startsWith('data: ')) continue
          const event = JSON.parse(trimmed.slice(6)) as {
            type: string
            text?: string
            tool?: string
            output?: string
          }
          handleEvent(event)
        }
      }
    } catch (reason) {
      if (!(reason instanceof DOMException && reason.name === 'AbortError')) {
        const message =
          reason instanceof Error ? reason.message : 'Agent 请求失败。'
        setMessages(value => [
          ...value,
          { role: 'assistant', content: '⚠ ' + message }
        ])
        setNotice(message)
      }
    } finally {
      setBusy(false)
      streamRef.current = null
    }
  }

  const cancel = () => {
    streamRef.current?.abort()
    setBusy(false)
    setNotice('已停止本次执行。')
  }

  const resolveConfirm = async (approve: boolean) => {
    if (!pendingConfirm) return
    const id = pendingConfirm.id
    setPendingConfirm(null)
    try {
      const response = await fetch('/api/v1/agent/approve', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirmId: id, approve })
      })
      if (!response.ok) {
        const data = (await response.json().catch(() => ({}))) as {
          error?: string
        }
        setNotice(data.error || (approve ? '批准失败。' : '拒绝失败。'))
      }
    } catch {
      setNotice(approve ? '批准失败：网络错误。' : '拒绝失败：网络错误。')
    }
  }

  const saveSettings = async () => {
    const response = await fetch('/api/v1/ai/providers', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ provider, model, baseURL, apiKey })
    })
    const data = (await response.json()) as { error?: string }
    if (!response.ok) {
      setNotice(data.error || '保存失败。')
      return
    }
    setAPIKey('')
    await loadProviders()
    setSettings(false)
    setNotice('AI 接口已保存。')
  }

  const fillExample = (text: string) => {
    setPrompt(text)
    promptRef.current?.focus()
  }

  const resizePrompt = () => {
    const el = promptRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, 180)}px`
  }

  const changeProvider = (id: string) => {
    setProvider(id)
    setModel('')
    const item = providers.find(value => value.ID === id)
    if (item) {
      setBaseURL(item.BaseURL)
      void loadModels(id)
    }
  }

  let lastUserIndex = -1
  for (let index = messages.length - 1; index >= 0; index--) {
    if (messages[index].role === 'user') {
      lastUserIndex = index
      break
    }
  }

  return (
    <section className="agent-workspace">
      <header className="agent-header">
        <div className="agent-header-left">
          <span className="agent-header-icon">
            <Sparkles className="size-4" />
          </span>
          <div className="agent-header-meta">
            <span className="agent-header-title">Agent</span>
            <span className="agent-header-root" title={root}>
              {root}
            </span>
          </div>
        </div>
        <div className="agent-header-actions">
          {busy && (
            <span className="agent-status-pill">
              <Loader2 className="spinner size-3 animate-spin" /> 执行中
            </span>
          )}
          {busy && (
            <button
              className="icon-button size-8 p-0"
              onClick={cancel}
              title="停止"
              aria-label="停止执行"
            >
              <X className="size-4" />
            </button>
          )}
          <button
            className="icon-button size-8 p-0"
            onClick={() => setSettings(value => !value)}
            title="AI 设置"
            aria-label="AI 设置"
          >
            <Settings2 className="size-4" />
          </button>
        </div>
      </header>

      <div className="agent-body">
      {settings ? (
        <section className="agent-settings">
          <header className="agent-settings-head">
            <div>
              <h3>AI 接口配置</h3>
              <p>
                密钥仅保存在本机；Agent 会在所选项目中读取文件、搜索代码并运行白名单命令。
              </p>
            </div>
            <button
              className="icon-button size-8 p-0"
              onClick={() => setSettings(false)}
              aria-label="关闭设置"
              title="关闭"
            >
              <X className="size-4" />
            </button>
          </header>
          <label className="agent-settings-label">
            服务商
            <div className="agent-provider-grid">
              {providers.map(item => (
                <button
                  className={
                    provider === item.ID
                      ? 'agent-provider-chip active'
                      : 'agent-provider-chip'
                  }
                  key={item.ID}
                  onClick={() => {
                    setProvider(item.ID)
                    setBaseURL(item.BaseURL)
                    setAPIKey('')
                  }}
                >
                  {item.Name}
                  {item.HasKey && <small>已配置</small>}
                </button>
              ))}
            </div>
          </label>
          <label className="agent-settings-label">
            接口地址
            <input
              value={baseURL}
              onChange={event => setBaseURL(event.target.value)}
              placeholder="https://api.example.com/v1"
            />
          </label>
          <label className="agent-settings-label">
            API Key
            <input
              type="password"
              value={apiKey}
              onChange={event => setAPIKey(event.target.value)}
              placeholder={current?.HasKey ? '重新填写以更新密钥' : '填写 API Key'}
            />
          </label>
          <footer className="agent-settings-actions">
            <button
              className="secondary-button"
              onClick={() => setSettings(false)}
            >
              返回
            </button>
            <button
              className="primary-button"
              disabled={!apiKey}
              onClick={() => void saveSettings()}
            >
              保存
            </button>
          </footer>
        </section>
      ) : (
        <div className="agent-main">
          <section className="agent-thread" ref={threadRef}>
            {messages.length === 0 && !busy && (
              <div className="agent-empty">
                <span className="agent-empty-icon">
                  <Sparkles className="size-6" />
                </span>
                <h3>让 ALemonX 来搞定它</h3>
                <div className="agent-examples">
                  {PROMPT_EXAMPLES.map(([label, text]) => (
                    <button key={label} onClick={() => fillExample(text)}>
                      <strong>{label}</strong>
                      <small>{text}</small>
                    </button>
                  ))}
                </div>
              </div>
            )}
            {messages.map((item, index) =>
              item.role === 'user' ? (
                <article className="agent-message-user" key={index}>
                  <div>{item.content}</div>
                  {index === lastUserIndex && !busy && (
                    <button
                      className="agent-message-edit"
                      onClick={() => {
                        setEditingIndex(index)
                        setPrompt(item.content)
                        promptRef.current?.focus()
                      }}
                      title="编辑这条消息"
                      aria-label="编辑这条消息"
                    >
                      <Pencil className="size-3.5" />
                    </button>
                  )}
                </article>
              ) : (
                <article className="agent-message-assistant" key={index}>
                  <span className="agent-message-avatar">
                    <Sparkles className="size-3.5" />
                  </span>
                  <div className="agent-message-body">
                    <span className="agent-message-label">Agent</span>
                    <div className="agent-markdown">
                      <Markdown
                        options={{
                          forceBlock: true,
                          overrides: {
                            a: {
                              component: ({ href, children, ...rest }) => (
                                <a href={href} target="_blank" rel="noreferrer" {...rest}>
                                  {children}
                                </a>
                              )
                            }
                          }
                        }}
                      >
                        {item.content}
                      </Markdown>
                    </div>
                  </div>
                </article>
              )
            )}
            {activity.length > 0 && (
              <div className="agent-timeline">
                {activity.map((item, index) => (
                  <div
                    className="agent-step"
                    data-status={item.status}
                    key={item.id}
                  >
                    <div className="agent-step-rail">
                      <span className="agent-step-node">
                        <ToolIcon name={item.tool} />
                      </span>
                      {index < activity.length - 1 && (
                        <span className="agent-step-line" />
                      )}
                    </div>
                    <div className="agent-step-body">
                      <div className="agent-step-head">
                        <div className="agent-step-title">
                          <span>{TOOL_LABEL[item.tool] || item.tool}</span>
                          <small>{TOOL_DESCRIPTION[item.tool]}</small>
                        </div>
                        <span className="agent-step-status">
                          <StepStatus status={item.status} />
                        </span>
                      </div>
                      {item.args && (
                        <details className="agent-step-args" open={item.status === 'running'}>
                          <summary>查看参数</summary>
                          <pre>{item.args}</pre>
                        </details>
                      )}
                      {item.output && (
                        <details className="agent-step-output" open={item.status === 'error'}>
                          <summary>
                            {item.status === 'error' ? '查看错误' : '查看结果'}
                          </summary>
                          <pre>{item.output}</pre>
                        </details>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            )}
            {busy && activity.length === 0 && messages.length > 0 && (
              <div className="agent-thinking">
                <Loader2 className="spinner size-3.5 animate-spin" />
                <span>Agent 正在思考…</span>
              </div>
            )}
          </section>
          <footer className="agent-composer">
            {editingIndex >= 0 && (
              <div className="agent-composer-editbar">
                <Pencil className="size-3" />
                正在编辑这条消息
                <button onClick={() => setEditingIndex(-1)}>取消</button>
              </div>
            )}
            {notice && (
              <small className="agent-composer-notice">{notice}</small>
            )}
            <div className="agent-composer-box">
              <textarea
                ref={promptRef}
                value={prompt}
                onChange={event => {
                  setPrompt(event.target.value)
                  resizePrompt()
                }}
                onKeyDown={event => {
                  if (event.key === 'Enter' && !event.shiftKey) {
                    event.preventDefault()
                    void send()
                  }
                }}
                rows={1}
              />
              <div className="agent-composer-bar">
                <div className="agent-composer-left">
                  <div className="agent-composer-tool">
                    <button
                      className="agent-composer-icon"
                      onClick={() => setMoreOpen(value => !value)}
                      disabled={busy}
                      title="更多功能"
                      aria-label="更多功能"
                    >
                      <Plus className="size-4" />
                    </button>
                    {moreOpen && (
                      <div className="agent-composer-popup">
                        <button onClick={() => newSession()} disabled={busy}>
                          <Plus className="size-3.5" />
                          新会话
                        </button>
                        <button
                          onClick={() => setSessionOpen(true)}
                          disabled={busy}
                        >
                          <Clock3 className="size-3.5" />
                          会话历史
                        </button>
                        <button
                          onClick={() => setSettings(true)}
                          disabled={busy}
                        >
                          <Settings2 className="size-3.5" />
                          AI 设置
                        </button>
                      </div>
                    )}
                  </div>
                  <div className="agent-composer-tool">
                    <button
                      className="agent-composer-icon agent-composer-access"
                      onClick={() => setAccessOpen(value => !value)}
                      disabled={busy}
                      title="权限模式"
                      aria-label="权限模式"
                    >
                      {access === 'full' ? (
                        <Unlock className="size-4" />
                      ) : access === 'auto' ? (
                        <ShieldCheck className="size-4" />
                      ) : (
                        <ShieldQuestion className="size-4" />
                      )}
                    </button>
                    {accessOpen && (
                      <div className="agent-composer-popup agent-access-card">
                        {(
                          [
                            ['ask', '请求批准', '每次修改文件前都征求你的同意'],
                            ['auto', '替我审核', '自动批准文件修改，你只看到结果'],
                            ['full', '完全访问', '全部操作自动执行，不做确认']
                          ] as const
                        ).map(([id, label, desc]) => (
                          <button
                            className={access === id ? 'active' : ''}
                            key={id}
                            onClick={() => {
                              setAccess(id)
                              setAccessOpen(false)
                            }}
                            disabled={busy}
                          >
                            <span className="agent-access-label">{label}</span>
                            <small>{desc}</small>
                          </button>
                        ))}
                      </div>
                    )}
                  </div>
                  <small className="agent-composer-hint">
                    {access === 'ask'
                      ? '请求批准'
                      : access === 'auto'
                        ? '替我审核'
                        : '完全访问'}
                  </small>
                </div>
                <div className="agent-composer-right">
                  <div className="agent-composer-tool">
                    <button
                      className="agent-composer-model"
                      onClick={() => setModelCardOpen(value => !value)}
                      disabled={busy}
                      title="选择 AI 服务与模型"
                    >
                      <Sparkles className="size-3.5" />
                      <span className="agent-composer-model-name">
                        {current?.Name || '选择服务'}
                      </span>
                      <ChevronDown className="size-3" />
                    </button>
                    {modelCardOpen && (
                      <div className="agent-composer-popup agent-model-card">
                        <strong>服务商</strong>
                        <div className="agent-model-providers">
                          {providers
                            .filter(item => item.HasKey)
                            .map(item => (
                              <button
                                className={
                                  provider === item.ID ? 'active' : ''
                                }
                                key={item.ID}
                                onClick={() => changeProvider(item.ID)}
                              >
                                {item.Name}
                              </button>
                            ))}
                        </div>
                        <strong>模型</strong>
                        <div className="agent-model-list">
                          {models.length === 0 ? (
                            <small>未获取到模型列表</small>
                          ) : (
                            models.map(item => (
                              <button
                                className={model === item ? 'active' : ''}
                                key={item}
                                onClick={() => setModel(item)}
                              >
                                {item}
                              </button>
                            ))
                          )}
                        </div>
                      </div>
                    )}
                  </div>
                  <button
                    className={busy ? 'agent-stop' : 'agent-send'}
                    disabled={!busy && !prompt.trim()}
                    onClick={busy ? () => cancel() : () => void send()}
                    aria-label={busy ? '停止' : '发送'}
                    title={busy ? '停止' : '发送'}
                  >
                    {busy ? (
                      <Square className="size-3.5" />
                    ) : (
                      <ArrowUp className="size-4" />
                    )}
                  </button>
                </div>
              </div>
            </div>
          </footer>
        </div>
      )}

      {sessionOpen && (
        <aside className="agent-sessions">
          <div className="agent-sessions-head">
            <strong>会话</strong>
            <button
              className="icon-button size-7 p-0"
              onClick={() => newSession()}
              title="新会话"
              aria-label="新会话"
            >
              <Plus className="size-3.5" />
            </button>
          </div>
          <div className="agent-sessions-list">
            {sessions.length === 0 && (
              <p className="agent-sessions-empty">还没有会话记录。</p>
            )}
            {sessions.map(item => (
              <div
                className={
                  item.id === sessionId
                    ? 'agent-session active'
                    : 'agent-session'
                }
                key={item.id}
              >
                <button
                  className="agent-session-main"
                  onClick={() => void openSession(item.id)}
                >
                  <span className="agent-session-title">{item.title}</span>
                  <small>{formatUpdated(item.updated)}</small>
                </button>
                <button
                  className="agent-session-delete"
                  onClick={() => void deleteSession(item.id)}
                  title="删除会话"
                  aria-label="删除会话"
                >
                  <Trash2 className="size-3.5" />
                </button>
              </div>
            ))}
          </div>
        </aside>
      )}
      </div>

      {pendingConfirm && (
        <div className="agent-confirm-overlay">
          <div className="agent-confirm-dialog" role="dialog" aria-modal="true">
            <header>
              <Pencil className="size-4" />
              <strong>Agent 请求修改项目</strong>
            </header>
            <p>
              工具 <code>{pendingConfirm.tool}</code> 想要修改项目文件。确认后才会写入。
            </p>
            {pendingConfirm.args && (
              <pre className="agent-confirm-args">{pendingConfirm.args}</pre>
            )}
            <footer>
              <button
                className="secondary-button"
                onClick={() => void resolveConfirm(false)}
              >
                拒绝
              </button>
              <button
                className="primary-button"
                onClick={() => void resolveConfirm(true)}
              >
                批准修改
              </button>
            </footer>
          </div>
        </div>
      )}
    </section>
  )
}

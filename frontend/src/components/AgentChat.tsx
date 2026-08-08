import { useStoreState } from '../store/guideStore'
import { DirectoryPicker } from './Dashboard'
import {
  ArrowUp,
  Check,
  ChevronDown,
  CircleX,
  ExternalLink,
  FileSearch,
  FileText,
  Folder,
  ListTodo,
  Loader2,
  Minimize2,
  Target,
  Pencil,
  Plus,
  Settings2,
  ShieldCheck,
  ShieldQuestion,
  Slash,
  Sparkles,
  Square,
  Terminal,
  Trash2,
  Unlock,
  X
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef } from 'react'
import { AgentMarkdown } from './AgentMarkdown'
import { Modal } from './Modal'
import cn from 'classnames'

type Provider = {
  ID: string
  Name: string
  BaseURL: string
  Model: string
  HasKey: boolean
}
type ChatMessage = { role: 'user' | 'assistant'; content: string }
type PickerMode = 'directory' | 'file' | 'extension'
type Activity = {
  id: number
  callId: string
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
  status?: 'idle' | 'running' | 'completed' | 'failed'
  turn?: number
  lastError?: string
  updated: string
}
type TaskMeta = {
  id: string
  sessionId: string
  status: string
  turn: number
  lastError?: string
  plan?: { goal: string; completion: string; currentStep: number; steps: Array<{ id: string; title: string; status: string }> }
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
	['修复当前报错', '收集并分析项目当前错误，定位原因，修复后运行验证。'],
	['介绍一下这个项目', '读取项目结构，帮我介绍这个机器人项目。'],
  ['加一个新命令', '给机器人新增一个打招呼的命令并验证。'],
  ['找功能实现位置', '搜索某个功能的实现位置并解释。'],
  ['修复最近的报错', '查看最近改动，修复导致的报错。']
]

const PROVIDER_KEY_LINKS: Record<string, { href: string; label: string }> = {
  openai: {
    href: 'https://platform.openai.com/api-keys',
    label: '获取 OpenAI API Key'
  },
  deepseek: {
    href: 'https://platform.deepseek.com/api_keys',
    label: '获取 DeepSeek API Key'
  },
  claude: {
    href: 'https://platform.claude.com/settings/keys',
    label: '获取 Claude API Key'
  }
}

// formatToolArgs renders a tool's JSON arguments as a short friendly line, so
// the timeline shows "读取 src/index.ts" instead of raw `{"path":"..."}`.
function formatToolArgs(tool: string, rawArgs: string): string {
  if (!rawArgs) return ''
  try {
    const args = JSON.parse(rawArgs) as Record<string, unknown>
    const path = typeof args.path === 'string' ? args.path : ''
    switch (tool) {
      case 'read_project_file':
        return path ? `读取 ${path}` : rawArgs
      case 'list_project_files':
        return '列出项目文件'
      case 'agent_search': {
        const pattern = typeof args.pattern === 'string' ? args.pattern : ''
        return pattern ? `搜索 ${pattern}` : rawArgs
      }
      case 'agent_edit_file': {
        const mode = typeof args.mode === 'string' ? args.mode : 'edit'
        if (mode === 'create') return `新建 ${path}`
        if (mode === 'delete') return `删除 ${path}`
        const hunks = Array.isArray(args.edits) ? args.edits.length : 0
        return `编辑 ${path}${hunks > 0 ? `（${hunks} 处）` : ''}`
      }
      case 'agent_run_command': {
        const command = typeof args.command === 'string' ? args.command : ''
        const sub = Array.isArray(args.args)
          ? (args.args as string[]).join(' ')
          : ''
        return `运行 ${command}${sub ? ` ${sub}` : ''}`
      }
      case 'agent_verify':
        return '运行验证'
      default:
        return rawArgs
    }
  } catch {
    return rawArgs
  }
}

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

// sessionToMessages keeps only the plain conversation turns when resuming a
// history. The persisted transcript also carries role="tool" result payloads
// (raw JSON / command output) and empty assistant tool-call frames; neither
// belongs in the chat timeline. Adjacent duplicate user turns are collapsed
// too: older transcripts once wrote the same user message twice.
function sessionToMessages(
  messages: Array<{ role: string; content?: string }>
): ChatMessage[] {
  const out: ChatMessage[] = []
  for (const message of messages) {
    if (
      (message.role !== 'user' && message.role !== 'assistant') ||
      !message.content
    ) {
      continue
    }
    const prev = out[out.length - 1]
    if (
      message.role === 'user' &&
      prev &&
      prev.role === 'user' &&
      prev.content === message.content
    ) {
      continue
    }
    out.push({ role: message.role, content: message.content })
  }
  return out
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
  const [providers, setProviders] = useStoreState<Provider[]>([])
  const [provider, setProvider] = useStoreState('')
  const [model, setModel] = useStoreState('')
  const [messages, setMessages] = useStoreState<ChatMessage[]>([])
  const [activity, setActivity] = useStoreState<Activity[]>([])
  const [prompt, setPrompt] = useStoreState('')
  const [busy, setBusy] = useStoreState(false)
  const [notice, setNotice] = useStoreState('')
  const [settings, setSettings] = useStoreState(false)
  const [baseURL, setBaseURL] = useStoreState('')
  const [apiKey, setAPIKey] = useStoreState('')
  const streamRef = useRef<AbortController | null>(null)
  const taskIdRef = useRef('')
  const activityId = useRef(0)
  const promptRef = useRef<HTMLTextAreaElement | null>(null)
  const threadRef = useRef<HTMLElement | null>(null)
  const [access, setAccess] = useStoreState<'ask' | 'auto' | 'full'>('ask')
  const [timelineOpen, setTimelineOpen] = useStoreState(false)
  const [pendingConfirm, setPendingConfirm] = useStoreState<{
    id: string
    tool: string
    args: string
    diff?: {
      path: string
      mode?: string
      content?: string
      hunks?: Array<{ old: string; new: string }>
    } | null
  } | null>(null)
  const [sessions, setSessions] = useStoreState<SessionMeta[]>([])
  const [tasks, setTasks] = useStoreState<TaskMeta[]>([])
  const [sessionId, setSessionId] = useStoreState('')
  const [sessionOpen, setSessionOpen] = useStoreState(false)
  const [models, setModels] = useStoreState<string[]>([])
  const [modelCardOpen, setModelCardOpen] = useStoreState(false)
  const [moreOpen, setMoreOpen] = useStoreState(false)
  const [accessOpen, setAccessOpen] = useStoreState(false)
  const [editingIndex, setEditingIndex] = useStoreState(-1)
  // 斜杠命令：slashOpen 控制 / 菜单，slashDialog 是当前命令弹窗。
  const [slashOpen, setSlashOpen] = useStoreState(false)
  const [slashDialog, setSlashDialog] = useStoreState<
    '' | 'compress' | 'goal' | 'plan'
  >('')
  const [goalText, setGoalText] = useStoreState('')
  const [planText, setPlanText] = useStoreState('')
  const [filePickerOpen, setFilePickerOpen] = useStoreState(false)
  const [filePickerMode, setFilePickerMode] = useStoreState<PickerMode>('directory')
  const [fileExtensions, setFileExtensions] = useStoreState('ts, tsx')
  const [slashIndex, setSlashIndex] = useStoreState(-1)

  const current = providers.find(item => item.ID === provider)

  const loadModels = useCallback(async (id: string) => {
    try {
      const response = await fetch(
        `/api/v1/ai/models?provider=${encodeURIComponent(id)}`
      )
      if (!response.ok) return
      const data = (await response.json()) as { models?: string[] }
      const list = data.models ?? []
      setModels(list)
      // 若当前 model 不在该 provider 的模型列表里（例如旧配置残留了
      // 别的服务商模型），回退到列表第一个，避免把脏模型发给后端。
      if (list.length > 0) {
        setModel(currentModel => {
          const stale = !list.includes(currentModel)
          return stale ? list[0] : currentModel
        })
      }
    } catch {
      // 模型列表加载失败不阻塞
    }
  }, [])

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

  const loadTasks = useCallback(async () => {
    try {
      const response = await fetch('/api/v1/agent/tasks')
      if (response.ok) setTasks((await response.json()) as TaskMeta[])
    } catch {
      // Task status is auxiliary to the conversation view.
    }
  }, [])
  useEffect(() => {
    void loadTasks()
    const timer = window.setInterval(() => void loadTasks(), 2000)
    return () => window.clearInterval(timer)
  }, [loadTasks])

  // When opened from the directory session tree, load that conversation.
  // Track the last loaded id so a repeated id is not re-fetched, while a new id
  // (switching records after starting a fresh session) loads every time.
  const lastLoadedSessionId = useRef('')
  useEffect(() => {
    if (
      !initialSessionId ||
      typeof initialSessionId !== 'string' ||
      initialSessionId === lastLoadedSessionId.current
    )
      return
    lastLoadedSessionId.current = initialSessionId
    void (async () => {
      try {
        const response = await fetch(
          `/api/v1/agent/sessions/${initialSessionId}`
        )
        if (!response.ok) return
        const data = (await response.json()) as {
          session: SessionMeta
          messages: Array<{ role: string; content: string }>
        }
        setSessionId(data.session.id)
        setMessages(sessionToMessages(data.messages))
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
    // 清除已加载记录，让之后点开同一记录仍能重新加载；并通知 Dashboard
    // 清空 agentSessionId，否则重开同一条记录时 prop 不变、不触发加载。
    lastLoadedSessionId.current = ''
    window.dispatchEvent(new CustomEvent('alx:agent-new-session'))
  }

  const openSession = async (id: string) => {
    if (busy || id === sessionId) return
    if (typeof id !== 'string' || !id.startsWith('s')) {
      console.warn('openSession 收到非法会话 ID：', id)
      setNotice('无法打开该对话记录。')
      return
    }
    try {
      const response = await fetch(`/api/v1/agent/sessions/${id}`)
      if (!response.ok) return
      const data = (await response.json()) as {
        session: SessionMeta
        messages: Array<{ role: string; content: string }>
      }
      setSessionId(data.session.id)
      setMessages(sessionToMessages(data.messages))
      setActivity([])
      setSessionOpen(false)
    } catch {
      setNotice('加载会话失败。')
    }
  }

  const deleteSession = async (id: string) => {
    if (busy) return
    if (typeof id !== 'string' || !id.startsWith('s')) {
      console.warn('deleteSession 收到非法会话 ID：', id)
      return
    }
    try {
      await fetch(`/api/v1/agent/sessions/${id}`, { method: 'DELETE' })
      if (id === sessionId) newSession()
      await loadSessions()
    } catch {
      setNotice('删除会话失败。')
    }
  }

  const resumeTask = async (id: string) => {
    const response = await fetch(`/api/v1/agent/tasks/${encodeURIComponent(id)}/resume`, { method: 'POST' })
    if (!response.ok) setNotice('任务无法恢复。')
    await loadTasks()
  }

  const rollbackTask = async (id: string) => {
    const response = await fetch(`/api/v1/agent/tasks/${encodeURIComponent(id)}/rollback`, { method: 'POST' })
    if (!response.ok) setNotice('回滚失败：可能有文件被外部修改。')
    await loadTasks()
  }

  const showTaskReport = async (id: string) => {
    const response = await fetch(`/api/v1/agent/tasks/${encodeURIComponent(id)}/report`)
    if (!response.ok) { setNotice('任务报告尚未生成。'); return }
    const report = (await response.json()) as { summary?: string; modifiedFiles?: string[] }
    setNotice(`${report.summary || '任务报告已生成。'}${report.modifiedFiles?.length ? ` 修改 ${report.modifiedFiles.length} 个文件。` : ''}`)
  }

  const handleEvent = useCallback(
    (event: {
      type: string
      text?: string
      tool?: string
      callId?: string
      output?: string
      diff?: {
        path: string
        mode?: string
        content?: string
        hunks?: Array<{ old: string; new: string }>
      } | null
    }) => {
      switch (event.type) {
        case 'tool':
          setActivity(value => [
            ...value,
            {
              id: activityId.current++,
              callId: event.callId || `event-${activityId.current}`,
              tool: event.tool || '',
              args: event.text || '',
              status: 'running'
            }
          ])
          break
        case 'result':
          setActivity(value =>
            value.map(item =>
              item.callId === event.callId && item.status === 'running'
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
          setTimelineOpen(false)
          setMessages(value => [
            ...value,
            { role: 'assistant', content: event.text || '完成。' }
          ])
          setNotice('')
          break
        case 'error':
          setTimelineOpen(false)
          setNotice(event.text || 'Agent 执行出错。')
          break
        case 'plan':
          setNotice(event.text ? `当前步骤：${event.text}` : '任务计划已更新。')
          break
        case 'review':
          try {
            const review = JSON.parse(event.text || '{}') as {
              goalSatisfied?: boolean
              summary?: string
            }
            setNotice(
              review.goalSatisfied
                ? 'Reviewer 已通过：' + (review.summary || '任务目标已满足。')
                : 'Reviewer 未通过：' + (review.summary || '请查看失败步骤。')
            )
          } catch {
            setNotice('Reviewer 已完成审查。')
          }
          break
        case 'confirm':
          setPendingConfirm({
            id: event.tool || '',
            tool: event.text || '',
            args: event.output || '',
            diff: event.diff ?? null
          })
          break
        case 'session':
          setSessionId(event.tool || '')
          void loadSessions()
          // 通知 Dashboard 刷新左侧"记录"里的会话列表，让新建的对话立即可见。
          window.dispatchEvent(new CustomEvent('alx:agent-session-created'))
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
    // 斜杠命令优先：若输入是 /压缩 /目标 /计划，执行命令而非发送给 Agent。
    if (handleSlashExecution(prompt.trim())) {
      return
    }
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
    setTimelineOpen(false)
    setNotice('Agent 正在执行…')
    setBusy(true)
    const controller = new AbortController()
    streamRef.current = controller
    // SSE 流式请求绕过 Vite 代理直连后端：开发模式下 Vite 的 http-proxy
    // 会破坏 chunked 的 text/event-stream（"Invalid character in chunk
    // size"）。后端对 5173 来源加了 dev CORS。生产环境前端与后端同源。
    const taskURL = import.meta.env.DEV
      ? 'http://localhost:17390/api/v1/agent/tasks'
      : '/api/v1/agent/tasks'
    try {
      console.log(
        '[agent] taskURL =',
        taskURL,
        'DEV =',
        import.meta.env.DEV
      )
      const createResponse = await fetch(taskURL, {
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
      if (!createResponse.ok) {
        const data = (await createResponse.json().catch(() => ({}))) as {
          error?: string
        }
        throw new Error(data.error || 'Agent 请求失败。')
      }
      const created = (await createResponse.json()) as { taskId?: string }
      if (!created.taskId) throw new Error('Agent 未返回任务 ID。')
      taskIdRef.current = created.taskId
      const streamURL = `${taskURL}/${encodeURIComponent(created.taskId)}/events`
      const response = await fetch(streamURL, { signal: controller.signal })
      if (!response.ok) throw new Error('无法订阅 Agent 任务事件。')
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
            callId?: string
            output?: string
          }
          handleEvent(event)
        }
      }
    } catch (reason) {
      if (!(reason instanceof DOMException && reason.name === 'AbortError')) {
        const message =
          reason instanceof Error ? reason.message : 'Agent 请求失败。'
        console.error('[agent] 请求错误：', reason, 'taskURL =', taskURL)
        setMessages(value => [
          ...value,
          { role: 'assistant', content: '⚠ ' + message }
        ])
        setNotice(message)
      }
    } finally {
      setBusy(false)
      streamRef.current = null
      taskIdRef.current = ''
    }
  }

  const cancel = () => {
    const taskId = taskIdRef.current
    if (taskId) {
      const taskURL = import.meta.env.DEV
        ? 'http://localhost:17390/api/v1/agent/tasks'
        : '/api/v1/agent/tasks'
      void fetch(`${taskURL}/${encodeURIComponent(taskId)}/cancel`, {
        method: 'POST'
      })
    }
    streamRef.current?.abort()
    setBusy(false)
    setNotice('已停止本次执行。')
  }

  // 估算当前上下文占用（前端近似：消息总字符数 / 预算）。
  const contextUsage = useMemo(() => {
    const totalChars = messages.reduce((sum, m) => sum + m.content.length, 0)
    const budget = 120 * 1024
    const usage = Math.min(100, Math.round((totalChars / budget) * 100))
    return Math.max(1, usage)
  }, [messages])

  const slashCommands = useMemo(
    () => [
      {
        id: 'compress',
        label: '压缩',
        hint: `查看当前聊天上下文 ${contextUsage}%`
      },
      {
        id: 'goal',
        label: '目标',
        hint: '设置要持续的目标'
      },
      {
        id: 'plan',
        label: '计划',
        hint: '开始设定计划'
      }
    ],
    [contextUsage]
  )

  // 执行斜杠命令。选中命令先插入输入框，用户回车时才触发。
  const runSlashCommand = (command: (typeof slashCommands)[number]) => {
    setSlashOpen(false)
    setPrompt(`/${command.label}`)
    promptRef.current?.focus()
  }

  // 检测输入是否为斜杠命令并执行。
  const handleSlashExecution = (value: string): boolean => {
    const match = value.match(/^\/(压缩|目标|计划)(?:\s+(.+))?$/)
    if (!match) return false
    const [, command, arg] = match
    if (command === '压缩') {
      setNotice(`当前聊天上下文已使用约 ${contextUsage}%。`)
      setSlashDialog('compress')
    } else if (command === '目标') {
      setGoalText(arg ?? '')
      setSlashDialog('goal')
    } else if (command === '计划') {
      setPlanText(arg ?? '')
      setSlashDialog('plan')
    }
    setPrompt('')
    return true
  }

  const resolveConfirm = useCallback(
    async (approve: boolean) => {
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
    },
    [pendingConfirm]
  )

  useEffect(() => {
    if (!pendingConfirm) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        void resolveConfirm(false)
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [pendingConfirm, resolveConfirm])

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

  const completedSteps = activity.filter(item => item.status === 'done').length
  const runningSteps = activity.filter(item => item.status === 'running').length
  const failedSteps = activity.filter(item => item.status === 'error').length
  const activitySummary = [
    `${activity.length} 个操作`,
    completedSteps > 0 && `完成 ${completedSteps}`,
    runningSteps > 0 && `进行中 ${runningSteps}`,
    failedSteps > 0 && `失败 ${failedSteps}`
  ]
    .filter(Boolean)
    .join(' · ')
  const activeTask = tasks.find(task => task.sessionId === sessionId && task.status === 'running')

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
          <button
            className="icon-button size-8 p-0"
            onClick={newSession}
            title="新增对话"
            aria-label="新增对话"
          >
            <Plus className="size-4" />
          </button>
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
                    <AgentMarkdown
                      content={item.content}
                      streaming={busy && index === messages.length - 1}
                    />
                  </div>
                </article>
              )
            )}
            {activeTask?.plan && (
              <div className="agent-plan-card">
                <strong>任务计划</strong>
                <small>{activeTask.plan.goal}</small>
                <div className="agent-plan-steps">
                  {activeTask.plan.steps.map((step, index) => (
                    <span key={step.id} data-status={step.status}>
                      {index + 1}. {step.title}
                    </span>
                  ))}
                </div>
              </div>
            )}
            {activity.length > 0 && (
              <div className="agent-timeline-wrap">
                <button
                  className="agent-timeline-toggle"
                  onClick={() => setTimelineOpen(value => !value)}
                  aria-expanded={timelineOpen}
                  aria-controls="agent-activity-timeline"
                >
                  <span className="agent-timeline-count">
                    {activitySummary}
                  </span>
                  <ChevronDown
                    className={cn(
                      'size-3.5 transition-transform',
                      timelineOpen && 'rotate-180'
                    )}
                  />
                </button>
                {timelineOpen && (
                  <div className="agent-timeline" id="agent-activity-timeline">
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
                            <div className="agent-step-args">
                              <span className="agent-step-args-text">
                                {formatToolArgs(item.tool, item.args)}
                              </span>
                            </div>
                          )}
                          {item.output && (
                            <details
                              className="agent-step-output"
                              open={item.status === 'error'}
                            >
                              <summary>
                                {item.status === 'error'
                                  ? '查看错误'
                                  : '查看结果'}
                              </summary>
                              <pre>{item.output}</pre>
                            </details>
                          )}
                        </div>
                      </div>
                    ))}
                  </div>
                )}
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
                  const value = event.target.value
                  setPrompt(value)
                  resizePrompt()
                  // 输入 / 且是首字符时弹出命令菜单。
                  if (value === '/') setSlashOpen(true)
                  else if (!value.startsWith('/')) setSlashOpen(false)
                }}
                onKeyDown={event => {
                  if (slashOpen) {
                    if (event.key === 'ArrowDown') {
                      event.preventDefault()
                      setSlashIndex(current =>
                        current >= slashCommands.length - 1 ? 0 : current + 1
                      )
                      return
                    }
                    if (event.key === 'ArrowUp') {
                      event.preventDefault()
                      setSlashIndex(current =>
                        current <= 0 ? slashCommands.length - 1 : current - 1
                      )
                      return
                    }
                    if (event.key === 'Enter') {
                      event.preventDefault()
                      const selected =
                        slashIndex >= 0 ? slashCommands[slashIndex] : slashCommands[0]
                      runSlashCommand(selected)
                      setSlashIndex(-1)
                      return
                    }
                    if (event.key === 'Tab') {
                      event.preventDefault()
                      const selected =
                        slashIndex >= 0 ? slashCommands[slashIndex] : slashCommands[0]
                      runSlashCommand(selected)
                      setSlashIndex(-1)
                      return
                    }
                    if (event.key === 'Escape') {
                      setSlashOpen(false)
                      setSlashIndex(-1)
                      return
                    }
                  }
                  if (
                    event.key === 'Enter' &&
                    !event.shiftKey &&
                    !event.nativeEvent.isComposing
                  ) {
                    event.preventDefault()
                    void send()
                  }
                  if (event.key === 'Escape') setSlashOpen(false)
                }}
                rows={1}
                aria-label="描述要交给 Agent 的任务"
                placeholder="描述任务，输入 / 查看命令，Enter 发送"
              />
              {slashOpen && (
                <div className="agent-slash-menu">
                  {slashCommands.map((command, index) => (
                    <button
                      className={cn(
                        'agent-slash-item',
                        index === slashIndex && 'active'
                      )}
                      key={command.id}
                      onClick={() => {
                        runSlashCommand(command)
                        setSlashIndex(-1)
                      }}
                      onMouseEnter={() => setSlashIndex(index)}
                    >
                      <span className="agent-slash-label">
                        /{command.label}
                      </span>
                      <small className="agent-slash-hint">{command.hint}</small>
                    </button>
                  ))}
                </div>
              )}
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
                        <button
                          onClick={() => {
                            setFilePickerOpen(true)
                            setMoreOpen(false)
                          }}
                          disabled={busy}
                        >
                          <Folder className="size-3.5" />
                          选择目录或文件
                        </button>
                        <button
                          onClick={() => newSession()}
                          disabled={busy}
                        >
                          <Plus className="size-3.5" />
                          新会话
                        </button>
                        <button
                          onClick={() => {
                            setPrompt('/目标 ')
                            setMoreOpen(false)
                            promptRef.current?.focus()
                          }}
                        >
                          <Target className="size-3.5" />
                          目标
                        </button>
                        <button
                          onClick={() => {
                            setPrompt('/计划 ')
                            setMoreOpen(false)
                            promptRef.current?.focus()
                          }}
                        >
                          <ListTodo className="size-3.5" />
                          计划
                        </button>
                        <button
                          onClick={() => {
                            setPrompt('/压缩')
                            setMoreOpen(false)
                            promptRef.current?.focus()
                          }}
                        >
                          <Minimize2 className="size-3.5" />
                          压缩
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
                            [
                              'auto',
                              '替我审核',
                              '自动批准文件修改，你只看到结果'
                            ],
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
                                className={provider === item.ID ? 'active' : ''}
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
      </div>

      <Modal
        open={settings}
        zIndex={220}
        ariaLabel="AI 接口配置"
        onClose={() => setSettings(false)}
      >
        <section className="agent-settings">
            <header className="agent-settings-head">
              <div>
                <h3>AI 接口配置</h3>
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
                placeholder={
                  current?.HasKey ? '重新填写以更新密钥' : '填写 API Key'
                }
              />
            </label>
            {PROVIDER_KEY_LINKS[provider] && (
              <a
                className="agent-key-link"
                href={PROVIDER_KEY_LINKS[provider].href}
                target="_blank"
                rel="noreferrer"
              >
                {PROVIDER_KEY_LINKS[provider].label}
                <ExternalLink className="size-3" />
              </a>
            )}
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
      </Modal>

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
                  <small>
                    {item.status === 'running'
                      ? `执行中 · 第 ${item.turn ?? 0} 轮`
                      : item.status === 'failed'
                        ? '上次执行失败'
                        : formatUpdated(item.updated)}
                  </small>
                </button>
                <button
                  className="agent-session-delete"
                  onClick={() => void deleteSession(item.id)}
                  title="删除会话"
                  aria-label="删除会话"
                >
                  <Trash2 className="size-3.5" />
                </button>
                {tasks.filter(task => task.sessionId === item.id && ['failed', 'paused', 'cancelled', 'completed'].includes(task.status)).slice(0, 1).map(task => (
                  <span className="agent-session-actions" key={task.id}>
                    {['failed', 'paused', 'cancelled'].includes(task.status) && (
                      <button className="agent-session-action" onClick={() => void resumeTask(task.id)} title="从 checkpoint 恢复">继续</button>
                    )}
                    {task.status === 'completed' && (
                      <>
                        <button className="agent-session-action" onClick={() => void showTaskReport(task.id)} title="查看任务报告">报告</button>
                        <button className="agent-session-action" onClick={() => void rollbackTask(task.id)} title="回滚 Agent 修改">回滚</button>
                      </>
                    )}
                  </span>
                ))}
              </div>
            ))}
          </div>
        </aside>
      )}

      {pendingConfirm && (
        <div className="agent-confirm-overlay">
          <div
            className="agent-confirm-dialog"
            role="dialog"
            aria-modal="true"
            aria-labelledby="agent-confirm-title"
          >
            <header>
              <Pencil className="size-4" />
              <strong id="agent-confirm-title">Agent 请求修改项目</strong>
            </header>
            <p>
              工具 <code>{pendingConfirm.tool}</code>{' '}
              想要修改项目文件。确认后才会写入。
            </p>
            {pendingConfirm.diff ? (
              <div className="agent-confirm-diff">
                <div className="agent-confirm-diff-path">
                  {pendingConfirm.diff.path}
                  {pendingConfirm.diff.mode === 'create' && <em>新建</em>}
                  {pendingConfirm.diff.mode === 'delete' && <em>删除</em>}
                </div>
                {pendingConfirm.diff.mode === 'create' && (
                  <pre className="agent-diff-added">
                    {pendingConfirm.diff.content}
                  </pre>
                )}
                {pendingConfirm.diff.mode === 'delete' && (
                  <pre className="agent-diff-removed">（将删除此文件）</pre>
                )}
                {!pendingConfirm.diff.mode &&
                  (pendingConfirm.diff.hunks ?? []).map((hunk, index) => (
                    <div className="agent-diff-hunk" key={index}>
                      <pre className="agent-diff-removed">{hunk.old}</pre>
                      <pre className="agent-diff-added">{hunk.new}</pre>
                    </div>
                  ))}
              </div>
            ) : (
              pendingConfirm.args && (
                <pre className="agent-confirm-args">{pendingConfirm.args}</pre>
              )
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

      {slashDialog && (
        <div className="agent-confirm-overlay">
          <div className="agent-confirm-dialog" role="dialog" aria-modal="true">
            <header>
              <Slash className="size-4" />
              <strong>
                {slashDialog === 'compress'
                  ? '压缩上下文'
                  : slashDialog === 'goal'
                    ? '设置持续目标'
                    : '开始设定计划'}
              </strong>
            </header>
            {slashDialog === 'compress' ? (
              <div className="agent-slash-compress">
                <div className="agent-slash-progress">
                  <div
                    className="agent-slash-progress-bar"
                    style={{ width: `${contextUsage}%` }}
                  />
                </div>
                <p>
                  当前聊天上下文已使用约 <strong>{contextUsage}%</strong>。
                  超过预算时会自动压缩较早的工具结果。
                </p>
              </div>
            ) : slashDialog === 'goal' ? (
              <label className="grid gap-1.5 text-xs font-medium text-slate-600">
                目标描述
                <textarea
                  className="agent-slash-input"
                  rows={3}
                  value={goalText}
                  onChange={event => setGoalText(event.target.value)}
                  placeholder="例如：始终遵循项目约定，修改前先验证"
                />
              </label>
            ) : (
              <label className="grid gap-1.5 text-xs font-medium text-slate-600">
                计划要点
                <textarea
                  className="agent-slash-input"
                  rows={3}
                  value={planText}
                  onChange={event => setPlanText(event.target.value)}
                  placeholder="例如：1. 读取结构 2. 定位实现 3. 修改 4. 验证"
                />
              </label>
            )}
            <footer>
              <button
                className="secondary-button"
                onClick={() => setSlashDialog('')}
              >
                关闭
              </button>
              {slashDialog !== 'compress' && (
                <button
                  className="primary-button"
                  disabled={
                    slashDialog === 'goal' ? !goalText.trim() : !planText.trim()
                  }
                  onClick={() => {
                    if (slashDialog === 'goal' && goalText.trim()) {
                      setPrompt(`请始终遵循以下目标：${goalText.trim()}`)
                    } else if (slashDialog === 'plan' && planText.trim()) {
                      setPrompt(`请按以下计划执行：\n${planText.trim()}`)
                    }
                    setSlashDialog('')
                    setGoalText('')
                    setPlanText('')
                    promptRef.current?.focus()
                  }}
                >
                  确定
                </button>
              )}
            </footer>
          </div>
        </div>
      )}

      <DirectoryPicker
        open={filePickerOpen}
        multiple={false}
        priority
        includeFiles
        selectionMode={filePickerMode}
        extensions={fileExtensions}
        onModeChange={mode => {
          setFilePickerMode(mode)
        }}
        onExtensionsChange={setFileExtensions}
        onClose={() => setFilePickerOpen(false)}
        onSelect={paths => {
          const path = paths[0]
          if (!path) return
          const target = filePickerMode === 'extension'
            ? `目录 ${path} 中所有 ${fileExtensions.trim() || '匹配格式'} 文件`
            : filePickerMode === 'file'
              ? `文件：${path}`
              : `目录：${path}`
          setPrompt(`请先查看${target}，基于其内容理解后继续。`)
          setFilePickerOpen(false)
          promptRef.current?.focus()
        }}
      />
    </section>
  )
}

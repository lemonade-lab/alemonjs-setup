import { useEffect, useState, type ReactNode } from 'react'
import { Settings } from 'lucide-react'

type Props = {
  busy: boolean
  content: string
  toolbar?: ReactNode
  onChange: (content: string) => void
  onSave: (content: string) => Promise<boolean>
}
type Values = Record<string, string>
const empty: Values = {
  port: '',
  serverPort: '',
  input: '',
  login: '',
  url: '',
  fullReceive: '',
  masterID: '',
  masterKey: '',
  botID: '',
  botKey: '',
  disabledRegular: '',
  disabledSelects: '',
  disabledUserID: '',
  disabledUserKey: '',
  redirectRegular: '',
  redirectTarget: '',
  mappingRegular: '',
  mappingTarget: '',
  repeatedEventTime: '',
  repeatedUserTime: '',
  apps: '',
  cbpTimeout: '',
  cbpReconnect: '',
  cbpHeartbeat: '',
  cbpHealthCheck: '',
  cbpUserAgent: '',
  cbpDeviceID: '',
  cbpFullReceive: ''
}
const managed = new Set([
  'port',
  'serverPort',
  'input',
  'login',
  'url',
  'is_full_receive',
  'master_id',
  'master_key',
  'bot_id',
  'bot_key',
  'disabled_text_regular',
  'disabled_selects',
  'disabled_user_id',
  'disabled_user_key',
  'redirect_text_regular',
  'redirect_text_target',
  'mapping_text',
  'processor',
  'apps',
  'cbp'
])
const quote = (value: string) => `'${value.replace(/'/g, "''")}'`
const clean = (value: string) =>
  value
    .trim()
    .replace(/^['"]|['"]$/g, '')
    .replace(/''/g, "'")
const mapLines = (key: string, values: string) =>
  values.trim()
    ? [
        `${key}:`,
        ...values.split(',').map(value => `  ${quote(value.trim())}: true`)
      ]
    : []
const scalar = (source: string, key: string) =>
  clean(source.match(new RegExp(`^${key}:\\s*(.+)$`, 'm'))?.[1] ?? '')
const nested = (source: string, parent: string, key: string) => {
  const block =
    source.match(
      new RegExp(`^\\s*${parent}:\\s*\\n([\\s\\S]*?)(?=^[^ \\t]|$)`, 'm')
    )?.[1] ?? ''
  return clean(block.match(new RegExp(`^\\s+${key}:\\s*(.+)$`, 'm'))?.[1] ?? '')
}
const mapped = (source: string, key: string) => {
  const block =
    source.match(
      new RegExp(`^${key}:\\s*\\n([\\s\\S]*?)(?=^[^ \\t]|$)`, 'm')
    )?.[1] ?? ''
  const values: string[] = []
  for (const line of block.split('\n')) {
    const match = line.match(
      /^\s+(?:-\s+)?['"]?([^:'"]+)['"]?\s*(?::\s*true)?\s*$/
    )
    if (match) values.push(clean(match[1]))
  }
  return values.join(', ')
}
function readValues(source: string): Values {
  const values = { ...empty }
  const map: Record<string, string> = {
    port: 'port',
    serverPort: 'serverPort',
    input: 'input',
    login: 'login',
    url: 'url',
    disabled_text_regular: 'disabledRegular',
    redirect_text_regular: 'redirectRegular',
    redirect_text_target: 'redirectTarget'
  }
  Object.entries(map).forEach(([yaml, field]) => {
    values[field] = scalar(source, yaml)
  })
  values.fullReceive = scalar(source, 'is_full_receive')
  values.masterID = mapped(source, 'master_id')
  values.masterKey = mapped(source, 'master_key')
  values.botID = mapped(source, 'bot_id')
  values.botKey = mapped(source, 'bot_key')
  values.disabledSelects = mapped(source, 'disabled_selects')
  values.disabledUserID = mapped(source, 'disabled_user_id')
  values.disabledUserKey = mapped(source, 'disabled_user_key')
  values.apps = mapped(source, 'apps')
  values.mappingRegular = nested(source, 'mapping_text', 'regular')
  values.mappingTarget = nested(source, 'mapping_text', 'target')
  values.repeatedEventTime = nested(source, 'processor', 'repeated_event_time')
  values.repeatedUserTime = nested(source, 'processor', 'repeated_user_time')
  values.cbpTimeout = nested(source, 'cbp', 'timeout')
  values.cbpReconnect = nested(source, 'cbp', 'reconnectInterval')
  values.cbpHeartbeat = nested(source, 'cbp', 'heartbeatInterval')
  values.cbpHealthCheck = nested(source, 'cbp', 'healthCheckInterval')
  values.cbpUserAgent = nested(source, 'headers', 'user-agent')
  values.cbpDeviceID = nested(source, 'headers', 'x-device-id')
  values.cbpFullReceive = nested(source, 'headers', 'x-full-receive')
  return values
}
function toYaml(values: Values) {
  const lines: string[] = []
  const add = (key: string, value: string) => {
    if (value.trim()) lines.push(`${key}: ${quote(value.trim())}`)
  }
  add('port', values.port)
  add('serverPort', values.serverPort)
  add('input', values.input)
  add('login', values.login)
  add('url', values.url)
  if (values.fullReceive) lines.push(`is_full_receive: ${values.fullReceive}`)
  lines.push(
    ...mapLines('master_id', values.masterID),
    ...mapLines('master_key', values.masterKey),
    ...mapLines('bot_id', values.botID),
    ...mapLines('bot_key', values.botKey)
  )
  add('disabled_text_regular', values.disabledRegular)
  lines.push(
    ...mapLines('disabled_selects', values.disabledSelects),
    ...mapLines('disabled_user_id', values.disabledUserID),
    ...mapLines('disabled_user_key', values.disabledUserKey)
  )
  add('redirect_text_regular', values.redirectRegular)
  add('redirect_text_target', values.redirectTarget)
  if (values.mappingRegular.trim() && values.mappingTarget.trim())
    lines.push(
      'mapping_text:',
      `  - regular: ${quote(values.mappingRegular.trim())}`,
      `    target: ${quote(values.mappingTarget.trim())}`
    )
  if (values.repeatedEventTime.trim() || values.repeatedUserTime.trim()) {
    lines.push('processor:')
    if (values.repeatedEventTime.trim())
      lines.push(`  repeated_event_time: ${values.repeatedEventTime.trim()}`)
    if (values.repeatedUserTime.trim())
      lines.push(`  repeated_user_time: ${values.repeatedUserTime.trim()}`)
  }
  if (values.apps.trim()) lines.push(...mapLines('apps', values.apps))
  const cbp = [
    ['timeout', values.cbpTimeout],
    ['reconnectInterval', values.cbpReconnect],
    ['heartbeatInterval', values.cbpHeartbeat],
    ['healthCheckInterval', values.cbpHealthCheck]
  ] as const
  if (
    cbp.some(([, value]) => value.trim()) ||
    values.cbpUserAgent.trim() ||
    values.cbpDeviceID.trim() ||
    values.cbpFullReceive
  ) {
    lines.push('cbp:')
    cbp.forEach(([key, value]) => {
      if (value.trim()) lines.push(`  ${key}: ${value.trim()}`)
    })
    if (
      values.cbpUserAgent.trim() ||
      values.cbpDeviceID.trim() ||
      values.cbpFullReceive
    ) {
      lines.push('  headers:')
      if (values.cbpUserAgent.trim())
        lines.push(`    user-agent: ${quote(values.cbpUserAgent.trim())}`)
      if (values.cbpDeviceID.trim())
        lines.push(`    x-device-id: ${quote(values.cbpDeviceID.trim())}`)
      if (values.cbpFullReceive)
        lines.push(`    x-full-receive: ${quote(values.cbpFullReceive)}`)
    }
  }
  return lines.length ? `${lines.join('\n')}\n` : ''
}
function mergeConfig(existing: string, generated: string) {
  const normalized = /^\{\s*\}$/.test(existing.trim()) ? '' : existing
  const lines = normalized.replace(/\r/g, '').split('\n')
  const kept: string[] = []
  for (let index = 0; index < lines.length;) {
    const match = lines[index].match(/^([^ \t#][^:]*):/)
    if (!match || !managed.has(match[1])) {
      kept.push(lines[index++])
      continue
    }
    index++
    while (
      index < lines.length &&
      (/^\s/.test(lines[index]) || lines[index].trim() === '')
    )
      index++
  }
  while (kept.length && !kept[kept.length - 1].trim()) kept.pop()
  return `${kept.length && generated ? `${kept.join('\n')}\n\n` : kept.join('\n')}${generated}`
}

export function RobotConfigForm({
  busy,
  content,
  toolbar,
  onChange,
  onSave
}: Props) {
  const [values, setValues] = useState<Values>(empty)
  useEffect(() => {
    setValues(readValues(content))
  }, [content])
  // Redux's YAML draft is the shared editing source for both modes.
  const set = (key: string, value: string) => {
    const next = { ...values, [key]: value }
    setValues(next)
    onChange(mergeConfig(content, toYaml(next)))
  }
  const inputClass =
    'min-h-9 w-full rounded-md border border-slate-300 bg-white px-2.5 text-sm font-normal text-slate-800 outline-none transition focus:border-brand-600 focus:ring-2 focus:ring-brand-100'
  const labelClass = 'grid gap-1 text-xs font-semibold text-slate-600'
  const field = (key: string, label: string, hint = '', title = '') => (
    <label className={labelClass} key={key} title={title || undefined}>
      {label}
      <input
        className={inputClass}
        value={values[key]}
        onChange={event => set(key, event.target.value)}
        placeholder={hint}
      />
    </label>
  )
  const group = (
    name: string,
    note: string,
    children: ReactNode,
    open = false
  ) => (
    <details
      className="group overflow-hidden rounded-xl border border-slate-200"
      open={open}
    >
      <summary className="flex min-h-12 cursor-pointer list-none items-center gap-2 px-3 text-sm font-semibold text-slate-700 marker:content-none [&::-webkit-details-marker]:hidden">
        <strong>{name}</strong>
        <span className="ml-auto text-[11px] font-semibold text-slate-400">
          {note}
        </span>
        <i className="text-lg font-normal text-slate-400 transition-transform group-open:rotate-90">
          ›
        </i>
      </summary>
      <div className="grid grid-cols-2 gap-3 border-t border-slate-100 p-3 max-sm:grid-cols-1">
        {children}
      </div>
    </details>
  )
  const save = () => onSave(mergeConfig(content, toYaml(values)))
  return (
    <section className="grid max-w-190 gap-4">
      <header className="workspace-page-header flex items-center justify-between gap-4">
        <div className="flex min-w-0 items-center gap-3">
          <span className="workspace-page-header-icon">
            <Settings className="size-4" />
          </span>
          <div className="workspace-page-header-meta">
            <strong>机器人配置</strong>
            <small>管理当前机器人的运行与连接参数</small>
          </div>
          {toolbar}
        </div>
        <button
          className="inline-flex min-h-9 items-center justify-center rounded-md bg-brand-600 px-3.5 text-xs font-semibold text-white transition hover:bg-brand-700 disabled:cursor-not-allowed disabled:opacity-50"
          disabled={busy}
          onClick={() => void save()}
        >
          {busy ? '保存中' : '保存'}
        </button>
      </header>
      {group(
        '常规运行',
        '常用',
        <>
          {field(
            'port',
            '运行端口',
            '17117',
            '机器人自身使用的端口；不确定时保持为空。'
          )}
          {field(
            'serverPort',
            '应用端口',
            '18110',
            '机器人应用对外提供服务的端口；不确定时保持为空。'
          )}
          {field(
            'input',
            '应用入口',
            'lib/index.js',
            '机器人启动时加载的文件。'
          )}
          {field(
            'login',
            '登录连接',
            '如 discord',
            '推荐在“运行”页直接选择已安装的平台。'
          )}
        </>,
        true
      )}
      {group(
        'CBP 运行',
        '常用',
        <>
          {field(
            'url',
            'CBP 地址',
            'ws://127.0.0.1:17117',
            '机器人连接到 CBP 服务的地址。'
          )}
          <label
            className={labelClass}
            title="是否接收所有 CBP 事件；不确定时保持不设置。"
          >
            全量接收
            <select
              className={inputClass}
              value={values.fullReceive}
              onChange={event => set('fullReceive', event.target.value)}
            >
              <option value="">不设置</option>
              <option value="true">开启</option>
              <option value="false">关闭</option>
            </select>
          </label>
        </>
      )}
      {group(
        '身份与权限',
        '按需',
        <>
          {field('masterID', '主人 ID', '多个用逗号分隔')}
          {field('masterKey', '主人 Key', '多个用逗号分隔')}
          {field('botID', '机器人 ID', '多个用逗号分隔')}
          {field('botKey', '机器人 Key', '多个用逗号分隔')}
        </>
      )}
      {group(
        '消息规则',
        '高级',
        <>
          {field('disabledRegular', '禁用文本正则', '/闭关')}
          {field(
            'disabledSelects',
            '禁用事件',
            'message.create, private.message.create'
          )}
          {field('disabledUserID', '禁用用户 ID', '多个用逗号分隔')}
          {field('disabledUserKey', '禁用用户 Key', '多个用逗号分隔')}
          {field('redirectRegular', '重定向正则', '^#')}
          {field('redirectTarget', '重定向目标', '/')}
          {field('mappingRegular', '映射匹配文本', '/帮助')}
          {field('mappingTarget', '映射替换文本', '/help')}
        </>
      )}
      {group(
        '运行与模块',
        '高级',
        <>
          {field('repeatedEventTime', '重复事件窗口（毫秒）', '60000')}
          {field('repeatedUserTime', '重复用户窗口（毫秒）', '1000')}
          {field('apps', '启用模块', 'alemonjs-openai, alemonjs-xianyu')}
        </>
      )}
      {group(
        'CBP 高级参数',
        '高级',
        <>
          {field('cbpTimeout', '连接超时（毫秒）', '180000')}
          {field('cbpReconnect', '重连间隔（毫秒）', '6000')}
          {field('cbpHeartbeat', '连接心跳（毫秒）', '18000')}
          {field('cbpHealthCheck', '连接健康检查（毫秒）', '30000')}
          {field('cbpUserAgent', 'User-Agent', 'platform')}
          {field('cbpDeviceID', '设备 ID', 'auto-generated')}
          <label className={labelClass}>
            CBP 全量接收
            <select
              className={inputClass}
              value={values.cbpFullReceive}
              onChange={event => set('cbpFullReceive', event.target.value)}
            >
              <option value="">不设置</option>
              <option value="1">开启</option>
              <option value="0">关闭</option>
            </select>
          </label>
        </>
      )}
    </section>
  )
}

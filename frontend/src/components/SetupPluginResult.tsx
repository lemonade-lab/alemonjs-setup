import { useMemo } from 'react'
import cn from 'classnames'
import { Loader2 } from 'lucide-react'

export type StatusLineKind = 'ok' | 'fail' | 'warn' | 'plain'
export type StatusLine = { kind: StatusLineKind; text: string }

// A line is structured when it starts with one of ✓/!/? followed by a single
// space. Leading whitespace is intentionally NOT trimmed, so indented lines
// (lists, tables) stay plain. A line starting with "\✓ "/"\! "/"\? " escapes
// the marker and renders as a plain line with the backslash removed.
const STATUS_PREFIX = /^([✓!?]) /
const STATUS_ESCAPE = /^\\([✓!?]) /

function parseStatusLine(raw: string): StatusLine {
  const line = raw.endsWith('\r') ? raw.slice(0, -1) : raw
  const escape = STATUS_ESCAPE.exec(line)
  if (escape) return { kind: 'plain', text: line.slice(1) }
  const match = STATUS_PREFIX.exec(line)
  if (match) {
    const kind =
      match[1] === '✓' ? 'ok' : match[1] === '!' ? 'fail' : 'warn'
    return { kind, text: line.slice(2) }
  }
  return { kind: 'plain', text: line }
}

function splitStatusLines(output: string): StatusLine[] {
  return output.split(/\r?\n/).map(parseStatusLine)
}

const cardChrome = {
  running:
    'border-sky-200 bg-sky-50/60 text-sky-800 dark:border-sky-900 dark:bg-sky-950/30 dark:text-sky-200',
  completed:
    'border-emerald-200 bg-emerald-50/60 text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-200',
  failed:
    'border-rose-200 bg-rose-50 text-rose-800 dark:border-rose-900 dark:bg-rose-950/30 dark:text-rose-200'
}

const rowColor = {
  ok: 'text-emerald-700 dark:text-emerald-300',
  fail: 'text-rose-700 dark:text-rose-300',
  warn: 'text-amber-700 dark:text-amber-300',
  plain: 'text-slate-700 dark:text-slate-300'
}

const rowSymbol = { ok: '✓', fail: '!', warn: '?', plain: '' }

function StatusRow({ line }: { line: StatusLine }) {
  return (
    <p
      className={cn(
        'grid grid-cols-[1.125rem_1fr] items-baseline gap-1.5 py-1 text-[11px] leading-5',
        rowColor[line.kind]
      )}
    >
      <span className="text-center font-bold" aria-hidden>
        {rowSymbol[line.kind]}
      </span>
      <span
        className={
          line.kind === 'plain' ? 'whitespace-pre-wrap font-mono' : undefined
        }
      >
        {line.text || ' '}
      </span>
    </p>
  )
}

function Skeleton() {
  return (
    <div className="grid gap-2 py-1">
      <div className="h-3 w-2/3 animate-pulse rounded bg-slate-200 dark:bg-slate-700" />
      <div className="h-3 w-1/2 animate-pulse rounded bg-slate-200 dark:bg-slate-700" />
      <div className="h-3 w-3/4 animate-pulse rounded bg-slate-200 dark:bg-slate-700" />
    </div>
  )
}

export function SetupPluginResult({
  status,
  output,
  error,
  onDismiss
}: {
  status: 'running' | 'completed' | 'failed'
  output?: string
  error?: string
  onDismiss?: () => void
}) {
  const lines = useMemo(() => splitStatusLines(output ?? ''), [output])
  // Plugins that emit no status markers keep the legacy plain-text card,
  // byte-for-byte identical to the previous <pre> rendering.
  const hasStructured = lines.some(line => line.kind !== 'plain')
  const title =
    status === 'running'
      ? '正在执行…'
      : status === 'failed'
        ? '操作未完成'
        : hasStructured
          ? '执行完成'
          : '操作完成'

  return (
    <section
      className={cn(
        'setup-plugin-result grid gap-2 rounded-xl border p-3 text-xs',
        cardChrome[status]
      )}
    >
      <header className="flex items-center justify-between gap-3">
        <strong className="flex items-center gap-1.5 text-sm font-semibold">
          {status === 'running' && (
            <Loader2 className="size-3.5 animate-spin" />
          )}
          {title}
        </strong>
        {status !== 'running' && onDismiss && (
          <button
            className="text-xs font-semibold underline underline-offset-2"
            onClick={onDismiss}
          >
            收起
          </button>
        )}
      </header>

      {status === 'failed' && error && (
        <div className="grid gap-1 rounded-md bg-rose-100/70 px-2 py-1.5 font-semibold dark:bg-rose-950/40">
          {error.split(/\r?\n/).map((text, index) => (
            <StatusRow key={index} line={{ kind: 'fail', text }} />
          ))}
        </div>
      )}

      {!hasStructured ? (
        <pre className="m-0 max-h-72 overflow-auto whitespace-pre-wrap rounded-lg bg-white/70 p-2 text-[11px] leading-5 text-slate-700 dark:bg-slate-950/50 dark:text-slate-200">
          {output ||
            (status === 'running' ? '正在等待插件返回结果…' : '插件未返回结果。')}
        </pre>
      ) : status === 'running' && !output ? (
        <Skeleton />
      ) : (
        <div className="grid">
          {lines.map((line, index) => (
            <StatusRow key={index} line={line} />
          ))}
          {status === 'running' && output && (
            <p className="py-1 text-slate-500 dark:text-slate-400">
              仍在执行…
            </p>
          )}
        </div>
      )}
    </section>
  )
}

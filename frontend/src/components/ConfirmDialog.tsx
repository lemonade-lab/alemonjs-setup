import { AlertTriangle, X } from 'lucide-react'
import { createPortal } from 'react-dom'

type Props = {
  open: boolean
  title: string
  message: string
  confirmLabel?: string
  cancelLabel?: string
  subtitle?: string
  busy?: boolean
  destructive?: boolean
  onCancel: () => void
  onConfirm: () => void
}

// Shared confirmation surface for any action that changes the local project.
// It deliberately uses the same geometry as the other application dialogs.
export function ConfirmDialog({ open, title, message, confirmLabel = '确认继续', cancelLabel = '取消', subtitle = '此操作会修改当前机器人项目或其运行状态。', busy, destructive = false, onCancel, onConfirm }: Props) {
  if (!open) return null
  const confirmClass = destructive
    ? 'border-red-700 bg-red-700 hover:border-red-800 hover:bg-red-800'
    : 'border-brand-600 bg-brand-600 hover:border-brand-700 hover:bg-brand-700'
  return createPortal(<div className="fixed inset-0 z-[95] flex items-center justify-center bg-slate-950/25 p-6" role="presentation" onMouseDown={onCancel}>
    <section className="grid w-full max-w-md gap-4 rounded-xl border border-slate-200 bg-white p-[18px] shadow-[0_20px_58px_rgb(15_23_42/0.22)]" role="dialog" aria-modal="true" aria-label={title} onMouseDown={(event) => event.stopPropagation()}>
      <header className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-2.5"><i className="inline-flex size-[34px] items-center justify-center rounded-lg bg-orange-50 text-orange-700"><AlertTriangle className="size-[17px]" /></i><div className="grid min-w-0 gap-0.5"><strong className="text-sm text-ink-950">{title}</strong><small className="text-[11px] text-slate-400">{subtitle}</small></div><button className="inline-flex size-8 items-center justify-center rounded-md border border-slate-300 bg-white text-slate-600 transition hover:border-slate-400 hover:bg-slate-50" onClick={onCancel} aria-label="关闭确认"><X className="size-4" /></button></header>
      <p className="m-0 whitespace-pre-line text-xs leading-5 text-slate-500">{message}</p>
      <footer className="flex justify-end gap-2"><button className="inline-flex min-h-8 items-center justify-center rounded-md border border-slate-300 bg-white px-3 text-xs font-semibold text-slate-600 transition hover:border-slate-400 hover:bg-slate-50" onClick={onCancel}>{cancelLabel}</button><button className={`inline-flex min-h-9 items-center justify-center rounded-md border px-3.5 text-xs font-semibold text-white transition disabled:cursor-not-allowed disabled:opacity-50 ${confirmClass}`} disabled={busy} onClick={onConfirm}>{busy ? '处理中…' : confirmLabel}</button></footer>
    </section>
  </div>, document.body)
}

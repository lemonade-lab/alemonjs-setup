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
  onCancel: () => void
  onConfirm: () => void
}

// Shared confirmation surface for any action that changes the local project.
// It deliberately uses the same geometry as the other application dialogs.
export function ConfirmDialog({ open, title, message, confirmLabel = '确认继续', cancelLabel = '取消', subtitle = '此操作会修改当前机器人项目或其运行状态。', busy, onCancel, onConfirm }: Props) {
  if (!open) return null
  return createPortal(<div className="app-dialog-backdrop" role="presentation" onMouseDown={onCancel}>
    <section className="app-dialog" role="dialog" aria-modal="true" aria-label={title} onMouseDown={(event) => event.stopPropagation()}>
      <header><i><AlertTriangle /></i><div><strong>{title}</strong><small>{subtitle}</small></div><button className="icon-button" onClick={onCancel} aria-label="关闭确认"><X /></button></header>
      <p>{message}</p>
      <footer><button className="secondary-button" onClick={onCancel}>{cancelLabel}</button><button className="primary-button" disabled={busy} onClick={onConfirm}>{busy ? '处理中…' : confirmLabel}</button></footer>
    </section>
  </div>, document.body)
}

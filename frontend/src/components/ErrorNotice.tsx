import { X } from 'lucide-react'

// Errors are transient guidance, not a second permanent page. Every instance
// therefore has the same close affordance and remains readable to screen readers.
export function ErrorNotice({ message, onClose }: { message: string; onClose: () => void }) {
  return <aside className="error" role="alert" aria-live="assertive"><span>{message}</span><button onClick={onClose} aria-label="关闭错误提示"><X /></button></aside>
}

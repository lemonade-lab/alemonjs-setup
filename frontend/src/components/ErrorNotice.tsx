import { X } from 'lucide-react'

// Errors are transient guidance, not a second permanent page. Every instance
// therefore has the same close affordance and remains readable to screen readers.
export function ErrorNotice({ message, onClose }: { message: string; onClose: () => void }) {
  return <aside className="fixed left-1/2 top-[18px] z-[100] flex w-[min(calc(100vw-32px),760px)] -translate-x-1/2 items-start gap-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2.5 text-sm font-medium text-red-800 shadow-[0_12px_32px_rgb(127_29_29_/_0.18)]" role="alert" aria-live="assertive"><span className="min-w-0 break-words">{message}</span><button className="-mr-1 -mt-0.5 inline-flex size-7 shrink-0 items-center justify-center rounded-md text-red-700 transition hover:bg-red-100 focus:outline-none focus:ring-2 focus:ring-red-300" onClick={onClose} aria-label="关闭错误提示"><X className="size-4" /></button></aside>
}

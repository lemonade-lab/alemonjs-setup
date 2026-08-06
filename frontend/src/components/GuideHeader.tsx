import { ArrowLeft, X } from 'lucide-react'
import { SetupUpdateButton } from './SetupUpdateButton'
import { ThemeToggle } from './ThemeToggle'

type GuideHeaderProps = { onBack: () => void; onClose: () => void; showBack: boolean }

export function GuideHeader({ onBack, onClose, showBack }: GuideHeaderProps) {
  return (
    <header className="relative flex h-[52px] shrink-0 items-center justify-between border-b border-slate-100 px-[18px]">
      <div className="flex items-center gap-2.5">{showBack && <button className="icon-button size-8 p-0" onClick={onBack} aria-label="返回" title="返回"><ArrowLeft className="size-4" /></button>}<a className="text-[0.84rem] font-extrabold tracking-[-0.01em] text-ink-950 no-underline transition hover:text-brand-600" href="https://alemonjs.com/" target="_blank" rel="noreferrer">ALEMONJS</a><SetupUpdateButton /><ThemeToggle /></div>
      <button
        className="inline-flex size-8 items-center justify-center rounded-md text-slate-500 transition hover:bg-slate-100 hover:text-slate-700 focus:outline-none focus:ring-2 focus:ring-brand-200"
        aria-label="关闭引导"
        title="关闭引导"
        onClick={onClose}
      >
        <X className="size-4" />
      </button>
    </header>
  )
}

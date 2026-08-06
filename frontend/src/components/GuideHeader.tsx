import { ArrowLeft, X } from 'lucide-react'
import { SetupUpdateButton } from './SetupUpdateButton'
import { ThemeToggle } from './ThemeToggle'
import { Button } from './Button'

type GuideHeaderProps = {
  onBack: () => void
  onClose: () => void
  showBack: boolean
}

export function GuideHeader({ onBack, onClose, showBack }: GuideHeaderProps) {
  return (
    <header className="relative flex h-[52px] shrink-0 items-center justify-between border-b border-slate-100 px-[18px]">
      <div className="flex items-center gap-2.5">
        {showBack && (
          <Button
            variant="icon"
            onClick={onBack}
            aria-label="返回"
            title="返回"
          >
            <ArrowLeft className="size-4" />
          </Button>
        )}
        <a
          className="text-[0.84rem] font-extrabold tracking-[-0.01em] text-ink-950 no-underline transition hover:text-brand-600"
          href="https://alemonjs.com/"
          target="_blank"
          rel="noreferrer"
        >
          ALemonX
        </a>
        <SetupUpdateButton />
        <ThemeToggle />
      </div>
      <Button
        variant="icon"
        className="border-transparent bg-transparent focus:outline-none focus:ring-2 focus:ring-brand-200"
        aria-label="关闭引导"
        title="关闭引导"
        onClick={onClose}
      >
        <X className="size-4" />
      </Button>
    </header>
  )
}

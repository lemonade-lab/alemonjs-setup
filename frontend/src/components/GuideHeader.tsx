import { ArrowLeft, X } from 'lucide-react'
import { SetupUpdateButton } from './SetupUpdateButton'

type GuideHeaderProps = { onBack: () => void; onClose: () => void; showBack: boolean }

export function GuideHeader({ onBack, onClose, showBack }: GuideHeaderProps) {
  return (
    <header className="guide-bar">
      <div className="guide-header-left">{showBack && <button className="top-back icon-button" onClick={onBack} aria-label="返回" title="返回"><ArrowLeft /></button>}<a className="workspace-name" href="https://alemonjs.com/" target="_blank" rel="noreferrer">ALEMONJS</a><SetupUpdateButton /></div>
      <button
        className="window-close"
        aria-label="关闭引导"
        title="关闭引导"
        onClick={onClose}
      >
        <X />
      </button>
    </header>
  )
}

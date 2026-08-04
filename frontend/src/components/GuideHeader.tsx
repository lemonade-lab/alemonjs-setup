type GuideHeaderProps = { onBack: () => void; onClose: () => void }

export function GuideHeader({ onBack, onClose }: GuideHeaderProps) {
  return (
    <header className="guide-bar">
      <button className="top-back" onClick={onBack}>
        ← 返回
      </button>
      <button
        className="window-close"
        aria-label="关闭引导"
        title="关闭引导"
        onClick={onClose}
      >
        ×
      </button>
    </header>
  )
}

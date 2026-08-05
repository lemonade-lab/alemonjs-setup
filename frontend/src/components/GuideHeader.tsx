type GuideHeaderProps = { onBack: () => void; onClose: () => void; showBack: boolean }

const links = [
  ['⌂', '官网', 'https://alemonjs.com/'],
]

export function HeaderLinks() {
  return <nav className="header-links" aria-label="AlemonJS 快捷链接">{links.map(([icon, label, href]) => <a href={href} key={label} target="_blank" rel="noreferrer" aria-label={label} title={label}>{icon}</a>)}</nav>
}

export function GuideHeader({ onBack, onClose, showBack }: GuideHeaderProps) {
  return (
    <header className="guide-bar">
      {showBack ? <button className="top-back" onClick={onBack}>← 返回</button> : <span />}
      <HeaderLinks />
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

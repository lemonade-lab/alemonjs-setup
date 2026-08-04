type Check = { id: string; name: string; suggestion: string }

type Props = { check: Check; onClose: () => void }

const links: Record<string, Array<{ label: string; note: string; href: string }>> = {
  node: [
    { label: 'Node.js LTS（推荐）', note: '官方安装页会自动提供适合当前系统的安装包。', href: 'https://nodejs.org/en/download' },
    { label: '全部 Node.js 版本', note: '需要指定旧版本或特殊架构时使用。', href: 'https://nodejs.org/dist/' },
  ],
  git: [
    { label: 'Git 官方下载', note: '选择 Windows、macOS 或 Linux 的对应安装包。', href: 'https://git-scm.com/downloads' },
  ],
  docker: [
    { label: 'Docker Desktop', note: 'Docker 官方会按当前系统提供安装包。', href: 'https://www.docker.com/products/docker-desktop/' },
  ],
}

export function EnvironmentFixDialog({ check, onClose }: Props) {
  const options = links[check.id] ?? []
  return <div className="environment-modal-backdrop" role="presentation" onMouseDown={onClose}><section className="environment-modal" role="dialog" aria-modal="true" aria-labelledby="environment-fix-title" onMouseDown={(event) => event.stopPropagation()}><button className="environment-modal-close" onClick={onClose} aria-label="关闭">×</button><p className="eyebrow">环境修复</p><h2 id="environment-fix-title">安装 {check.name}</h2><p>{check.suggestion || '请选择官方安装包，安装完成后回到这里重新检查。'}</p><div className="environment-options">{options.map((option) => <a href={option.href} target="_blank" rel="noreferrer" key={option.href}><strong>{option.label}</strong><small>{option.note}</small></a>)}</div><button className="environment-modal-done" onClick={onClose}>安装完成后重新检查</button></section></div>
}

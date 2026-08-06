import { ArrowUpRight, X } from 'lucide-react'

type Check = { id: string; name: string; suggestion: string }

type Props = { check: Check; onClose: () => void }

const links: Record<
  string,
  Array<{ label: string; note: string; href: string }>
> = {
  node: [
    {
      label: 'Node.js LTS（推荐）',
      note: '官方安装页会自动提供适合当前系统的安装包。',
      href: 'https://nodejs.org/en/download'
    },
    {
      label: '全部 Node.js 版本',
      note: '需要指定旧版本或特殊架构时使用。',
      href: 'https://nodejs.org/dist/'
    }
  ],
  git: [
    {
      label: 'Git 官方下载',
      note: '选择 Windows、macOS 或 Linux 的对应安装包。',
      href: 'https://git-scm.com/downloads'
    }
  ],
  docker: [
    {
      label: 'Docker Desktop',
      note: 'Docker 官方会按当前系统提供安装包。',
      href: 'https://www.docker.com/products/docker-desktop/'
    }
  ]
}

export function EnvironmentFixDialog({ check, onClose }: Props) {
  const options = links[check.id] ?? []
  return (
    <div
      className="fixed inset-0 z-[200] flex items-center justify-center bg-slate-950/35 p-4"
      role="presentation"
      onMouseDown={onClose}
    >
      <section
        className="relative w-full max-w-[440px] rounded-xl border border-slate-200 bg-white p-6 shadow-[0_24px_60px_rgb(15_23_42_/_0.24)]"
        role="dialog"
        aria-modal="true"
        aria-labelledby="environment-fix-title"
        onMouseDown={event => event.stopPropagation()}
      >
        <button
          className="absolute right-3 top-3 inline-flex size-8 items-center justify-center rounded-md text-slate-500 transition hover:bg-slate-100 hover:text-slate-700 focus:outline-none focus:ring-2 focus:ring-brand-200"
          onClick={onClose}
          aria-label="关闭"
        >
          <X className="size-4" />
        </button>
        <h2
          id="environment-fix-title"
          className="mr-9 text-base font-semibold text-ink-950"
        >
          安装 {check.name}
        </h2>
        <p className="mt-2 text-sm leading-6 text-slate-500">
          {check.suggestion || '请选择官方安装包，完成后返回环境面板重新检查。'}
        </p>
        <div className="mt-5 grid gap-2">
          {options.map(option => (
            <a
              className="flex items-center justify-between gap-3 rounded-lg border border-slate-200 p-3 text-brand-700 transition hover:border-brand-200 hover:bg-brand-50"
              href={option.href}
              target="_blank"
              rel="noreferrer"
              key={option.href}
            >
              <span className="grid min-w-0 gap-1">
                <strong className="text-sm font-semibold">
                  {option.label}
                </strong>
                <small className="text-xs leading-5 text-slate-500">
                  {option.note}
                </small>
              </span>
              <ArrowUpRight className="size-4 shrink-0 text-slate-400" />
            </a>
          ))}
        </div>
        <footer className="mt-5 flex justify-end">
          <button className="primary-button" onClick={onClose}>
            完成
          </button>
        </footer>
      </section>
    </div>
  )
}

import type { Report } from './types'

type Props = {
  title: string
  report: Report | null
  checking: boolean
  onCheck: () => void
}

export function EnvironmentCheckPanel({
  title,
  report,
  checking,
  onCheck
}: Props) {
  const ready = Boolean(report?.ready)

  return (
    <section className="mx-auto grid w-full max-w-160 gap-5 pt-8">
      <header className="flex items-start justify-between gap-5 rounded-2xl border border-slate-200 bg-white p-5 shadow-[0_12px_30px_rgb(28_26_23/0.06)]">
        <div className="flex items-start gap-3">
          <i
            className={`mt-0.5 inline-flex h-9 w-9 items-center justify-center rounded-xl border text-base font-extrabold not-italic ${ready ? 'border-emerald-400 bg-transparent text-emerald-700' : 'border-amber-300 bg-amber-100 text-amber-700'}`}
          >
            {ready ? '✓' : '!'}
          </i>
          <div>
            <h1 className="m-0 text-xl font-bold tracking-tight text-slate-800">
              {title}
            </h1>
            <p className="mt-1.5 text-sm text-slate-500">
              {checking || !report
                ? '正在检查所需工具…'
                : ready
                  ? '环境已就绪，可以继续。'
                  : '有项目需要先处理。'}
            </p>
          </div>
        </div>
        <button
          className="rounded-lg border border-slate-200 bg-white px-3 py-2 text-xs font-bold text-slate-600 transition hover:border-brand-200 hover:text-brand-600 disabled:cursor-wait disabled:opacity-50"
          onClick={onCheck}
          disabled={checking}
        >
          {checking ? '检查中' : '重新检查'}
        </button>
      </header>
      {checking || !report ? (
        <div className="flex min-h-32 items-center justify-center rounded-2xl border border-dashed border-slate-200 bg-slate-50">
          <span className="checking-indicator" />
        </div>
      ) : (
        <div className="grid gap-2 sm:grid-cols-2">
          {report.checks.map(check => (
            <article
              className={`flex min-h-20 items-start gap-3 rounded-xl border p-4 ${check.status === 'ready' ? 'border-emerald-300 bg-transparent' : 'border-amber-200 bg-amber-50'}`}
              key={check.id}
            >
              <i
                className={`inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-full border text-xs font-extrabold not-italic ${check.status === 'ready' ? 'border-emerald-500 bg-transparent text-emerald-700' : 'border-amber-500 bg-amber-500 text-white'}`}
              >
                {check.status === 'ready' ? '✓' : '!'}
              </i>
              <div className="min-w-0">
                <strong className="block text-sm font-bold text-slate-700">
                  {check.name}
                </strong>
                <span className="mt-1 block wrap-break-word text-xs leading-5 text-slate-500">
                  {check.detail}
                </span>
                {check.status !== 'ready' && check.suggestion && (
                  <small className="mt-1 block text-xs leading-5 text-amber-700">
                    {check.suggestion}
                  </small>
                )}
              </div>
            </article>
          ))}
        </div>
      )}
    </section>
  )
}

import { useCallback, useEffect, useState } from 'react'

type Project = { id: string; name: string; path: string }
type Policy = { projectRoot: string; mode: string; autoAllowed: boolean }
type Overview = { metrics: { incidents: number; openTodos: number; maintenanceRuns: number; resolved: number; rollbacks: number }; policies: Policy[]; paused: boolean; nodeId?: string }

export function OpsOverview({ projects, onOpenProject }: { projects: Project[]; onOpenProject: (id: string) => void }) {
  const [overview, setOverview] = useState<Overview | null>(null)
  const [busy, setBusy] = useState(false)
  const load = useCallback(async () => { const response = await fetch('/api/v1/ops/overview'); if (response.ok) setOverview(await response.json() as Overview) }, [])
  useEffect(() => {
    void load()
    const refresh = () => void load()
    window.addEventListener('alx:ops-changed', refresh)
    return () => window.removeEventListener('alx:ops-changed', refresh)
  }, [load])
  const control = async (action: 'pause' | 'resume' | 'emergency-stop') => { setBusy(true); try { await fetch(`/api/v1/ops/monitor/${action}`, { method: 'POST' }); await load() } finally { setBusy(false) } }
  return <section className="grid min-h-0 gap-4 overflow-auto p-5" aria-label="全局运维总览">
    <header className="flex flex-wrap items-center justify-between gap-3"><div><h1 className="text-xl font-semibold">全局运维总览</h1><p className="text-xs text-slate-500">统一查看所有受管机器人和自动维护状态。</p></div><div className="flex gap-2"><button className="secondary-button" disabled={busy} onClick={() => void control(overview?.paused ? 'resume' : 'pause')}>{overview?.paused ? '恢复自动维护' : '暂停自动维护'}</button><button className="secondary-button" disabled={busy} onClick={() => void control('emergency-stop')}>紧急停止全部</button></div></header>
    {overview && <div className="grid grid-cols-2 gap-2 md:grid-cols-5">{[['事件', overview.metrics.incidents], ['待办', overview.metrics.openTodos], ['维护中', overview.metrics.maintenanceRuns], ['已恢复', overview.metrics.resolved], ['回滚', overview.metrics.rollbacks]].map(([label, value]) => <div className="rounded-lg border border-slate-200 bg-white p-3" key={String(label)}><small className="text-xs text-slate-500">{label}</small><strong className="block text-lg">{value}</strong></div>)}</div>}
    <section className="rounded-lg border border-slate-200 bg-white p-4"><h2 className="mb-3 font-semibold">受管项目</h2><div className="grid gap-2">{projects.map(project => { const policy = overview?.policies.find(item => item.projectRoot === project.path); return <button className="flex items-center justify-between rounded-md border border-slate-100 p-3 text-left hover:bg-slate-50" key={project.id} onClick={() => onOpenProject(project.id)}><span><strong className="block text-sm">{project.name}</strong><small className="text-xs text-slate-500">{project.path}</small></span><span className="text-xs text-slate-500">{policy?.mode ?? 'observe'} · {policy?.autoAllowed ? '白名单' : '未授权'}</span></button> })}{projects.length === 0 && <p className="text-sm text-slate-500">暂无受管项目。</p>}</div></section>
    {overview?.nodeId && <p className="text-xs text-slate-400">当前执行节点：{overview.nodeId}</p>}
  </section>
}

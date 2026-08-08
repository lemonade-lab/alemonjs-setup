import { useEffect, useState } from 'react'
import { ChevronRight, Folder, Home } from 'lucide-react'

type DirectoryData = {
  path: string
  parent: string
  roots: string[]
  directories: Array<{ name: string; path: string }>
}

// FilePicker 是一个轻量目录选择器，用于 Agent 输入框的"选择目录或文件"。
// 它自包含（不依赖 Dashboard），通过 /api/v1/directories 拉取目录，展示
// 面包屑导航 + 目录列表，点击目录进入，确认返回路径。
export function FilePicker({
  initial,
  onSelect
}: {
  initial: string
  onSelect: (path: string) => void
}) {
  const [path, setPath] = useState(initial || '')
  const [data, setData] = useState<DirectoryData | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    const controller = new AbortController()
    const params = new URLSearchParams(path ? { path } : {})
    void fetch(`/api/v1/directories?${params}`, { signal: controller.signal })
      .then(async response => {
        if (!response.ok) {
          const body = (await response.json().catch(() => ({}))) as {
            error?: string
          }
          throw new Error(body.error || '目录无法读取。')
        }
        return response.json() as Promise<DirectoryData>
      })
      .then(result => {
        setData(result)
        setError('')
        if (!path) setPath(result.path)
      })
      .catch((reason: unknown) => {
        if (!(reason instanceof DOMException && reason.name === 'AbortError')) {
          setError(reason instanceof Error ? reason.message : '目录无法读取。')
        }
      })
    return () => controller.abort()
  }, [path])

  const breadcrumbs = data
    ? (() => {
        const parts = data.path.split('/').filter(Boolean)
        const crumbs: Array<{ name: string; path: string }> = []
        let current = ''
        for (const part of parts) {
          current += '/' + part
          crumbs.push({ name: part, path: current })
        }
        return crumbs
      })()
    : []

  return (
    <div className="file-picker">
      <div className="file-picker-path">
        <button
          className="file-picker-crumb"
          onClick={() => setPath(data?.roots[0] ?? '')}
          title="根目录"
        >
          <Home className="size-3.5" />
        </button>
        {breadcrumbs.map((crumb, index) => (
          <span className="file-picker-crumb-group" key={crumb.path}>
            <ChevronRight className="size-3 text-slate-400" />
            <button
              className="file-picker-crumb"
              onClick={() => setPath(crumb.path)}
            >
              {crumb.name}
            </button>
            {index === breadcrumbs.length - 1 && <span className="sr-only">当前</span>}
          </span>
        ))}
      </div>
      {error && <p className="file-picker-error">{error}</p>}
      <div className="file-picker-list">
        {(data?.directories ?? []).map(directory => (
          <button
            className="file-picker-item"
            key={directory.path}
            onClick={() => setPath(directory.path)}
            onDoubleClick={() => onSelect(directory.path)}
            title={directory.path}
          >
            <Folder className="size-4 shrink-0 text-amber-500" />
            <span className="min-w-0 flex-1 truncate">{directory.name}</span>
          </button>
        ))}
        {(data?.directories ?? []).length === 0 && (
          <p className="file-picker-empty">此目录没有子目录</p>
        )}
      </div>
      <footer className="file-picker-actions">
        <button
          className="secondary-button"
          onClick={() => onSelect(path)}
          disabled={!path}
        >
          选择当前目录
        </button>
      </footer>
    </div>
  )
}

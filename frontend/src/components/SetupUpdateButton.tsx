import { RefreshCw, X } from 'lucide-react'
import { useState } from 'react'
import { useLazySetupUpdateQuery } from '../store/workspaceApi'

export function SetupUpdateButton() {
  const [check, { data, isFetching, error }] = useLazySetupUpdateQuery()
  const [open, setOpen] = useState(false)
  return <div className="setup-update"><button className="setup-update-button" onClick={() => { setOpen(true); void check() }} disabled={isFetching} aria-label="检查应用更新" title={isFetching ? '正在检查更新' : '检查更新'}><RefreshCw /></button>{open && <section className="setup-update-popover"><header><strong>应用更新</strong><button onClick={() => setOpen(false)} aria-label="关闭更新提示"><X /></button></header>{isFetching ? <p>正在比对 GitHub 正式版本…</p> : error ? <p>暂时无法检查更新，请稍后重试。</p> : data?.available ? <><p>发现新版本 <b>{data.latest}</b><small>当前 {data.current}</small></p>{data.platformMatched && data.downloadUrl && <a className="primary-button" href={data.downloadUrl} target="_blank" rel="noreferrer">下载 {data.assetName}</a>}{data.releaseUrl && <a className="secondary-button" href={data.releaseUrl} target="_blank" rel="noreferrer">打开 GitHub 发布页</a>}</> : data ? <p>已是最新版本 <b>{data.current}</b>{data.releaseUrl && <a className="secondary-button" href={data.releaseUrl} target="_blank" rel="noreferrer">打开 GitHub 发布页</a>}</p> : null}</section>}</div>
}

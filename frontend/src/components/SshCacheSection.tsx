import React, { useEffect, useRef, useState } from 'react'
import { Eraser, FileText, Loader2, RefreshCw, Trash2 } from 'lucide-react'
import type { SSHCacheClearResult, SSHCacheInfo } from '../types'
import { api } from '../wailsApi'
import { errMsg } from '../utils/errors'
import { useActionDialogs } from '../hooks/useActionDialogs'

function formatCacheClearSummary(res: SSHCacheClearResult): string {
  const parts: string[] = []
  if (res.removedKnownHosts > 0) parts.push(`${res.removedKnownHosts} 条主机指纹`)
  if (res.clearedDirCaches > 0) parts.push(`${res.clearedDirCaches} 条目录缓存`)
  return parts.length > 0 ? `已清理 ${parts.join('与 ')}` : '未发现旧指纹缓存，已刷新会话缓存'
}

export const SshCacheSection: React.FC<{ onToast: (msg: string, type?: 'info' | 'error') => void }> = ({ onToast }) => {
  const [info, setInfo] = useState<SSHCacheInfo | null>(null)
  const [loading, setLoading] = useState(false)
  const [host, setHost] = useState('')
  const [port, setPort] = useState('')
  const [busyHost, setBusyHost] = useState(false)
  const [busyAll, setBusyAll] = useState(false)
  const busyHostRef = useRef(false)
  const busyAllRef = useRef(false)
  const { confirm, dialogNode } = useActionDialogs()

  const refresh = async () => {
    setLoading(true)
    try { setInfo(await api.getSshCacheInfo()) }
    catch (err) { onToast(errMsg(err), 'error') }
    finally { setLoading(false) }
  }

  useEffect(() => { void refresh() }, [])

  const handleClearHost = async () => {
    if (busyHostRef.current) return
    const h = host.trim()
    if (!h) return onToast('请输入要清理的主机地址或 IP', 'error')
    const rawPort = port.trim()
    let parsedPort = 0
    if (rawPort !== '') {
      if (!/^\d+$/.test(rawPort)) {
        return onToast('端口必须为 1–65535 之间的整数', 'error')
      }
      parsedPort = Number.parseInt(rawPort, 10)
      if (!Number.isInteger(parsedPort) || parsedPort < 1 || parsedPort > 65535) {
        return onToast('端口必须为 1–65535 之间的整数', 'error')
      }
    }
    busyHostRef.current = true
    setBusyHost(true)
    try {
      const res = await api.clearSshHostCache(h, parsedPort)
      onToast(`${h}：${formatCacheClearSummary(res)}`)
      setHost('')
      setPort('')
      await refresh()
    } catch (err) {
      onToast(errMsg(err), 'error')
    } finally {
      busyHostRef.current = false
      setBusyHost(false)
    }
  }

  const handleClearAll = async () => {
    if (busyAllRef.current) return
    const ok = await confirm({
      title: '清理全部主机指纹与缓存',
      message: '确定清理全部主机指纹与目录缓存吗？',
      confirmLabel: '清理全部',
    })
    if (!ok || busyAllRef.current) return
    busyAllRef.current = true
    setBusyAll(true)
    try {
      onToast(formatCacheClearSummary(await api.clearAllSshCache()))
      await refresh()
    } catch (err) {
      onToast(errMsg(err), 'error')
    } finally {
      busyAllRef.current = false
      setBusyAll(false)
    }
  }

  const onHostInputKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.nativeEvent.isComposing || e.keyCode === 229) return
    if (e.key === 'Enter') {
      e.preventDefault()
      if (!busyHostRef.current) void handleClearHost()
    }
  }

  return (
    <section className="space-y-2.5">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 text-xs font-semibold text-cyan-300 uppercase tracking-wider">
          <Eraser className="w-3.5 h-3.5" />主机指纹
        </div>
        <div className="flex items-center gap-2">
          <span className="text-[11px] px-2 py-0.5 rounded border bg-ag-elevated text-ag-muted border-ag-border">
            指纹 {info?.knownHostCount ?? 0} · 目录缓存 {info?.dirCacheCount ?? 0}
          </span>
          <button type="button" onClick={() => void refresh()} disabled={loading} title="刷新" className="p-1 rounded hover:bg-ag-hover text-ag-muted hover:text-ag-text">
            <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </div>

      <div className="rounded-xl bg-[#0d1017] border border-ag-border p-3.5 space-y-3">
        {info?.knownHostsPath && (
          <div className="flex items-center gap-2 text-[11px] font-mono text-ag-muted" title={info.knownHostsPath}>
            <FileText className="w-3.5 h-3.5 text-ag-subtle shrink-0" /><span className="truncate">{info.knownHostsPath}</span>
          </div>
        )}
        <div className="flex flex-wrap items-center gap-2">
          <input type="text" disabled={busyHost} className="ag-input font-mono text-xs py-1.5 flex-1 min-w-[160px] disabled:opacity-60" placeholder="主机 IP 或域名" value={host} onChange={(e) => setHost(e.target.value)} onKeyDown={onHostInputKeyDown} />
          <input type="text" inputMode="numeric" disabled={busyHost} className="ag-input font-mono text-xs py-1.5 w-24 disabled:opacity-60" placeholder="端口" value={port} onChange={(e) => setPort(e.target.value)} onKeyDown={onHostInputKeyDown} />
          <button type="button" disabled={busyHost || !host.trim()} onClick={() => void handleClearHost()} className="ag-btn-secondary text-xs py-1.5 px-3 shrink-0">
            {busyHost ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Eraser className="w-3.5 h-3.5 text-cyan-400" />}清理指定主机
          </button>
        </div>
        <div className="pt-1 flex items-center justify-end border-t border-ag-border/60">
          <button type="button" disabled={busyAll} onClick={() => void handleClearAll()} className="ag-btn-ghost text-xs text-rose-300 hover:text-rose-200 py-1 px-2.5">
            {busyAll ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Trash2 className="w-3.5 h-3.5" />}全部清除
          </button>
        </div>
      </div>
      {dialogNode}
    </section>
  )
}

export const ClearHostCacheButton: React.FC<{ host: string; port: number; onResult: (res: { ok: boolean; text: string }) => void }> = ({ host, port, onResult }) => {
  const [busy, setBusy] = useState(false)
  const busyRef = useRef(false)
  const run = async () => {
    if (busyRef.current) return
    const h = host.trim()
    if (!h) return
    busyRef.current = true
    setBusy(true)
    try { onResult({ ok: true, text: formatCacheClearSummary(await api.clearSshHostCache(h, port || 22)) }) }
    catch (err) { onResult({ ok: false, text: errMsg(err) }) }
    finally {
      busyRef.current = false
      setBusy(false)
    }
  }
  return (
    <button type="button" disabled={busy || !host.trim()} onClick={() => void run()} title="清理该主机指纹与目录缓存" className="ag-btn-secondary text-xs">
      {busy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Eraser className="w-3.5 h-3.5 text-cyan-400" />}清理主机指纹
    </button>
  )
}

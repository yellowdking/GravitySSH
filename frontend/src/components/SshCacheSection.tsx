import React, { useEffect, useState } from 'react'
import { Eraser, FileText, Loader2, RefreshCw, Trash2 } from 'lucide-react'
import type { SSHCacheClearResult, SSHCacheInfo } from '../types'
import { api } from '../wailsApi'
import { errMsg } from '../utils/errors'
import { useActionDialogs } from '../hooks/useActionDialogs'

function formatCacheClearSummary(res: SSHCacheClearResult): string {
  const parts: string[] = []
  if (res.removedKnownHosts > 0) parts.push(`${res.removedKnownHosts} 条主机指纹缓存`)
  if (res.clearedDirCaches > 0) parts.push(`${res.clearedDirCaches} 条目录缓存`)
  return parts.length > 0 ? `已清理 ${parts.join('与 ')}` : '未发现该主机旧指纹缓存，已刷新会话缓存'
}

export const SshCacheSection: React.FC<{ onToast: (msg: string, type?: 'info' | 'error') => void }> = ({ onToast }) => {
  const [info, setInfo] = useState<SSHCacheInfo | null>(null)
  const [loading, setLoading] = useState(false)
  const [host, setHost] = useState('')
  const [port, setPort] = useState('')
  const [busyHost, setBusyHost] = useState(false)
  const [busyAll, setBusyAll] = useState(false)
  const { confirm, dialogNode } = useActionDialogs()

  const refresh = async () => {
    setLoading(true)
    try { setInfo(await api.getSshCacheInfo()) }
    catch (err) { onToast(errMsg(err), 'error') }
    finally { setLoading(false) }
  }

  useEffect(() => { void refresh() }, [])

  const handleClearHost = async () => {
    const h = host.trim()
    if (!h) return onToast('请输入要清理的主机地址或 IP', 'error')
    setBusyHost(true)
    try {
      const res = await api.clearSshHostCache(h, Number.parseInt(port.trim(), 10) || 0)
      onToast(`${h}：${formatCacheClearSummary(res)}`)
      setHost('')
      setPort('')
      await refresh()
    } catch (err) {
      onToast(errMsg(err), 'error')
    } finally { setBusyHost(false) }
  }

  const handleClearAll = async () => {
    const ok = await confirm({
      title: '清理全部 SSH 缓存',
      message: '确定清理全部 SSH 主机指纹缓存（known_hosts）与目录缓存吗？\n\n首次清理前会自动备份到 known_hosts.gssh.bak，不会删除已保存的服务器、私钥或快捷命令。',
      confirmLabel: '清理全部',
    })
    if (!ok) return
    setBusyAll(true)
    try {
      onToast(formatCacheClearSummary(await api.clearAllSshCache()))
      await refresh()
    } catch (err) {
      onToast(errMsg(err), 'error')
    } finally { setBusyAll(false) }
  }

  return (
    <section className="space-y-2.5">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 text-xs font-semibold text-cyan-300 uppercase tracking-wider">
          <Eraser className="w-3.5 h-3.5" />SSH 缓存历史清理
        </div>
        <div className="flex items-center gap-2">
          <span className="text-[11px] px-2 py-0.5 rounded border bg-ag-elevated text-ag-muted border-ag-border">
            指纹 {info?.knownHostCount ?? 0} 条 · 目录缓存 {info?.dirCacheCount ?? 0}
          </span>
          <button type="button" onClick={() => void refresh()} disabled={loading} title="刷新缓存统计" className="p-1 rounded hover:bg-ag-hover text-ag-muted hover:text-ag-text">
            <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </div>

      <div className="rounded-xl bg-[#0d1017] border border-ag-border p-3.5 space-y-3">
        <p className="text-[11px] text-ag-muted leading-relaxed">
          重装主机后复用原 IP/密钥可能因旧 <span className="font-mono text-ag-text">known_hosts</span> 缓存导致连接异常。清理不影响已保存的服务器、私钥与快捷命令。
        </p>
        {info?.knownHostsPath && (
          <div className="flex items-center gap-2 text-[11px] font-mono text-ag-muted" title={info.knownHostsPath}>
            <FileText className="w-3.5 h-3.5 text-ag-subtle shrink-0" /><span className="truncate">{info.knownHostsPath}</span>
          </div>
        )}
        <div className="flex flex-wrap items-center gap-2">
          <input type="text" className="ag-input font-mono text-xs py-1.5 flex-1 min-w-[160px]" placeholder="主机 IP 或域名（如 192.168.1.10）" value={host} onChange={(e) => setHost(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); void handleClearHost() } }} />
          <input type="number" min={1} max={65535} className="ag-input font-mono text-xs py-1.5 w-24" placeholder="端口(可选)" value={port} onChange={(e) => setPort(e.target.value)} />
          <button type="button" disabled={busyHost || !host.trim()} onClick={() => void handleClearHost()} className="ag-btn-secondary text-xs py-1.5 px-3 shrink-0">
            {busyHost ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Eraser className="w-3.5 h-3.5 text-cyan-400" />}清理指定主机
          </button>
        </div>
        <div className="pt-1 flex items-center justify-between gap-2 border-t border-ag-border/60">
          <span className="text-[11px] text-ag-subtle">清空前自动备份至 known_hosts.gssh.bak</span>
          <button type="button" disabled={busyAll} onClick={() => void handleClearAll()} className="ag-btn-ghost text-xs text-rose-300 hover:text-rose-200 py-1 px-2.5">
            {busyAll ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Trash2 className="w-3.5 h-3.5" />}一键清理全部 SSH 缓存
          </button>
        </div>
      </div>
      {dialogNode}
    </section>
  )
}

export const ClearHostCacheButton: React.FC<{ host: string; port: number; onResult: (res: { ok: boolean; text: string }) => void }> = ({ host, port, onResult }) => {
  const [busy, setBusy] = useState(false)
  const run = async () => {
    const h = host.trim()
    if (!h) return
    setBusy(true)
    try { onResult({ ok: true, text: formatCacheClearSummary(await api.clearSshHostCache(h, port || 22)) }) }
    catch (err) { onResult({ ok: false, text: errMsg(err) }) }
    finally { setBusy(false) }
  }
  return (
    <button type="button" disabled={busy || !host.trim()} onClick={() => void run()} title="清理 ~/.ssh/known_hosts 中该主机的旧指纹与目录缓存" className="ag-btn-secondary text-xs">
      {busy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Eraser className="w-3.5 h-3.5 text-cyan-400" />}清理该主机 SSH 缓存
    </button>
  )
}

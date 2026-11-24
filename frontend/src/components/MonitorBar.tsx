import React, { useEffect, useState } from 'react'
import { Activity, Cpu, HardDrive, MemoryStick, Network, Wifi } from 'lucide-react'
import type { HostMetrics } from '../types'
import { api } from '../wailsApi'

interface Props {
  sessionId: string
  status: string
  active: boolean
  enabled: boolean
}

function pctTone(pct: number): string {
  if (pct >= 90) return 'text-rose-400'
  if (pct >= 75) return 'text-amber-300'
  return 'text-ag-text'
}

function latTone(ms: number): string {
  if (ms >= 220) return 'text-rose-400'
  if (ms >= 100) return 'text-amber-300'
  return 'text-emerald-400'
}

function fmtRate(kbps: number): string {
  if (kbps >= 1024) return `${(kbps / 1024).toFixed(1)}M/s`
  return `${kbps >= 100 ? Math.round(kbps) : kbps.toFixed(1)}K/s`
}

function fmtMem(mb: number): string {
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)}G`
  return `${mb}M`
}

export const MonitorBar: React.FC<Props> = ({ sessionId, status, active, enabled }) => {
  const [m, setM] = useState<HostMetrics | null>(null)

  useEffect(() => {
    if (!enabled || !active || status !== 'connected') return
    let cancelled = false
    const poll = async () => {
      try {
        const res = await api.getSessionMetrics(sessionId)
        if (!cancelled) setM(res)
      } catch { /* ignore transient sampling error */ }
    }
    const initTimer = setTimeout(() => { void poll() }, 600)
    const interval = setInterval(() => { void poll() }, 3000)
    return () => {
      cancelled = true
      clearTimeout(initTimer)
      clearInterval(interval)
    }
  }, [sessionId, status, active, enabled])

  if (!enabled || status !== 'connected') return null
  if (m && !m.supported) return null

  const loadWarn = m && m.cores > 0 && m.load1 >= m.cores * 2
    ? 'text-rose-400'
    : m && m.cores > 0 && m.load1 >= m.cores
    ? 'text-amber-300'
    : 'text-ag-muted'

  return (
    <div className="h-7 px-3 border-t border-ag-border bg-[#0d1017] flex items-center justify-between gap-3 text-[11px] font-mono tabular-nums select-none shrink-0 overflow-hidden">
      {!m ? (
        <span className="text-ag-subtle flex items-center gap-1.5">
          <Activity className="w-3 h-3 text-indigo-400 animate-pulse" />正在采集主机状态...
        </span>
      ) : (
        <div className="flex items-center gap-3.5 min-w-0 overflow-hidden">
          <span className={`inline-flex items-center gap-1 shrink-0 ${latTone(m.latencyMs)}`} title={`SSH 往返延迟 ${m.latencyMs}ms`}>
            <Wifi className="w-3 h-3" />
            <span className="w-[46px]">{m.latencyMs}ms</span>
          </span>

          <span className={`inline-flex items-center gap-1 shrink-0 ${pctTone(m.cpuPercent)}`} title={`CPU 使用率 ${m.cpuPercent.toFixed(1)}% (${m.cores} 核)`}>
            <Cpu className="w-3 h-3 text-sky-400" />
            <span className="text-ag-subtle">CPU</span>
            <span className="w-[38px] text-right">{m.cpuPercent.toFixed(0)}%</span>
          </span>

          <span className={`inline-flex items-center gap-1 shrink-0 ${pctTone(m.memPercent)}`} title={`内存 ${fmtMem(m.memUsedMb)} / ${fmtMem(m.memTotalMb)} (${m.memPercent.toFixed(1)}%)`}>
            <MemoryStick className="w-3 h-3 text-violet-400" />
            <span className="text-ag-subtle">MEM</span>
            <span>{fmtMem(m.memUsedMb)}/{fmtMem(m.memTotalMb)}</span>
            <span className="text-ag-subtle">({m.memPercent.toFixed(0)}%)</span>
          </span>

          {m.diskMount && (
            <span className={`hidden sm:inline-flex items-center gap-1 shrink-0 ${pctTone(m.diskPercent)}`} title={`最满分区 ${m.diskMount} (${m.diskPercent.toFixed(0)}%)`}>
              <HardDrive className="w-3 h-3 text-amber-400" />
              <span className="text-ag-subtle truncate max-w-[68px]">{m.diskMount}</span>
              <span>{m.diskPercent.toFixed(0)}%</span>
            </span>
          )}

          <span className="hidden md:inline-flex items-center gap-1.5 shrink-0 text-ag-muted" title={`网络速率 下行 ${fmtRate(m.netRxKbps)} / 上行 ${fmtRate(m.netTxKbps)}`}>
            <Network className="w-3 h-3 text-emerald-400" />
            <span>↓{fmtRate(m.netRxKbps)}</span>
            <span>↑{fmtRate(m.netTxKbps)}</span>
          </span>

          <span className={`hidden lg:inline-flex items-center gap-1 shrink-0 ${loadWarn}`} title={`1 分钟平均负载 ${m.load1.toFixed(2)} / ${m.cores} 核`}>
            <Activity className="w-3 h-3" />
            <span>{m.load1.toFixed(2)}/{m.cores}c</span>
          </span>

          {m.hasGpu && (
            <span className={`hidden xl:inline-flex items-center gap-1 shrink-0 ${pctTone(m.gpuUtil)}`} title={`GPU ${m.gpuName || ''} ${m.gpuUtil.toFixed(0)}%`}>
              <span className="text-emerald-300">GPU</span>
              <span>{m.gpuUtil.toFixed(0)}%</span>
            </span>
          )}
        </div>
      )}
    </div>
  )
}

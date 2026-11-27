import React from 'react'
import { AlertTriangle, GitMerge, Laptop, Loader2, RefreshCcw, Smartphone } from 'lucide-react'
import type { SyncReceivePreview } from '../syncTypes'

interface Props {
  preview: SyncReceivePreview
  mode: 'merge' | 'overwrite'
  applying: boolean
  onModeChange: (m: 'merge' | 'overwrite') => void
  onDiscard: () => void
  onApply: () => void
}

function formatPlatformLabel(raw: string): string {
  const p = (raw || '').trim().toLowerCase()
  if (p === 'android') return 'Android'
  if (p === 'windows') return 'Windows'
  if (p === 'darwin' || p === 'macos') return 'macOS'
  if (p === 'ios') return 'iOS'
  if (p === 'linux') return 'Linux'
  return raw?.trim() || '未知平台'
}

function formatExportedAt(raw: string): string {
  if (!raw) return '未知时间'
  const date = new Date(raw)
  return Number.isNaN(date.getTime()) ? '未知时间' : date.toLocaleString()
}

export const SyncPreviewCard: React.FC<Props> = ({ preview, mode, applying, onModeChange, onDiscard, onApply }) => {
  const platformKey = (preview.sourcePlatform || '').trim().toLowerCase()
  const isMobile = platformKey === 'android' || platformKey === 'ios'
  const platformLabel = formatPlatformLabel(preview.sourcePlatform)
  const exportedAtText = formatExportedAt(preview.exportedAt)
  const stats = [
    ['服务器', preview.serverCount],
    ['SSH 密钥', preview.keyCount],
    ['快捷命令', preview.shortcutCount],
    ['运维模板', preview.templateCount],
  ]

  return (
    <div className="space-y-4">
      <div className="rounded-xl bg-[#0d111a] border border-indigo-500/30 p-4 space-y-3">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <div className="w-8 h-8 rounded-lg bg-indigo-500/15 border border-indigo-500/30 flex items-center justify-center text-indigo-300">
              {isMobile ? <Smartphone className="w-4 h-4" /> : <Laptop className="w-4 h-4" />}
            </div>
            <div>
              <div className="text-xs font-semibold text-ag-text">
                来自 {preview.sourceDevice || `${platformLabel} 设备`}
              </div>
              <div className="text-[11px] text-ag-muted">
                {platformLabel} · 导出于 {exportedAtText}
              </div>
            </div>
          </div>
          <span className="text-[10px] px-2 py-0.5 rounded-full bg-emerald-500/15 text-emerald-300 border border-emerald-500/30">
            已解密校验
          </span>
        </div>

        <div className="grid grid-cols-4 gap-2 text-center">
          {stats.map(([label, count]) => (
            <div key={String(label)} className="rounded-lg bg-ag-panel border border-ag-border py-2">
              <div className="text-base font-bold text-ag-text font-mono">{count}</div>
              <div className="text-[10px] text-ag-muted">{label}</div>
            </div>
          ))}
        </div>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-2.5">
        <button
          type="button"
          disabled={applying}
          onClick={() => onModeChange('merge')}
          className={`text-left p-3 rounded-xl border transition disabled:opacity-60 disabled:cursor-not-allowed ${mode === 'merge' ? 'bg-indigo-500/15 border-indigo-400/60' : 'bg-[#0d111a] border-ag-border hover:border-ag-borderStrong'}`}
        >
          <div className="flex items-center gap-1.5 text-xs font-semibold text-ag-text">
            <GitMerge className="w-3.5 h-3.5 text-indigo-300" />
            <span>安全合并（推荐）</span>
          </div>
          <p className="text-[11px] text-ag-muted mt-1 leading-relaxed">
            按 ID / 地址 / 指纹去重更新，绝不删除本机独有配置。
          </p>
        </button>

        <button
          type="button"
          disabled={applying}
          onClick={() => onModeChange('overwrite')}
          className={`text-left p-3 rounded-xl border transition disabled:opacity-60 disabled:cursor-not-allowed ${mode === 'overwrite' ? 'bg-rose-500/15 border-rose-400/60' : 'bg-[#0d111a] border-ag-border hover:border-ag-borderStrong'}`}
        >
          <div className="flex items-center gap-1.5 text-xs font-semibold text-rose-200">
            <AlertTriangle className="w-3.5 h-3.5 text-rose-400" />
            <span>覆盖本地</span>
          </div>
          <p className="text-[11px] text-ag-muted mt-1 leading-relaxed">
            替换为同步包内容，保留本地应用设置与保险库密码。
          </p>
        </button>
      </div>

      <div className="flex items-center justify-end gap-2">
        <button type="button" onClick={onDiscard} className="ag-btn-ghost" disabled={applying}>放弃</button>
        <button
          type="button"
          onClick={onApply}
          disabled={applying}
          className={`px-4 py-2 rounded-lg text-xs font-medium text-white transition flex items-center gap-1.5 disabled:opacity-50 disabled:cursor-not-allowed ${mode === 'overwrite' ? 'bg-rose-600 hover:bg-rose-500' : 'bg-indigo-500 hover:bg-indigo-400 shadow-glow'}`}
        >
          {applying ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <RefreshCcw className="w-3.5 h-3.5" />}
          {applying ? '正在写入...' : mode === 'merge' ? '确认安全合并' : '确认覆盖本地'}
        </button>
      </div>
    </div>
  )
}

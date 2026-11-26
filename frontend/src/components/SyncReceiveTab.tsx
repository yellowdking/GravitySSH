import React, { useState } from 'react'
import { CheckCircle2, Download, Loader2 } from 'lucide-react'
import type { SyncImportSummary, SyncReceivePreview } from '../syncTypes'
import { api } from '../wailsApi'
import { SyncPreviewCard } from './SyncPreviewCard'

interface Props {
  relayUrl: string
  onSynced: () => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

function formatSyncInput(raw: string): string {
  const trimmed = raw.trim()
  if (/^gssh-sync:\/\//i.test(trimmed)) {
    const m = trimmed.match(/[?&]code=([^&\s]+)/i)
    if (m) {
      try { return formatSyncInput(decodeURIComponent(m[1])) } catch { return trimmed }
    }
  }
  const clean = trimmed.toUpperCase().replace(/[^23456789ABCDEFGHJKMNPQRSTVWXYZ]/g, '').slice(0, 8)
  return clean.length > 4 ? `${clean.slice(0, 4)}-${clean.slice(4)}` : clean
}

export const SyncReceiveTab: React.FC<Props> = ({ relayUrl, onSynced, onToast }) => {
  const [inputCode, setInputCode] = useState('')
  const [fetching, setFetching] = useState(false)
  const [applying, setApplying] = useState(false)
  const [preview, setPreview] = useState<SyncReceivePreview | null>(null)
  const [mode, setMode] = useState<'merge' | 'overwrite'>('merge')
  const [summary, setSummary] = useState<SyncImportSummary | null>(null)

  const handleFetch = async () => {
    if (!inputCode.trim()) { onToast('请输入 8 位同步码或粘贴链接', 'error'); return }
    setFetching(true)
    setSummary(null)
    try {
      setPreview(await api.previewSyncReceive(inputCode, relayUrl))
      setMode('merge')
    } catch (err) {
      onToast(String(err), 'error')
    } finally {
      setFetching(false)
    }
  }

  const handleApply = async () => {
    if (!preview) return
    setApplying(true)
    try {
      const res = await api.applySyncReceive(preview.previewId, mode)
      setSummary(res)
      setPreview(null)
      setInputCode('')
      onSynced()
      onToast(mode === 'merge' ? '已安全合并同步配置到本地保险库' : '已使用同步配置覆盖本地数据')
    } catch (err) {
      onToast(String(err), 'error')
    } finally {
      setApplying(false)
    }
  }

  if (summary) {
    return (
      <div className="space-y-4">
        <div className="rounded-xl bg-emerald-500/10 border border-emerald-500/30 p-4 space-y-2">
          <div className="flex items-center gap-2 text-sm font-semibold text-emerald-300">
            <CheckCircle2 className="w-4 h-4" />
            <span>同步完成（{summary.mode === 'merge' ? '安全合并' : '覆盖本地'}）</span>
          </div>
          <div className="grid grid-cols-2 gap-2 text-xs text-ag-text pt-1">
            <div>服务器：+{summary.serversAdded} 新增 / {summary.serversUpdated} 更新</div>
            <div>SSH 密钥：+{summary.keysAdded} 新增 / {summary.keysUpdated} 更新</div>
            <div>快捷命令：+{summary.shortcutsAdded} 新增 / {summary.shortcutsUpdated} 更新</div>
            <div>运维模板：+{summary.templatesAdded} 新增 / {summary.templatesUpdated} 更新</div>
          </div>
        </div>
        <button onClick={() => setSummary(null)} className="w-full py-2.5 rounded-xl bg-ag-elevated hover:bg-ag-hover border border-ag-border text-xs text-ag-text transition">
          继续接收其他同步码
        </button>
      </div>
    )
  }

  if (preview) {
    return (
      <SyncPreviewCard
        preview={preview}
        mode={mode}
        applying={applying}
        onModeChange={setMode}
        onDiscard={() => setPreview(null)}
        onApply={() => void handleApply()}
      />
    )
  }

  return (
    <div className="space-y-4">
      <div className="space-y-2">
        <label className="text-xs font-medium text-ag-text block">
          输入 8 位同步码（支持直接粘贴 <span className="font-mono text-indigo-300">gssh-sync://</span> 链接）
        </label>
        <input
          type="text"
          value={inputCode}
          onChange={(e) => setInputCode(formatSyncInput(e.target.value))}
          onKeyDown={(e) => { if (e.key === 'Enter' && !fetching) void handleFetch() }}
          placeholder="XXXX-XXXX"
          autoFocus
          className="w-full rounded-xl bg-[#0b0d12] border border-ag-border px-4 py-3 text-center font-mono text-xl tracking-[0.22em] text-ag-text placeholder:text-ag-subtle focus:outline-none focus:border-indigo-500/70 transition"
        />
        <p className="text-[11px] text-ag-muted">
          点击获取后将从中继拉取密文并在本机解密预览（领取即焚），预览后再选择合并或覆盖。
        </p>
      </div>
      <button onClick={() => void handleFetch()} disabled={fetching || inputCode.replace(/-/g, '').length < 8} className="w-full py-3 rounded-xl bg-indigo-500 hover:bg-indigo-400 disabled:opacity-50 text-white text-sm font-medium transition shadow-glow flex items-center justify-center gap-2">
        {fetching ? <Loader2 className="w-4 h-4 animate-spin" /> : <Download className="w-4 h-4" />}
        {fetching ? '正在拉取并解密...' : '获取并解密'}
      </button>
    </div>
  )
}

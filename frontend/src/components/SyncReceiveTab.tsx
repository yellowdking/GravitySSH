import React, { useEffect, useState } from 'react'
import { Eye, EyeOff, Loader2, ShieldAlert, ShieldCheck, XCircle } from 'lucide-react'
import type { SyncImportSummary } from '../syncTypes'
import { useSyncFlow } from '../hooks/useSyncFlow'
import { errMsg } from '../utils/errors'
import {
  formatBytes,
  formatSyncCodeOrUriInput,
  isReadySyncCodeOrUri,
  isSynctoolUri,
} from '../utils/syncUri'
import { api } from '../wailsApi'
import { SyncPreviewCard, SyncSummaryCard } from './SyncPreviewCard'
import { SyncSasPrompt } from './SyncSasPrompt'

interface Props {
  onBusyChange?: (busy: boolean) => void
  onSynced: () => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const SyncReceiveTab: React.FC<Props> = ({ onBusyChange, onSynced, onToast }) => {
  const [inputCodeOrUri, setInputCodeOrUri] = useState('')
  const [showRawUri, setShowRawUri] = useState(false)
  const [importMode, setImportMode] = useState<'merge' | 'overwrite'>('merge')
  const [applying, setApplying] = useState(false)
  const [summary, setSummary] = useState<SyncImportSummary | null>(null)

  const flow = useSyncFlow({ onToast })

  const busy = flow.starting || flow.confirmingSas || applying
  useEffect(() => {
    onBusyChange?.(busy)
    return () => {
      onBusyChange?.(false)
    }
  }, [busy, onBusyChange])

  const st = flow.status?.state
  const preview = flow.status?.preview ?? null
  const uriWithSecret = isSynctoolUri(inputCodeOrUri) && inputCodeOrUri.includes('#')

  const handleStartReceive = async () => {
    if (!isReadySyncCodeOrUri(inputCodeOrUri)) {
      onToast('请粘贴完整的 synctool://pair 链接或输入 8 位房间码', 'error')
      return
    }
    setSummary(null)
    setImportMode('merge')
    const started = await flow.startFlow(() => api.startSyncReceive(inputCodeOrUri.trim()))
    if (started) {
      setInputCodeOrUri('')
      setShowRawUri(false)
    }
  }

  const handleDiscardPreview = () => {
    if (applying) return
    flow.resetFlow(true)
  }

  const handleApplyPreview = async () => {
    if (!preview || applying) return
    setApplying(true)
    try {
      const res = await api.applySyncReceive(preview.previewId, importMode)
      onSynced()
      setSummary(res)
      flow.resetFlow(false)
      onToast(importMode === 'merge' ? '已安全合并同步配置到本地保险库' : '已使用同步配置覆盖本地数据')
    } catch (err) {
      onToast(errMsg(err), 'error')
    } finally {
      setApplying(false)
    }
  }

  if (summary) {
    return (
      <SyncSummaryCard
        summary={summary}
        continueLabel="继续接收其他配置"
        onContinue={() => setSummary(null)}
      />
    )
  }

  if (st === 'ready_preview' && preview) {
    return (
      <SyncPreviewCard
        preview={preview}
        mode={importMode}
        applying={applying}
        onModeChange={setImportMode}
        onDiscard={handleDiscardPreview}
        onApply={() => void handleApplyPreview()}
      />
    )
  }

  if (flow.session) {
    return (
      <div className="space-y-4">
        {st === 'waiting_peer' && (
          <div className="rounded-xl bg-[#0d111a] border border-indigo-500/35 p-5 space-y-2.5 text-center">
            <div className="flex items-center justify-center gap-2 text-xs font-medium text-indigo-300">
              <Loader2 className="w-4 h-4 animate-spin" />
              <span>已进入同步房间，正在与发送端进行 X25519 加密握手...</span>
            </div>
            <p className="text-[11px] text-ag-muted">
              若使用 8 位房间码模式，握手完成后将弹出 6 位安全码供双方当面核对。
            </p>
          </div>
        )}

        {st === 'awaiting_sas_confirm' && (
          <SyncSasPrompt
            variant="confirm"
            sasCode={flow.status?.sas}
            approveLabel="一致，允许接收"
            busy={flow.confirmingSas}
            onConfirm={(approved) => void flow.confirmSas(approved)}
          />
        )}

        {st === 'transferring' && (
          <div className="rounded-xl bg-emerald-500/10 border border-emerald-500/30 p-4 flex items-center justify-between gap-2 text-xs text-emerald-200">
            <span className="flex items-center gap-2">
              <Loader2 className="w-4 h-4 animate-spin shrink-0 text-emerald-300" />
              安全核验已通过，正在拉取并解密校验配置包...
            </span>
            {(flow.status?.bytesTotal ?? 0) > 0 && (
              <span className="font-mono">
                {formatBytes(flow.status?.bytesDone)} / {formatBytes(flow.status?.bytesTotal)}
              </span>
            )}
          </div>
        )}

        {(st === 'error' || st === 'cancelled') && (
          <div className="rounded-xl bg-rose-500/10 border border-rose-500/35 p-4 space-y-3">
            <div className="flex items-center gap-2 text-xs text-rose-200">
              <ShieldAlert className="w-4 h-4 shrink-0 text-rose-300" />
              <span>{flow.status?.message || flow.error || '接收已取消或校验未通过'}</span>
            </div>
            <button
              type="button"
              onClick={() => flow.resetFlow(false)}
              className="w-full py-2 rounded-lg bg-ag-elevated hover:bg-ag-hover border border-ag-border text-xs text-ag-text"
            >
              返回重新输入
            </button>
          </div>
        )}

        {flow.isActive && st !== 'awaiting_sas_confirm' && (
          <button
            type="button"
            disabled={flow.cancelling}
            onClick={() => void flow.cancelFlow()}
            className="w-full py-2.5 rounded-xl bg-rose-500/[0.12] hover:bg-rose-500/20 border border-rose-500/30 text-xs font-medium text-rose-300 flex items-center justify-center gap-1.5 transition disabled:opacity-50"
          >
            <XCircle className="w-4 h-4" />
            取消接收
          </button>
        )}
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {flow.error && (
        <div className="rounded-xl bg-rose-500/10 border border-rose-500/35 px-3.5 py-2.5 flex items-center gap-2 text-xs text-rose-200">
          <ShieldAlert className="w-4 h-4 shrink-0 text-rose-300" />
          <span>{flow.error}</span>
        </div>
      )}

      <div className="space-y-2">
        <label className="text-xs font-medium text-ag-text block">
          粘贴配对链接（<span className="font-mono text-indigo-300">synctool://pair?...</span>）或输入 8 位房间码
        </label>
        <div className="relative">
          <input
            type={uriWithSecret && !showRawUri ? 'password' : 'text'}
            autoComplete="off"
            spellCheck={false}
            disabled={flow.starting}
            value={inputCodeOrUri}
            onChange={(e) => {
              if (flow.error) flow.setError('')
              setInputCodeOrUri(formatSyncCodeOrUriInput(e.target.value))
            }}
            onKeyDown={(e) => {
              if (e.nativeEvent.isComposing || e.keyCode === 229) return
              if (e.key === 'Enter' && isReadySyncCodeOrUri(inputCodeOrUri)) void handleStartReceive()
            }}
            placeholder="XXXX-XXXX 或 synctool://pair?..."
            autoFocus
            className={`w-full rounded-xl bg-[#0b0d12] border border-ag-border px-4 py-3 font-mono text-ag-text placeholder:text-ag-subtle focus:outline-none focus:border-indigo-500/70 transition disabled:opacity-60 ${
              isSynctoolUri(inputCodeOrUri) ? 'text-xs pr-10' : 'text-center text-xl tracking-[0.22em]'
            }`}
          />
          {uriWithSecret && (
            <button
              type="button"
              onClick={() => setShowRawUri((v) => !v)}
              aria-label={showRawUri ? '隐藏链接密钥' : '显示链接内容'}
              className="absolute right-3 top-1/2 -translate-y-1/2 text-ag-muted hover:text-ag-text"
            >
              {showRawUri ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
            </button>
          )}
        </div>
        <p className="text-[11px] text-ag-muted leading-relaxed">
          支持接收一次性速传链接（若链接携带自定义中继将自动识别连接）、8 位房间码（需双向核对 6 位安全码），或已配对设备的实时直传码。
        </p>
      </div>

      <button
        type="button"
        onClick={() => void handleStartReceive()}
        disabled={flow.starting || !isReadySyncCodeOrUri(inputCodeOrUri)}
        className="w-full py-3 rounded-xl bg-indigo-500 hover:bg-indigo-400 disabled:opacity-50 text-white text-sm font-medium transition shadow-glow flex items-center justify-center gap-2"
      >
        {flow.starting ? <Loader2 className="w-4 h-4 animate-spin" /> : <ShieldCheck className="w-4 h-4" />}
        {flow.starting ? '正在连接加密房间...' : '连接房间并解密预览'}
      </button>
    </div>
  )
}

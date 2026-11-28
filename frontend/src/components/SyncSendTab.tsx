import React, { useEffect, useState } from 'react'
import {
  Check, CheckCircle2, Clock, Copy, KeyRound, Loader2,
  QrCode, RefreshCw, ShieldAlert, Trash2,
} from 'lucide-react'
import type { SyncRelayStatus } from '../syncTypes'
import { useCopyFeedback } from '../hooks/useCopyFeedback'
import { fmtSec, useSyncCountdown } from '../hooks/useSyncCountdown'
import { useSyncFlow } from '../hooks/useSyncFlow'
import { formatBytes, formatRoomCodeDisplay } from '../utils/syncUri'
import { api } from '../wailsApi'
import { QrCodeSvg } from './QrCodeSvg'
import { SyncSasPrompt } from './SyncSasPrompt'

interface Props {
  relay: SyncRelayStatus | null
  onBusyChange?: (busy: boolean) => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const SyncSendTab: React.FC<Props> = ({ relay, onBusyChange, onToast }) => {
  const [sendMode, setSendMode] = useState<1 | 2>(1)
  const [includeRelay, setIncludeRelay] = useState(true)

  const { copied: copiedCode, copy: copyCode } = useCopyFeedback()
  const { copied: copiedUri, copy: copyUri } = useCopyFeedback()

  const flow = useSyncFlow({
    onToast,
    onCompleted: () => {
      onToast('发送完成')
    },
  })

  const busy = flow.starting || flow.submittingSas || flow.cancelling
  useEffect(() => {
    onBusyChange?.(busy)
    return () => {
      onBusyChange?.(false)
    }
  }, [busy, onBusyChange])

  const isCustomRelay = relay?.mode === 'custom'
  const pairUri = flow.status?.pairUri || flow.session?.pairUri || ''
  const roomCode = flow.status?.roomCode || flow.session?.roomCode || ''
  const expiresAt = flow.status?.expiresAt || flow.session?.expiresAt
  const left = useSyncCountdown(expiresAt)
  const st = flow.status?.state

  const handleStart = async () => {
    await flow.startFlow(() =>
      api.startSyncOneShotSend(sendMode, isCustomRelay ? includeRelay : false)
    )
  }

  if (!flow.session) {
    return (
      <div className="space-y-4">
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-2.5">
          <button
            type="button"
            onClick={() => setSendMode(1)}
            className={`text-left p-3.5 rounded-xl border transition ${
              sendMode === 1
                ? 'bg-indigo-500/15 border-indigo-400/60'
                : 'bg-[#0d111a] border-ag-border hover:border-ag-borderStrong'
            }`}
          >
            <div className="flex items-center gap-2 text-xs font-semibold text-ag-text">
              <QrCode className="w-4 h-4 text-indigo-300" />
              <span>二维码发送</span>
            </div>
          </button>

          <button
            type="button"
            onClick={() => setSendMode(2)}
            className={`text-left p-3.5 rounded-xl border transition ${
              sendMode === 2
                ? 'bg-indigo-500/15 border-indigo-400/60'
                : 'bg-[#0d111a] border-ag-border hover:border-ag-borderStrong'
            }`}
          >
            <div className="flex items-center gap-2 text-xs font-semibold text-ag-text">
              <KeyRound className="w-4 h-4 text-cyan-300" />
              <span>房间码发送</span>
            </div>
          </button>
        </div>

        {isCustomRelay && (
          <label className="flex items-center justify-between gap-2 rounded-xl bg-[#0d111a] border border-amber-500/30 px-3.5 py-2.5 cursor-pointer">
            <span className="text-xs text-amber-200">携带自定义中继配置</span>
            <input
              type="checkbox"
              checked={includeRelay}
              onChange={(e) => setIncludeRelay(e.target.checked)}
              className="rounded border-ag-border bg-ag-panel text-indigo-500 focus:ring-0"
            />
          </label>
        )}

        <button
          type="button"
          disabled={flow.starting}
          onClick={() => void handleStart()}
          className="w-full py-3 rounded-xl bg-indigo-500 hover:bg-indigo-400 disabled:opacity-60 text-white text-sm font-medium transition shadow-glow flex items-center justify-center gap-2"
        >
          {flow.starting ? <Loader2 className="w-4 h-4 animate-spin" /> : sendMode === 1 ? <QrCode className="w-4 h-4" /> : <KeyRound className="w-4 h-4" />}
          {flow.starting ? '创建中...' : sendMode === 1 ? '生成二维码' : '生成房间码'}
        </button>
      </div>
    )
  }

  if (st === 'completed') {
    return (
      <div className="space-y-4">
        <div className="rounded-xl bg-emerald-500/10 border border-emerald-500/35 p-5 text-center">
          <div className="flex items-center justify-center gap-2 text-sm font-semibold text-emerald-300">
            <CheckCircle2 className="w-5 h-5" />
            <span>发送完成</span>
          </div>
        </div>
        <button
          type="button"
          onClick={() => flow.resetFlow()}
          className="w-full py-2.5 rounded-xl bg-ag-elevated hover:bg-ag-hover border border-ag-border text-xs font-medium text-ag-text flex items-center justify-center gap-1.5"
        >
          <RefreshCw className="w-3.5 h-3.5" />
          继续发送
        </button>
      </div>
    )
  }

  const sess = flow.session
  const hasCounts =
    sess.serverCount !== undefined ||
    sess.keyCount !== undefined ||
    sess.shortcutCount !== undefined ||
    sess.templateCount !== undefined

  return (
    <div className="space-y-4">
      <div className="rounded-xl bg-[#0d111a] border border-ag-border p-4 flex flex-col sm:flex-row items-center gap-4">
        {sess.mode === 1 && pairUri && <QrCodeSvg value={pairUri} size={184} />}
        <div className="flex-1 w-full space-y-2.5 text-center sm:text-left">
          <div className="text-[11px] text-ag-muted">
            {sess.mode === 1 ? '请使用接收端扫码或复制配对链接' : '请在接收端输入以下房间码'}
          </div>

          {roomCode && (
            <div className="flex items-center justify-center sm:justify-start gap-2">
              <span className="font-mono text-2xl font-bold tracking-[0.18em] text-indigo-200 bg-ag-elevated px-3.5 py-2 rounded-xl border border-indigo-500/35 select-all">
                {formatRoomCodeDisplay(roomCode)}
              </span>
              <button
                type="button"
                onClick={() => void copyCode(formatRoomCodeDisplay(roomCode)).then((ok) => ok && onToast('房间码已复制'))}
                title="复制房间码"
                aria-label="复制房间码"
                className="p-2.5 rounded-xl bg-ag-elevated hover:bg-ag-hover border border-ag-border text-ag-text"
              >
                {copiedCode ? <Check className="w-4 h-4 text-emerald-400" /> : <Copy className="w-4 h-4" />}
              </button>
            </div>
          )}

          {sess.mode === 1 && pairUri && (
            <div>
              <button
                type="button"
                onClick={() => void copyUri(pairUri).then((ok) => ok && onToast('配对链接已复制'))}
                className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-ag-elevated hover:bg-ag-hover border border-ag-border text-xs text-ag-text"
              >
                {copiedUri ? <Check className="w-3.5 h-3.5 text-emerald-400" /> : <Copy className="w-3.5 h-3.5" />}
                复制配对链接
              </button>
            </div>
          )}

          <div className="inline-flex items-center gap-1.5 text-xs px-2.5 py-1 rounded-lg bg-cyan-500/10 border border-cyan-500/30 text-cyan-300">
            <Clock className="w-3.5 h-3.5" />
            <span>{left > 0 ? `剩余 ${fmtSec(left)}` : '等待响应...'}</span>
          </div>

          {hasCounts && (
            <div className="grid grid-cols-4 gap-1.5 pt-1 text-center">
              {[
                ['主机', sess.serverCount ?? 0],
                ['密钥', sess.keyCount ?? 0],
                ['快捷', sess.shortcutCount ?? 0],
                ['模板', sess.templateCount ?? 0],
              ].map(([k, v]) => (
                <div key={String(k)} className="rounded-lg bg-ag-panel border border-ag-border py-1.5 px-1">
                  <div className="text-sm font-semibold text-ag-text font-mono">{v}</div>
                  <div className="text-[10px] text-ag-muted">{k}</div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      {st === 'waiting_peer' && (
        <div className="rounded-xl bg-indigo-500/10 border border-indigo-500/25 px-4 py-3 flex items-center gap-2.5 text-xs text-indigo-200">
          <Loader2 className="w-4 h-4 animate-spin shrink-0 text-indigo-300" />
          <span>等待接收端连接...</span>
        </div>
      )}

      {st === 'awaiting_sas_input' && (
        <SyncSasPrompt
          variant="input"
          attemptsRemaining={flow.status?.sasAttemptsRemaining}
          busy={flow.submittingSas}
          onSubmit={(code) => flow.submitSasInput(code)}
          onCancel={() => void flow.cancelFlow()}
        />
      )}

      {st === 'transferring' && (
        <div className="rounded-xl bg-emerald-500/10 border border-emerald-500/30 px-4 py-3 flex items-center justify-between gap-2 text-xs text-emerald-200">
          <span className="flex items-center gap-2">
            <Loader2 className="w-4 h-4 animate-spin shrink-0 text-emerald-300" />
            正在发送...
          </span>
          {(flow.status?.bytesTotal ?? 0) > 0 && (
            <span className="font-mono">
              {formatBytes(flow.status?.bytesDone)} / {formatBytes(flow.status?.bytesTotal)}
            </span>
          )}
        </div>
      )}

      {(st === 'error' || st === 'cancelled') && (
        <div className="rounded-xl bg-rose-500/10 border border-rose-500/35 px-4 py-3 flex items-center gap-2.5 text-xs text-rose-200">
          <ShieldAlert className="w-4 h-4 shrink-0 text-rose-300" />
          <span>{flow.status?.message || flow.error || '发送已取消或已过期'}</span>
        </div>
      )}

      {st !== 'awaiting_sas_input' && (
        <div className="flex justify-between gap-2">
          {flow.isActive ? (
            <button
              type="button"
              disabled={busy}
              onClick={() => void flow.cancelFlow()}
              className="px-3 py-2 rounded-lg text-xs font-medium bg-rose-500/[0.12] hover:bg-rose-500/20 text-rose-300 border border-rose-500/30 flex items-center gap-1.5 disabled:opacity-50"
            >
              {flow.cancelling ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Trash2 className="w-3.5 h-3.5" />}
              取消发送
            </button>
          ) : (
            <button
              type="button"
              onClick={() => flow.resetFlow()}
              className="px-3 py-2 rounded-lg text-xs font-medium bg-ag-elevated hover:bg-ag-hover text-ag-muted hover:text-ag-text border border-ag-border"
            >
              返回
            </button>
          )}
          <button
            type="button"
            disabled={busy}
            onClick={() => void handleStart()}
            className="px-3 py-2 rounded-lg text-xs font-medium bg-ag-elevated hover:bg-ag-hover text-ag-text border border-ag-border flex items-center gap-1.5 disabled:opacity-50"
          >
            {flow.starting ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <RefreshCw className="w-3.5 h-3.5" />}
            重新生成
          </button>
        </div>
      )}
    </div>
  )
}

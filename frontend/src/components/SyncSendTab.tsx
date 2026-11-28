import React, { useEffect, useState } from 'react'
import {
  Check, CheckCircle2, Clock, Copy, KeyRound, Loader2,
  QrCode, RefreshCw, ShieldAlert, ShieldCheck, Trash2,
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
      onToast('✓ 对方已成功接收配置，临时房间与会话密钥已焚毁')
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
              <span>扫码速传 (QR)</span>
            </div>
            <p className="text-[11px] text-ag-muted mt-1 leading-relaxed">
              内嵌 128-bit 一次性带外密钥，手机扫码即自动握手解密，阅后即焚。
            </p>
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
              <span>8 位码 + 6 位安全码</span>
            </div>
            <p className="text-[11px] text-ag-muted mt-1 leading-relaxed">
              无需摄像头，输入 8 位房间码并通过 6 位安全码双向防掉包核验。
            </p>
          </button>
        </div>

        {isCustomRelay && (
          <label className="flex items-center justify-between gap-2 rounded-xl bg-[#0d111a] border border-amber-500/30 px-3.5 py-2.5 cursor-pointer">
            <span className="text-xs text-amber-200">
              携带自定义中继（接收端扫码可免配置直接通过私有中继拉取）
            </span>
            <input
              type="checkbox"
              checked={includeRelay}
              onChange={(e) => setIncludeRelay(e.target.checked)}
              className="rounded border-ag-border bg-ag-panel text-indigo-500 focus:ring-0"
            />
          </label>
        )}

        <div className="rounded-xl bg-[#0d111a] border border-ag-border p-3.5 flex items-start gap-2.5">
          <ShieldCheck className="w-4 h-4 text-indigo-300 shrink-0 mt-0.5" />
          <p className="text-xs text-ag-muted leading-relaxed">
            一次性迁移不保存设备信任状态：全程使用临时 X25519 密钥协商与分块 AEAD 加密直传，传输结束或取消后中继即刻销毁房间。
          </p>
        </div>

        <button
          type="button"
          disabled={flow.starting}
          onClick={() => void handleStart()}
          className="w-full py-3 rounded-xl bg-indigo-500 hover:bg-indigo-400 disabled:opacity-60 text-white text-sm font-medium transition shadow-glow flex items-center justify-center gap-2"
        >
          {flow.starting ? <Loader2 className="w-4 h-4 animate-spin" /> : sendMode === 1 ? <QrCode className="w-4 h-4" /> : <KeyRound className="w-4 h-4" />}
          {flow.starting ? '正在创建一次性加密通道...' : sendMode === 1 ? '生成扫码速传二维码' : '生成 8 位同步房间码'}
        </button>
      </div>
    )
  }

  if (st === 'completed') {
    return (
      <div className="space-y-4">
        <div className="rounded-xl bg-emerald-500/10 border border-emerald-500/35 p-5 space-y-2 text-center">
          <div className="flex items-center justify-center gap-2 text-sm font-semibold text-emerald-300">
            <CheckCircle2 className="w-5 h-5" />
            <span>✓ 对方已成功接收配置，临时房间已焚毁</span>
          </div>
          <p className="text-xs text-ag-muted">
            本次端到端加密直传已完成且校验通过，未在远端中继遗留任何数据。
          </p>
        </div>
        <button
          type="button"
          onClick={() => flow.resetFlow()}
          className="w-full py-2.5 rounded-xl bg-ag-elevated hover:bg-ag-hover border border-ag-border text-xs font-medium text-ag-text flex items-center justify-center gap-1.5"
        >
          <RefreshCw className="w-3.5 h-3.5" />
          返回并发起新的发送
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
            {sess.mode === 1
              ? '接收端使用 GravitySSH 扫码即可自动接收（或复制配对链接）'
              : '请在接收端输入以下 8 位房间码，随后在此输入对方显示的 6 位安全码'}
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
                onClick={() => void copyUri(pairUri).then((ok) => ok && onToast('一次性速传链接已复制（含密钥，请勿公开转发）'))}
                className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-ag-elevated hover:bg-ag-hover border border-ag-border text-xs text-ag-text"
              >
                {copiedUri ? <Check className="w-3.5 h-3.5 text-emerald-400" /> : <Copy className="w-3.5 h-3.5" />}
                复制一次性配对链接
              </button>
            </div>
          )}

          <div className="inline-flex items-center gap-1.5 text-xs px-2.5 py-1 rounded-lg bg-cyan-500/10 border border-cyan-500/30 text-cyan-300">
            <Clock className="w-3.5 h-3.5" />
            <span>{left > 0 ? `房间倒计时 ${fmtSec(left)}` : '等待中继确认状态...'}</span>
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
          <span>等待接收端扫码或输入房间码连接...</span>
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
            正在端到端分块加密传输配置...
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
          <span>{flow.status?.message || flow.error || '发送已中止或房间已过期'}</span>
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
              切换发送方式
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

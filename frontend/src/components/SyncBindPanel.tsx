import React, { useState } from 'react'
import {
  Check, Clock, Copy, Eye, EyeOff, KeyRound, Link2, Loader2, QrCode, ShieldAlert, ShieldCheck, X,
} from 'lucide-react'
import type { SyncPairedDevice, SyncRelayStatus } from '../syncTypes'
import { useCopyFeedback } from '../hooks/useCopyFeedback'
import { fmtSec, useSyncCountdown } from '../hooks/useSyncCountdown'
import { useSyncFlow } from '../hooks/useSyncFlow'
import {
  formatRoomCodeDisplay,
  formatSyncCodeOrUriInput,
  isReadySyncCodeOrUri,
  isSynctoolUri,
} from '../utils/syncUri'
import { api } from '../wailsApi'
import { QrCodeSvg } from './QrCodeSvg'
import { SyncSasPrompt } from './SyncSasPrompt'

interface Props {
  relay: SyncRelayStatus | null
  onClose: () => void
  onDeviceBound: (dev?: SyncPairedDevice) => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const SyncBindPanel: React.FC<Props> = ({ relay, onClose, onDeviceBound, onToast }) => {
  const [roleTab, setRoleTab] = useState<'initiator' | 'responder'>('initiator')
  const [offerMode, setOfferMode] = useState<1 | 2>(1)
  const [includeRelay, setIncludeRelay] = useState(true)
  const [joinInput, setJoinInput] = useState('')
  const [showRawUri, setShowRawUri] = useState(false)

  const { copied: copiedCode, copy: copyCode } = useCopyFeedback()
  const { copied: copiedUri, copy: copyUri } = useCopyFeedback()

  const flow = useSyncFlow({
    onToast,
    onCompleted: (st) => {
      const label = st.pairedDevice?.label || '新设备'
      onToast(`✓ 已完成设备双向加密绑定：${label}`)
      onDeviceBound(st.pairedDevice)
    },
  })

  const pairUri = flow.status?.pairUri || flow.session?.pairUri || ''
  const roomCode = flow.status?.roomCode || flow.session?.roomCode || ''
  const expiresAt = flow.status?.expiresAt || flow.session?.expiresAt
  const left = useSyncCountdown(expiresAt)
  const isCustomRelay = relay?.mode === 'custom'
  const uriInputMode = isSynctoolUri(joinInput) && joinInput.includes('#')

  const handleStartOffer = async () => {
    await flow.startFlow(() =>
      api.startSyncDeviceBind(offerMode, isCustomRelay ? includeRelay : false)
    )
  }

  const handleAcceptBind = async () => {
    if (!isReadySyncCodeOrUri(joinInput)) {
      onToast('请输入完整的 synctool://pair 链接或 8 位房间码', 'error')
      return
    }
    const started = await flow.startFlow(() => api.acceptSyncDeviceBind(joinInput.trim()))
    if (started) setJoinInput('')
  }

  const handleClosePanel = () => {
    if (flow.isActive) void flow.cancelFlow()
    onClose()
  }

  const st = flow.status?.state

  return (
    <div className="rounded-xl bg-[#0d111a] border border-indigo-500/35 p-4 space-y-3.5">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2 text-xs font-semibold text-indigo-200">
          <Link2 className="w-4 h-4 text-indigo-300" />
          <span>绑定新设备（配对一次，后续支持零往返一键推送与离线收件箱）</span>
        </div>
        <button
          type="button"
          onClick={handleClosePanel}
          aria-label="关闭绑定面板"
          className="p-1 rounded-lg hover:bg-ag-hover text-ag-muted hover:text-ag-text"
        >
          <X className="w-3.5 h-3.5" />
        </button>
      </div>

      {!flow.session ? (
        <>
          <div className="grid grid-cols-2 gap-1.5 p-1 rounded-xl bg-[#090c12] border border-ag-border">
            <button
              type="button"
              onClick={() => setRoleTab('initiator')}
              className={`py-1.5 rounded-lg text-xs font-medium transition ${
                roleTab === 'initiator'
                  ? 'bg-indigo-500/20 text-indigo-200 border border-indigo-500/35'
                  : 'text-ag-muted hover:text-ag-text'
              }`}
            >
              生成配对码（发起方）
            </button>
            <button
              type="button"
              onClick={() => setRoleTab('responder')}
              className={`py-1.5 rounded-lg text-xs font-medium transition ${
                roleTab === 'responder'
                  ? 'bg-indigo-500/20 text-indigo-200 border border-indigo-500/35'
                  : 'text-ag-muted hover:text-ag-text'
              }`}
            >
              输入配对链接 / 房间码
            </button>
          </div>

          {roleTab === 'initiator' ? (
            <div className="space-y-3">
              <div className="grid grid-cols-2 gap-2">
                <button
                  type="button"
                  onClick={() => setOfferMode(1)}
                  className={`p-2.5 rounded-xl border text-left transition ${
                    offerMode === 1
                      ? 'bg-indigo-500/15 border-indigo-400/60 text-ag-text'
                      : 'bg-[#090c12] border-ag-border text-ag-muted hover:text-ag-text'
                  }`}
                >
                  <div className="flex items-center gap-1.5 text-xs font-semibold">
                    <QrCode className="w-3.5 h-3.5 text-indigo-300" />
                    二维码配对（推荐）
                  </div>
                  <p className="text-[10px] text-ag-muted mt-1">128-bit 带外密钥，手机扫码一步完成</p>
                </button>
                <button
                  type="button"
                  onClick={() => setOfferMode(2)}
                  className={`p-2.5 rounded-xl border text-left transition ${
                    offerMode === 2
                      ? 'bg-indigo-500/15 border-indigo-400/60 text-ag-text'
                      : 'bg-[#090c12] border-ag-border text-ag-muted hover:text-ag-text'
                  }`}
                >
                  <div className="flex items-center gap-1.5 text-xs font-semibold">
                    <KeyRound className="w-3.5 h-3.5 text-cyan-300" />
                    8 位码 + SAS
                  </div>
                  <p className="text-[10px] text-ag-muted mt-1">适合桌面端互绑，双端核对 6 位安全码</p>
                </button>
              </div>

              {isCustomRelay && (
                <label className="flex items-center justify-between gap-2 rounded-lg bg-[#090c12] border border-amber-500/25 px-3 py-2 cursor-pointer">
                  <span className="text-[11px] text-amber-200">
                    携带自定义中继配置（对方扫码可自动导入私有中继）
                  </span>
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
                onClick={() => void handleStartOffer()}
                className="w-full py-2.5 rounded-xl bg-indigo-500 hover:bg-indigo-400 disabled:opacity-50 text-white text-xs font-medium flex items-center justify-center gap-1.5 shadow-glow transition"
              >
                {flow.starting ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <ShieldCheck className="w-3.5 h-3.5" />}
                {flow.starting ? '正在创建加密绑定房间...' : offerMode === 1 ? '生成配对二维码' : '生成 8 位绑定房间码'}
              </button>
            </div>
          ) : (
            <div className="space-y-2.5">
              <div className="relative">
                <input
                  type={uriInputMode && !showRawUri ? 'password' : 'text'}
                  autoComplete="off"
                  spellCheck={false}
                  disabled={flow.starting}
                  value={joinInput}
                  onChange={(e) => setJoinInput(formatSyncCodeOrUriInput(e.target.value))}
                  onKeyDown={(e) => {
                    if (e.nativeEvent.isComposing || e.keyCode === 229) return
                    if (e.key === 'Enter' && isReadySyncCodeOrUri(joinInput)) void handleAcceptBind()
                  }}
                  placeholder="粘贴 synctool://pair?... 链接或输入 XXXX-XXXX 房间码"
                  className="w-full rounded-xl bg-[#090c12] border border-ag-border px-3.5 py-2.5 pr-9 font-mono text-xs text-ag-text placeholder:text-ag-subtle focus:outline-none focus:border-indigo-500/60"
                />
                {uriInputMode && (
                  <button
                    type="button"
                    onClick={() => setShowRawUri((v) => !v)}
                    aria-label={showRawUri ? '隐藏配对链接密钥' : '显示配对链接'}
                    className="absolute right-2.5 top-1/2 -translate-y-1/2 text-ag-muted hover:text-ag-text"
                  >
                    {showRawUri ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
                  </button>
                )}
              </div>
              <button
                type="button"
                disabled={flow.starting || !isReadySyncCodeOrUri(joinInput)}
                onClick={() => void handleAcceptBind()}
                className="w-full py-2.5 rounded-xl bg-indigo-500 hover:bg-indigo-400 disabled:opacity-50 text-white text-xs font-medium flex items-center justify-center gap-1.5 shadow-glow transition"
              >
                {flow.starting ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <ShieldCheck className="w-3.5 h-3.5" />}
                {flow.starting ? '正在连接配对房间...' : '加入房间并绑定设备'}
              </button>
            </div>
          )}
        </>
      ) : (
        <div className="space-y-3">
          {st === 'waiting_peer' && flow.session.role === 'initiator' && (
            <div className="flex flex-col sm:flex-row items-center gap-4 rounded-xl bg-[#090c12] border border-ag-border p-3.5">
              {flow.session.mode === 1 && pairUri && <QrCodeSvg value={pairUri} size={172} />}
              <div className="flex-1 w-full space-y-2.5 text-center sm:text-left">
                <div className="text-[11px] text-ag-muted">
                  {flow.session.mode === 1
                    ? '请用另一台设备的 GravitySSH 扫描二维码（或复制完整配对链接）'
                    : '请在另一台设备的「输入配对链接或 8 位房间码」中输入：'}
                </div>
                {roomCode && (
                  <div className="flex items-center justify-center sm:justify-start gap-2">
                    <span className="font-mono text-xl font-bold tracking-[0.18em] text-indigo-200 bg-ag-elevated px-3 py-1.5 rounded-lg border border-indigo-500/35 select-all">
                      {formatRoomCodeDisplay(roomCode)}
                    </span>
                    <button
                      type="button"
                      onClick={() => void copyCode(formatRoomCodeDisplay(roomCode)).then((ok) => ok && onToast('8 位房间码已复制'))}
                      className="p-2 rounded-lg bg-ag-elevated hover:bg-ag-hover border border-ag-border text-ag-text"
                      title="复制房间码"
                    >
                      {copiedCode ? <Check className="w-3.5 h-3.5 text-emerald-400" /> : <Copy className="w-3.5 h-3.5" />}
                    </button>
                  </div>
                )}
                {flow.session.mode === 1 && pairUri && (
                  <button
                    type="button"
                    onClick={() => void copyUri(pairUri).then((ok) => ok && onToast('完整配对链接已复制（含一次性密钥，请勿发至公开群聊）'))}
                    className="inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg bg-ag-elevated hover:bg-ag-hover border border-ag-border text-[11px] text-ag-text"
                  >
                    {copiedUri ? <Check className="w-3.5 h-3.5 text-emerald-400" /> : <Copy className="w-3.5 h-3.5" />}
                    复制完整配对链接
                  </button>
                )}
                <div className="flex items-center justify-center sm:justify-start gap-2 text-[11px] text-cyan-300">
                  <Clock className="w-3.5 h-3.5" />
                  <span>{left > 0 ? `房间有效倒计时 ${fmtSec(left)}` : '等待中继确认状态...'}</span>
                </div>
              </div>
            </div>
          )}

          {st === 'waiting_peer' && flow.session.role !== 'initiator' && (
            <div className="rounded-xl bg-indigo-500/10 border border-indigo-500/30 px-3.5 py-3 flex items-center gap-2.5 text-xs text-indigo-200">
              <Loader2 className="w-4 h-4 animate-spin shrink-0" />
              <span>正在与对端交换加密身份公钥并建立信任通道...</span>
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

          {st === 'awaiting_sas_confirm' && (
            <SyncSasPrompt
              variant="confirm"
              sasCode={flow.status?.sas}
              approveLabel="一致，允许绑定"
              busy={flow.confirmingSas}
              onConfirm={(approved) => void flow.confirmSas(approved)}
            />
          )}

          {st === 'transferring' && (
            <div className="rounded-xl bg-indigo-500/10 border border-indigo-500/30 px-3.5 py-3 flex items-center gap-2.5 text-xs text-indigo-200">
              <Loader2 className="w-4 h-4 animate-spin shrink-0" />
              <span>正在签名验证设备名片并派发专属投递令牌...</span>
            </div>
          )}

          {(st === 'error' || st === 'cancelled') && (
            <div className="rounded-xl bg-rose-500/10 border border-rose-500/35 p-3 flex items-center justify-between gap-2 text-xs text-rose-200">
              <span className="flex items-center gap-2">
                <ShieldAlert className="w-4 h-4 shrink-0 text-rose-300" />
                {flow.status?.message || flow.error || '绑定已取消或失败'}
              </span>
              <button
                type="button"
                onClick={() => flow.resetFlow()}
                className="px-2.5 py-1 rounded-lg bg-ag-elevated hover:bg-ag-hover border border-ag-border text-ag-text shrink-0"
              >
                重试
              </button>
            </div>
          )}

          {flow.isActive && st !== 'awaiting_sas_input' && st !== 'awaiting_sas_confirm' && (
            <div className="flex justify-end">
              <button
                type="button"
                disabled={flow.cancelling}
                onClick={() => void flow.cancelFlow()}
                className="px-3 py-1.5 rounded-lg bg-rose-500/15 hover:bg-rose-500/25 border border-rose-500/30 text-xs text-rose-200"
              >
                取消绑定
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

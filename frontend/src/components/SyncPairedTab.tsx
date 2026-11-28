import React, { useState } from 'react'
import {
  AlertTriangle, Check, CheckCircle2, Clock, Copy, Download, GitMerge,
  Inbox, Laptop, Loader2, Plus, RefreshCw, Send, ShieldAlert, Smartphone, Trash2, Zap,
} from 'lucide-react'
import type {
  SyncImportSummary,
  SyncInboxItem,
  SyncPairedDevice,
  SyncRelayStatus,
} from '../syncTypes'
import { useCopyFeedback } from '../hooks/useCopyFeedback'
import { fmtSec, useSyncCountdown } from '../hooks/useSyncCountdown'
import { useSyncFlow } from '../hooks/useSyncFlow'
import { errMsg } from '../utils/errors'
import {
  formatBytes,
  formatPlatformLabel,
  formatRoomCodeDisplay,
  formatSyncTimestamp,
} from '../utils/syncUri'
import { api } from '../wailsApi'
import { QrCodeSvg } from './QrCodeSvg'
import { SyncBindPanel } from './SyncBindPanel'
import { SyncSummaryCard } from './SyncPreviewCard'

interface Props {
  relay: SyncRelayStatus | null
  pairedDevices: SyncPairedDevice[]
  inboxItems: SyncInboxItem[]
  loadingInbox: boolean
  onRefreshAll: () => Promise<void> | void
  onRefreshInbox: () => Promise<void> | void
  onSynced: () => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const SyncPairedTab: React.FC<Props> = ({
  relay,
  pairedDevices,
  inboxItems,
  loadingInbox,
  onRefreshAll,
  onRefreshInbox,
  onSynced,
  onToast,
}) => {
  const [showBindPanel, setShowBindPanel] = useState(false)
  const [selectedInboxId, setSelectedInboxId] = useState<string | null>(null)
  const [inboxMode, setInboxMode] = useState<'merge' | 'overwrite'>('merge')
  const [applyingItemId, setApplyingItemId] = useState<string | null>(null)
  const [dismissingItemId, setDismissingItemId] = useState<string | null>(null)
  const [inboxSummary, setInboxSummary] = useState<SyncImportSummary | null>(null)

  const [pushingDeviceId, setPushingDeviceId] = useState<string | null>(null)
  const [sentSpoolByDevice, setSentSpoolByDevice] = useState<Record<string, string>>({})
  const [suggestPipeByDevice, setSuggestPipeByDevice] = useState<Record<string, string>>({})
  const [confirmRevokeId, setConfirmRevokeId] = useState<string | null>(null)
  const [revokingId, setRevokingId] = useState<string | null>(null)

  const { copied, copy } = useCopyFeedback()

  // Live pipe fallback flow when user clicks [改用实时直传]
  const pipeFlow = useSyncFlow({
    onToast,
    onCompleted: () => {
      onToast('✓ 实时直传完成，对方已成功接收配置')
    },
  })
  const pipeUri = pipeFlow.status?.pairUri || pipeFlow.session?.pairUri || ''
  const pipeRoom = pipeFlow.status?.roomCode || pipeFlow.session?.roomCode || ''
  const pipeLeft = useSyncCountdown(pipeFlow.status?.expiresAt || pipeFlow.session?.expiresAt)

  const handleApplyInbox = async (item: SyncInboxItem) => {
    if (applyingItemId) return
    setApplyingItemId(item.itemId)
    try {
      const res = await api.applyInboxSync(item.itemId, inboxMode)
      setSelectedInboxId(null)
      setInboxSummary(res)
      onSynced()
      await onRefreshInbox()
      onToast(
        inboxMode === 'merge'
          ? '已解密合并配置并向中继发送签名销毁回执'
          : '已覆盖导入配置并向中继发送签名销毁回执'
      )
    } catch (err) {
      onToast(errMsg(err), 'error')
    } finally {
      setApplyingItemId(null)
    }
  }

  const handleDismissInbox = async (itemId: string) => {
    if (dismissingItemId) return
    setDismissingItemId(itemId)
    try {
      await api.dismissInboxItem(itemId)
      if (selectedInboxId === itemId) setSelectedInboxId(null)
      await onRefreshInbox()
      onToast('已销毁该离线同步包')
    } catch (err) {
      onToast(errMsg(err), 'error')
    } finally {
      setDismissingItemId(null)
    }
  }

  const handleSendToDevice = async (deviceId: string, deliveryMode: 'auto' | 'pipe') => {
    if (pushingDeviceId) return
    setPushingDeviceId(deviceId)
    try {
      const res = await api.sendSyncToDevice(deviceId, deliveryMode)
      if (res.status === 'sent_spool') {
        setSuggestPipeByDevice((prev) => {
          const next = { ...prev }
          delete next[deviceId]
          return next
        })
        setSentSpoolByDevice((prev) => ({
          ...prev,
          [deviceId]: res.message || '已投递至对方离线收件箱（0-RTT 端到端加密）',
        }))
        onToast(res.message || '✓ 已一键加密推送至对方收件箱')
      } else if (res.status === 'suggest_pipe') {
        setSuggestPipeByDevice((prev) => ({
          ...prev,
          [deviceId]: res.message || '当前中继未开启离线暂存或配额已满，可改用在线实时直传',
        }))
      } else if (res.status === 'pipe_offer_ready' && res.sessionId) {
        setSuggestPipeByDevice((prev) => {
          const next = { ...prev }
          delete next[deviceId]
          return next
        })
        pipeFlow.adoptSession({
          sessionId: res.sessionId,
          purpose: 'direct',
          role: 'initiator',
          mode: 0,
          state: 'waiting_peer',
          pairUri: res.pairUri,
          roomCode: res.roomCode,
          expiresAt: res.expiresAt,
        })
        onToast('实时直传房间已就绪，请在目标设备上扫码或输入房间码接收')
      }
    } catch (err) {
      onToast(errMsg(err), 'error')
    } finally {
      setPushingDeviceId(null)
    }
  }

  const handleRevoke = async (dev: SyncPairedDevice) => {
    if (revokingId) return
    setRevokingId(dev.deviceId)
    try {
      await api.revokeSyncDevice(dev.deviceId)
      setConfirmRevokeId(null)
      await onRefreshAll()
      onToast(`已吊销设备「${dev.label || dev.deviceId.slice(0, 8)}」的投递权限`)
    } catch (err) {
      onToast(errMsg(err), 'error')
    } finally {
      setRevokingId(null)
    }
  }

  return (
    <div className="space-y-4">
      {inboxSummary && (
        <SyncSummaryCard
          summary={inboxSummary}
          continueLabel="返回已配对设备列表"
          onContinue={() => setInboxSummary(null)}
        />
      )}

      {/* Two-Phase Inbox Banner */}
      {inboxItems.length > 0 && (
        <div className="rounded-xl bg-indigo-950/30 border border-indigo-500/40 p-3.5 space-y-3">
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-2 text-xs font-semibold text-indigo-200">
              <Inbox className="w-4 h-4 text-indigo-300" />
              <span>待处理加密收件箱（{inboxItems.length}）</span>
            </div>
            <button
              type="button"
              disabled={loadingInbox}
              onClick={() => void onRefreshInbox()}
              className="inline-flex items-center gap-1 text-[11px] text-ag-muted hover:text-ag-text"
            >
              <RefreshCw className={`w-3 h-3 ${loadingInbox ? 'animate-spin' : ''}`} />
              刷新
            </button>
          </div>

          <div className="space-y-2.5">
            {inboxItems.map((item) => {
              const isExpanded = selectedInboxId === item.itemId
              const isApplying = applyingItemId === item.itemId
              const isDismissing = dismissingItemId === item.itemId
              const isMobile = ['android', 'ios'].includes((item.senderPlatform || '').toLowerCase())

              return (
                <div
                  key={item.itemId}
                  className="rounded-xl bg-[#0b0d12] border border-ag-border p-3 space-y-2.5"
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="flex items-center gap-2 min-w-0">
                      <div className="w-7 h-7 rounded-lg bg-indigo-500/15 border border-indigo-500/30 flex items-center justify-center text-indigo-300 shrink-0">
                        {isMobile ? <Smartphone className="w-3.5 h-3.5" /> : <Laptop className="w-3.5 h-3.5" />}
                      </div>
                      <div className="min-w-0">
                        <div className="text-xs font-semibold text-ag-text truncate">
                          来自 {item.senderLabel || item.senderDeviceId.slice(0, 8)}（{formatPlatformLabel(item.senderPlatform)}）
                        </div>
                        <div className="text-[10px] text-ag-muted">
                          {formatSyncTimestamp(item.createdAt)} · 密文体积 {formatBytes(item.totalBytes)}
                        </div>
                      </div>
                    </div>

                    {!isExpanded && (
                      <div className="flex items-center gap-1.5 shrink-0">
                        {!item.pairingBroken && (
                          <button
                            type="button"
                            disabled={Boolean(applyingItemId || dismissingItemId)}
                            onClick={() => {
                              setSelectedInboxId(item.itemId)
                              setInboxMode('merge')
                            }}
                            className="px-2.5 py-1.5 rounded-lg bg-indigo-500 hover:bg-indigo-400 text-white text-xs font-medium flex items-center gap-1 transition disabled:opacity-50"
                          >
                            <Download className="w-3.5 h-3.5" />
                            导入配置
                          </button>
                        )}
                        <button
                          type="button"
                          disabled={Boolean(applyingItemId || dismissingItemId)}
                          onClick={() => void handleDismissInbox(item.itemId)}
                          className="px-2.5 py-1.5 rounded-lg bg-rose-500/15 hover:bg-rose-500/25 border border-rose-500/30 text-rose-200 text-xs flex items-center gap-1 transition disabled:opacity-50"
                        >
                          {isDismissing ? <Loader2 className="w-3 h-3 animate-spin" /> : <Trash2 className="w-3 h-3" />}
                          销毁
                        </button>
                      </div>
                    )}
                  </div>

                  {/* Manifest counts rendered immediately before downloading part_id=1 blob */}
                  <div className="grid grid-cols-4 gap-1.5 text-center">
                    {[
                      ['主机', item.serverCount],
                      ['密钥', item.keyCount],
                      ['快捷', item.shortcutCount],
                      ['模板', item.templateCount],
                    ].map(([k, v]) => (
                      <div key={String(k)} className="rounded-lg bg-ag-panel border border-ag-border py-1">
                        <span className="text-xs font-bold font-mono text-ag-text">{v}</span>
                        <span className="text-[10px] text-ag-muted ml-1">{k}</span>
                      </div>
                    ))}
                  </div>

                  {(item.pairingBroken || item.errorMessage) && (
                    <div className="rounded-lg bg-amber-500/10 border border-amber-500/35 px-2.5 py-1.5 flex items-center gap-2 text-[11px] text-amber-200">
                      <ShieldAlert className="w-3.5 h-3.5 shrink-0 text-amber-400" />
                      <span>{item.errorMessage || '发送方签名校验异常或配对已失效，请销毁后重新绑定设备'}</span>
                    </div>
                  )}

                  {isExpanded && (
                    <div className="pt-2 border-t border-ag-border space-y-2.5">
                      <div className="grid grid-cols-2 gap-2">
                        <button
                          type="button"
                          disabled={isApplying}
                          onClick={() => setInboxMode('merge')}
                          className={`p-2.5 rounded-lg border text-left transition ${
                            inboxMode === 'merge'
                              ? 'bg-indigo-500/15 border-indigo-400/60'
                              : 'bg-ag-panel border-ag-border hover:border-ag-borderStrong'
                          }`}
                        >
                          <div className="flex items-center gap-1 text-xs font-semibold text-ag-text">
                            <GitMerge className="w-3.5 h-3.5 text-indigo-300" />
                            安全合并（推荐）
                          </div>
                          <p className="text-[10px] text-ag-muted mt-0.5">保留本机独有服务器与密钥</p>
                        </button>
                        <button
                          type="button"
                          disabled={isApplying}
                          onClick={() => setInboxMode('overwrite')}
                          className={`p-2.5 rounded-lg border text-left transition ${
                            inboxMode === 'overwrite'
                              ? 'bg-rose-500/15 border-rose-400/60'
                              : 'bg-ag-panel border-ag-border hover:border-ag-borderStrong'
                          }`}
                        >
                          <div className="flex items-center gap-1 text-xs font-semibold text-rose-200">
                            <AlertTriangle className="w-3.5 h-3.5 text-rose-400" />
                            覆盖本地
                          </div>
                          <p className="text-[10px] text-ag-muted mt-0.5">替换为同步包，保留已配对设备</p>
                        </button>
                      </div>
                      <div className="flex items-center justify-end gap-2">
                        <button
                          type="button"
                          disabled={isApplying}
                          onClick={() => setSelectedInboxId(null)}
                          className="ag-btn-ghost text-xs py-1 px-2.5"
                        >
                          取消
                        </button>
                        <button
                          type="button"
                          disabled={isApplying}
                          onClick={() => void handleApplyInbox(item)}
                          className={`px-3 py-1.5 rounded-lg text-xs font-medium text-white flex items-center gap-1.5 transition disabled:opacity-50 ${
                            inboxMode === 'overwrite' ? 'bg-rose-600 hover:bg-rose-500' : 'bg-indigo-500 hover:bg-indigo-400'
                          }`}
                        >
                          {isApplying ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <CheckCircle2 className="w-3.5 h-3.5" />}
                          {isApplying ? '正在拉取解密并导入...' : inboxMode === 'merge' ? '确认合并并焚毁' : '确认覆盖并焚毁'}
                        </button>
                      </div>
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        </div>
      )}

      {/* Active Direct Pipe Offer Card (when fallback pipe mode is triggered) */}
      {pipeFlow.session && (
        <div className="rounded-xl bg-[#0d111a] border border-cyan-500/40 p-4 space-y-3">
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-2 text-xs font-semibold text-cyan-200">
              <Zap className="w-4 h-4 text-cyan-300" />
              <span>已配对设备实时直传通道</span>
            </div>
            <button
              type="button"
              onClick={() => (pipeFlow.isActive ? void pipeFlow.cancelFlow() : pipeFlow.resetFlow())}
              className="text-[11px] text-ag-muted hover:text-ag-text"
            >
              {pipeFlow.isActive ? '取消直传' : '关闭'}
            </button>
          </div>
          {pipeFlow.status?.state === 'completed' ? (
            <div className="rounded-lg bg-emerald-500/10 border border-emerald-500/30 px-3 py-2.5 text-xs text-emerald-200 flex items-center gap-2">
              <CheckCircle2 className="w-4 h-4 text-emerald-400" />
              <span>✓ 实时加密直传已完成，中继房间已销毁</span>
            </div>
          ) : (
            <div className="flex flex-col sm:flex-row items-center gap-4">
              {pipeUri && <QrCodeSvg value={pipeUri} size={160} />}
              <div className="flex-1 space-y-2 text-center sm:text-left">
                <div className="text-[11px] text-ag-muted">请在目标设备的「一次性接收」中扫码或输入房间码：</div>
                {pipeRoom && (
                  <div className="flex items-center justify-center sm:justify-start gap-2">
                    <span className="font-mono text-xl font-bold tracking-[0.18em] text-cyan-200 bg-ag-elevated px-3 py-1.5 rounded-lg border border-cyan-500/35 select-all">
                      {formatRoomCodeDisplay(pipeRoom)}
                    </span>
                    <button
                      type="button"
                      onClick={() => void copy(formatRoomCodeDisplay(pipeRoom)).then((ok) => ok && onToast('直传房间码已复制'))}
                      className="p-2 rounded-lg bg-ag-elevated hover:bg-ag-hover border border-ag-border text-ag-text"
                    >
                      {copied ? <Check className="w-3.5 h-3.5 text-emerald-400" /> : <Copy className="w-3.5 h-3.5" />}
                    </button>
                  </div>
                )}
                <div className="inline-flex items-center gap-1.5 text-[11px] text-cyan-300">
                  <Clock className="w-3.5 h-3.5" />
                  <span>{pipeLeft > 0 ? `有效倒计时 ${fmtSec(pipeLeft)}` : '正在传输或等待确认...'}</span>
                </div>
              </div>
            </div>
          )}
        </div>
      )}

      {/* Paired Devices Section */}
      <div className="space-y-2.5">
        <div className="flex items-center justify-between gap-2">
          <div className="text-xs font-semibold text-ag-text">
            已配对信任设备（{pairedDevices.length}）
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              disabled={loadingInbox}
              onClick={() => void onRefreshInbox()}
              className="px-2.5 py-1 rounded-lg bg-ag-elevated hover:bg-ag-hover border border-ag-border text-[11px] text-ag-muted hover:text-ag-text flex items-center gap-1 transition"
            >
              <RefreshCw className={`w-3 h-3 ${loadingInbox ? 'animate-spin' : ''}`} />
              检查收件箱
            </button>
            {!showBindPanel && (
              <button
                type="button"
                onClick={() => setShowBindPanel(true)}
                className="px-2.5 py-1 rounded-lg bg-indigo-500/20 hover:bg-indigo-500/30 border border-indigo-500/40 text-[11px] font-medium text-indigo-200 flex items-center gap-1 transition"
              >
                <Plus className="w-3.5 h-3.5" />
                绑定新设备
              </button>
            )}
          </div>
        </div>

        {showBindPanel && (
          <SyncBindPanel
            relay={relay}
            onClose={() => setShowBindPanel(false)}
            onDeviceBound={() => {
              setShowBindPanel(false)
              void onRefreshAll()
            }}
            onToast={onToast}
          />
        )}

        {pairedDevices.length === 0 && !showBindPanel ? (
          <div className="rounded-xl bg-[#0d111a] border border-ag-border p-5 text-center space-y-3">
            <p className="text-xs text-ag-muted leading-relaxed">
              尚未绑定个人设备。绑定手机或另一台电脑后，无需每次扫码或对码，即可享受 <strong>Ed25519 签名认证一键推送</strong>与<strong>离线收件箱同步</strong>。
            </p>
            <button
              type="button"
              onClick={() => setShowBindPanel(true)}
              className="inline-flex items-center gap-1.5 px-4 py-2 rounded-xl bg-indigo-500 hover:bg-indigo-400 text-white text-xs font-medium shadow-glow transition"
            >
              <Plus className="w-4 h-4" />
              + 绑定新设备
            </button>
          </div>
        ) : (
          <div className="space-y-2">
            {pairedDevices.map((dev) => {
              const isMobile = ['android', 'ios'].includes((dev.platform || '').toLowerCase())
              const isPushing = pushingDeviceId === dev.deviceId
              const isRevoking = revokingId === dev.deviceId
              const suggestMsg = suggestPipeByDevice[dev.deviceId]
              const sentMsg = sentSpoolByDevice[dev.deviceId]

              return (
                <div
                  key={dev.deviceId}
                  className="rounded-xl bg-[#0d111a] border border-ag-border p-3.5 space-y-2.5"
                >
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex items-center gap-2.5 min-w-0">
                      <div className="w-8 h-8 rounded-lg bg-ag-elevated border border-ag-border flex items-center justify-center text-indigo-300 shrink-0">
                        {isMobile ? <Smartphone className="w-4 h-4" /> : <Laptop className="w-4 h-4" />}
                      </div>
                      <div className="min-w-0">
                        <div className="flex items-center gap-1.5">
                          <span className="text-xs font-semibold text-ag-text truncate">
                            {dev.label || `${formatPlatformLabel(dev.platform)} 设备`}
                          </span>
                          <span className="text-[10px] px-1.5 py-0.5 rounded bg-ag-elevated text-ag-muted font-mono">
                            {dev.deviceId.slice(0, 8)}
                          </span>
                        </div>
                        <div className="text-[11px] text-ag-muted">
                          {formatPlatformLabel(dev.platform)} · 绑定于 {formatSyncTimestamp(dev.pairedAt || dev.issuedAt)}
                        </div>
                      </div>
                    </div>

                    <div className="flex items-center gap-1.5 shrink-0">
                      <button
                        type="button"
                        disabled={Boolean(pushingDeviceId || revokingId)}
                        onClick={() => void handleSendToDevice(dev.deviceId, 'auto')}
                        className="px-3 py-1.5 rounded-lg bg-indigo-500 hover:bg-indigo-400 text-white text-xs font-medium flex items-center gap-1.5 transition disabled:opacity-50"
                      >
                        {isPushing ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Send className="w-3.5 h-3.5" />}
                        一键推送
                      </button>

                      {confirmRevokeId === dev.deviceId ? (
                        <>
                          <button
                            type="button"
                            disabled={isRevoking}
                            onClick={() => void handleRevoke(dev)}
                            className="px-2.5 py-1.5 rounded-lg bg-rose-600 hover:bg-rose-500 text-white text-xs font-medium flex items-center gap-1 disabled:opacity-50"
                          >
                            {isRevoking && <Loader2 className="w-3 h-3 animate-spin" />}
                            确认吊销
                          </button>
                          <button
                            type="button"
                            disabled={isRevoking}
                            onClick={() => setConfirmRevokeId(null)}
                            className="px-2 py-1.5 rounded-lg text-xs text-ag-muted hover:text-ag-text"
                          >
                            取消
                          </button>
                        </>
                      ) : (
                        <button
                          type="button"
                          disabled={Boolean(pushingDeviceId || revokingId)}
                          onClick={() => setConfirmRevokeId(dev.deviceId)}
                          className="px-2.5 py-1.5 rounded-lg bg-rose-500/[0.12] hover:bg-rose-500/20 border border-rose-500/30 text-rose-300 text-xs flex items-center gap-1 transition disabled:opacity-50"
                        >
                          <Trash2 className="w-3.5 h-3.5" />
                          吊销
                        </button>
                      )}
                    </div>
                  </div>

                  {sentMsg && !suggestMsg && (
                    <div className="rounded-lg bg-emerald-500/10 border border-emerald-500/25 px-2.5 py-1.5 text-[11px] text-emerald-300 flex items-center gap-1.5">
                      <CheckCircle2 className="w-3.5 h-3.5 shrink-0" />
                      <span>{sentMsg}</span>
                    </div>
                  )}

                  {suggestMsg && (
                    <div className="rounded-lg bg-amber-500/10 border border-amber-500/35 px-3 py-2 flex flex-wrap items-center justify-between gap-2 text-[11px] text-amber-200">
                      <span className="flex items-center gap-1.5">
                        <AlertTriangle className="w-3.5 h-3.5 shrink-0 text-amber-400" />
                        {suggestMsg}
                      </span>
                      <button
                        type="button"
                        disabled={Boolean(pushingDeviceId)}
                        onClick={() => void handleSendToDevice(dev.deviceId, 'pipe')}
                        className="px-2.5 py-1 rounded-md bg-amber-500/25 hover:bg-amber-500/35 border border-amber-400/40 text-amber-100 font-medium flex items-center gap-1 shrink-0 transition"
                      >
                        <Zap className="w-3 h-3" />
                        改用实时直传
                      </button>
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}

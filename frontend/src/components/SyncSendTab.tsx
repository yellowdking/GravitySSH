import React, { useEffect, useRef, useState } from 'react'
import {
  AlertTriangle, Check, CheckCircle2, Clock, Copy, Laptop, Loader2,
  QrCode, RefreshCw, ShieldAlert, ShieldCheck, Smartphone, Trash2, XCircle,
} from 'lucide-react'
import type { SyncSendPollResult, SyncSendSession } from '../syncTypes'
import { api } from '../wailsApi'
import { errMsg } from '../utils/errors'
import { useCopyFeedback } from '../hooks/useCopyFeedback'
import { fmtSec, useSyncCountdown } from '../hooks/useSyncCountdown'
import { useSyncPolling } from '../hooks/useSyncPolling'
import { QrCodeSvg } from './QrCodeSvg'

interface Props {
  relayUrl: string
  onToast: (msg: string, type?: 'info' | 'error') => void
}

type SendStage = 'waiting' | 'requested' | 'approved' | 'consumed' | 'rejected' | 'expired' | 'error'

const isActiveStage = (stage: SendStage) =>
  stage === 'waiting' || stage === 'requested' || stage === 'approved'

export const SyncSendTab: React.FC<Props> = ({ relayUrl, onToast }) => {
  const [sess, setSess] = useState<SyncSendSession | null>(null)
  const [stage, setStage] = useState<SendStage>('waiting')
  const [pendingReq, setPendingReq] = useState<SyncSendPollResult | null>(null)
  const [statusMessage, setStatusMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const [decisionBusy, setDecisionBusy] = useState(false)
  const [revoking, setRevoking] = useState(false)

  const left = useSyncCountdown(sess?.expiresAt)
  const { copied, copy, reset: resetCopy } = useCopyFeedback()

  const sessRef = useRef<SyncSendSession | null>(null)
  sessRef.current = sess
  const stageRef = useRef<SendStage>(stage)
  stageRef.current = stage
  const relayRef = useRef(relayUrl)
  relayRef.current = relayUrl

  useEffect(() => {
    return () => {
      const s = sessRef.current
      if (s && isActiveStage(stageRef.current)) {
        void api.cancelSyncSend(s.sessionId, s.relayUrl || relayRef.current).catch(() => {})
      }
    }
  }, [])

  useSyncPolling({
    enabled: Boolean(sess && (stage === 'waiting' || stage === 'approved') && left > 0),
    intervalMs: 1500,
    poll: async () => {
      const current = sessRef.current
      if (!current) return false
      const res = await api.pollSyncSend(current.sessionId)
      if (sessRef.current?.sessionId !== current.sessionId) return false
      switch (res.status) {
        case 'waiting':
          return true
        case 'requested':
          setPendingReq(res)
          setStage('requested')
          return false
        case 'approved':
          setStage('approved')
          return true
        case 'consumed':
          setStage('consumed')
          setStatusMessage(res.message || '对方已成功接收配置，中继密文已焚毁')
          onToast('对方已成功接收配置，中继密文已焚毁')
          return false
        case 'rejected':
          setStage('rejected')
          setStatusMessage(res.message || '已拒绝该同步请求')
          return false
        case 'expired':
          setStage('expired')
          setStatusMessage(res.message || '同步码已过期，请重新生成')
          return false
        case 'error':
          setStage('error')
          setStatusMessage(res.message || '配对验证失败')
          onToast(res.message || '配对验证失败', 'error')
          return false
        default:
          return true
      }
    },
  })

  const gen = async () => {
    setBusy(true)
    try {
      if (sess && isActiveStage(stage)) {
        await api.cancelSyncSend(sess.sessionId, sess.relayUrl || relayUrl).catch(() => {})
      }
      const next = await api.startSyncSend(relayUrl)
      setSess(next)
      setStage('waiting')
      setPendingReq(null)
      setStatusMessage('')
      resetCopy()
      onToast('同步码已生成，等待接收端确认后才会加密上传')
    } catch (e) {
      onToast(errMsg(e), 'error')
    } finally {
      setBusy(false)
    }
  }

  const revoke = async () => {
    if (!sess) return
    setRevoking(true)
    try {
      await api.cancelSyncSend(sess.sessionId, sess.relayUrl || relayUrl)
      setSess(null)
      setPendingReq(null)
      setStage('waiting')
      setStatusMessage('')
      onToast('已撤销同步码')
    } catch (e) {
      onToast(errMsg(e), 'error')
    } finally {
      setRevoking(false)
    }
  }

  const approve = async () => {
    if (!sess || !pendingReq?.requestId || !pendingReq.receiverCode) return
    setDecisionBusy(true)
    try {
      await api.approveSyncSend(sess.sessionId, pendingReq.requestId, pendingReq.receiverCode)
      setStage('approved')
      setStatusMessage('已同意并上传加密数据，等待接收端拉取...')
      onToast('已同意并上传双向加密数据')
    } catch (e) {
      onToast(errMsg(e), 'error')
    } finally {
      setDecisionBusy(false)
    }
  }

  const reject = async () => {
    if (!sess || !pendingReq?.requestId) return
    setDecisionBusy(true)
    try {
      await api.rejectSyncSend(sess.sessionId, pendingReq.requestId)
      setStage('rejected')
      setPendingReq(null)
      setStatusMessage('已拒绝该接收端请求，未上传任何保险库数据')
      onToast('已拒绝不一致的同步请求')
    } catch (e) {
      onToast(errMsg(e), 'error')
    } finally {
      setDecisionBusy(false)
    }
  }

  const handleCopyCode = async () => {
    if (!sess) return
    const ok = await copy(sess.code)
    if (ok) onToast('同步码已复制')
    else onToast('复制失败，请手动记录', 'error')
  }

  if (!sess) {
    return (
      <div className="space-y-4">
        <div className="rounded-xl bg-[#0d111a] border border-ag-border p-4 space-y-2">
          <div className="flex items-center gap-2 text-xs font-medium text-indigo-300">
            <ShieldCheck className="w-4 h-4 shrink-0" /><span>双向核对 · 端到端零知识加密同步</span>
          </div>
          <p className="text-xs text-ag-muted leading-relaxed">
            生成同步码后暂不上传保险库数据。只有当接收端提交请求且您当面核对双方屏幕上的确认码一致并点击同意后，才使用双端密钥联合加密上传，领取即焚。
          </p>
        </div>
        <button onClick={() => void gen()} disabled={busy} className="w-full py-3 rounded-xl bg-indigo-500 hover:bg-indigo-400 disabled:opacity-60 text-white text-sm font-medium transition shadow-glow flex items-center justify-center gap-2">
          {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : <QrCode className="w-4 h-4" />}
          {busy ? '正在初始化通道...' : '一键生成同步码'}
        </button>
      </div>
    )
  }

  if (stage === 'consumed') {
    return (
      <div className="space-y-4">
        <div className="rounded-xl bg-emerald-500/10 border border-emerald-500/35 p-5 space-y-2 text-center">
          <div className="flex items-center justify-center gap-2 text-sm font-semibold text-emerald-300">
            <CheckCircle2 className="w-5 h-5" />
            <span>✓ 对方已成功接收配置，中继密文已焚毁</span>
          </div>
          <p className="text-xs text-ag-muted">
            接收端已使用双方同步码与确认码解密预览，中继服务器上的密文已自动销毁。
          </p>
        </div>
        <button onClick={() => void gen()} disabled={busy} className="w-full py-2.5 rounded-xl bg-ag-elevated hover:bg-ag-hover border border-ag-border text-xs font-medium text-ag-text flex items-center justify-center gap-1.5">
          {busy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <RefreshCw className="w-3.5 h-3.5" />}
          再次生成同步码
        </button>
      </div>
    )
  }

  const expired = left <= 0 || stage === 'expired'
  const stats = [['服务器', sess.serverCount], ['密钥', sess.keyCount], ['快捷', sess.shortcutCount], ['模板', sess.templateCount]]
  const isAndroidReq = (pendingReq?.receiverPlatform || '').toLowerCase() === 'android'

  return (
    <div className="space-y-4">
      <div className="rounded-xl bg-[#0d111a] border border-ag-border p-4 flex flex-col sm:flex-row items-center gap-4">
        <QrCodeSvg value={sess.qrText} size={156} />
        <div className="flex-1 w-full space-y-2.5 text-center sm:text-left">
          <div className="text-[11px] text-ag-muted">发送端同步码（在接收端输入或扫码发起验证）</div>
          <div className="flex items-center justify-center sm:justify-start gap-2">
            <span className="font-mono text-2xl font-bold tracking-[0.18em] text-indigo-200 bg-ag-elevated px-3.5 py-2 rounded-xl border border-indigo-500/35 select-all">{sess.code}</span>
            <button onClick={() => void handleCopyCode()} title="复制" aria-label="复制同步码" className="p-2.5 rounded-xl bg-ag-elevated hover:bg-ag-hover border border-ag-border text-ag-text">
              {copied ? <Check className="w-4 h-4 text-emerald-400" /> : <Copy className="w-4 h-4" />}
            </button>
          </div>
          <div className={`inline-flex items-center gap-1.5 text-xs px-2.5 py-1 rounded-lg border ${expired ? 'bg-rose-500/10 border-rose-500/30 text-rose-300' : 'bg-cyan-500/10 border-cyan-500/30 text-cyan-300'}`}>
            <Clock className="w-3.5 h-3.5" /><span>{expired ? '已过期，请重新生成' : `倒计时 ${fmtSec(left)} · 双向确认后上传`}</span>
          </div>
          <div className="grid grid-cols-4 gap-1.5 pt-1 text-center">
            {stats.map(([k, v]) => (
              <div key={String(k)} className="rounded-lg bg-ag-panel border border-ag-border py-1.5 px-1">
                <div className="text-sm font-semibold text-ag-text font-mono">{v}</div>
                <div className="text-[10px] text-ag-muted">{k}</div>
              </div>
            ))}
          </div>
        </div>
      </div>

      {stage === 'waiting' && !expired && (
        <div className="rounded-xl bg-indigo-500/10 border border-indigo-500/25 px-4 py-3 flex items-center gap-2.5 text-xs text-indigo-200">
          <Loader2 className="w-4 h-4 animate-spin shrink-0 text-indigo-300" />
          <span>等待接收端输入同步码并提交确认...</span>
        </div>
      )}

      {stage === 'requested' && pendingReq && (
        <div className="rounded-xl bg-amber-500/10 border border-amber-500/40 p-4 space-y-3">
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <div className="w-8 h-8 rounded-lg bg-amber-500/20 border border-amber-500/35 flex items-center justify-center text-amber-300">
                {isAndroidReq ? <Smartphone className="w-4 h-4" /> : <Laptop className="w-4 h-4" />}
              </div>
              <div>
                <div className="text-xs font-semibold text-ag-text">
                  收到接收端请求：{pendingReq.receiverDevice || '未知设备'}
                </div>
                <div className="text-[11px] text-ag-muted">
                  平台：{pendingReq.receiverPlatform || 'unknown'}
                </div>
              </div>
            </div>
            <span className="text-[10px] px-2 py-0.5 rounded-full bg-amber-500/20 text-amber-200 border border-amber-500/35">
              待确认
            </span>
          </div>

          <div className="rounded-xl bg-[#0b0d12] border border-amber-500/35 p-3 text-center space-y-1">
            <div className="text-[11px] text-ag-muted">对方接收确认码（请核对是否与对方屏幕完全一致）</div>
            <div className="font-mono text-2xl font-bold tracking-[0.22em] text-amber-300 select-all">
              {pendingReq.receiverCode}
            </div>
          </div>

          <div className="flex items-start gap-2 text-[11px] text-amber-200 leading-relaxed">
            <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5 text-amber-300" />
            <span>请务必确认对方设备屏幕显示的「本机确认码」与上方完全一致。若不一致或非本人操作，请立即拒绝！</span>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 pt-1">
            <button
              onClick={() => void reject()}
              disabled={decisionBusy}
              className="py-2.5 px-3 rounded-xl text-xs font-medium bg-rose-500/[0.12] hover:bg-rose-500/20 text-rose-300 border border-rose-500/35 flex items-center justify-center gap-1.5 disabled:opacity-50"
            >
              <XCircle className="w-4 h-4" />
              不一致，拒绝
            </button>
            <button
              onClick={() => void approve()}
              disabled={decisionBusy}
              className="py-2.5 px-3 rounded-xl text-xs font-medium bg-emerald-600 hover:bg-emerald-500 text-white shadow-glow flex items-center justify-center gap-1.5 disabled:opacity-50"
            >
              {decisionBusy ? <Loader2 className="w-4 h-4 animate-spin" /> : <CheckCircle2 className="w-4 h-4" />}
              {decisionBusy ? '正在加密上传...' : '确认一致，同意发送'}
            </button>
          </div>
        </div>
      )}

      {stage === 'approved' && (
        <div className="rounded-xl bg-emerald-500/10 border border-emerald-500/30 px-4 py-3 flex items-center gap-2.5 text-xs text-emerald-200">
          <Loader2 className="w-4 h-4 animate-spin shrink-0 text-emerald-300" />
          <span>✓ 已同意并上传加密数据，等待接收端拉取...</span>
        </div>
      )}

      {(stage === 'rejected' || stage === 'error') && (
        <div className="rounded-xl bg-rose-500/10 border border-rose-500/35 px-4 py-3 flex items-center gap-2.5 text-xs text-rose-200">
          <ShieldAlert className="w-4 h-4 shrink-0 text-rose-300" />
          <span>{statusMessage || '配对已中止'}</span>
        </div>
      )}

      <div className="flex justify-between gap-2">
        <button onClick={() => void revoke()} disabled={revoking || decisionBusy} className="px-3 py-2 rounded-lg text-xs font-medium bg-rose-500/[0.12] hover:bg-rose-500/20 text-rose-300 border border-rose-500/30 flex items-center gap-1.5 disabled:opacity-50">
          {revoking ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Trash2 className="w-3.5 h-3.5" />}撤销同步码
        </button>
        <button onClick={() => void gen()} disabled={busy || decisionBusy} className="px-3 py-2 rounded-lg text-xs font-medium bg-ag-elevated hover:bg-ag-hover text-ag-text border border-ag-border flex items-center gap-1.5 disabled:opacity-50">
          {busy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <RefreshCw className="w-3.5 h-3.5" />}重新生成
        </button>
      </div>
    </div>
  )
}

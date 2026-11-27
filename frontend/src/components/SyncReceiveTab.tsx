import React, { useCallback, useEffect, useRef, useState } from 'react'
import { CheckCircle2, Clock, Loader2, RefreshCw, ShieldAlert, ShieldCheck, XCircle } from 'lucide-react'
import type { SyncImportSummary, SyncReceivePreview, SyncReceiveWaitSession } from '../syncTypes'
import { api } from '../wailsApi'
import { errMsg } from '../utils/errors'
import { fmtSec, useSyncCountdown } from '../hooks/useSyncCountdown'
import { useSyncPolling } from '../hooks/useSyncPolling'
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

function isRetryablePollError(msg: string): boolean {
  return (
    msg.includes('连接同步中继失败') ||
    msg.includes('读取同步响应失败') ||
    msg.includes('请求过于频繁') ||
    /HTTP\s*5\d\d/.test(msg) ||
    /timeout|timed out|ECONNRESET|network/i.test(msg)
  )
}

export const SyncReceiveTab: React.FC<Props> = ({ relayUrl, onSynced, onToast }) => {
  const [recvCode, setRecvCode] = useState('')
  const [inputCode, setInputCode] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [waitSess, setWaitSess] = useState<SyncReceiveWaitSession | null>(null)
  const [errorBanner, setErrorBanner] = useState('')
  const [applying, setApplying] = useState(false)
  const [preview, setPreview] = useState<SyncReceivePreview | null>(null)
  const [mode, setMode] = useState<'merge' | 'overwrite'>('merge')
  const [summary, setSummary] = useState<SyncImportSummary | null>(null)

  const waitLeft = useSyncCountdown(waitSess?.expiresAt)

  const mountedRef = useRef(true)
  const waitSessRef = useRef<SyncReceiveWaitSession | null>(null)
  waitSessRef.current = waitSess
  const previewRef = useRef<SyncReceivePreview | null>(null)
  previewRef.current = preview

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      if (waitSessRef.current) {
        void api.cancelSyncReceive(waitSessRef.current.waitId).catch(() => {})
      }
      if (previewRef.current) {
        void api.clearSyncPreview(previewRef.current.previewId).catch(() => {})
      }
    }
  }, [])

  const refreshRecvCode = useCallback(async () => {
    try {
      const code = await api.generateReceiverCode()
      if (mountedRef.current) setRecvCode(code)
    } catch (err) {
      if (mountedRef.current) onToast(errMsg(err), 'error')
    }
  }, [onToast])

  useEffect(() => {
    let cancelled = false
    void api.generateReceiverCode().then((code) => {
      if (!cancelled && mountedRef.current) setRecvCode(code)
    }).catch((err) => {
      if (!cancelled && mountedRef.current) onToast(errMsg(err), 'error')
    })
    return () => { cancelled = true }
  }, [onToast])

  useSyncPolling({
    enabled: Boolean(waitSess),
    intervalMs: 1500,
    poll: async () => {
      const current = waitSessRef.current
      if (!current || !mountedRef.current) return false
      try {
        const res = await api.pollSyncReceive(current.waitId)
        if (!mountedRef.current) {
          if (res.status === 'ready' && res.preview) {
            void api.clearSyncPreview(res.preview.previewId).catch(() => {})
          }
          return false
        }
        if (waitSessRef.current?.waitId !== current.waitId) {
          if (res.status === 'ready' && res.preview) {
            void api.clearSyncPreview(res.preview.previewId).catch(() => {})
          }
          return false
        }
        switch (res.status) {
          case 'pending':
            return true
          case 'rejected': {
            const msg = res.message || '发送端已拒绝此次同步请求'
            setWaitSess(null)
            setErrorBanner(msg)
            onToast(msg, 'error')
            void refreshRecvCode()
            return false
          }
          case 'expired': {
            const msg = res.message || '同步码已过期或发送端已取消'
            setWaitSess(null)
            setErrorBanner(msg)
            onToast(msg, 'error')
            return false
          }
          case 'ready':
            if (res.preview) {
              setWaitSess(null)
              setErrorBanner('')
              setPreview(res.preview)
              setMode('merge')
              return false
            }
            return true
          default:
            return true
        }
      } catch (err) {
        if (!mountedRef.current || waitSessRef.current?.waitId !== current.waitId) return false
        const msg = errMsg(err)
        if (!isRetryablePollError(msg)) {
          setWaitSess(null)
          setErrorBanner(msg)
          onToast(msg, 'error')
          void refreshRecvCode()
          return false
        }
        return true
      }
    },
  })

  const handleStartVerify = async () => {
    if (!inputCode.trim()) { onToast('请输入发送端 8 位同步码或粘贴链接', 'error'); return }
    if (!recvCode.trim()) { onToast('正在生成本机确认码，请稍候', 'error'); return }
    setSubmitting(true)
    setErrorBanner('')
    setSummary(null)
    try {
      const ws = await api.startSyncReceive(inputCode, recvCode, relayUrl)
      if (!mountedRef.current) {
        void api.cancelSyncReceive(ws.waitId).catch(() => {})
        return
      }
      setWaitSess(ws)
    } catch (err) {
      if (mountedRef.current) {
        const msg = errMsg(err)
        setErrorBanner(msg)
        onToast(msg, 'error')
      }
    } finally {
      if (mountedRef.current) setSubmitting(false)
    }
  }

  const handleCancelWait = () => {
    if (!waitSess) return
    const wid = waitSess.waitId
    setWaitSess(null)
    void api.cancelSyncReceive(wid).catch(() => {})
  }

  const handleDiscardPreview = () => {
    if (preview) {
      void api.clearSyncPreview(preview.previewId).catch(() => {})
    }
    setPreview(null)
    void refreshRecvCode()
  }

  const handleApply = async () => {
    if (!preview) return
    setApplying(true)
    try {
      const res = await api.applySyncReceive(preview.previewId, mode)
      onSynced()
      if (!mountedRef.current) return
      setSummary(res)
      setPreview(null)
      setInputCode('')
      setErrorBanner('')
      onToast(mode === 'merge' ? '已安全合并同步配置到本地保险库' : '已使用同步配置覆盖本地数据')
    } catch (err) {
      if (mountedRef.current) onToast(errMsg(err), 'error')
    } finally {
      if (mountedRef.current) setApplying(false)
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
        <button
          onClick={() => {
            setSummary(null)
            void refreshRecvCode()
          }}
          className="w-full py-2.5 rounded-xl bg-ag-elevated hover:bg-ag-hover border border-ag-border text-xs text-ag-text transition"
        >
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
        onDiscard={handleDiscardPreview}
        onApply={() => void handleApply()}
      />
    )
  }

  if (waitSess) {
    return (
      <div className="space-y-4">
        <div className="rounded-xl bg-[#0d111a] border border-indigo-500/35 p-5 space-y-3 text-center">
          <div className="flex items-center justify-center gap-2 text-xs font-medium text-indigo-300">
            <Loader2 className="w-4 h-4 animate-spin" />
            <span>已提交给发送端，等待发送端核对本机确认码并点击同意</span>
          </div>
          <div className="rounded-xl bg-[#0b0d12] border border-indigo-500/40 py-3 px-4 inline-block">
            <div className="text-[11px] text-ag-muted mb-1">本机确认码</div>
            <div className="font-mono text-2xl font-bold tracking-[0.22em] text-indigo-200 select-all">
              {waitSess.receiverCode}
            </div>
          </div>
          <p className="text-xs text-ag-muted leading-relaxed">
            请在发送端设备上核对屏幕显示的接收确认码为 <span className="font-mono text-indigo-200">{waitSess.receiverCode}</span> 后点击「确认一致，同意发送」。
          </p>
          <div className="inline-flex items-center gap-1.5 text-xs px-2.5 py-1 rounded-lg bg-cyan-500/10 border border-cyan-500/30 text-cyan-300">
            <Clock className="w-3.5 h-3.5" />
            <span>{waitLeft <= 0 ? '即将过期' : `等待超时 ${fmtSec(waitLeft)}`}</span>
          </div>
        </div>
        <button
          onClick={handleCancelWait}
          className="w-full py-2.5 rounded-xl bg-rose-500/[0.12] hover:bg-rose-500/20 border border-rose-500/30 text-xs font-medium text-rose-300 flex items-center justify-center gap-1.5 transition"
        >
          <XCircle className="w-4 h-4" />
          取消等待
        </button>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="rounded-xl bg-[#0d111a] border border-ag-border p-3.5 flex items-center justify-between gap-3">
        <div className="space-y-0.5">
          <div className="text-[11px] text-ag-muted">本机确认码（发送端核对用）</div>
          <div className="font-mono text-lg font-bold tracking-[0.2em] text-indigo-200 select-all">
            {recvCode || '---- ----'}
          </div>
        </div>
        <button
          type="button"
          onClick={() => void refreshRecvCode()}
          disabled={submitting}
          className="px-2.5 py-1.5 rounded-lg bg-ag-elevated hover:bg-ag-hover border border-ag-border text-xs text-ag-text flex items-center gap-1.5 disabled:opacity-50"
        >
          <RefreshCw className="w-3.5 h-3.5" />
          刷新本机码
        </button>
      </div>

      {errorBanner && (
        <div className="rounded-xl bg-rose-500/10 border border-rose-500/35 px-3.5 py-2.5 flex items-center gap-2 text-xs text-rose-200">
          <ShieldAlert className="w-4 h-4 shrink-0 text-rose-300" />
          <span>{errorBanner}</span>
        </div>
      )}

      <div className="space-y-2">
        <label className="text-xs font-medium text-ag-text block">
          输入发送端 8 位同步码（支持直接粘贴 <span className="font-mono text-indigo-300">gssh-sync://</span> 链接）
        </label>
        <input
          type="text"
          value={inputCode}
          onChange={(e) => setInputCode(formatSyncInput(e.target.value))}
          onKeyDown={(e) => { if (e.key === 'Enter' && !submitting) void handleStartVerify() }}
          placeholder="XXXX-XXXX"
          autoFocus
          className="w-full rounded-xl bg-[#0b0d12] border border-ag-border px-4 py-3 text-center font-mono text-xl tracking-[0.22em] text-ag-text placeholder:text-ag-subtle focus:outline-none focus:border-indigo-500/70 transition"
        />
        <p className="text-[11px] text-ag-muted">
          点击后将使用发送端同步码加密提交本机确认码，待发送端核对一致并同意后，再用双方密钥联合解密预览。
        </p>
      </div>
      <button
        onClick={() => void handleStartVerify()}
        disabled={submitting || !recvCode || inputCode.replace(/-/g, '').length < 8}
        className="w-full py-3 rounded-xl bg-indigo-500 hover:bg-indigo-400 disabled:opacity-50 text-white text-sm font-medium transition shadow-glow flex items-center justify-center gap-2"
      >
        {submitting ? <Loader2 className="w-4 h-4 animate-spin" /> : <ShieldCheck className="w-4 h-4" />}
        {submitting ? '正在提交验证请求...' : '发起验证并接收'}
      </button>
    </div>
  )
}

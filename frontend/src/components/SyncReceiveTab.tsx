import React, { useCallback, useEffect, useState } from 'react'
import { CheckCircle2, Clock, Loader2, RefreshCw, ShieldAlert, ShieldCheck, XCircle } from 'lucide-react'
import type { SyncImportSummary, SyncReceivePreview, SyncReceiveWaitSession } from '../syncTypes'
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

const fmtSec = (n: number) =>
  `${String(Math.floor(Math.max(0, n) / 60)).padStart(2, '0')}:${String(Math.max(0, n) % 60).padStart(2, '0')}`

export const SyncReceiveTab: React.FC<Props> = ({ relayUrl, onSynced, onToast }) => {
  const [recvCode, setRecvCode] = useState('')
  const [inputCode, setInputCode] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [waitSess, setWaitSess] = useState<SyncReceiveWaitSession | null>(null)
  const [waitLeft, setWaitLeft] = useState(0)
  const [errorBanner, setErrorBanner] = useState('')
  const [applying, setApplying] = useState(false)
  const [preview, setPreview] = useState<SyncReceivePreview | null>(null)
  const [mode, setMode] = useState<'merge' | 'overwrite'>('merge')
  const [summary, setSummary] = useState<SyncImportSummary | null>(null)

  const refreshRecvCode = useCallback(async () => {
    try {
      const code = await api.generateReceiverCode()
      setRecvCode(code)
    } catch (err) {
      onToast(String(err), 'error')
    }
  }, [onToast])

  useEffect(() => {
    let cancelled = false
    void api.generateReceiverCode().then((code) => {
      if (!cancelled) setRecvCode(code)
    }).catch((err) => {
      if (!cancelled) onToast(String(err), 'error')
    })
    return () => { cancelled = true }
  }, [onToast])

  useEffect(() => {
    if (!waitSess) { setWaitLeft(0); return }
    const expiresMs = new Date(waitSess.expiresAt).getTime()
    let id: ReturnType<typeof setInterval> | null = null
    const tick = () => {
      const rem = Math.max(0, Math.floor((expiresMs - Date.now()) / 1000))
      setWaitLeft(rem)
      if (rem <= 0 && id !== null) {
        clearInterval(id)
        id = null
      }
      return rem
    }
    if (tick() > 0) {
      id = setInterval(tick, 1000)
    }
    return () => {
      if (id !== null) clearInterval(id)
    }
  }, [waitSess])

  useEffect(() => {
    if (!waitSess) return
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | null = null

    const pollOnce = async () => {
      try {
        const res = await api.pollSyncReceive(waitSess.waitId)
        if (cancelled) return
        switch (res.status) {
          case 'pending':
            timer = setTimeout(() => { void pollOnce() }, 1500)
            return
          case 'rejected': {
            const msg = res.message || '发送端已拒绝此次同步请求'
            setWaitSess(null)
            setErrorBanner(msg)
            onToast(msg, 'error')
            void refreshRecvCode()
            return
          }
          case 'expired': {
            const msg = res.message || '同步码已过期或发送端已取消'
            setWaitSess(null)
            setErrorBanner(msg)
            onToast(msg, 'error')
            return
          }
          case 'ready':
            if (res.preview) {
              setWaitSess(null)
              setErrorBanner('')
              setPreview(res.preview)
              setMode('merge')
              return
            }
            timer = setTimeout(() => { void pollOnce() }, 1500)
            return
          default:
            timer = setTimeout(() => { void pollOnce() }, 1500)
        }
      } catch (err) {
        if (cancelled) return
        const msg = String(err)
        if (msg.includes('解密') || msg.includes('校验')) {
          setWaitSess(null)
          setErrorBanner(msg)
          onToast(msg, 'error')
          return
        }
        timer = setTimeout(() => { void pollOnce() }, 1500)
      }
    }

    timer = setTimeout(() => { void pollOnce() }, 1500)
    return () => {
      cancelled = true
      if (timer !== null) {
        clearTimeout(timer)
        timer = null
      }
    }
  }, [waitSess, onToast, refreshRecvCode])

  const handleStartVerify = async () => {
    if (!inputCode.trim()) { onToast('请输入发送端 8 位同步码或粘贴链接', 'error'); return }
    if (!recvCode.trim()) { onToast('正在生成本机确认码，请稍候', 'error'); return }
    setSubmitting(true)
    setErrorBanner('')
    setSummary(null)
    try {
      const ws = await api.startSyncReceive(inputCode, recvCode, relayUrl)
      setWaitSess(ws)
    } catch (err) {
      const msg = String(err)
      setErrorBanner(msg)
      onToast(msg, 'error')
    } finally {
      setSubmitting(false)
    }
  }

  const handleCancelWait = () => {
    if (!waitSess) return
    const wid = waitSess.waitId
    setWaitSess(null)
    void api.cancelSyncReceive(wid)
  }

  const handleDiscardPreview = () => {
    if (preview) {
      void api.clearSyncPreview(preview.previewId)
    }
    setPreview(null)
    void refreshRecvCode()
  }

  const handleApply = async () => {
    if (!preview) return
    setApplying(true)
    try {
      const res = await api.applySyncReceive(preview.previewId, mode)
      setSummary(res)
      setPreview(null)
      setInputCode('')
      setErrorBanner('')
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

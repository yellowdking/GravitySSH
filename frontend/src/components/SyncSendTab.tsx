import React, { useEffect, useState } from 'react'
import { Check, Clock, Copy, Loader2, QrCode, RefreshCw, ShieldCheck, Trash2 } from 'lucide-react'
import type { SyncSendSession } from '../syncTypes'
import { api } from '../wailsApi'
import { QrCodeSvg } from './QrCodeSvg'

interface Props {
  relayUrl: string
  onToast: (msg: string, type?: 'info' | 'error') => void
}

const fmtSec = (n: number) => `${String(Math.floor(Math.max(0, n) / 60)).padStart(2, '0')}:${String(Math.max(0, n) % 60).padStart(2, '0')}`

export const SyncSendTab: React.FC<Props> = ({ relayUrl, onToast }) => {
  const [sess, setSess] = useState<SyncSendSession | null>(null)
  const [busy, setBusy] = useState(false)
  const [revoking, setRevoking] = useState(false)
  const [copied, setCopied] = useState(false)
  const [left, setLeft] = useState(0)

  useEffect(() => {
    if (!sess) { setLeft(0); return }
    const tick = () => setLeft(Math.max(0, Math.floor((new Date(sess.expiresAt).getTime() - Date.now()) / 1000)))
    tick()
    const id = setInterval(tick, 1000)
    return () => clearInterval(id)
  }, [sess])

  const gen = async () => {
    setBusy(true)
    try {
      setSess(await api.startSyncSend(relayUrl)); setCopied(false)
      onToast('同步码已生成，中继仅内存暂存 5 分钟')
    } catch (e) { onToast(String(e), 'error') } finally { setBusy(false) }
  }

  const revoke = async () => {
    if (!sess) return
    setRevoking(true)
    try {
      await api.cancelSyncSend(sess.code, sess.relayUrl || relayUrl)
      setSess(null); onToast('已撤销同步码')
    } catch (e) { onToast(String(e), 'error') } finally { setRevoking(false) }
  }

  const copy = async () => {
    if (!sess) return
    try {
      await navigator.clipboard.writeText(sess.code)
      setCopied(true); onToast('同步码已复制')
      setTimeout(() => setCopied(false), 1800)
    } catch { onToast('复制失败，请手动记录', 'error') }
  }

  if (!sess) {
    return (
      <div className="space-y-4">
        <div className="rounded-xl bg-[#0d111a] border border-ag-border p-4 space-y-2">
          <div className="flex items-center gap-2 text-xs font-medium text-indigo-300">
            <ShieldCheck className="w-4 h-4 shrink-0" /><span>端到端零知识加密同步</span>
          </div>
          <p className="text-xs text-ag-muted leading-relaxed">
            本机用 8 位同步码派生 AES-256-GCM 密钥加密服务器、SSH 密钥、快捷命令与模板，中继仅内存暂存 5 分钟，领取即焚。
          </p>
        </div>
        <button onClick={() => void gen()} disabled={busy} className="w-full py-3 rounded-xl bg-indigo-500 hover:bg-indigo-400 disabled:opacity-60 text-white text-sm font-medium transition shadow-glow flex items-center justify-center gap-2">
          {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : <QrCode className="w-4 h-4" />}
          {busy ? '正在加密并上传...' : '一键生成同步码'}
        </button>
      </div>
    )
  }

  const expired = left <= 0
  const stats = [['服务器', sess.serverCount], ['密钥', sess.keyCount], ['快捷', sess.shortcutCount], ['模板', sess.templateCount]]

  return (
    <div className="space-y-4">
      <div className="rounded-xl bg-[#0d111a] border border-ag-border p-4 flex flex-col sm:flex-row items-center gap-4">
        <QrCodeSvg value={sess.qrText} size={156} />
        <div className="flex-1 w-full space-y-2.5 text-center sm:text-left">
          <div className="text-[11px] text-ag-muted">在 Android 或另一台 GravitySSH 输入或扫描</div>
          <div className="flex items-center justify-center sm:justify-start gap-2">
            <span className="font-mono text-2xl font-bold tracking-[0.18em] text-indigo-200 bg-ag-elevated px-3.5 py-2 rounded-xl border border-indigo-500/35 select-all">{sess.code}</span>
            <button onClick={() => void copy()} title="复制" className="p-2.5 rounded-xl bg-ag-elevated hover:bg-ag-hover border border-ag-border text-ag-text">
              {copied ? <Check className="w-4 h-4 text-emerald-400" /> : <Copy className="w-4 h-4" />}
            </button>
          </div>
          <div className={`inline-flex items-center gap-1.5 text-xs px-2.5 py-1 rounded-lg border ${expired ? 'bg-rose-500/10 border-rose-500/30 text-rose-300' : 'bg-cyan-500/10 border-cyan-500/30 text-cyan-300'}`}>
            <Clock className="w-3.5 h-3.5" /><span>{expired ? '已过期，请重新生成' : `倒计时 ${fmtSec(left)} · 领取即焚`}</span>
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

      <div className="flex justify-between gap-2">
        <button onClick={() => void revoke()} disabled={revoking} className="px-3 py-2 rounded-lg text-xs font-medium bg-rose-500/12 hover:bg-rose-500/20 text-rose-300 border border-rose-500/30 flex items-center gap-1.5">
          {revoking ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Trash2 className="w-3.5 h-3.5" />}撤销同步码
        </button>
        <button onClick={() => void gen()} disabled={busy} className="px-3 py-2 rounded-lg text-xs font-medium bg-ag-elevated hover:bg-ag-hover text-ag-text border border-ag-border flex items-center gap-1.5">
          {busy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <RefreshCw className="w-3.5 h-3.5" />}重新生成
        </button>
      </div>
    </div>
  )
}

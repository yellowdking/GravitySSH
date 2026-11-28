import React, { useState } from 'react'
import { AlertTriangle, CheckCircle2, KeyRound, Loader2, ShieldAlert, XCircle } from 'lucide-react'
import { formatSasDisplay } from '../utils/syncUri'

interface SyncSasInputProps {
  variant: 'input'
  attemptsRemaining?: number
  busy: boolean
  onSubmit: (sasDigits: string) => Promise<boolean> | void
  onCancel: () => void
}

interface SyncSasConfirmProps {
  variant: 'confirm'
  sasCode?: string
  approveLabel?: string
  busy: boolean
  onConfirm: (approved: boolean) => void
}

export type SyncSasPromptProps = SyncSasInputProps | SyncSasConfirmProps

export const SyncSasPrompt: React.FC<SyncSasPromptProps> = (props) => {
  const [digits, setDigits] = useState('')

  if (props.variant === 'confirm') {
    const formatted = formatSasDisplay(props.sasCode)
    return (
      <div className="rounded-xl bg-amber-500/10 border border-amber-500/40 p-4 space-y-3.5">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2 text-xs font-semibold text-amber-200">
            <ShieldAlert className="w-4 h-4 text-amber-400 shrink-0" />
            <span>请核对 6 位安全码 (SAS 双向认证)</span>
          </div>
          <span className="text-[10px] px-2 py-0.5 rounded-full bg-amber-500/20 text-amber-200 border border-amber-500/35">
            必须人工核验
          </span>
        </div>

        <div className="rounded-xl bg-[#0b0d12] border border-amber-500/40 py-3.5 px-4 text-center space-y-1">
          <div className="text-[11px] text-ag-muted">本机派生的 6 位安全码（请告知发送端输入并当面核对）</div>
          <div className="font-mono text-3xl font-bold tracking-[0.26em] text-amber-300 select-all">
            {formatted}
          </div>
        </div>

        <div className="flex items-start gap-2 text-[11px] text-amber-200/95 leading-relaxed bg-amber-950/35 rounded-lg p-2.5 border border-amber-500/25">
          <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5 text-amber-400" />
          <span>
            反社工安全提醒：请务必<strong>当面查看发送端屏幕</strong>或通过<strong>实时语音</strong>核对双方 6 位安全码完全一致。切勿盲点通过，若不一致请立即中断！
          </span>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 pt-0.5">
          <button
            type="button"
            disabled={props.busy}
            onClick={() => props.onConfirm(false)}
            className="py-2.5 px-3 rounded-xl text-xs font-medium bg-rose-500/15 hover:bg-rose-500/25 text-rose-200 border border-rose-500/35 flex items-center justify-center gap-1.5 transition disabled:opacity-50"
          >
            <XCircle className="w-4 h-4" />
            不一致，立即中断
          </button>
          <button
            type="button"
            disabled={props.busy}
            onClick={() => props.onConfirm(true)}
            className="py-2.5 px-3 rounded-xl text-xs font-medium bg-emerald-600 hover:bg-emerald-500 text-white shadow-glow flex items-center justify-center gap-1.5 transition disabled:opacity-50"
          >
            {props.busy ? <Loader2 className="w-4 h-4 animate-spin" /> : <CheckCircle2 className="w-4 h-4" />}
            {props.busy ? '正在确认...' : props.approveLabel || '一致，允许接收'}
          </button>
        </div>
      </div>
    )
  }

  const remaining = props.attemptsRemaining ?? 3
  const cleanDigits = digits.replace(/\D/g, '').slice(0, 6)
  const displayValue = cleanDigits.length > 3 ? `${cleanDigits.slice(0, 3)}-${cleanDigits.slice(3)}` : cleanDigits

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (props.busy || cleanDigits.length !== 6) return
    const ok = await props.onSubmit(cleanDigits)
    if (ok === false) setDigits('')
  }

  return (
    <form onSubmit={(e) => void handleSubmit(e)} className="rounded-xl bg-indigo-500/10 border border-indigo-500/35 p-4 space-y-3.5">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2 text-xs font-semibold text-indigo-200">
          <KeyRound className="w-4 h-4 text-indigo-300 shrink-0" />
          <span>接收端已加入房间 · 请输入对方屏幕的 6 位安全码</span>
        </div>
        <span className="text-[10px] px-2 py-0.5 rounded-full bg-indigo-500/20 text-indigo-200 border border-indigo-500/35">
          剩余尝试 {remaining} 次
        </span>
      </div>

      <input
        type="text"
        inputMode="numeric"
        autoComplete="off"
        autoFocus
        disabled={props.busy}
        value={displayValue}
        onChange={(e) => setDigits(e.target.value.replace(/\D/g, '').slice(0, 6))}
        placeholder="000-000"
        aria-label="6 位安全码输入"
        className="w-full rounded-xl bg-[#0b0d12] border border-indigo-500/40 px-4 py-3 text-center font-mono text-2xl font-bold tracking-[0.26em] text-indigo-100 placeholder:text-ag-subtle focus:outline-none focus:border-indigo-400 transition disabled:opacity-60"
      />

      <div className="flex items-start gap-2 text-[11px] text-amber-200/95 leading-relaxed bg-amber-950/30 rounded-lg p-2.5 border border-amber-500/25">
        <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5 text-amber-400" />
        <span>
          反社工警告：请仅输入您<strong>本人当面看到</strong>或<strong>实时通话确认</strong>的接收端 6 位数字。切勿输入他人通过聊天文字发来的验证码，输错 3 次房间将自动焚毁。
        </span>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
        <button
          type="button"
          disabled={props.busy}
          onClick={props.onCancel}
          className="py-2.5 px-3 rounded-xl text-xs font-medium bg-rose-500/15 hover:bg-rose-500/25 text-rose-200 border border-rose-500/30 flex items-center justify-center gap-1.5 transition disabled:opacity-50"
        >
          <XCircle className="w-4 h-4" />
          取消并销毁房间
        </button>
        <button
          type="submit"
          disabled={props.busy || cleanDigits.length !== 6}
          className="py-2.5 px-3 rounded-xl text-xs font-medium bg-indigo-500 hover:bg-indigo-400 text-white shadow-glow flex items-center justify-center gap-1.5 transition disabled:opacity-50"
        >
          {props.busy ? <Loader2 className="w-4 h-4 animate-spin" /> : <CheckCircle2 className="w-4 h-4" />}
          {props.busy ? '正在核验并传输...' : '提交核验并发送'}
        </button>
      </div>
    </form>
  )
}

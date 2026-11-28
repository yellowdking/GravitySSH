import React, { useState } from 'react'
import { CheckCircle2, KeyRound, Loader2, ShieldAlert, XCircle } from 'lucide-react'
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
        <div className="flex items-center gap-2 text-xs font-semibold text-amber-200">
          <ShieldAlert className="w-4 h-4 text-amber-400 shrink-0" />
          <span>请核对 6 位安全码</span>
        </div>

        <div className="rounded-xl bg-[#0b0d12] border border-amber-500/40 py-3.5 px-4 text-center space-y-1">
          <div className="text-[11px] text-ag-muted">请将此 6 位安全码告知对方并核对一致</div>
          <div className="font-mono text-3xl font-bold tracking-[0.26em] text-amber-300 select-all">
            {formatted}
          </div>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 pt-0.5">
          <button
            type="button"
            disabled={props.busy}
            onClick={() => props.onConfirm(false)}
            className="py-2.5 px-3 rounded-xl text-xs font-medium bg-rose-500/15 hover:bg-rose-500/25 text-rose-200 border border-rose-500/35 flex items-center justify-center gap-1.5 transition disabled:opacity-50"
          >
            <XCircle className="w-4 h-4" />
            不一致，中断
          </button>
          <button
            type="button"
            disabled={props.busy}
            onClick={() => props.onConfirm(true)}
            className="py-2.5 px-3 rounded-xl text-xs font-medium bg-emerald-600 hover:bg-emerald-500 text-white shadow-glow flex items-center justify-center gap-1.5 transition disabled:opacity-50"
          >
            {props.busy ? <Loader2 className="w-4 h-4 animate-spin" /> : <CheckCircle2 className="w-4 h-4" />}
            {props.busy ? '确认中...' : props.approveLabel || '一致，确认接收'}
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
          <span>请输入对方显示的 6 位安全码</span>
        </div>
        <span className="text-[10px] px-2 py-0.5 rounded-full bg-indigo-500/20 text-indigo-200 border border-indigo-500/35">
          剩余 {remaining} 次
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

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
        <button
          type="button"
          disabled={props.busy}
          onClick={props.onCancel}
          className="py-2.5 px-3 rounded-xl text-xs font-medium bg-rose-500/15 hover:bg-rose-500/25 text-rose-200 border border-rose-500/30 flex items-center justify-center gap-1.5 transition disabled:opacity-50"
        >
          <XCircle className="w-4 h-4" />
          取消
        </button>
        <button
          type="submit"
          disabled={props.busy || cleanDigits.length !== 6}
          className="py-2.5 px-3 rounded-xl text-xs font-medium bg-indigo-500 hover:bg-indigo-400 text-white shadow-glow flex items-center justify-center gap-1.5 transition disabled:opacity-50"
        >
          {props.busy ? <Loader2 className="w-4 h-4 animate-spin" /> : <CheckCircle2 className="w-4 h-4" />}
          {props.busy ? '验证中...' : '确认'}
        </button>
      </div>
    </form>
  )
}

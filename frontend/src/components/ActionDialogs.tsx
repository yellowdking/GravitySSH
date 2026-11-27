import React, { useEffect, useState } from 'react'
import { AlertTriangle, HelpCircle, X } from 'lucide-react'
import { ModalShell } from './ModalShell'

export interface ConfirmDialogProps {
  open: boolean
  title: string
  message: string
  confirmLabel?: string
  cancelLabel?: string
  danger?: boolean
  zIndexClass?: string
  onConfirm: () => void
  onCancel: () => void
}

export const ConfirmDialog: React.FC<ConfirmDialogProps> = ({
  open,
  title,
  message,
  confirmLabel = '确定',
  cancelLabel = '取消',
  danger = true,
  zIndexClass = 'z-[70]',
  onConfirm,
  onCancel,
}) => (
  <ModalShell open={open} onClose={onCancel} zIndexClass={zIndexClass} backdropClassName="bg-black/75">
    <div className="w-full max-w-md rounded-2xl bg-ag-panel border border-ag-borderStrong shadow-ag overflow-hidden">
      <div className="px-5 py-3.5 border-b border-ag-border bg-[#11151f] flex items-center justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          <AlertTriangle className={`w-4 h-4 shrink-0 ${danger ? 'text-rose-400' : 'text-amber-300'}`} />
          <h3 className="text-sm font-semibold text-ag-text truncate">{title}</h3>
        </div>
        <button type="button" onClick={onCancel} aria-label="关闭确认弹窗" className="p-1 rounded-lg hover:bg-ag-hover text-ag-muted hover:text-ag-text">
          <X className="w-4 h-4" />
        </button>
      </div>
      <div className="p-5 text-xs text-ag-text leading-relaxed whitespace-pre-line">{message}</div>
      <div className="px-5 py-3.5 border-t border-ag-border bg-[#11151f] flex justify-end gap-2">
        <button type="button" onClick={onCancel} className="ag-btn-ghost text-xs">{cancelLabel}</button>
        <button
          type="button"
          autoFocus
          onClick={onConfirm}
          className={danger
            ? 'inline-flex items-center justify-center rounded-lg bg-rose-600 hover:bg-rose-500 text-white font-medium text-xs px-4 py-2 transition'
            : 'ag-btn-primary text-xs'}
        >
          {confirmLabel}
        </button>
      </div>
    </div>
  </ModalShell>
)

export interface PromptDialogProps {
  open: boolean
  title: string
  message?: string
  defaultValue?: string
  placeholder?: string
  presets?: string[]
  validate?: (val: string) => string | null
  confirmLabel?: string
  cancelLabel?: string
  zIndexClass?: string
  onConfirm: (val: string) => void
  onCancel: () => void
}

export const PromptDialog: React.FC<PromptDialogProps> = ({
  open,
  title,
  message,
  defaultValue = '',
  placeholder = '',
  presets,
  validate,
  confirmLabel = '确定',
  cancelLabel = '取消',
  zIndexClass = 'z-[70]',
  onConfirm,
  onCancel,
}) => {
  const [value, setValue] = useState(defaultValue)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (open) {
      setValue(defaultValue)
      setError(null)
    }
  }, [open, defaultValue])

  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    if (validate) {
      const err = validate(value)
      if (err) {
        setError(err)
        return
      }
    }
    onConfirm(value)
  }

  return (
    <ModalShell open={open} onClose={onCancel} zIndexClass={zIndexClass} backdropClassName="bg-black/75">
      <form onSubmit={submit} className="w-full max-w-md rounded-2xl bg-ag-panel border border-ag-borderStrong shadow-ag overflow-hidden">
        <div className="px-5 py-3.5 border-b border-ag-border bg-[#11151f] flex items-center justify-between gap-2">
          <div className="flex items-center gap-2 min-w-0">
            <HelpCircle className="w-4 h-4 text-indigo-400 shrink-0" />
            <h3 className="text-sm font-semibold text-ag-text truncate">{title}</h3>
          </div>
          <button type="button" onClick={onCancel} aria-label="关闭输入弹窗" className="p-1 rounded-lg hover:bg-ag-hover text-ag-muted hover:text-ag-text">
            <X className="w-4 h-4" />
          </button>
        </div>
        <div className="p-5 space-y-3">
          {message && <p className="text-xs text-ag-muted leading-relaxed">{message}</p>}
          {presets && presets.length > 0 && (
            <div className="flex flex-wrap gap-1.5">
              {presets.map((p) => (
                <button
                  key={p}
                  type="button"
                  onClick={() => { setValue(p); setError(null) }}
                  className={`px-2.5 py-1 rounded-lg text-xs font-mono border transition ${
                    value.trim().toLowerCase() === p.toLowerCase()
                      ? 'bg-indigo-500/20 border-indigo-400/60 text-indigo-200'
                      : 'bg-[#0d1017] border-ag-border text-ag-muted hover:text-ag-text'
                  }`}
                >
                  {p}
                </button>
              ))}
            </div>
          )}
          <input
            type="text"
            autoFocus
            value={value}
            placeholder={placeholder}
            onChange={(e) => { setValue(e.target.value); setError(null) }}
            className="ag-input font-mono text-xs"
          />
          {error && <p className="text-xs text-rose-300">{error}</p>}
        </div>
        <div className="px-5 py-3.5 border-t border-ag-border bg-[#11151f] flex justify-end gap-2">
          <button type="button" onClick={onCancel} className="ag-btn-ghost text-xs">{cancelLabel}</button>
          <button type="submit" className="ag-btn-primary text-xs">{confirmLabel}</button>
        </div>
      </form>
    </ModalShell>
  )
}

import React, { useEffect, useMemo, useState } from 'react'
import { AlertTriangle, CheckCircle2, ShieldAlert, Sparkles, X } from 'lucide-react'
import { inspectPasteSafety, stripHiddenChars } from '../utils/pasteGuard'
import { ModalShell } from './ModalShell'

interface Props {
  open: boolean
  initialText: string
  bracketedPasteActive: boolean
  onConfirm: (finalText: string) => void
  onCancel: () => void
}

export const PasteGuardModal: React.FC<Props> = ({
  open,
  initialText,
  bracketedPasteActive,
  onConfirm,
  onCancel,
}) => {
  const [text, setText] = useState(initialText)

  useEffect(() => {
    if (open) setText(initialText)
  }, [open, initialText])

  const live = useMemo(
    () => inspectPasteSafety(text, bracketedPasteActive),
    [text, bracketedPasteActive]
  )

  if (!open) return null

  return (
    <ModalShell open={open} onClose={onCancel} backdropClassName="bg-black/75">
      <div className="w-full max-w-xl rounded-2xl bg-ag-panel border border-amber-500/40 shadow-ag overflow-hidden flex flex-col max-h-[85vh]">
        <div className="px-5 py-3.5 border-b border-ag-border bg-[#11151f] flex items-center justify-between">
          <div className="flex items-center gap-2.5">
            <div className="w-8 h-8 rounded-lg bg-amber-500/15 border border-amber-500/35 flex items-center justify-center text-amber-300">
              <ShieldAlert className="w-4 h-4" />
            </div>
            <div>
              <h3 className="text-sm font-semibold text-ag-text">粘贴安全检查</h3>
              <p className="text-[11px] text-ag-muted">检测到潜在风险内容，可修改后再发送到终端</p>
            </div>
          </div>
          <button type="button" onClick={onCancel} className="p-1.5 rounded-lg hover:bg-ag-hover text-ag-muted hover:text-ag-text">
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="p-5 space-y-3.5 overflow-y-auto flex-1">
          <div className="space-y-1.5">
            {live.warnings.length === 0 ? (
              <div className="flex items-center gap-2 rounded-xl px-3 py-2 text-xs border bg-emerald-500/10 border-emerald-500/30 text-emerald-200">
                <CheckCircle2 className="w-3.5 h-3.5 shrink-0" />
                <span>当前内容已无明显安全风险，可放心粘贴</span>
              </div>
            ) : (
              live.warnings.map((w, idx) => (
                <div
                  key={`${w.kind}-${idx}`}
                  className={`flex items-start gap-2 rounded-xl px-3 py-2 text-xs border ${
                    w.kind === 'danger' || w.kind === 'hidden-chars' || w.kind === 'homoglyph'
                      ? 'bg-rose-500/10 border-rose-500/30 text-rose-200'
                      : 'bg-amber-500/10 border-amber-500/30 text-amber-200'
                  }`}
                >
                  <AlertTriangle className="w-3.5 h-3.5 shrink-0 mt-0.5" />
                  <div>
                    <span className="font-semibold">{w.label}：</span>
                    <span className="opacity-90">{w.detail}</span>
                  </div>
                </div>
              ))
            )}
          </div>

          <div>
            <div className="flex items-center justify-between mb-1.5">
              <span className="text-xs text-ag-muted">即将粘贴的内容（可直接编辑）</span>
              {live.hasHiddenChars && (
                <button
                  type="button"
                  onClick={() => setText((prev) => stripHiddenChars(prev))}
                  className="inline-flex items-center gap-1 text-[11px] px-2 py-0.5 rounded bg-indigo-500/15 text-indigo-300 border border-indigo-500/30 hover:bg-indigo-500/25"
                >
                  <Sparkles className="w-3 h-3" />一键清除隐藏字符
                </button>
              )}
            </div>
            <textarea
              rows={8}
              spellCheck={false}
              value={text}
              onChange={(e) => setText(e.target.value)}
              className="ag-input font-mono text-xs leading-relaxed resize-y"
            />
          </div>
        </div>

        <div className="px-5 py-3.5 border-t border-ag-border bg-[#11151f] flex justify-end gap-2">
          <button type="button" onClick={onCancel} className="ag-btn-ghost">取消粘贴</button>
          <button
            type="button"
            disabled={!text}
            onClick={() => onConfirm(text)}
            className="inline-flex items-center gap-1.5 rounded-lg bg-amber-500 hover:bg-amber-400 text-[#0b0d12] font-semibold text-xs px-4 py-2 transition disabled:opacity-40"
          >
            确认粘贴到终端
          </button>
        </div>
      </div>
    </ModalShell>
  )
}

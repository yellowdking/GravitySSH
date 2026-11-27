import React from 'react'
import { AlertCircle, CheckCircle2, Download, Upload, X } from 'lucide-react'
import type { TransferProgress } from '../types'

interface Props {
  transfers: TransferProgress[]
  onDismiss: (transferId: string) => void
  onClearFinished?: () => void
}

export const SftpTransferBar: React.FC<Props> = ({ transfers, onDismiss, onClearFinished }) => {
  if (transfers.length === 0) return null
  const finishedCount = transfers.filter((tr) => tr.status === 'done' || tr.status === 'error').length
  return (
    <div className="shrink-0 max-h-40 overflow-y-auto p-2.5 border-t border-ag-border bg-[#0d1017] space-y-2">
      {onClearFinished && finishedCount > 0 && (
        <div className="flex items-center justify-between text-[10px] text-ag-muted px-0.5">
          <span>传输任务 ({transfers.length})</span>
          <button
            type="button"
            onClick={onClearFinished}
            className="text-ag-subtle hover:text-ag-text transition"
          >
            清空已结束 ({finishedCount})
          </button>
        </div>
      )}
      {transfers.map((tr) => (
        <div key={tr.transferId} className="rounded-lg bg-ag-panel border border-ag-border p-2 text-[11px]">
          <div className="flex items-center justify-between gap-2 mb-1">
            <span className="truncate font-medium text-ag-text flex items-center gap-1 min-w-0">
              {tr.status === 'done' ? (
                <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400 shrink-0" />
              ) : tr.status === 'error' ? (
                <AlertCircle className="w-3.5 h-3.5 text-rose-400 shrink-0" />
              ) : tr.direction === 'upload' ? (
                <Upload className="w-3 h-3 text-sky-400 shrink-0" />
              ) : (
                <Download className="w-3 h-3 text-indigo-400 shrink-0" />
              )}
              <span className="truncate">{tr.fileName}</span>
            </span>
            <div className="flex items-center gap-1 shrink-0">
              <span className="font-mono text-ag-muted">{tr.percent}%</span>
              {(tr.status === 'error' || tr.status === 'done') && (
                <button
                  type="button"
                  onClick={() => onDismiss(tr.transferId)}
                  title="关闭"
                  aria-label="关闭传输提示"
                  className="p-0.5 rounded hover:bg-ag-hover text-ag-subtle hover:text-ag-text"
                >
                  <X className="w-3 h-3" />
                </button>
              )}
            </div>
          </div>
          <div className="h-1.5 rounded-full bg-[#0b0d12] overflow-hidden">
            <div
              className={`h-full transition-all ${
                tr.status === 'error'
                  ? 'bg-rose-500'
                  : tr.status === 'done'
                  ? 'bg-emerald-400'
                  : 'bg-gradient-to-r from-indigo-500 to-sky-400'
              }`}
              style={{ width: `${tr.percent}%` }}
            />
          </div>
          {tr.status === 'error' && tr.error && (
            <div className="mt-1 text-[10px] text-rose-300 break-all leading-snug">{tr.error}</div>
          )}
        </div>
      ))}
    </div>
  )
}

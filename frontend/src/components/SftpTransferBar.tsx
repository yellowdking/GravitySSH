import React from 'react'
import { AlertCircle, CheckCircle2, Download, Upload } from 'lucide-react'
import type { TransferProgress } from '../types'

export const SftpTransferBar: React.FC<{ transfers: TransferProgress[] }> = ({ transfers }) => {
  if (transfers.length === 0) return null
  return (
    <div className="p-2.5 border-t border-ag-border bg-[#0d1017] space-y-2">
      {transfers.map((tr) => (
        <div key={tr.transferId} className="rounded-lg bg-ag-panel border border-ag-border p-2 text-[11px]">
          <div className="flex items-center justify-between gap-2 mb-1">
            <span className="truncate font-medium text-ag-text flex items-center gap-1">
              {tr.status === 'done' ? (
                <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400 shrink-0" />
              ) : tr.status === 'error' ? (
                <AlertCircle className="w-3.5 h-3.5 text-rose-400 shrink-0" />
              ) : tr.direction === 'upload' ? (
                <Upload className="w-3 h-3 text-sky-400 shrink-0" />
              ) : (
                <Download className="w-3 h-3 text-indigo-400 shrink-0" />
              )}
              {tr.fileName}
            </span>
            <span className="font-mono text-ag-muted shrink-0">{tr.percent}%</span>
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
        </div>
      ))}
    </div>
  )
}

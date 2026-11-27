import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { AlertCircle, CheckCircle2, Download, Upload, X } from 'lucide-react'
import type { SessionInfo, TransferProgress } from '../types'
import { api } from '../wailsApi'
import { markRunningTransfersInterrupted, pruneTransferMap } from '../hooks/useSftpState'

interface Props {
  sessionId: string
  sessionStatus: SessionInfo['status']
  onUploadDone: () => void
}

export const SftpTransferBar: React.FC<Props> = React.memo(({ sessionId, sessionStatus, onUploadDone }) => {
  const [transfers, setTransfers] = useState<Record<string, TransferProgress>>({})
  const dismissTimersRef = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map())
  const onUploadDoneRef = useRef(onUploadDone)
  onUploadDoneRef.current = onUploadDone

  const dismissTransfer = useCallback((transferId: string) => {
    const existing = dismissTimersRef.current.get(transferId)
    if (existing !== undefined) {
      clearTimeout(existing)
      dismissTimersRef.current.delete(transferId)
    }
    setTransfers((prev) => {
      if (!(transferId in prev)) return prev
      const next = { ...prev }
      delete next[transferId]
      return next
    })
  }, [])

  const clearFinishedTransfers = useCallback(() => {
    setTransfers((prev) => {
      let changed = false
      const next: Record<string, TransferProgress> = {}
      for (const [id, tr] of Object.entries(prev)) {
        if (tr.status === 'done' || tr.status === 'error') {
          changed = true
          const existing = dismissTimersRef.current.get(id)
          if (existing !== undefined) {
            clearTimeout(existing)
            dismissTimersRef.current.delete(id)
          }
        } else {
          next[id] = tr
        }
      }
      return changed ? next : prev
    })
  }, [])

  useEffect(() => {
    const timers = dismissTimersRef.current
    return () => {
      for (const timer of timers.values()) clearTimeout(timer)
      timers.clear()
    }
  }, [])

  useEffect(() => {
    if (sessionStatus === 'connected') return
    setTransfers((prev) => markRunningTransfersInterrupted(prev))
  }, [sessionStatus])

  useEffect(() => {
    return api.onEvent<TransferProgress>('sftp:progress', (prog) => {
      if (prog.sessionId !== sessionId) return
      setTransfers((prev) => {
        const updated = { ...prev }
        delete updated[prog.transferId]
        updated[prog.transferId] = prog
        const { next, evictedIds } = pruneTransferMap(updated)
        for (const id of evictedIds) {
          const t = dismissTimersRef.current.get(id)
          if (t !== undefined) {
            clearTimeout(t)
            dismissTimersRef.current.delete(id)
          }
        }
        return next
      })
      if (prog.status === 'done') {
        if (prog.direction === 'upload') onUploadDoneRef.current()
        const existing = dismissTimersRef.current.get(prog.transferId)
        if (existing !== undefined) clearTimeout(existing)
        const timer = setTimeout(() => {
          dismissTimersRef.current.delete(prog.transferId)
          setTransfers((p) => {
            if (!(prog.transferId in p)) return p
            const n = { ...p }
            delete n[prog.transferId]
            return n
          })
        }, 3500)
        dismissTimersRef.current.set(prog.transferId, timer)
      } else if (prog.status === 'error') {
        const existing = dismissTimersRef.current.get(prog.transferId)
        if (existing !== undefined) {
          clearTimeout(existing)
          dismissTimersRef.current.delete(prog.transferId)
        }
      }
    })
  }, [sessionId])

  const transferList = useMemo(() => Object.values(transfers), [transfers])
  if (transferList.length === 0) return null
  const finishedCount = transferList.filter((tr) => tr.status === 'done' || tr.status === 'error').length

  return (
    <div className="shrink-0 max-h-40 overflow-y-auto p-2.5 border-t border-ag-border bg-[#0d1017] space-y-2">
      {finishedCount > 0 && (
        <div className="flex items-center justify-between text-[10px] text-ag-muted px-0.5">
          <span>传输任务 ({transferList.length})</span>
          <button
            type="button"
            onClick={clearFinishedTransfers}
            className="text-ag-subtle hover:text-ag-text transition"
          >
            清空已结束 ({finishedCount})
          </button>
        </div>
      )}
      {transferList.map((tr) => (
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
                  onClick={() => dismissTransfer(tr.transferId)}
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
})

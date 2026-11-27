import React from 'react'
import { BookmarkPlus, CheckSquare, Copy, Play, Square, Terminal, Trash2 } from 'lucide-react'
import type { CommandRecord } from '../types'

const HISTORY_TIME_FMT = new Intl.DateTimeFormat('zh-CN', {
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
})

function formatHistoryTime(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : HISTORY_TIME_FMT.format(d)
}

interface Props {
  rec: CommandRecord
  checked: boolean
  hasActiveSession: boolean
  onToggle: () => void
  onSaveAsShortcut: () => void
  onRun: (execute: boolean) => void
  onCopy: () => void
  onDelete: () => void
}

export const HistoryRow: React.FC<Props> = ({
  rec,
  checked,
  hasActiveSession,
  onToggle,
  onSaveAsShortcut,
  onRun,
  onCopy,
  onDelete,
}) => {
  const timeStr = formatHistoryTime(rec.executedAt)

  return (
    <div
      className={`group rounded-xl border p-2.5 transition ${
        checked ? 'bg-indigo-500/[0.12] border-indigo-500/45' : 'bg-ag-panel/80 border-ag-border hover:border-ag-borderStrong'
      }`}
    >
      <div className="flex items-start gap-2">
        <button onClick={onToggle} className="mt-0.5 text-ag-muted hover:text-indigo-300 shrink-0" title="勾选以保存为快捷命令">
          {checked ? <CheckSquare className="w-4 h-4 text-indigo-400" /> : <Square className="w-4 h-4" />}
        </button>
        <div className="min-w-0 flex-1">
          <div className="font-mono text-xs text-ag-text break-all leading-relaxed">{rec.command}</div>
          <div className="mt-1.5 flex items-center justify-between text-[10px] text-ag-subtle">
            <span className="truncate">{rec.serverName} · {timeStr}</span>
            <div className="flex items-center gap-0.5 opacity-80 group-hover:opacity-100">
              <button onClick={onSaveAsShortcut} title="保存为加密快捷命令" className="p-1 rounded hover:bg-indigo-500/20 text-indigo-300"><BookmarkPlus className="w-3.5 h-3.5" /></button>
              {hasActiveSession && (
                <>
                  <button onClick={() => onRun(true)} title="立即执行" className="p-1 rounded hover:bg-emerald-500/20 text-emerald-400"><Play className="w-3.5 h-3.5" /></button>
                  <button onClick={() => onRun(false)} title="填入终端" className="p-1 rounded hover:bg-sky-500/20 text-sky-400"><Terminal className="w-3.5 h-3.5" /></button>
                </>
              )}
              <button onClick={onCopy} title="复制" className="p-1 rounded hover:bg-ag-hover text-ag-muted hover:text-ag-text"><Copy className="w-3.5 h-3.5" /></button>
              <button onClick={onDelete} title="删除" className="p-1 rounded hover:bg-rose-500/20 text-ag-muted hover:text-rose-400"><Trash2 className="w-3.5 h-3.5" /></button>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

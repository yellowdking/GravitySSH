import React from 'react'
import { Edit3, Pin, Play, Terminal, Trash2 } from 'lucide-react'
import type { CommandShortcut } from '../types'

interface Props {
  sc: CommandShortcut
  hasActiveSession: boolean
  onTogglePin: () => void
  onEdit: () => void
  onDelete: () => void
  onRun: (execute: boolean) => void
}

export const ShortcutCard: React.FC<Props> = ({ sc, hasActiveSession, onTogglePin, onEdit, onDelete, onRun }) => (
  <div className="group rounded-xl bg-ag-panel border border-ag-border hover:border-violet-500/40 p-3 transition flex flex-col justify-between gap-2">
    <div>
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-1.5 min-w-0">
          {sc.pinned && <Pin className="w-3 h-3 text-amber-400 shrink-0" />}
          <span className="text-xs font-semibold text-ag-text truncate">{sc.name}</span>
          <span className="text-[10px] px-1.5 py-0.2 rounded bg-ag-elevated text-ag-muted shrink-0">{sc.category}</span>
        </div>
        <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition shrink-0">
          <button onClick={onTogglePin} title="置顶" className="p-1 rounded hover:bg-ag-hover text-ag-muted hover:text-amber-300"><Pin className="w-3 h-3" /></button>
          <button onClick={onEdit} title="编辑" className="p-1 rounded hover:bg-ag-hover text-ag-muted hover:text-ag-text"><Edit3 className="w-3 h-3" /></button>
          <button onClick={onDelete} title="删除" className="p-1 rounded hover:bg-rose-500/20 text-ag-muted hover:text-rose-400"><Trash2 className="w-3 h-3" /></button>
        </div>
      </div>
      {sc.description && <p className="text-[11px] text-ag-muted mt-1">{sc.description}</p>}
      <pre className="mt-2 p-2 rounded-lg bg-[#0b0d12] border border-ag-border/80 font-mono text-[11px] text-indigo-200/95 overflow-x-auto whitespace-pre-wrap break-all">{sc.command}</pre>
    </div>
    <div className="flex items-center justify-between pt-1">
      <span className="text-[10px] text-ag-subtle">{sc.useCount > 0 ? `已调用 ${sc.useCount} 次` : '就绪'}</span>
      <div className="flex items-center gap-1.5">
        <button disabled={!hasActiveSession} onClick={() => onRun(false)} className="inline-flex items-center gap-1 px-2 py-1 rounded-md bg-ag-elevated hover:bg-ag-hover border border-ag-border text-[11px] text-ag-text transition disabled:opacity-40">
          <Terminal className="w-3 h-3 text-sky-400" />填入
        </button>
        <button disabled={!hasActiveSession} onClick={() => onRun(true)} className="inline-flex items-center gap-1 px-2.5 py-1 rounded-md bg-indigo-500 hover:bg-indigo-400 text-[11px] text-white font-medium transition disabled:opacity-40">
          <Play className="w-3 h-3" />执行
        </button>
      </div>
    </div>
  </div>
)

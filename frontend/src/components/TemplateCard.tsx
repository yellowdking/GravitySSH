import React from 'react'
import { BookmarkPlus, Edit2, Play, ShieldCheck, Trash2 } from 'lucide-react'
import type { BuiltinTemplate } from '../types'

interface Props {
  tpl: BuiltinTemplate
  expanded: boolean
  busy: boolean
  hasActiveSession: boolean
  onToggleExpand: () => void
  onEdit: () => void
  onDelete: () => void
  onImport: () => void
  onRunNow?: () => void
}

export const TemplateCard: React.FC<Props> = ({
  tpl, expanded, busy, hasActiveSession, onToggleExpand, onEdit, onDelete, onImport, onRunNow,
}) => (
  <div className="rounded-xl bg-[#0d1017] border border-ag-border p-3.5 space-y-2">
    <div className="flex items-start justify-between gap-2">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5 flex-wrap">
          <span className="text-xs font-semibold text-ag-text">{tpl.name}</span>
          <span className="text-[10px] px-1.5 py-0.2 rounded bg-violet-500/15 text-violet-300 border border-violet-500/25">{tpl.category}</span>
          {tpl.presetId && (
            <span className="text-[10px] px-1.5 py-0.2 rounded bg-emerald-500/10 text-emerald-300 border border-emerald-500/25">预置</span>
          )}
        </div>
        {tpl.description && <p className="text-[11px] text-ag-muted mt-1">{tpl.description}</p>}
        {tpl.safetyNote && (
          <p className="text-[11px] text-emerald-300/90 mt-1 flex items-center gap-1">
            <ShieldCheck className="w-3 h-3 shrink-0" /><span>{tpl.safetyNote}</span>
          </p>
        )}
      </div>
      <div className="flex items-center gap-1 shrink-0">
        <button type="button" disabled={busy} onClick={onEdit} title="编辑模板" className="p-1.5 rounded-lg hover:bg-ag-hover text-ag-muted hover:text-ag-text">
          <Edit2 className="w-3.5 h-3.5" />
        </button>
        <button type="button" disabled={busy} onClick={onDelete} title="删除模板" className="p-1.5 rounded-lg hover:bg-rose-500/20 text-ag-muted hover:text-rose-400">
          <Trash2 className="w-3.5 h-3.5" />
        </button>
        <button type="button" disabled={busy} onClick={onImport} className="ag-btn-secondary text-xs py-1 px-2">
          <BookmarkPlus className="w-3.5 h-3.5 text-indigo-400" />加入快捷库
        </button>
        {onRunNow && (
          <button
            type="button"
            disabled={!hasActiveSession || busy}
            onClick={onRunNow}
            className="ag-btn-primary text-xs py-1 px-2"
            title={hasActiveSession ? '发送到当前终端执行' : '请先连接服务器'}
          >
            <Play className="w-3 h-3" />执行
          </button>
        )}
      </div>
    </div>
    <div>
      <button type="button" onClick={onToggleExpand} className="text-[11px] text-indigo-400 hover:text-indigo-300">
        {expanded ? '收起脚本内容 ▲' : '预览完整脚本 ▼'}
      </button>
      {expanded && (
        <pre className="mt-1.5 p-2.5 rounded-lg bg-[#090b10] border border-ag-border font-mono text-[11px] text-indigo-200/90 max-h-52 overflow-auto whitespace-pre-wrap break-all">
          {tpl.command}
        </pre>
      )}
    </div>
  </div>
)

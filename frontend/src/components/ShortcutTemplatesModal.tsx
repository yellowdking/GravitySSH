import React, { useEffect, useState } from 'react'
import { BookmarkPlus, Play, ShieldCheck, Sparkles, X } from 'lucide-react'
import type { BuiltinTemplate } from '../types'
import { api } from '../wailsApi'

interface Props {
  open: boolean
  hasActiveSession: boolean
  onClose: () => void
  onImport: (tpl: BuiltinTemplate) => Promise<void>
  onRunNow?: (tpl: BuiltinTemplate) => Promise<void>
}

export const ShortcutTemplatesModal: React.FC<Props> = ({ open, hasActiveSession, onClose, onImport, onRunNow }) => {
  const [templates, setTemplates] = useState<BuiltinTemplate[]>([])
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [busyId, setBusyId] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    void api.listBuiltinTemplates().then((list) => setTemplates(list || [])).catch(() => {})
  }, [open])

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 bg-black/70 backdrop-blur-sm flex items-center justify-center p-4">
      <div className="w-full max-w-2xl rounded-2xl bg-ag-panel border border-ag-borderStrong shadow-ag overflow-hidden flex flex-col max-h-[86vh]">
        <div className="px-5 py-4 border-b border-ag-border bg-[#11151f] flex items-center justify-between">
          <div className="flex items-center gap-2.5">
            <div className="w-8 h-8 rounded-lg bg-violet-500/15 border border-violet-500/30 flex items-center justify-center text-violet-300">
              <Sparkles className="w-4 h-4" />
            </div>
            <div>
              <h3 className="text-sm font-semibold text-ag-text">内置 VPS 运维快捷模板</h3>
              <p className="text-[11px] text-ag-muted">开 BBR · 建 SWAP · 换源 · 装 Docker · 安全改 SSH 端口（可预览、导入或直接执行）</p>
            </div>
          </div>
          <button type="button" onClick={onClose} className="p-1.5 rounded-lg hover:bg-ag-hover text-ag-muted hover:text-ag-text">
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="flex-1 overflow-y-auto p-4 space-y-2.5">
          {templates.map((tpl) => {
            const expanded = expandedId === tpl.id
            const busy = busyId === tpl.id
            return (
              <div key={tpl.id} className="rounded-xl bg-[#0d1017] border border-ag-border p-3.5 space-y-2">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="text-xs font-semibold text-ag-text">{tpl.name}</span>
                      <span className="text-[10px] px-1.5 py-0.2 rounded bg-violet-500/15 text-violet-300 border border-violet-500/25">
                        {tpl.category}
                      </span>
                    </div>
                    <p className="text-[11px] text-ag-muted mt-1">{tpl.description}</p>
                    <p className="text-[11px] text-emerald-300/90 mt-1 flex items-center gap-1">
                      <ShieldCheck className="w-3 h-3 shrink-0" />
                      <span>{tpl.safetyNote}</span>
                    </p>
                  </div>
                  <div className="flex items-center gap-1.5 shrink-0">
                    <button
                      type="button"
                      disabled={busy}
                      onClick={async () => {
                        setBusyId(tpl.id)
                        try { await onImport(tpl) } finally { setBusyId(null) }
                      }}
                      className="ag-btn-secondary text-xs py-1 px-2.5"
                    >
                      <BookmarkPlus className="w-3.5 h-3.5 text-indigo-400" />加入快捷库
                    </button>
                    {onRunNow && (
                      <button
                        type="button"
                        disabled={!hasActiveSession || busy}
                        onClick={async () => {
                          setBusyId(tpl.id)
                          try { await onRunNow(tpl); onClose() } finally { setBusyId(null) }
                        }}
                        className="ag-btn-primary text-xs py-1 px-2.5"
                        title={hasActiveSession ? '发送到当前终端执行' : '请先连接服务器'}
                      >
                        <Play className="w-3 h-3" />执行
                      </button>
                    )}
                  </div>
                </div>

                <div>
                  <button
                    type="button"
                    onClick={() => setExpandedId(expanded ? null : tpl.id)}
                    className="text-[11px] text-indigo-400 hover:text-indigo-300"
                  >
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
          })}
        </div>

        <div className="px-5 py-3 border-t border-ag-border bg-[#11151f] flex justify-end">
          <button type="button" onClick={onClose} className="ag-btn-ghost text-xs">关闭</button>
        </div>
      </div>
    </div>
  )
}

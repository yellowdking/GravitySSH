import React, { useEffect, useState } from 'react'
import { BookmarkPlus, X } from 'lucide-react'
import type { CommandShortcut } from '../types'

interface Props {
  open: boolean
  initial: Partial<CommandShortcut> | null
  title: string
  onClose: () => void
  onSubmit: (sc: Partial<CommandShortcut>) => Promise<void>
}

export const ShortcutEditorModal: React.FC<Props> = ({ open, initial, title, onClose, onSubmit }) => {
  const [draft, setDraft] = useState<Partial<CommandShortcut>>({})

  useEffect(() => {
    if (open && initial) setDraft(initial)
  }, [open, initial])

  if (!open || !initial) return null

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    await onSubmit(draft)
  }

  return (
    <div className="fixed inset-0 z-50 bg-black/70 backdrop-blur-sm flex items-center justify-center p-4">
      <form onSubmit={handleSubmit} className="w-full max-w-lg rounded-2xl bg-ag-panel border border-ag-borderStrong shadow-ag p-5 space-y-3.5">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-semibold text-ag-text flex items-center gap-2">
            <BookmarkPlus className="w-4 h-4 text-indigo-400" />
            {title}
          </h3>
          <button type="button" onClick={onClose} className="p-1 text-ag-muted hover:text-ag-text">
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="block text-xs text-ag-muted mb-1">快捷方式名称 *</label>
            <input required className="ag-input" value={draft.name || ''} onChange={(e) => setDraft({ ...draft, name: e.target.value })} />
          </div>
          <div>
            <label className="block text-xs text-ag-muted mb-1">分类标签</label>
            <input className="ag-input" value={draft.category || ''} onChange={(e) => setDraft({ ...draft, category: e.target.value })} />
          </div>
        </div>

        <div>
          <label className="block text-xs text-ag-muted mb-1">命令内容 *</label>
          <textarea required rows={4} className="ag-input font-mono text-xs" value={draft.command || ''} onChange={(e) => setDraft({ ...draft, command: e.target.value })} />
        </div>

        <div>
          <label className="block text-xs text-ag-muted mb-1">说明（可选）</label>
          <input className="ag-input" value={draft.description || ''} onChange={(e) => setDraft({ ...draft, description: e.target.value })} />
        </div>

        <div className="flex items-center gap-5 text-xs text-ag-muted">
          <label className="flex items-center gap-1.5 cursor-pointer">
            <input type="checkbox" checked={!!draft.autoExecute} onChange={(e) => setDraft({ ...draft, autoExecute: e.target.checked })} className="accent-indigo-500" />
            点击时自动回车执行
          </label>
          <label className="flex items-center gap-1.5 cursor-pointer">
            <input type="checkbox" checked={!!draft.pinned} onChange={(e) => setDraft({ ...draft, pinned: e.target.checked })} className="accent-indigo-500" />
            置顶并在命令补齐中优先推荐
          </label>
        </div>

        <div className="flex justify-end gap-2 pt-2">
          <button type="button" onClick={onClose} className="ag-btn-ghost">取消</button>
          <button type="submit" className="ag-btn-primary">加密保存快捷方式</button>
        </div>
      </form>
    </div>
  )
}

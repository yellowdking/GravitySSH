import React, { useEffect, useState } from 'react'
import { BookmarkPlus, X } from 'lucide-react'
import type { CommandShortcut } from '../types'
import { ModalShell } from './ModalShell'

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

  const rawCommand = draft.command || ''
  const lineCount =
    rawCommand.trim() === ''
      ? 0
      : rawCommand
          .replace(/\r\n/g, '\n')
          .split('\n')
          .filter((l) => l.trim().length > 0).length

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    await onSubmit(draft)
  }

  return (
    <ModalShell open={open} onClose={onClose}>
      <form
        onSubmit={handleSubmit}
        className="w-full max-w-2xl rounded-2xl bg-ag-panel border border-ag-borderStrong shadow-ag p-5 space-y-3.5 max-h-[90vh] flex flex-col"
      >
        <div className="flex items-center justify-between shrink-0">
          <h3 className="text-sm font-semibold text-ag-text flex items-center gap-2">
            <BookmarkPlus className="w-4 h-4 text-indigo-400" />
            {title}
          </h3>
          <button type="button" onClick={onClose} className="p-1 text-ag-muted hover:text-ag-text">
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="grid grid-cols-2 gap-3 shrink-0">
          <div>
            <label className="block text-xs text-ag-muted mb-1">快捷方式名称 *</label>
            <input
              required
              className="ag-input"
              placeholder="例如：一键部署与重启服务"
              value={draft.name || ''}
              onChange={(e) => setDraft({ ...draft, name: e.target.value })}
            />
          </div>
          <div>
            <label className="block text-xs text-ag-muted mb-1">分类标签</label>
            <input
              className="ag-input"
              placeholder="例如：运维 / Docker / 部署"
              value={draft.category || ''}
              onChange={(e) => setDraft({ ...draft, category: e.target.value })}
            />
          </div>
        </div>

        <div className="flex-1 min-h-0 flex flex-col">
          <div className="flex items-center justify-between mb-1">
            <label className="text-xs text-ag-muted">命令脚本内容 *</label>
            <span className="text-[11px] font-mono px-2 py-0.5 rounded bg-indigo-500/[0.12] text-indigo-300 border border-indigo-500/20">
              共 {lineCount} 行命令
            </span>
          </div>
          <textarea
            required
            rows={12}
            spellCheck={false}
            placeholder={'支持粘贴或编写多行命令脚本（每行一条命令，可包含数十行）：\ncd /opt/app\ngit pull\ndocker compose build\ndocker compose up -d\ndocker compose logs --tail=50'}
            className="ag-input font-mono text-xs leading-relaxed min-h-[220px] resize-y"
            value={rawCommand}
            onChange={(e) => setDraft({ ...draft, command: e.target.value })}
          />
        </div>

        <div className="shrink-0">
          <label className="block text-xs text-ag-muted mb-1">说明（可选）</label>
          <input
            className="ag-input"
            placeholder="简述该命令或脚本的用途"
            value={draft.description || ''}
            onChange={(e) => setDraft({ ...draft, description: e.target.value })}
          />
        </div>

        <div className="flex flex-wrap items-center gap-5 text-xs text-ag-muted shrink-0">
          <label className="flex items-center gap-1.5 cursor-pointer">
            <input
              type="checkbox"
              checked={!!draft.autoExecute}
              onChange={(e) => setDraft({ ...draft, autoExecute: e.target.checked })}
              className="accent-indigo-500"
            />
            点击时自动回车逐行执行
          </label>
          <label className="flex items-center gap-1.5 cursor-pointer">
            <input
              type="checkbox"
              checked={!!draft.pinned}
              onChange={(e) => setDraft({ ...draft, pinned: e.target.checked })}
              className="accent-indigo-500"
            />
            置顶显示
          </label>
        </div>

        <div className="flex justify-end gap-2 pt-2 shrink-0">
          <button type="button" onClick={onClose} className="ag-btn-ghost">取消</button>
          <button type="submit" className="ag-btn-primary">保存快捷方式</button>
        </div>
      </form>
    </ModalShell>
  )
}

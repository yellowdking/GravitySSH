import React, { useMemo, useState } from 'react'
import { Bookmark, Lock, Plus, Search } from 'lucide-react'
import type { CommandShortcut } from '../types'
import { api } from '../wailsApi'
import { ShortcutEditorModal } from './ShortcutEditorModal'
import { ShortcutCard } from './ShortcutCard'

interface ShortcutsPanelProps {
  shortcuts: CommandShortcut[]
  activeSessionId?: string
  compact?: boolean
  onRunShortcut: (sc: CommandShortcut, execute: boolean) => void
  onChanged: () => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const ShortcutsPanel: React.FC<ShortcutsPanelProps> = ({
  shortcuts,
  activeSessionId,
  compact = false,
  onRunShortcut,
  onChanged,
  onToast,
}) => {
  const [search, setSearch] = useState('')
  const [selectedCat, setSelectedCat] = useState('全部')
  const [editing, setEditing] = useState<Partial<CommandShortcut> | null>(null)

  const categories = useMemo(() => {
    const set = new Set<string>(['全部'])
    shortcuts.forEach((s) => set.add(s.category || '常用'))
    return Array.from(set)
  }, [shortcuts])

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    return [...shortcuts]
      .filter((s) => {
        if (selectedCat !== '全部' && (s.category || '常用') !== selectedCat) return false
        if (!q) return true
        return s.name.toLowerCase().includes(q) || s.command.toLowerCase().includes(q) || (s.description || '').toLowerCase().includes(q)
      })
      .sort((a, b) => Number(b.pinned) - Number(a.pinned) || b.useCount - a.useCount)
  }, [shortcuts, search, selectedCat])

  const handleSave = async (draft: Partial<CommandShortcut>) => {
    try { await api.saveShortcut(draft); setEditing(null); onChanged(); onToast('快捷命令已加密保存') }
    catch (err) { onToast(String(err), 'error') }
  }

  return (
    <div className={`flex flex-col h-full ${compact ? 'w-[360px] shrink-0 bg-ag-sidebar border-l border-ag-border' : 'flex-1 bg-ag-bg'}`}>
      <div className="px-4 py-3 border-b border-ag-border bg-[#0f1219] space-y-2.5">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Bookmark className="w-4 h-4 text-violet-400" />
            <span className="text-sm font-semibold text-ag-text">快捷命令库</span>
            <span className="inline-flex items-center gap-0.5 text-[10px] px-1.5 py-0.5 rounded bg-emerald-500/12 text-emerald-300 border border-emerald-500/20">
              <Lock className="w-2.5 h-2.5" />DPAPI 加密
            </span>
          </div>
          <button onClick={() => setEditing({ id: '', name: '', command: '', description: '', category: '常用', autoExecute: true, pinned: false })} className="ag-btn-primary text-xs py-1 px-2.5">
            <Plus className="w-3.5 h-3.5" />新建
          </button>
        </div>
        <div className="relative">
          <Search className="w-3.5 h-3.5 text-ag-subtle absolute left-2.5 top-1/2 -translate-y-1/2" />
          <input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="搜索快捷命令名称或内容..." className="w-full rounded-lg bg-[#0b0d12] border border-ag-border pl-8 pr-2.5 py-1.5 text-xs text-ag-text placeholder:text-ag-subtle focus:outline-none focus:border-violet-500/50" />
        </div>
        <div className="flex items-center gap-1 overflow-x-auto pb-0.5">
          {categories.map((cat) => (
            <button key={cat} onClick={() => setSelectedCat(cat)} className={`px-2 py-0.5 rounded-md text-[11px] whitespace-nowrap transition ${selectedCat === cat ? 'bg-violet-500/20 text-violet-200 border border-violet-500/35' : 'text-ag-muted hover:text-ag-text bg-ag-panel border border-transparent'}`}>{cat}</button>
          ))}
        </div>
      </div>

      <div className={`flex-1 overflow-y-auto p-3 ${compact ? 'space-y-2' : 'grid grid-cols-1 md:grid-cols-2 gap-3 content-start max-w-5xl w-full mx-auto'}`}>
        {filtered.length === 0 ? (
          <div className="col-span-full py-14 text-center text-xs text-ag-subtle">没有找到快捷命令，您可以点击右上角新建，或从「命令历史」勾选保存</div>
        ) : (
          filtered.map((sc) => (
            <ShortcutCard
              key={sc.id}
              sc={sc}
              hasActiveSession={!!activeSessionId}
              onTogglePin={() => { void api.saveShortcut({ ...sc, pinned: !sc.pinned }).then(onChanged) }}
              onEdit={() => setEditing(sc)}
              onDelete={() => { if (window.confirm(`确定删除快捷命令 "${sc.name}" 吗？`)) void api.deleteShortcut(sc.id).then(() => { onChanged(); onToast(`已删除快捷命令 ${sc.name}`) }) }}
              onRun={(exec) => onRunShortcut(sc, exec)}
            />
          ))
        )}
      </div>

      <ShortcutEditorModal open={!!editing} initial={editing} title={editing?.id ? '编辑快捷命令' : '新建加密快捷命令'} onClose={() => setEditing(null)} onSubmit={handleSave} />
    </div>
  )
}

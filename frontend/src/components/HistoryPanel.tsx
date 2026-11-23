import React, { useMemo, useState } from 'react'
import { BookmarkPlus, History, Search, X } from 'lucide-react'
import type { CommandRecord, CommandShortcut } from '../types'
import { api } from '../wailsApi'
import { ShortcutEditorModal } from './ShortcutEditorModal'
import { HistoryRow } from './HistoryRow'

interface HistoryPanelProps {
  history: CommandRecord[]
  activeServerId?: string
  activeSessionId?: string
  compact?: boolean
  onRunCommand: (cmd: string, execute: boolean) => void
  onShortcutSaved: (sc: CommandShortcut) => void
  onHistoryChanged: () => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const HistoryPanel: React.FC<HistoryPanelProps> = ({
  history,
  activeServerId,
  activeSessionId,
  compact = false,
  onRunCommand,
  onShortcutSaved,
  onHistoryChanged,
  onToast,
}) => {
  const [search, setSearch] = useState('')
  const [onlyCurrentServer, setOnlyCurrentServer] = useState(!!activeServerId)
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [savingDraft, setSavingDraft] = useState<Partial<CommandShortcut> | null>(null)

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    return history.filter((h) => {
      if (onlyCurrentServer && activeServerId && h.serverId !== activeServerId) return false
      if (!q) return true
      return h.command.toLowerCase().includes(q) || h.serverName.toLowerCase().includes(q)
    })
  }, [history, search, onlyCurrentServer, activeServerId])

  const openSaveModal = (commands: string[]) => {
    if (commands.length === 0) return
    const ordered = commands.length > 1 ? [...commands].reverse() : commands
    setSavingDraft({
      name: ordered[0].length > 28 ? ordered[0].slice(0, 28) + '...' : ordered[0],
      command: ordered.join(' && '),
      description: commands.length > 1 ? `由 ${commands.length} 条历史命令组合保存` : '来自操作命令历史',
      category: '常用',
      autoExecute: true,
      pinned: false,
    })
  }

  const submitShortcut = async (draft: Partial<CommandShortcut>) => {
    try {
      const saved = await api.saveShortcut(draft)
      onShortcutSaved(saved)
      setSelectedIds([])
      setSavingDraft(null)
      onToast(`已将命令加密保存为快捷方式 "${saved.name}"`)
    } catch (err) { onToast(String(err), 'error') }
  }

  const handleClear = async () => {
    const scope = onlyCurrentServer && activeServerId ? '当前服务器' : '所有服务器'
    if (!window.confirm(`确定清空${scope}的命令历史记录吗？`)) return
    await api.clearHistory(onlyCurrentServer && activeServerId ? activeServerId : '')
    setSelectedIds([])
    onHistoryChanged()
    onToast('已清空命令历史')
  }

  return (
    <div className={`flex flex-col h-full bg-ag-sidebar ${compact ? 'w-[360px] shrink-0 border-l border-ag-border' : 'flex-1 bg-ag-bg'}`}>
      <div className="px-4 py-3 border-b border-ag-border bg-[#0f1219] space-y-2.5">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <History className="w-4 h-4 text-emerald-400" />
            <span className="text-sm font-semibold text-ag-text">操作命令记录</span>
            <span className="text-[11px] px-1.5 py-0.2 rounded bg-ag-elevated text-ag-muted">{filtered.length}</span>
          </div>
          {filtered.length > 0 && <button onClick={handleClear} className="text-[11px] text-ag-subtle hover:text-rose-400 transition">清空记录</button>}
        </div>
        <div className="flex items-center gap-2">
          <div className="relative flex-1">
            <Search className="w-3.5 h-3.5 text-ag-subtle absolute left-2.5 top-1/2 -translate-y-1/2" />
            <input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="搜索历史命令..." className="w-full rounded-lg bg-[#0b0d12] border border-ag-border pl-8 pr-2.5 py-1.5 text-xs text-ag-text placeholder:text-ag-subtle focus:outline-none focus:border-emerald-500/50" />
          </div>
          {activeServerId && (
            <button onClick={() => setOnlyCurrentServer((v) => !v)} className={`px-2.5 py-1.5 rounded-lg text-[11px] border transition shrink-0 ${onlyCurrentServer ? 'bg-emerald-500/15 border-emerald-500/35 text-emerald-300' : 'bg-ag-panel border-ag-border text-ag-muted'}`}>
              {onlyCurrentServer ? '仅当前机器' : '全部机器'}
            </button>
          )}
        </div>
      </div>

      {selectedIds.length > 0 && (
        <div className="px-3 py-2 bg-indigo-500/15 border-b border-indigo-500/35 flex items-center justify-between gap-2 text-xs">
          <span className="text-indigo-200 font-medium">已选 {selectedIds.length} 条命令</span>
          <div className="flex items-center gap-1.5">
            <button onClick={() => openSaveModal(filtered.filter((h) => selectedIds.includes(h.id)).map((h) => h.command))} className="inline-flex items-center gap-1 px-2.5 py-1 rounded-md bg-indigo-500 hover:bg-indigo-400 text-white font-medium transition">
              <BookmarkPlus className="w-3.5 h-3.5" />存为快捷方式
            </button>
            <button onClick={() => setSelectedIds([])} className="p-1 rounded hover:bg-ag-hover text-ag-muted"><X className="w-3.5 h-3.5" /></button>
          </div>
        </div>
      )}

      <div className="flex-1 overflow-y-auto p-2.5 space-y-1.5">
        {filtered.length === 0 ? (
          <div className="py-14 text-center text-xs text-ag-subtle space-y-1">
            <p>暂无命令操作记录</p>
            <p className="text-[11px]">在终端执行过的每条命令都会自动记录，支持勾选保存为快捷方式</p>
          </div>
        ) : (
          filtered.map((rec) => (
            <HistoryRow
              key={rec.id}
              rec={rec}
              checked={selectedIds.includes(rec.id)}
              hasActiveSession={!!activeSessionId}
              onToggle={() => setSelectedIds((p) => (p.includes(rec.id) ? p.filter((x) => x !== rec.id) : [...p, rec.id]))}
              onSaveAsShortcut={() => openSaveModal([rec.command])}
              onRun={(exec) => onRunCommand(rec.command, exec)}
              onCopy={() => { void navigator.clipboard.writeText(rec.command); onToast('已复制命令') }}
              onDelete={() => { void api.deleteHistoryEntry(rec.id).then(() => { setSelectedIds((p) => p.filter((x) => x !== rec.id)); onHistoryChanged() }) }}
            />
          ))
        )}
      </div>

      <ShortcutEditorModal open={!!savingDraft} initial={savingDraft} title="保存为加密快捷方式" onClose={() => setSavingDraft(null)} onSubmit={submitShortcut} />
    </div>
  )
}

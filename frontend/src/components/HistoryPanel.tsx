import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { BookmarkPlus, History, Search, X } from 'lucide-react'
import type { CommandRecord, CommandShortcut } from '../types'
import { api } from '../wailsApi'
import { copyToClipboard, errMsg } from '../utils/errors'
import { useActionDialogs } from '../hooks/useActionDialogs'
import { ShortcutEditorModal } from './ShortcutEditorModal'
import { HistoryRow } from './HistoryRow'

interface HistoryPanelProps {
  history: CommandRecord[]
  activeServerId?: string
  activeSessionId?: string
  compact?: boolean
  onRunCommand: (cmd: string, execute: boolean) => void | Promise<boolean | void>
  onShortcutSaved: (sc: CommandShortcut) => void
  onHistoryChanged: () => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const HistoryPanel: React.FC<HistoryPanelProps> = React.memo(({
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
  const [selectedIds, setSelectedIds] = useState<Set<string>>(() => new Set())
  const [savingDraft, setSavingDraft] = useState<Partial<CommandShortcut> | null>(null)
  const { confirm, dialogNode } = useActionDialogs()

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    return history.filter((h) => {
      if (onlyCurrentServer && activeServerId && h.serverId !== activeServerId) return false
      if (!q) return true
      return h.command.toLowerCase().includes(q) || h.serverName.toLowerCase().includes(q)
    })
  }, [history, search, onlyCurrentServer, activeServerId])

  useEffect(() => {
    setSelectedIds((prev) => {
      if (prev.size === 0) return prev
      const validIds = new Set(history.map((h) => h.id))
      let removed = false
      for (const id of prev) {
        if (!validIds.has(id)) {
          removed = true
          break
        }
      }
      if (!removed) return prev
      const next = new Set<string>()
      for (const id of prev) {
        if (validIds.has(id)) next.add(id)
      }
      return next
    })
  }, [history])

  const visibleSelected = useMemo(
    () => filtered.filter((h) => selectedIds.has(h.id)),
    [filtered, selectedIds]
  )

  const openSaveModal = useCallback((commands: string[]) => {
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
  }, [])

  const submitShortcut = useCallback(async (draft: Partial<CommandShortcut>) => {
    try {
      const saved = await api.saveShortcut(draft)
      onShortcutSaved(saved)
      setSelectedIds(new Set())
      setSavingDraft(null)
      onToast(`已将命令加密保存为快捷命令 "${saved.name}"`)
    } catch (err) { onToast(errMsg(err), 'error') }
  }, [onShortcutSaved, onToast])

  const handleClear = useCallback(async () => {
    const scope = onlyCurrentServer && activeServerId ? '当前服务器' : '所有服务器'
    const ok = await confirm({
      title: '清空命令历史',
      message: `确定清空${scope}的命令历史记录吗？`,
      confirmLabel: '清空',
    })
    if (!ok) return
    try {
      await api.clearHistory(onlyCurrentServer && activeServerId ? activeServerId : '')
      setSelectedIds(new Set())
      onHistoryChanged()
      onToast('已清空命令历史')
    } catch (err) {
      onToast(errMsg(err), 'error')
    }
  }, [onlyCurrentServer, activeServerId, confirm, onHistoryChanged, onToast])

  const handleToggleId = useCallback((id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const handleSaveSingleAsShortcut = useCallback((cmd: string) => {
    openSaveModal([cmd])
  }, [openSaveModal])

  const handleRun = useCallback((cmd: string, exec: boolean) => {
    void onRunCommand(cmd, exec)
  }, [onRunCommand])

  const handleCopyCommand = useCallback(async (cmd: string) => {
    const ok = await copyToClipboard(cmd)
    if (ok) onToast('已复制命令')
    else onToast('复制命令失败', 'error')
  }, [onToast])

  const handleDeleteEntry = useCallback(async (id: string) => {
    try {
      await api.deleteHistoryEntry(id)
      setSelectedIds((prev) => {
        if (!prev.has(id)) return prev
        const next = new Set(prev)
        next.delete(id)
        return next
      })
      onHistoryChanged()
    } catch (err) {
      onToast(errMsg(err), 'error')
    }
  }, [onHistoryChanged, onToast])

  const hasActiveSession = !!activeSessionId

  return (
    <div className={`flex flex-col h-full ${compact ? 'w-[360px] shrink-0 border-l border-ag-border bg-ag-sidebar' : 'flex-1 bg-ag-bg'}`}>
      <div className="px-4 py-3 border-b border-ag-border bg-[#0f1219] space-y-2.5">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <History className="w-4 h-4 text-emerald-400" />
            <span className="text-sm font-semibold text-ag-text">操作命令记录</span>
            <span className="text-[11px] px-1.5 py-0.5 rounded bg-ag-elevated text-ag-muted">{filtered.length}</span>
          </div>
          {filtered.length > 0 && <button type="button" onClick={() => void handleClear()} className="text-[11px] text-ag-subtle hover:text-rose-400 transition">清空记录</button>}
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

      {visibleSelected.length > 0 && (
        <div className="px-3 py-2 bg-indigo-500/15 border-b border-indigo-500/35 flex items-center justify-between gap-2 text-xs">
          <span className="text-indigo-200 font-medium">已选 {visibleSelected.length} 条命令</span>
          <div className="flex items-center gap-1.5">
            <button type="button" onClick={() => openSaveModal(visibleSelected.map((h) => h.command))} className="inline-flex items-center gap-1 px-2.5 py-1 rounded-md bg-indigo-500 hover:bg-indigo-400 text-white font-medium transition">
              <BookmarkPlus className="w-3.5 h-3.5" />存为快捷命令
            </button>
            <button type="button" onClick={() => setSelectedIds(new Set())} aria-label="取消选择" className="p-1 rounded hover:bg-ag-hover text-ag-muted"><X className="w-3.5 h-3.5" /></button>
          </div>
        </div>
      )}

      <div className="flex-1 overflow-y-auto p-2.5 space-y-1.5">
        {filtered.length === 0 ? (
          <div className="py-14 text-center text-xs text-ag-subtle space-y-1">
            <p>暂无命令操作记录</p>
            <p className="text-[11px]">在终端执行过的每条命令都会自动记录，支持勾选保存为快捷命令</p>
          </div>
        ) : (
          filtered.map((rec) => (
            <HistoryRow
              key={rec.id}
              rec={rec}
              checked={selectedIds.has(rec.id)}
              hasActiveSession={hasActiveSession}
              onToggle={handleToggleId}
              onSaveAsShortcut={handleSaveSingleAsShortcut}
              onRun={handleRun}
              onCopy={handleCopyCommand}
              onDelete={handleDeleteEntry}
            />
          ))
        )}
      </div>

      <ShortcutEditorModal open={!!savingDraft} initial={savingDraft} title="保存为加密快捷命令" onClose={() => setSavingDraft(null)} onSubmit={submitShortcut} />
      {dialogNode}
    </div>
  )
})

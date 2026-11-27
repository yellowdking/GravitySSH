import React, { useMemo, useState } from 'react'
import type { BuiltinTemplate, CommandShortcut } from '../types'
import { api } from '../wailsApi'
import { ShortcutEditorModal } from './ShortcutEditorModal'
import { ShortcutCard } from './ShortcutCard'
import { ShortcutTemplatesModal } from './ShortcutTemplatesModal'
import { ShortcutsHeader } from './ShortcutsHeader'

interface ShortcutsPanelProps {
  shortcuts: CommandShortcut[]
  activeSessionId?: string
  compact?: boolean
  onRunShortcut: (sc: CommandShortcut, execute: boolean) => void
  onRunCommand?: (cmd: string, execute: boolean) => void
  onChanged: () => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const ShortcutsPanel: React.FC<ShortcutsPanelProps> = ({
  shortcuts, activeSessionId, compact = false, onRunShortcut, onRunCommand, onChanged, onToast,
}) => {
  const [search, setSearch] = useState('')
  const [selectedCat, setSelectedCat] = useState('全部')
  const [editing, setEditing] = useState<Partial<CommandShortcut> | null>(null)
  const [templatesOpen, setTemplatesOpen] = useState(false)

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
    try {
      await api.saveShortcut(draft)
      setEditing(null)
      onChanged()
      onToast('快捷命令已保存')
    } catch (err) {
      onToast(String(err), 'error')
    }
  }

  const handleImportTemplate = async (tpl: BuiltinTemplate) => {
    try {
      await api.saveShortcut({
        id: '', name: tpl.name, command: tpl.command, description: tpl.description,
        category: tpl.category, autoExecute: true, pinned: false,
      })
      onChanged()
      onToast(`已将「${tpl.name}」加入快捷命令库`)
    } catch (err) {
      onToast(String(err), 'error')
    }
  }

  const handleRunTemplateNow = async (tpl: BuiltinTemplate) => {
    if (!activeSessionId) {
      onToast('请先在左侧连接一台服务器', 'error')
      return
    }
    if (!onRunCommand) return
    onRunCommand(tpl.command + '\r', false)
    onToast(`已下发运维脚本：${tpl.name}`)
  }

  return (
    <div className={`flex flex-col h-full min-w-0 ${compact ? 'w-full bg-ag-sidebar' : 'flex-1 bg-ag-bg'}`}>
      <ShortcutsHeader
        search={search}
        categories={categories}
        selectedCat={selectedCat}
        onSearchChange={setSearch}
        onSelectCat={setSelectedCat}
        onOpenTemplates={() => setTemplatesOpen(true)}
        onNewShortcut={() => setEditing({ id: '', name: '', command: '', description: '', category: '常用', autoExecute: true, pinned: false })}
      />

      <div className={`flex-1 overflow-y-auto p-3 ${compact ? 'space-y-2' : 'grid grid-cols-1 md:grid-cols-2 gap-3 content-start max-w-5xl w-full mx-auto'}`}>
        {filtered.length === 0 ? (
          <div className="col-span-full py-14 text-center text-xs text-ag-subtle">
            没有找到快捷命令，您可以点击右上角新建、导入「运维模板」，或从「命令历史」勾选保存
          </div>
        ) : (
          filtered.map((sc) => (
            <ShortcutCard
              key={sc.id}
              sc={sc}
              hasActiveSession={!!activeSessionId}
              onTogglePin={() => { void api.saveShortcut({ ...sc, pinned: !sc.pinned }).then(onChanged) }}
              onEdit={() => setEditing(sc)}
              onDelete={() => {
                if (window.confirm(`确定删除快捷命令 "${sc.name}" 吗？`)) {
                  void api.deleteShortcut(sc.id).then(() => { onChanged(); onToast(`已删除快捷命令 ${sc.name}`) })
                }
              }}
              onRun={(exec) => onRunShortcut(sc, exec)}
            />
          ))
        )}
      </div>

      <ShortcutEditorModal open={!!editing} initial={editing} title={editing?.id ? '编辑快捷命令' : '新建快捷命令'} onClose={() => setEditing(null)} onSubmit={handleSave} />
      <ShortcutTemplatesModal open={templatesOpen} hasActiveSession={!!activeSessionId} onClose={() => setTemplatesOpen(false)} onImport={handleImportTemplate} onRunNow={onRunCommand ? handleRunTemplateNow : undefined} onToast={onToast} />
    </div>
  )
}

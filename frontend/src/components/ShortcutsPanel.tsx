import React, { useCallback, useEffect, useMemo, useState } from 'react'
import type { BuiltinTemplate, CommandShortcut } from '../types'
import { api } from '../wailsApi'
import { errMsg } from '../utils/errors'
import { useActionDialogs } from '../hooks/useActionDialogs'
import { ShortcutEditorModal } from './ShortcutEditorModal'
import { ShortcutCard } from './ShortcutCard'
import { ShortcutTemplatesModal } from './ShortcutTemplatesModal'
import { ShortcutsHeader } from './ShortcutsHeader'

interface ShortcutsPanelProps {
  shortcuts: CommandShortcut[]
  activeSessionId?: string
  compact?: boolean
  onRunShortcut: (sc: CommandShortcut, execute: boolean) => void
  onRunCommand?: (cmd: string, execute: boolean) => boolean | void | Promise<boolean | void>
  onChanged: () => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const ShortcutsPanel: React.FC<ShortcutsPanelProps> = React.memo(({
  shortcuts, activeSessionId, compact = false, onRunShortcut, onRunCommand, onChanged, onToast,
}) => {
  const [search, setSearch] = useState('')
  const [selectedCat, setSelectedCat] = useState('全部')
  const [editing, setEditing] = useState<Partial<CommandShortcut> | null>(null)
  const [templatesOpen, setTemplatesOpen] = useState(false)
  const { confirm, dialogNode } = useActionDialogs()

  const categories = useMemo(() => {
    const set = new Set<string>(['全部'])
    shortcuts.forEach((s) => set.add(s.category || '常用'))
    return Array.from(set)
  }, [shortcuts])

  const activeCat = categories.includes(selectedCat) ? selectedCat : '全部'

  useEffect(() => {
    if (selectedCat !== '全部' && !categories.includes(selectedCat)) {
      setSelectedCat('全部')
    }
  }, [categories, selectedCat])

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    return [...shortcuts]
      .filter((s) => {
        if (activeCat !== '全部' && (s.category || '常用') !== activeCat) return false
        if (!q) return true
        return s.name.toLowerCase().includes(q) || s.command.toLowerCase().includes(q) || (s.description || '').toLowerCase().includes(q)
      })
      .sort((a, b) => Number(b.pinned) - Number(a.pinned) || b.useCount - a.useCount)
  }, [shortcuts, search, activeCat])

  const handleSave = async (draft: Partial<CommandShortcut>) => {
    try {
      await api.saveShortcut(draft)
      setEditing(null)
      onChanged()
      onToast('快捷命令已保存')
    } catch (err) {
      onToast(errMsg(err), 'error')
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
      onToast(errMsg(err), 'error')
    }
  }

  const handleRunTemplateNow = async (tpl: BuiltinTemplate): Promise<boolean> => {
    if (!activeSessionId) {
      onToast('请先在左侧连接一台服务器', 'error')
      return false
    }
    if (!onRunCommand) return false
    try {
      const res = await onRunCommand(tpl.command, true)
      if (res === false) return false
      onToast(`已下发运维脚本：${tpl.name}`)
      return true
    } catch (err) {
      onToast(errMsg(err), 'error')
      return false
    }
  }

  const handleTogglePin = useCallback(async (sc: CommandShortcut) => {
    try {
      await api.saveShortcut({ ...sc, pinned: !sc.pinned })
      onChanged()
    } catch (err) {
      onToast(errMsg(err), 'error')
    }
  }, [onChanged, onToast])

  const handleEditShortcut = useCallback((sc: CommandShortcut) => {
    setEditing(sc)
  }, [])

  const handleDeleteShortcut = useCallback(async (sc: CommandShortcut) => {
    const ok = await confirm({
      title: '删除快捷命令',
      message: `确定删除快捷命令 "${sc.name}" 吗？`,
      confirmLabel: '删除',
    })
    if (!ok) return
    try {
      await api.deleteShortcut(sc.id)
      onChanged()
      onToast(`已删除快捷命令 ${sc.name}`)
    } catch (err) {
      onToast(errMsg(err), 'error')
    }
  }, [confirm, onChanged, onToast])

  const handleTogglePinCb = useCallback((sc: CommandShortcut) => { void handleTogglePin(sc) }, [handleTogglePin])
  const handleDeleteCb = useCallback((sc: CommandShortcut) => { void handleDeleteShortcut(sc) }, [handleDeleteShortcut])

  return (
    <div className={`flex flex-col h-full min-w-0 ${compact ? 'w-full bg-ag-sidebar' : 'flex-1 bg-ag-bg'}`}>
      <ShortcutsHeader
        search={search}
        categories={categories}
        selectedCat={activeCat}
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
              onTogglePin={handleTogglePinCb}
              onEdit={handleEditShortcut}
              onDelete={handleDeleteCb}
              onRun={onRunShortcut}
            />
          ))
        )}
      </div>

      <ShortcutEditorModal open={!!editing} initial={editing} title={editing?.id ? '编辑快捷命令' : '新建快捷命令'} onClose={() => setEditing(null)} onSubmit={handleSave} />
      <ShortcutTemplatesModal open={templatesOpen} hasActiveSession={!!activeSessionId} onClose={() => setTemplatesOpen(false)} onImport={handleImportTemplate} onRunNow={onRunCommand ? handleRunTemplateNow : undefined} onToast={onToast} />
      {dialogNode}
    </div>
  )
})

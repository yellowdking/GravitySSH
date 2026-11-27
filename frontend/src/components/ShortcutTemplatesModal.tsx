import React, { useCallback, useEffect, useState } from 'react'
import { Plus, RotateCcw, Sparkles, X } from 'lucide-react'
import type { BuiltinTemplate } from '../types'
import { api } from '../wailsApi'
import { ModalShell } from './ModalShell'
import { TemplateEditorModal } from './TemplateEditorModal'
import { TemplateCard } from './TemplateCard'

interface Props {
  open: boolean
  hasActiveSession: boolean
  onClose: () => void
  onImport: (tpl: BuiltinTemplate) => Promise<void>
  onRunNow?: (tpl: BuiltinTemplate) => Promise<void>
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const ShortcutTemplatesModal: React.FC<Props> = ({ open, hasActiveSession, onClose, onImport, onRunNow, onToast }) => {
  const [templates, setTemplates] = useState<BuiltinTemplate[]>([])
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [busyId, setBusyId] = useState<string | null>(null)
  const [loadingPresets, setLoadingPresets] = useState(false)
  const [editing, setEditing] = useState<Partial<BuiltinTemplate> | null>(null)
  const [saving, setSaving] = useState(false)

  const refresh = useCallback(async () => {
    try {
      const list = await api.listOpsTemplates()
      setTemplates(list || [])
    } catch (err) {
      onToast(`读取运维模板失败: ${err}`, 'error')
    }
  }, [onToast])

  useEffect(() => { if (open) void refresh() }, [open, refresh])
  if (!open) return null

  const openNew = () => setEditing({ id: '', name: '', category: '自定义', description: '', safetyNote: '', command: '' })

  const handleLoadPresets = async () => {
    setLoadingPresets(true)
    try {
      const res = await api.loadPresetTemplates()
      setTemplates(res.templates || [])
      if (res.added > 0) {
        onToast(`已加载 ${res.added} 个预置模板${res.skipped > 0 ? `（跳过 ${res.skipped} 个已存在的原预置模板）` : ''}`)
      } else {
        onToast(`原预置模板均已存在（已跳过 ${res.skipped} 个），未覆盖任何现有模板`)
      }
    } catch (err) {
      onToast(`加载预置模板失败: ${err}`, 'error')
    } finally {
      setLoadingPresets(false)
    }
  }

  const handleSaveTemplate = async (draft: Partial<BuiltinTemplate>) => {
    setSaving(true)
    try {
      await api.saveOpsTemplate(draft)
      setEditing(null)
      await refresh()
      onToast(draft.id ? '运维模板已更新' : '新运维模板已保存')
    } catch (err) {
      onToast(String(err), 'error')
    } finally {
      setSaving(false)
    }
  }

  const handleDeleteTemplate = async (tpl: BuiltinTemplate) => {
    if (!window.confirm(`确定删除运维模板「${tpl.name}」吗？${tpl.presetId ? '\n（删除原预置模板后可随时点击「加载预置模板」重新恢复）' : ''}`)) return
    setBusyId(tpl.id)
    try {
      await api.deleteOpsTemplate(tpl.id)
      await refresh()
      onToast(`已删除模板「${tpl.name}」`)
    } catch (err) {
      onToast(String(err), 'error')
    } finally {
      setBusyId(null)
    }
  }

  return (
    <ModalShell open={open} onClose={onClose}>
      <div className="w-full max-w-2xl rounded-2xl bg-ag-panel border border-ag-borderStrong shadow-ag overflow-hidden flex flex-col max-h-[86vh]">
        <div className="px-5 py-3.5 border-b border-ag-border bg-[#11151f] flex items-center justify-between gap-3">
          <div className="flex items-center gap-2.5 min-w-0">
            <div className="w-8 h-8 rounded-lg bg-violet-500/15 border border-violet-500/30 flex items-center justify-center text-violet-300 shrink-0">
              <Sparkles className="w-4 h-4" />
            </div>
            <div className="min-w-0">
              <h3 className="text-sm font-semibold text-ag-text">VPS 运维模板</h3>
              <p className="text-[11px] text-ag-muted truncate">支持编辑、新增、删除及一键补充预置模板（不覆盖现有）</p>
            </div>
          </div>
          <div className="flex items-center gap-1.5 shrink-0">
            <button type="button" disabled={loadingPresets} onClick={() => void handleLoadPresets()} title="补充缺失的原预置模板，自动跳过已有模板且不覆盖" className="ag-btn-secondary text-xs py-1 px-2.5">
              <RotateCcw className={`w-3.5 h-3.5 text-emerald-400 ${loadingPresets ? 'animate-spin' : ''}`} />加载预置模板
            </button>
            <button type="button" onClick={openNew} className="ag-btn-primary text-xs py-1 px-2.5">
              <Plus className="w-3.5 h-3.5" />新建模板
            </button>
            <button type="button" onClick={onClose} className="p-1.5 rounded-lg hover:bg-ag-hover text-ag-muted hover:text-ag-text">
              <X className="w-4 h-4" />
            </button>
          </div>
        </div>

        <div className="flex-1 overflow-y-auto p-4 space-y-2.5">
          {templates.length === 0 ? (
            <div className="py-12 text-center space-y-3">
              <p className="text-xs text-ag-subtle">当前暂无运维模板</p>
              <div className="flex items-center justify-center gap-2">
                <button type="button" onClick={() => void handleLoadPresets()} className="ag-btn-secondary text-xs">
                  <RotateCcw className="w-3.5 h-3.5 text-emerald-400" />一键加载预置模板
                </button>
                <button type="button" onClick={openNew} className="ag-btn-primary text-xs">
                  <Plus className="w-3.5 h-3.5" />新建自定义模板
                </button>
              </div>
            </div>
          ) : templates.map((tpl) => (
            <TemplateCard
              key={tpl.id}
              tpl={tpl}
              expanded={expandedId === tpl.id}
              busy={busyId === tpl.id}
              hasActiveSession={hasActiveSession}
              onToggleExpand={() => setExpandedId(expandedId === tpl.id ? null : tpl.id)}
              onEdit={() => setEditing(tpl)}
              onDelete={() => void handleDeleteTemplate(tpl)}
              onImport={async () => { setBusyId(tpl.id); try { await onImport(tpl) } finally { setBusyId(null) } }}
              onRunNow={onRunNow ? async () => { setBusyId(tpl.id); try { await onRunNow(tpl); onClose() } finally { setBusyId(null) } } : undefined}
            />
          ))}
        </div>

        <div className="px-5 py-3 border-t border-ag-border bg-[#11151f] flex items-center justify-between text-[11px] text-ag-subtle">
          <span>共 {templates.length} 个运维模板 · 随加密配置保存</span>
          <button type="button" onClick={onClose} className="ag-btn-ghost text-xs">关闭</button>
        </div>
      </div>

      <TemplateEditorModal open={!!editing} initial={editing} saving={saving} onClose={() => setEditing(null)} onSubmit={handleSaveTemplate} />
    </ModalShell>
  )
}

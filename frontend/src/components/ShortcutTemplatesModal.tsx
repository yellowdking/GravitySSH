import React, { useCallback, useEffect, useRef, useState } from 'react'
import { Plus, RotateCcw, Sparkles, X } from 'lucide-react'
import type { BuiltinTemplate } from '../types'
import { api } from '../wailsApi'
import { errMsg } from '../utils/errors'
import { useActionDialogs } from '../hooks/useActionDialogs'
import { ModalShell } from './ModalShell'
import { TemplateEditorModal } from './TemplateEditorModal'
import { TemplateCard } from './TemplateCard'

interface Props {
  open: boolean
  hasActiveSession: boolean
  onClose: () => void
  onImport: (tpl: BuiltinTemplate) => Promise<void>
  onRunNow?: (tpl: BuiltinTemplate) => Promise<boolean | void>
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const ShortcutTemplatesModal: React.FC<Props> = ({ open, hasActiveSession, onClose, onImport, onRunNow, onToast }) => {
  const [templates, setTemplates] = useState<BuiltinTemplate[]>([])
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [busyId, setBusyId] = useState<string | null>(null)
  const [loadingPresets, setLoadingPresets] = useState(false)
  const [editing, setEditing] = useState<Partial<BuiltinTemplate> | null>(null)
  const [saving, setSaving] = useState(false)
  const { confirm, dialogNode } = useActionDialogs()

  const mountedRef = useRef(true)
  const busyRef = useRef(false)
  const loadingPresetsRef = useRef(false)
  const savingRef = useRef(false)

  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  const refresh = useCallback(async () => {
    try {
      const list = await api.listOpsTemplates()
      if (mountedRef.current) setTemplates(list || [])
    } catch (err) {
      if (mountedRef.current) onToast(`读取运维模板失败: ${errMsg(err)}`, 'error')
    }
  }, [onToast])

  useEffect(() => { if (open) void refresh() }, [open, refresh])

  const anyBusy = busyId !== null || loadingPresets || saving
  const isAnyBusyRef = () => busyRef.current || loadingPresetsRef.current || savingRef.current

  const handleClose = () => {
    if (isAnyBusyRef()) return
    onClose()
  }

  const openNew = () => {
    if (isAnyBusyRef()) return
    setEditing({ id: '', name: '', category: '自定义', description: '', safetyNote: '', command: '' })
  }

  const handleLoadPresets = async () => {
    if (isAnyBusyRef()) return
    loadingPresetsRef.current = true
    setLoadingPresets(true)
    try {
      const res = await api.loadPresetTemplates()
      if (!mountedRef.current) return
      setTemplates(res.templates || [])
      if (res.added > 0) {
        onToast(`已加载 ${res.added} 个预置模板${res.skipped > 0 ? `（跳过 ${res.skipped} 个已存在的原预置模板）` : ''}`)
      } else {
        onToast(`原预置模板均已存在（已跳过 ${res.skipped} 个），未覆盖任何现有模板`)
      }
    } catch (err) {
      if (mountedRef.current) onToast(`加载预置模板失败: ${errMsg(err)}`, 'error')
    } finally {
      loadingPresetsRef.current = false
      if (mountedRef.current) setLoadingPresets(false)
    }
  }

  const handleSaveTemplate = async (draft: Partial<BuiltinTemplate>) => {
    if (savingRef.current) return
    savingRef.current = true
    setSaving(true)
    try {
      await api.saveOpsTemplate(draft)
      if (!mountedRef.current) return
      setEditing(null)
      await refresh()
      if (mountedRef.current) onToast(draft.id ? '运维模板已更新' : '新运维模板已保存')
    } catch (err) {
      if (mountedRef.current) onToast(errMsg(err), 'error')
    } finally {
      savingRef.current = false
      if (mountedRef.current) setSaving(false)
    }
  }

  const handleDeleteTemplate = async (tpl: BuiltinTemplate) => {
    if (isAnyBusyRef()) return
    const ok = await confirm({
      title: '删除运维模板',
      message: `确定删除运维模板「${tpl.name}」吗？${tpl.presetId ? '\n（删除原预置模板后可随时点击「加载预置模板」重新恢复）' : ''}`,
      confirmLabel: '删除',
    })
    if (!ok || isAnyBusyRef()) return
    busyRef.current = true
    setBusyId(tpl.id)
    try {
      await api.deleteOpsTemplate(tpl.id)
      await refresh()
      if (mountedRef.current) onToast(`已删除模板「${tpl.name}」`)
    } catch (err) {
      if (mountedRef.current) onToast(errMsg(err), 'error')
    } finally {
      busyRef.current = false
      if (mountedRef.current) setBusyId(null)
    }
  }

  const handleImportCard = async (tpl: BuiltinTemplate) => {
    if (isAnyBusyRef()) return
    busyRef.current = true
    setBusyId(tpl.id)
    try {
      await onImport(tpl)
    } finally {
      busyRef.current = false
      if (mountedRef.current) setBusyId(null)
    }
  }

  const handleRunCardNow = async (tpl: BuiltinTemplate) => {
    if (!onRunNow || isAnyBusyRef()) return
    busyRef.current = true
    setBusyId(tpl.id)
    let succeeded = false
    try {
      const res = await onRunNow(tpl)
      succeeded = res !== false
    } finally {
      busyRef.current = false
      if (mountedRef.current) setBusyId(null)
    }
    if (succeeded && mountedRef.current) onClose()
  }

  return (
    <ModalShell open={open} onClose={handleClose}>
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
            <button type="button" disabled={anyBusy} onClick={() => void handleLoadPresets()} title="补充缺失的原预置模板，自动跳过已有模板且不覆盖" className="ag-btn-secondary text-xs py-1 px-2.5">
              <RotateCcw className={`w-3.5 h-3.5 text-emerald-400 ${loadingPresets ? 'animate-spin' : ''}`} />加载预置模板
            </button>
            <button type="button" disabled={anyBusy} onClick={openNew} className="ag-btn-primary text-xs py-1 px-2.5">
              <Plus className="w-3.5 h-3.5" />新建模板
            </button>
            <button type="button" disabled={anyBusy} onClick={handleClose} aria-label="关闭运维模板窗口" className="p-1.5 rounded-lg hover:bg-ag-hover text-ag-muted hover:text-ag-text disabled:opacity-40 disabled:cursor-not-allowed">
              <X className="w-4 h-4" />
            </button>
          </div>
        </div>

        <div className="flex-1 overflow-y-auto p-4 space-y-2.5">
          {templates.length === 0 ? (
            <div className="py-12 text-center space-y-3">
              <p className="text-xs text-ag-subtle">当前暂无运维模板</p>
              <div className="flex items-center justify-center gap-2">
                <button type="button" disabled={anyBusy} onClick={() => void handleLoadPresets()} className="ag-btn-secondary text-xs">
                  <RotateCcw className={`w-3.5 h-3.5 text-emerald-400 ${loadingPresets ? 'animate-spin' : ''}`} />一键加载预置模板
                </button>
                <button type="button" disabled={anyBusy} onClick={openNew} className="ag-btn-primary text-xs">
                  <Plus className="w-3.5 h-3.5" />新建自定义模板
                </button>
              </div>
            </div>
          ) : templates.map((tpl) => (
            <TemplateCard
              key={tpl.id}
              tpl={tpl}
              expanded={expandedId === tpl.id}
              busy={anyBusy}
              hasActiveSession={hasActiveSession}
              onToggleExpand={() => setExpandedId(expandedId === tpl.id ? null : tpl.id)}
              onEdit={() => { if (!isAnyBusyRef()) setEditing(tpl) }}
              onDelete={() => void handleDeleteTemplate(tpl)}
              onImport={() => void handleImportCard(tpl)}
              onRunNow={onRunNow ? () => void handleRunCardNow(tpl) : undefined}
            />
          ))}
        </div>

        <div className="px-5 py-3 border-t border-ag-border bg-[#11151f] flex items-center justify-between text-[11px] text-ag-subtle">
          <span>共 {templates.length} 个运维模板 · 随加密配置保存</span>
          <button type="button" disabled={anyBusy} onClick={handleClose} className="ag-btn-ghost text-xs">关闭</button>
        </div>
      </div>

      <TemplateEditorModal open={!!editing} initial={editing} saving={saving} onClose={() => { if (!savingRef.current) setEditing(null) }} onSubmit={handleSaveTemplate} />
      {dialogNode}
    </ModalShell>
  )
}

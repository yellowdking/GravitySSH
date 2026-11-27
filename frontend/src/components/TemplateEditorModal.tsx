import React, { useEffect, useState } from 'react'
import { Save, Sparkles, X } from 'lucide-react'
import type { BuiltinTemplate } from '../types'
import { ModalShell } from './ModalShell'

interface Props {
  open: boolean
  initial: Partial<BuiltinTemplate> | null
  saving: boolean
  onClose: () => void
  onSubmit: (draft: Partial<BuiltinTemplate>) => Promise<void>
}

export const TemplateEditorModal: React.FC<Props> = ({ open, initial, saving, onClose, onSubmit }) => {
  const [name, setName] = useState('')
  const [category, setCategory] = useState('自定义')
  const [description, setDescription] = useState('')
  const [safetyNote, setSafetyNote] = useState('')
  const [command, setCommand] = useState('')

  useEffect(() => {
    if (!open) return
    setName(initial?.name || '')
    setCategory(initial?.category || '自定义')
    setDescription(initial?.description || '')
    setSafetyNote(initial?.safetyNote || '')
    setCommand(initial?.command || '')
  }, [open, initial])

  if (!open) return null

  const lineCount = command === '' ? 0 : command.replace(/\r\n/g, '\n').split('\n').length
  const isEdit = !!initial?.id

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim() || !command.trim()) return
    await onSubmit({
      id: initial?.id || '',
      presetId: initial?.presetId || '',
      name: name.trim(),
      category: category.trim() || '自定义',
      description: description.trim(),
      safetyNote: safetyNote.trim(),
      command,
    })
  }

  return (
    <ModalShell open={open} onClose={onClose} zIndexClass="z-[60]" backdropClassName="bg-black/75">
      <div className="w-full max-w-2xl rounded-2xl bg-ag-panel border border-ag-borderStrong shadow-ag overflow-hidden flex flex-col max-h-[88vh]">
        <div className="px-5 py-3.5 border-b border-ag-border bg-[#11151f] flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Sparkles className="w-4 h-4 text-violet-400" />
            <h3 className="text-sm font-semibold text-ag-text">{isEdit ? '编辑运维模板' : '新建运维模板'}</h3>
          </div>
          <button type="button" onClick={onClose} className="p-1.5 rounded-lg hover:bg-ag-hover text-ag-muted hover:text-ag-text">
            <X className="w-4 h-4" />
          </button>
        </div>

        <form onSubmit={(e) => void handleSubmit(e)} className="flex-1 overflow-y-auto p-5 space-y-3.5">
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            <div className="sm:col-span-2">
              <label className="block text-xs text-ag-muted mb-1">模板名称 *</label>
              <input value={name} onChange={(e) => setName(e.target.value)} required placeholder="例如：清理系统日志 / 配置 Nginx" className="ag-input" />
            </div>
            <div>
              <label className="block text-xs text-ag-muted mb-1">分类</label>
              <input value={category} onChange={(e) => setCategory(e.target.value)} placeholder="网络优化 / 安全加固 / 自定义" className="ag-input" />
            </div>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div>
              <label className="block text-xs text-ag-muted mb-1">功能说明（可选）</label>
              <input value={description} onChange={(e) => setDescription(e.target.value)} placeholder="简要说明脚本用途" className="ag-input" />
            </div>
            <div>
              <label className="block text-xs text-ag-muted mb-1">安全提示（可选）</label>
              <input value={safetyNote} onChange={(e) => setSafetyNote(e.target.value)} placeholder="例如：执行前会自动备份原配置" className="ag-input" />
            </div>
          </div>

          <div>
            <div className="flex items-center justify-between mb-1">
              <label className="text-xs text-ag-muted">脚本内容（支持多行命令）*</label>
              <span className="text-[11px] font-mono text-ag-subtle">{lineCount} 行</span>
            </div>
            <textarea
              rows={11}
              required
              spellCheck={false}
              value={command}
              onChange={(e) => setCommand(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Tab') {
                  e.preventDefault()
                  const t = e.currentTarget
                  const start = t.selectionStart
                  const end = t.selectionEnd
                  setCommand(command.slice(0, start) + '  ' + command.slice(end))
                  requestAnimationFrame(() => { t.selectionStart = t.selectionEnd = start + 2 })
                }
              }}
              placeholder="输入要执行的 Shell 脚本，可包含多条或数十条命令..."
              className="ag-input font-mono text-xs leading-relaxed resize-y"
            />
          </div>

          <div className="pt-2 flex justify-end gap-2">
            <button type="button" onClick={onClose} className="ag-btn-ghost">取消</button>
            <button type="submit" disabled={saving || !name.trim() || !command.trim()} className="ag-btn-primary">
              <Save className="w-3.5 h-3.5" />{saving ? '保存中...' : '保存模板'}
            </button>
          </div>
        </form>
      </div>
    </ModalShell>
  )
}

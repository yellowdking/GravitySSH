import React, { useEffect, useState } from 'react'
import { FileCode2, Loader2, Save, X } from 'lucide-react'
import { insertTabIndent } from '../utils/errors'
import { useActionDialogs } from '../hooks/useActionDialogs'
import { ModalShell } from './ModalShell'

interface Props {
  open: boolean
  remotePath: string
  initialContent: string
  saving: boolean
  onSave: (content: string) => Promise<void>
  onClose: () => void
}

export const SftpEditorModal: React.FC<Props> = ({ open, remotePath, initialContent, saving, onSave, onClose }) => {
  const [content, setContent] = useState(initialContent)
  const { confirm, dialogNode } = useActionDialogs()

  useEffect(() => {
    if (open) setContent(initialContent)
  }, [open, initialContent, remotePath])

  const dirty = content !== initialContent
  const lineCount = content === '' ? 0 : content.replace(/\r\n/g, '\n').split('\n').length

  const handleClose = async () => {
    if (dirty) {
      const ok = await confirm({
        title: '放弃未保存的修改',
        message: '文件有未保存的修改，确定放弃修改并关闭吗？',
        confirmLabel: '放弃并关闭',
      })
      if (!ok) return
    }
    onClose()
  }

  return (
    <ModalShell open={open} onClose={() => { void handleClose() }} backdropClassName="bg-black/75">
      <div className="w-full max-w-4xl h-[82vh] rounded-2xl bg-ag-panel border border-ag-borderStrong shadow-ag flex flex-col overflow-hidden">
        <div className="px-4 py-3 border-b border-ag-border bg-[#11151f] flex items-center justify-between gap-3 shrink-0">
          <div className="flex items-center gap-2 min-w-0">
            <FileCode2 className="w-4 h-4 text-indigo-400 shrink-0" />
            <span className="text-xs font-mono text-ag-text truncate" title={remotePath}>{remotePath}</span>
            {dirty && (
              <span className="text-[10px] px-1.5 py-0.5 rounded bg-amber-500/15 text-amber-300 border border-amber-500/30 shrink-0">
                未保存
              </span>
            )}
          </div>
          <div className="flex items-center gap-2 shrink-0">
            <span className="text-[11px] font-mono text-ag-subtle">{lineCount} 行 · Ctrl+S 保存</span>
            <button
              type="button"
              disabled={saving || !dirty}
              onClick={() => void onSave(content)}
              className="ag-btn-primary text-xs py-1.5 px-3"
            >
              {saving ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Save className="w-3.5 h-3.5" />}
              {saving ? '保存中...' : '保存回传'}
            </button>
            <button type="button" onClick={() => { void handleClose() }} aria-label="关闭文件编辑器" className="p-1.5 rounded-lg hover:bg-ag-hover text-ag-muted hover:text-ag-text">
              <X className="w-4 h-4" />
            </button>
          </div>
        </div>

        <textarea
          value={content}
          onChange={(e) => setContent(e.target.value)}
          onKeyDown={(e) => {
            if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
              e.preventDefault()
              if (dirty && !saving) void onSave(content)
            } else if (e.key === 'Escape') {
              e.preventDefault()
              void handleClose()
            } else {
              insertTabIndent(e, content, setContent)
            }
          }}
          spellCheck={false}
          className="flex-1 w-full bg-[#0b0d12] text-[#e6eaf5] font-mono text-[13px] leading-relaxed p-4 resize-none focus:outline-none"
        />
      </div>
      {dialogNode}
    </ModalShell>
  )
}

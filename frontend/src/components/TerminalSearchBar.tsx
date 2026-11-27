import React from 'react'
import { Search, X } from 'lucide-react'

interface Props {
  value: string
  onChange: (val: string) => void
  onPrev: () => void
  onNext: () => void
  onClose: () => void
}

export const TerminalSearchBar: React.FC<Props> = ({ value, onChange, onPrev, onNext, onClose }) => (
  <div className="px-3 py-1.5 bg-ag-panel border-b border-ag-border flex items-center gap-2 text-xs">
    <Search className="w-3.5 h-3.5 text-ag-subtle" />
    <input
      autoFocus
      value={value}
      onChange={(e) => onChange(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === 'Enter') {
          if (e.shiftKey) onPrev()
          else onNext()
        } else if (e.key === 'Escape') {
          onClose()
        }
      }}
      placeholder="在终端输出中搜索..."
      className="flex-1 bg-transparent text-ag-text focus:outline-none"
    />
    <button onClick={onPrev} className="px-2 py-0.5 rounded bg-ag-elevated hover:bg-ag-hover text-ag-muted">上一个</button>
    <button onClick={onNext} className="px-2 py-0.5 rounded bg-ag-elevated hover:bg-ag-hover text-ag-muted">下一个</button>
    <button type="button" onClick={onClose} aria-label="关闭搜索" className="p-1 text-ag-muted hover:text-ag-text"><X className="w-3.5 h-3.5" /></button>
  </div>
)

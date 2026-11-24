import React from 'react'
import { FileText, Folder, Sparkles } from 'lucide-react'
import type { CompletionItem } from '../types'

interface Props {
  suggestions: CompletionItem[]
  selectedIdx: number
  onSelectIndex: (idx: number) => void
  onApply: (item: CompletionItem) => void
}

function getSourceBadge(item: CompletionItem): { label: string; cls: string; icon?: React.ReactNode } {
  switch (item.source) {
    case 'dir':
      return {
        label: '目录',
        cls: 'bg-sky-500/20 text-sky-300 border-sky-500/35',
        icon: <Folder className="w-2.5 h-2.5" />,
      }
    case 'file':
      return {
        label: '文件',
        cls: 'bg-slate-500/20 text-slate-300 border-slate-400/30',
        icon: <FileText className="w-2.5 h-2.5" />,
      }
    case 'shortcut':
      return { label: '快捷', cls: 'bg-violet-500/20 text-violet-300 border-violet-500/30' }
    case 'history':
      return { label: '历史', cls: 'bg-emerald-500/15 text-emerald-300 border-emerald-500/25' }
    default:
      return { label: item.category || '内置', cls: 'bg-indigo-500/15 text-indigo-300 border-indigo-500/25' }
  }
}

export const AutocompletePopup: React.FC<Props> = ({ suggestions, selectedIdx, onSelectIndex, onApply }) => {
  if (suggestions.length === 0) return null

  return (
    <div className="absolute left-4 bottom-4 z-30 w-[560px] max-w-[calc(100%-2rem)] rounded-xl bg-[#131824]/96 backdrop-blur-md border border-indigo-500/40 shadow-ag overflow-hidden">
      <div className="px-3 py-1.5 bg-[#0f131e] border-b border-ag-border flex items-center justify-between text-[11px] text-ag-muted">
        <span className="flex items-center gap-1.5 text-indigo-300 font-medium">
          <Sparkles className="w-3 h-3" />
          智能命令与路径补齐 · 点击自动补全
        </span>
        <span className="text-ag-subtle">↑↓ 选择 · Tab / 点击补齐 · Esc 关闭</span>
      </div>
      <div className="max-h-64 overflow-y-auto divide-y divide-ag-border/40">
        {suggestions.map((item, idx) => {
          const isSelected = idx === selectedIdx
          const badge = getSourceBadge(item)

          return (
            <button
              key={`${item.source}-${item.command}-${idx}`}
              type="button"
              onMouseDown={(e) => {
                e.preventDefault()
                onApply(item)
              }}
              onMouseEnter={() => onSelectIndex(idx)}
              className={`w-full text-left px-3 py-2 flex items-center justify-between gap-3 transition ${
                isSelected ? 'bg-indigo-500/20' : 'hover:bg-ag-hover/70'
              }`}
            >
              <div className="flex items-center gap-2 min-w-0 flex-1">
                <span className={`inline-flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded border shrink-0 font-medium ${badge.cls}`}>
                  {badge.icon}
                  {badge.label}
                </span>
                <span className="font-mono text-xs text-ag-text font-medium truncate">{item.command}</span>
              </div>
              <span className="text-[11px] text-ag-muted truncate max-w-[200px] shrink-0">{item.description}</span>
            </button>
          )
        })}
      </div>
    </div>
  )
}

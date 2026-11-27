import React from 'react'
import { FileText, Folder, Sparkles } from 'lucide-react'
import type { CompletionItem } from '../types'
import { getActiveTokenPrefix } from '../utils/autocomplete'

interface Props {
  suggestions: CompletionItem[]
  selectedIdx: number
  inputLine?: string
  onSelectIndex: (idx: number) => void
  onApply: (item: CompletionItem) => void
}

const HIDE_DESC = new Set(['dir', 'file', 'history', 'history_local'])

function getSourceBadge(item: CompletionItem): { label: string; cls: string; icon?: React.ReactNode } {
  switch (item.source) {
    case 'dir':
      return {
        label: '目录',
        cls: 'bg-sky-500/20 text-sky-300 border-sky-500/35',
        icon: <Folder className="w-2.5 h-2.5 pointer-events-none" />,
      }
    case 'file':
      return {
        label: '文件',
        cls: 'bg-emerald-500/20 text-emerald-300 border-emerald-500/35',
        icon: <FileText className="w-2.5 h-2.5 pointer-events-none" />,
      }
    case 'subcmd':
      return { label: item.category || '命令', cls: 'bg-indigo-500/20 text-indigo-300 border-indigo-500/35' }
    case 'flag':
      return { label: '参数', cls: 'bg-amber-500/20 text-amber-300 border-amber-500/35' }
    case 'history_local':
      return { label: '本机历史', cls: 'bg-slate-500/20 text-slate-300 border-slate-400/30' }
    case 'history':
      return { label: '历史', cls: 'bg-slate-500/15 text-slate-300 border-slate-400/25' }
    default:
      return { label: item.category || '命令', cls: 'bg-violet-500/20 text-violet-300 border-violet-500/30' }
  }
}

function resolvePrefix(item: CompletionItem, inputLine?: string): string {
  if (inputLine === undefined) return ''
  if (item.source === 'history' || item.source === 'history_local') {
    const full = inputLine.trimStart()
    if (full && item.command.toLowerCase().startsWith(full.toLowerCase())) return full
  }
  return getActiveTokenPrefix(inputLine, item.source)
}

function renderHighlighted(command: string, prefix: string) {
  if (prefix && command.toLowerCase().startsWith(prefix.toLowerCase())) {
    const n = Math.min(prefix.length, command.length)
    return (
      <>
        <span className="pointer-events-none text-cyan-400 font-bold">{command.slice(0, n)}</span>
        <span className="pointer-events-none text-ag-text">{command.slice(n)}</span>
      </>
    )
  }
  return <span className="pointer-events-none text-ag-text">{command}</span>
}

export const AutocompletePopup: React.FC<Props> = ({
  suggestions, selectedIdx, inputLine, onSelectIndex, onApply,
}) => {
  const visible = suggestions.slice(0, 5)
  if (visible.length === 0) return null

  return (
    <div className="pointer-events-auto absolute top-2 right-3 z-30 w-[min(340px,calc(100%-1.5rem))] max-w-[calc(100%-1.5rem)] max-h-[calc(100%-1rem)] flex flex-col rounded-xl bg-[#111622]/[0.92] backdrop-blur-md border border-indigo-500/35 shadow-ag overflow-hidden select-none">
      <div className="shrink-0 px-2.5 py-1 bg-[#0c101a]/95 border-b border-ag-border/80 flex items-center justify-between text-[10px] text-ag-muted">
        <span className="flex items-center gap-1 text-indigo-300 font-medium">
          <Sparkles className="w-3 h-3 shrink-0" />
          智能补齐
        </span>
        <span className="text-ag-subtle">↑↓ · Tab · Esc</span>
      </div>
      <div className="overflow-y-auto min-h-0 divide-y divide-ag-border/35">
        {visible.map((item, idx) => {
          const isSelected = idx === selectedIdx
          const badge = getSourceBadge(item)
          const prefix = resolvePrefix(item, inputLine)
          const showDesc = Boolean(item.description) && !HIDE_DESC.has(item.source)

          return (
            <button
              key={`${item.source}-${item.command}-${idx}`}
              type="button"
              onMouseDown={(e) => {
                e.preventDefault()
                e.stopPropagation()
                onApply(item)
              }}
              onClick={(e) => {
                e.preventDefault()
                e.stopPropagation()
              }}
              onMouseEnter={() => onSelectIndex(idx)}
              className={`w-full min-h-[34px] py-1.5 px-3 text-left flex items-center justify-between gap-2 cursor-pointer select-none transition ${
                isSelected ? 'bg-indigo-500/[0.22]' : 'hover:bg-ag-hover/70'
              }`}
            >
              <span className="pointer-events-none font-mono text-[12px] font-medium truncate min-w-0 flex-1">
                {renderHighlighted(item.command, prefix)}
              </span>
              <div className="pointer-events-none flex items-center gap-1.5 shrink-0">
                {showDesc && (
                  <span className="pointer-events-none text-[10px] text-ag-muted truncate max-w-[112px]">
                    {item.description}
                  </span>
                )}
                <span className={`pointer-events-none inline-flex items-center gap-0.5 text-[10px] leading-none px-1.5 py-0.5 rounded border shrink-0 font-medium ${badge.cls}`}>
                  {badge.icon}
                  {badge.label}
                </span>
              </div>
            </button>
          )
        })}
      </div>
    </div>
  )
}

import React from 'react'
import { Bookmark, Plus, Search, Sparkles } from 'lucide-react'

interface Props {
  search: string
  categories: string[]
  selectedCat: string
  onSearchChange: (v: string) => void
  onSelectCat: (cat: string) => void
  onOpenTemplates: () => void
  onNewShortcut: () => void
}

export const ShortcutsHeader: React.FC<Props> = ({
  search, categories, selectedCat, onSearchChange, onSelectCat, onOpenTemplates, onNewShortcut,
}) => (
  <div className="px-4 py-3 border-b border-ag-border bg-[#0f1219] space-y-2.5">
    <div className="flex items-center justify-between gap-2">
      <div className="flex items-center gap-2 min-w-0">
        <Bookmark className="w-4 h-4 text-violet-400 shrink-0" />
        <span className="text-sm font-semibold text-ag-text truncate">快捷命令库</span>
      </div>
      <div className="flex items-center gap-1.5 shrink-0">
        <button
          onClick={onOpenTemplates}
          title="开 BBR / 建 SWAP / 换源 / 装 Docker / 安全改 SSH 端口"
          className="ag-btn-secondary text-xs py-1 px-2"
        >
          <Sparkles className="w-3.5 h-3.5 text-violet-400" />运维模板
        </button>
        <button onClick={onNewShortcut} className="ag-btn-primary text-xs py-1 px-2.5">
          <Plus className="w-3.5 h-3.5" />新建
        </button>
      </div>
    </div>
    <div className="relative">
      <Search className="w-3.5 h-3.5 text-ag-subtle absolute left-2.5 top-1/2 -translate-y-1/2" />
      <input
        value={search}
        onChange={(e) => onSearchChange(e.target.value)}
        placeholder="搜索快捷命令名称或内容..."
        className="w-full rounded-lg bg-[#0b0d12] border border-ag-border pl-8 pr-2.5 py-1.5 text-xs text-ag-text placeholder:text-ag-subtle focus:outline-none focus:border-violet-500/50"
      />
    </div>
    <div className="flex items-center gap-1 overflow-x-auto pb-0.5">
      {categories.map((cat) => (
        <button
          key={cat}
          onClick={() => onSelectCat(cat)}
          className={`px-2 py-0.5 rounded-md text-[11px] whitespace-nowrap transition ${
            selectedCat === cat
              ? 'bg-violet-500/20 text-violet-200 border border-violet-500/35'
              : 'text-ag-muted hover:text-ag-text bg-ag-panel border border-transparent'
          }`}
        >
          {cat}
        </button>
      ))}
    </div>
  </div>
)

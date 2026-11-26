import React, { useMemo, useState } from 'react'
import { Bookmark, KeyRound, PanelLeftClose, Plus, Search } from 'lucide-react'
import type { ServerView, SessionInfo } from '../types'
import { SidebarRail } from './SidebarRail'
import { SidebarServerList } from './SidebarServerList'
import { ResizeHandle } from './ResizeHandle'

export type SidebarTab = 'servers' | 'keys' | 'shortcuts' | 'history'

interface SidebarProps {
  activeNav: SidebarTab
  collapsed: boolean
  width: number
  onToggleCollapse: () => void
  onWidthChange: (nextWidth: number) => void
  onWidthCommit: (finalWidth: number) => void
  onSelectNav: (tab: SidebarTab) => void
  servers: ServerView[]
  sessions: SessionInfo[]
  keyCount: number
  shortcutCount: number
  historyCount: number
  onAddServer: () => void
  onEditServer: (srv: ServerView) => void
  onDeleteServer: (srv: ServerView) => void
  onConnectServer: (srv: ServerView) => void
  onOpenSync: () => void
  onOpenSettings: () => void
}

export const Sidebar: React.FC<SidebarProps> = ({
  activeNav, collapsed, width, onToggleCollapse, onWidthChange, onWidthCommit,
  onSelectNav, servers, sessions, keyCount, shortcutCount,
  onAddServer, onEditServer, onDeleteServer, onConnectServer, onOpenSync, onOpenSettings,
}) => {
  const [search, setSearch] = useState('')

  const connectedServerIds = useMemo(() => {
    const set = new Set<string>()
    for (const s of sessions) {
      if (s.status === 'connected' || s.status === 'connecting') set.add(s.serverId)
    }
    return set
  }, [sessions])

  const groupedServers = useMemo(() => {
    const q = search.trim().toLowerCase()
    const map = new Map<string, ServerView[]>()
    for (const s of servers) {
      if (q && !(s.name.toLowerCase().includes(q) || s.host.toLowerCase().includes(q) || s.username.toLowerCase().includes(q) || (s.group || '').toLowerCase().includes(q))) continue
      const g = s.group?.trim() || '默认分组'
      if (!map.has(g)) map.set(g, [])
      map.get(g)!.push(s)
    }
    return Array.from(map.entries()).sort((a, b) => a[0].localeCompare(b[0]))
  }, [servers, search])

  return (
    <div className="flex h-full select-none shrink-0">
      <SidebarRail activeNav={activeNav} collapsed={collapsed} onSelectNav={onSelectNav} onToggleCollapse={onToggleCollapse} onOpenSync={onOpenSync} onOpenSettings={onOpenSettings} />

      {!collapsed && (
        <>
          <div style={{ width }} className="bg-ag-sidebar border-r border-ag-border flex flex-col min-w-0 shrink-0">
            <div className="px-3 pt-3 pb-2.5 border-b border-ag-border/80">
              <div className="flex items-center justify-between gap-1.5 mb-2.5">
                <span className="text-[13px] font-semibold tracking-wide text-ag-text truncate">GravitySSH</span>
                <div className="flex items-center gap-1 shrink-0">
                  <button onClick={onAddServer} className="inline-flex items-center gap-1 rounded-md bg-indigo-500 hover:bg-indigo-400 text-white text-xs font-medium px-2 py-1 transition shadow-glow">
                    <Plus className="w-3.5 h-3.5" />添加
                  </button>
                  <button onClick={onToggleCollapse} title="折叠侧边栏" className="p-1 rounded-md text-ag-muted hover:text-ag-text hover:bg-ag-hover/70 transition">
                    <PanelLeftClose className="w-3.5 h-3.5" />
                  </button>
                </div>
              </div>
              <div className="relative">
                <Search className="w-3.5 h-3.5 text-ag-subtle absolute left-2.5 top-1/2 -translate-y-1/2" />
                <input
                  type="text"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="搜索服务器 / IP / 分组..."
                  className="w-full rounded-lg bg-[#0b0d12] border border-ag-border pl-8 pr-2.5 py-1.5 text-xs text-ag-text placeholder:text-ag-subtle focus:outline-none focus:border-indigo-500/60 transition"
                />
              </div>
            </div>

            <div className="flex-1 overflow-y-auto px-2 py-2 space-y-2">
              <SidebarServerList totalServers={servers.length} groupedServers={groupedServers} connectedServerIds={connectedServerIds} onAddServer={onAddServer} onConnectServer={onConnectServer} onEditServer={onEditServer} onDeleteServer={onDeleteServer} />
            </div>

            <div className="p-2 border-t border-ag-border/80 bg-[#0d1017]/80 grid grid-cols-2 gap-1.5">
              <button onClick={() => onSelectNav('keys')} className={`flex items-center justify-center gap-1.5 py-1.5 px-2 rounded-lg text-xs transition border truncate ${activeNav === 'keys' ? 'bg-indigo-500/15 border-indigo-500/40 text-indigo-200' : 'bg-ag-panel border-ag-border text-ag-muted hover:text-ag-text hover:bg-ag-elevated'}`}>
                <KeyRound className="w-3.5 h-3.5 shrink-0" />
                <span className="truncate">密钥 ({keyCount})</span>
              </button>
              <button onClick={() => onSelectNav('shortcuts')} className={`flex items-center justify-center gap-1.5 py-1.5 px-2 rounded-lg text-xs transition border truncate ${activeNav === 'shortcuts' ? 'bg-indigo-500/15 border-indigo-500/40 text-indigo-200' : 'bg-ag-panel border-ag-border text-ag-muted hover:text-ag-text hover:bg-ag-elevated'}`}>
                <Bookmark className="w-3.5 h-3.5 shrink-0" />
                <span className="truncate">快捷 ({shortcutCount})</span>
              </button>
            </div>
          </div>
          <ResizeHandle width={width} min={180} max={440} direction="right" onChange={onWidthChange} onCommit={onWidthCommit} title="拖动调整侧边栏宽度" />
        </>
      )}
    </div>
  )
}

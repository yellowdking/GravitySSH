import React, { useMemo, useState } from 'react'
import { Bookmark, ChevronDown, ChevronRight, KeyRound, Plus, Search, Server } from 'lucide-react'
import type { ServerView, SessionInfo } from '../types'
import { ServerListItem } from './ServerListItem'
import { SidebarRail } from './SidebarRail'

export type SidebarTab = 'servers' | 'keys' | 'shortcuts' | 'history'

interface SidebarProps {
  activeNav: SidebarTab
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
  onOpenSettings: () => void
}

export const Sidebar: React.FC<SidebarProps> = ({
  activeNav,
  onSelectNav,
  servers,
  sessions,
  keyCount,
  shortcutCount,
  onAddServer,
  onEditServer,
  onDeleteServer,
  onConnectServer,
  onOpenSettings,
}) => {
  const [search, setSearch] = useState('')
  const [collapsedGroups, setCollapsedGroups] = useState<Record<string, boolean>>({})

  const connectedServerIds = useMemo(() => {
    const set = new Set<string>()
    for (const s of sessions) {
      if (s.status === 'connected' || s.status === 'connecting') set.add(s.serverId)
    }
    return set
  }, [sessions])

  const groupedServers = useMemo(() => {
    const q = search.trim().toLowerCase()
    const filtered = servers.filter((s) => {
      if (!q) return true
      return (
        s.name.toLowerCase().includes(q) ||
        s.host.toLowerCase().includes(q) ||
        s.username.toLowerCase().includes(q) ||
        (s.group || '').toLowerCase().includes(q)
      )
    })
    const map = new Map<string, ServerView[]>()
    for (const s of filtered) {
      const g = s.group?.trim() || '默认分组'
      if (!map.has(g)) map.set(g, [])
      map.get(g)!.push(s)
    }
    return Array.from(map.entries()).sort((a, b) => a[0].localeCompare(b[0]))
  }, [servers, search])

  return (
    <div className="flex h-full select-none shrink-0">
      <SidebarRail activeNav={activeNav} onSelectNav={onSelectNav} onOpenSettings={onOpenSettings} />

      <div className="w-[268px] bg-ag-sidebar border-r border-ag-border flex flex-col">
        <div className="px-3.5 pt-3.5 pb-2.5 border-b border-ag-border/80">
          <div className="flex items-center justify-between mb-2.5">
            <div className="flex items-center gap-2">
              <span className="text-[13px] font-semibold tracking-wide text-ag-text">GravitySSH</span>
              <span className="text-[10px] px-1.5 py-0.5 rounded bg-indigo-500/15 text-indigo-300 border border-indigo-500/25 font-medium">VAULT</span>
            </div>
            <button onClick={onAddServer} className="inline-flex items-center gap-1 rounded-md bg-indigo-500 hover:bg-indigo-400 text-white text-xs font-medium px-2 py-1 transition shadow-glow">
              <Plus className="w-3.5 h-3.5" />
              添加
            </button>
          </div>
          <div className="relative">
            <Search className="w-3.5 h-3.5 text-ag-subtle absolute left-2.5 top-1/2 -translate-y-1/2" />
            <input type="text" value={search} onChange={(e) => setSearch(e.target.value)} placeholder="搜索服务器 / IP / 分组..." className="w-full rounded-lg bg-[#0b0d12] border border-ag-border pl-8 pr-2.5 py-1.5 text-xs text-ag-text placeholder:text-ag-subtle focus:outline-none focus:border-indigo-500/60 transition" />
          </div>
        </div>

        <div className="flex-1 overflow-y-auto px-2 py-2 space-y-2">
          {servers.length === 0 ? (
            <div className="px-3 py-10 text-center">
              <div className="w-10 h-10 mx-auto mb-3 rounded-xl bg-ag-elevated border border-ag-border flex items-center justify-center text-ag-muted">
                <Server className="w-5 h-5" />
              </div>
              <p className="text-xs text-ag-muted mb-3">还没有添加服务器连接</p>
              <button onClick={onAddServer} className="ag-btn-primary text-xs py-1.5 px-3"><Plus className="w-3.5 h-3.5" />添加第一台服务器</button>
            </div>
          ) : groupedServers.length === 0 ? (
            <p className="text-xs text-ag-subtle text-center py-8">未找到匹配的服务器</p>
          ) : (
            groupedServers.map(([group, list]) => {
              const collapsed = !!collapsedGroups[group]
              return (
                <div key={group}>
                  <button onClick={() => setCollapsedGroups((prev) => ({ ...prev, [group]: !prev[group] }))} className="w-full flex items-center justify-between px-2 py-1 text-[11px] font-medium uppercase tracking-wider text-ag-subtle hover:text-ag-muted transition">
                    <span className="flex items-center gap-1">{collapsed ? <ChevronRight className="w-3 h-3" /> : <ChevronDown className="w-3 h-3" />}{group}</span>
                    <span className="text-[10px] bg-ag-elevated px-1.5 rounded">{list.length}</span>
                  </button>
                  {!collapsed && (
                    <div className="mt-1 space-y-1">
                      {list.map((srv) => (
                        <ServerListItem key={srv.id} server={srv} isLive={connectedServerIds.has(srv.id)} onConnect={onConnectServer} onEdit={onEditServer} onDelete={onDeleteServer} />
                      ))}
                    </div>
                  )}
                </div>
              )
            })
          )}
        </div>

        <div className="p-2 border-t border-ag-border/80 bg-[#0d1017]/80 grid grid-cols-2 gap-1.5">
          <button onClick={() => onSelectNav('keys')} className={`flex items-center justify-center gap-1.5 py-1.5 px-2 rounded-lg text-xs transition border ${activeNav === 'keys' ? 'bg-indigo-500/15 border-indigo-500/40 text-indigo-200' : 'bg-ag-panel border-ag-border text-ag-muted hover:text-ag-text hover:bg-ag-elevated'}`}>
            <KeyRound className="w-3.5 h-3.5" />
            SSH 密钥 ({keyCount})
          </button>
          <button onClick={() => onSelectNav('shortcuts')} className={`flex items-center justify-center gap-1.5 py-1.5 px-2 rounded-lg text-xs transition border ${activeNav === 'shortcuts' ? 'bg-indigo-500/15 border-indigo-500/40 text-indigo-200' : 'bg-ag-panel border-ag-border text-ag-muted hover:text-ag-text hover:bg-ag-elevated'}`}>
            <Bookmark className="w-3.5 h-3.5" />
            快捷 ({shortcutCount})
          </button>
        </div>
      </div>
    </div>
  )
}

import React, { useState } from 'react'
import { ChevronDown, ChevronRight, Plus, Server } from 'lucide-react'
import type { ServerView } from '../types'
import { ServerListItem } from './ServerListItem'

interface Props {
  totalServers: number
  groupedServers: [string, ServerView[]][]
  connectedServerIds: Set<string>
  onAddServer: () => void
  onConnectServer: (srv: ServerView) => void
  onEditServer: (srv: ServerView) => void
  onDeleteServer: (srv: ServerView) => void
}

export const SidebarServerList: React.FC<Props> = ({
  totalServers,
  groupedServers,
  connectedServerIds,
  onAddServer,
  onConnectServer,
  onEditServer,
  onDeleteServer,
}) => {
  const [collapsedGroups, setCollapsedGroups] = useState<Record<string, boolean>>({})

  if (totalServers === 0) {
    return (
      <div className="px-3 py-10 text-center">
        <div className="w-10 h-10 mx-auto mb-3 rounded-xl bg-ag-elevated border border-ag-border flex items-center justify-center text-ag-muted">
          <Server className="w-5 h-5" />
        </div>
        <p className="text-xs text-ag-muted mb-3">还没有添加服务器连接</p>
        <button onClick={onAddServer} className="ag-btn-primary text-xs py-1.5 px-3">
          <Plus className="w-3.5 h-3.5" />添加第一台服务器
        </button>
      </div>
    )
  }

  if (groupedServers.length === 0) {
    return <p className="text-xs text-ag-subtle text-center py-8">未找到匹配的服务器</p>
  }

  return (
    <>
      {groupedServers.map(([group, list]) => {
        const isGroupCollapsed = !!collapsedGroups[group]
        return (
          <div key={group}>
            <button
              onClick={() => setCollapsedGroups((prev) => ({ ...prev, [group]: !prev[group] }))}
              className="w-full flex items-center justify-between px-2 py-1 text-[11px] font-medium uppercase tracking-wider text-ag-subtle hover:text-ag-muted transition"
            >
              <span className="flex items-center gap-1 truncate">
                {isGroupCollapsed ? <ChevronRight className="w-3 h-3 shrink-0" /> : <ChevronDown className="w-3 h-3 shrink-0" />}
                <span className="truncate">{group}</span>
              </span>
              <span className="text-[10px] bg-ag-elevated px-1.5 rounded shrink-0">{list.length}</span>
            </button>
            {!isGroupCollapsed && (
              <div className="mt-1 space-y-1">
                {list.map((srv) => (
                  <ServerListItem
                    key={srv.id}
                    server={srv}
                    isLive={connectedServerIds.has(srv.id)}
                    onConnect={onConnectServer}
                    onEdit={onEditServer}
                    onDelete={onDeleteServer}
                  />
                ))}
              </div>
            )}
          </div>
        )
      })}
    </>
  )
}

import React from 'react'
import {
  Bookmark,
  History,
  KeyRound,
  PanelLeftClose,
  PanelLeftOpen,
  RefreshCw,
  Server,
  Settings,
  Terminal,
} from 'lucide-react'
import type { SidebarTab } from './Sidebar'

interface Props {
  activeNav: SidebarTab
  collapsed: boolean
  onSelectNav: (tab: SidebarTab) => void
  onToggleCollapse: () => void
  onOpenSync: () => void
  onOpenSettings: () => void
}

export const SidebarRail: React.FC<Props> = ({
  activeNav,
  collapsed,
  onSelectNav,
  onToggleCollapse,
  onOpenSync,
  onOpenSettings,
}) => {
  const navItems: { id: SidebarTab; icon: React.ReactNode; label: string }[] = [
    { id: 'servers', icon: <Server className="w-[18px] h-[18px]" />, label: '服务器列表' },
    { id: 'keys', icon: <KeyRound className="w-[18px] h-[18px]" />, label: 'SSH 密钥库' },
    { id: 'shortcuts', icon: <Bookmark className="w-[18px] h-[18px]" />, label: '快捷命令' },
    { id: 'history', icon: <History className="w-[18px] h-[18px]" />, label: '命令历史' },
  ]

  return (
    <div className="w-12 shrink-0 bg-[#0d1017] border-r border-ag-border flex flex-col items-center py-3 justify-between">
      <div className="flex flex-col items-center gap-1.5 w-full">
        <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-indigo-500/25 to-violet-500/25 border border-indigo-400/30 flex items-center justify-center mb-2 shadow-glow">
          <Terminal className="w-4 h-4 text-indigo-300" />
        </div>
        <button
          onClick={onToggleCollapse}
          title={collapsed ? '展开侧边栏' : '折叠侧边栏'}
          className="w-9 h-9 rounded-lg flex items-center justify-center text-ag-muted hover:text-indigo-300 hover:bg-ag-hover/70 transition mb-1"
        >
          {collapsed ? <PanelLeftOpen className="w-[17px] h-[17px]" /> : <PanelLeftClose className="w-[17px] h-[17px]" />}
        </button>
        {navItems.map((item) => {
          const active = activeNav === item.id
          return (
            <button
              key={item.id}
              onClick={() => onSelectNav(item.id)}
              title={item.label}
              className={`relative w-9 h-9 rounded-lg flex items-center justify-center transition ${
                active ? 'bg-indigo-500/18 text-indigo-300' : 'text-ag-muted hover:text-ag-text hover:bg-ag-hover/70'
              }`}
            >
              {active && <span className="absolute -left-1.5 top-2 bottom-2 w-[3px] rounded-r-full bg-indigo-400" />}
              {item.icon}
            </button>
          )
        })}
      </div>

      <div className="flex flex-col items-center gap-2">
        <button
          onClick={onOpenSync}
          title="多设备零知识同步"
          className="w-9 h-9 rounded-lg flex items-center justify-center text-cyan-300/85 hover:text-cyan-200 hover:bg-cyan-500/15 transition"
        >
          <RefreshCw className="w-[17px] h-[17px]" />
        </button>
        <button
          onClick={onOpenSettings}
          title="设置"
          className="w-9 h-9 rounded-lg flex items-center justify-center text-ag-muted hover:text-ag-text hover:bg-ag-hover/70 transition"
        >
          <Settings className="w-[18px] h-[18px]" />
        </button>
      </div>
    </div>
  )
}

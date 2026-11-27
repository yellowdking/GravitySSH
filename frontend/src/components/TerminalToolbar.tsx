import React from 'react'
import { Bookmark, FolderTree, History, RefreshCw, Search, Sparkles, Waypoints } from 'lucide-react'
import type { SessionInfo } from '../types'

interface Props {
  session: SessionInfo
  autocompleteEnabled: boolean
  showSearch: boolean
  showSftp: boolean
  rightDrawer: 'none' | 'history' | 'shortcuts'
  onReconnect: (serverId: string) => void
  onToggleAutocomplete: () => void
  onToggleSearch: () => void
  onToggleSftp: () => void
  onToggleRightDrawer: (mode: 'history' | 'shortcuts') => void
}

export const TerminalToolbar: React.FC<Props> = React.memo(({
  session,
  autocompleteEnabled,
  showSearch,
  showSftp,
  rightDrawer,
  onReconnect,
  onToggleAutocomplete,
  onToggleSearch,
  onToggleSftp,
  onToggleRightDrawer,
}) => {
  const statusColor =
    session.status === 'connected'
      ? 'bg-emerald-400'
      : session.status === 'connecting'
      ? 'bg-amber-400 animate-pulse'
      : 'bg-rose-400'

  return (
    <div className="h-9 px-3 border-b border-ag-border bg-[#0f1219] flex items-center justify-between gap-2 text-xs shrink-0">
      <div className="flex items-center gap-2 min-w-0">
        <span className={`w-2 h-2 rounded-full shrink-0 ${statusColor}`} />
        <span className="font-mono text-ag-text truncate">
          {session.username}@{session.host}:{session.port}
        </span>
        {session.viaSummary && (
          <span className="hidden md:inline-flex items-center gap-1 text-[11px] px-2 py-0.5 rounded bg-amber-500/[0.12] text-amber-300 border border-amber-500/20 truncate max-w-[260px]">
            <Waypoints className="w-3 h-3 shrink-0" />
            {session.viaSummary}
          </span>
        )}
      </div>

      <div className="flex items-center gap-1 shrink-0">
        {(session.status === 'disconnected' || session.status === 'error') && (
          <button
            onClick={() => onReconnect(session.serverId)}
            className="inline-flex items-center gap-1 px-2 py-1 rounded-md bg-indigo-500/20 text-indigo-300 hover:bg-indigo-500/30 border border-indigo-500/30 transition"
          >
            <RefreshCw className="w-3 h-3" />
            重新连接
          </button>
        )}

        <button
          onClick={onToggleAutocomplete}
          title="点击快速开关智能命令补齐（也可以在设置中详细配置）"
          className={`inline-flex items-center gap-1 px-2 py-1 rounded-md border transition ${
            autocompleteEnabled
              ? 'bg-indigo-500/15 border-indigo-500/35 text-indigo-300'
              : 'bg-transparent border-ag-border text-ag-subtle hover:text-ag-muted'
          }`}
        >
          <Sparkles className="w-3 h-3" />
          补齐{autocompleteEnabled ? '开' : '关'}
        </button>

        <button
          onClick={onToggleSearch}
          title="搜索终端内容 (Ctrl+F)"
          className={`p-1.5 rounded-md transition ${
            showSearch ? 'bg-ag-hover text-ag-text' : 'text-ag-muted hover:text-ag-text hover:bg-ag-hover/70'
          }`}
        >
          <Search className="w-3.5 h-3.5" />
        </button>

        <button
          onClick={onToggleSftp}
          title="展开/收起 SFTP 文件管理面板"
          className={`inline-flex items-center gap-1 px-2 py-1 rounded-md border transition ${
            showSftp
              ? 'bg-sky-500/15 border-sky-500/35 text-sky-300'
              : 'border-transparent text-ag-muted hover:text-ag-text hover:bg-ag-hover/70'
          }`}
        >
          <FolderTree className="w-3.5 h-3.5" />
          SFTP
        </button>

        <button
          onClick={() => onToggleRightDrawer('shortcuts')}
          title="快捷命令面板"
          className={`inline-flex items-center gap-1 px-2 py-1 rounded-md border transition ${
            rightDrawer === 'shortcuts'
              ? 'bg-violet-500/15 border-violet-500/35 text-violet-300'
              : 'border-transparent text-ag-muted hover:text-ag-text hover:bg-ag-hover/70'
          }`}
        >
          <Bookmark className="w-3.5 h-3.5" />
          快捷
        </button>

        <button
          onClick={() => onToggleRightDrawer('history')}
          title="命令记录面板（可勾选保存为快捷方式）"
          className={`inline-flex items-center gap-1 px-2 py-1 rounded-md border transition ${
            rightDrawer === 'history'
              ? 'bg-emerald-500/15 border-emerald-500/35 text-emerald-300'
              : 'border-transparent text-ag-muted hover:text-ag-text hover:bg-ag-hover/70'
          }`}
        >
          <History className="w-3.5 h-3.5" />
          记录
        </button>
      </div>
    </div>
  )
})

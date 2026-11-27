import React from 'react'
import { ArrowUpCircle, Terminal, X } from 'lucide-react'
import type { SessionInfo, UpdateCheckResult } from '../types'
import { api } from '../wailsApi'

interface Props {
  sessions: SessionInfo[]
  activeSessionId: string | null
  isServersView: boolean
  updateInfo: UpdateCheckResult | null
  onSelectSession: (sessionId: string) => void
  onCloseSession: (sessionId: string) => void
}

export const SessionTabsBar: React.FC<Props> = ({
  sessions,
  activeSessionId,
  isServersView,
  updateInfo,
  onSelectSession,
  onCloseSession,
}) => (
  <div className="h-10 bg-[#0d1017] border-b border-ag-border flex items-center justify-between px-2 gap-2 shrink-0 select-none">
    <div className="flex items-center gap-1 overflow-x-auto min-w-0 flex-1">
      {sessions.map((sess) => {
        const isSelected = isServersView && activeSessionId === sess.sessionId
        const dotCls =
          sess.status === 'connected'
            ? 'bg-emerald-400'
            : sess.status === 'connecting'
            ? 'bg-amber-400 animate-pulse'
            : 'bg-rose-400'
        return (
          <div
            key={sess.sessionId}
            onClick={() => onSelectSession(sess.sessionId)}
            className={`group flex items-center gap-2 px-3 py-1.5 rounded-lg text-xs cursor-pointer transition border shrink-0 max-w-[220px] ${
              isSelected
                ? 'bg-ag-panel border-ag-borderStrong text-ag-text shadow-sm'
                : 'bg-transparent border-transparent text-ag-muted hover:text-ag-text hover:bg-ag-elevated/60'
            }`}
          >
            <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${dotCls}`} />
            <span className="truncate font-medium">{sess.serverName}</span>
            <button
              type="button"
              aria-label={`关闭会话 ${sess.serverName}`}
              onClick={(e) => {
                e.stopPropagation()
                onCloseSession(sess.sessionId)
              }}
              className="p-0.5 rounded hover:bg-ag-hover text-ag-subtle hover:text-ag-text opacity-60 group-hover:opacity-100"
            >
              <X className="w-3 h-3" />
            </button>
          </div>
        )
      })}
      {sessions.length === 0 && (
        <div className="px-2 text-xs text-ag-subtle flex items-center gap-1.5">
          <Terminal className="w-3.5 h-3.5 text-indigo-400" />
          点击左侧服务器建立连接，或按下方按钮新建主机
        </div>
      )}
    </div>

    {updateInfo?.hasUpdate && (
      <button
        type="button"
        onClick={() => void api.openExternalURL(updateInfo.releaseUrl || updateInfo.downloadUrl)}
        title={`发现新版本 v${updateInfo.latestVersion}，点击前往 GitHub 下载`}
        className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg bg-emerald-500/15 hover:bg-emerald-500/25 border border-emerald-500/35 text-emerald-300 text-[11px] font-medium shrink-0 transition"
      >
        <ArrowUpCircle className="w-3.5 h-3.5" />
        <span>新版本 v{updateInfo.latestVersion}</span>
      </button>
    )}
  </div>
)

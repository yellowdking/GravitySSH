import React from 'react'
import { Edit3, Globe, KeyRound, Trash2, Waypoints } from 'lucide-react'
import type { ServerView } from '../types'

interface Props {
  server: ServerView
  isLive: boolean
  onConnect: (srv: ServerView) => void
  onEdit: (srv: ServerView) => void
  onDelete: (srv: ServerView) => void
}

export const ServerListItem: React.FC<Props> = ({ server, isLive, onConnect, onEdit, onDelete }) => {
  const hasProxy = server.proxy?.type && server.proxy.type !== 'none'
  const hasJump = server.jumpHost?.mode && server.jumpHost.mode !== 'none'
  const hasKey = !!server.sshKeyId

  return (
    <div
      onDoubleClick={() => onConnect(server)}
      className="group relative rounded-lg px-2.5 py-2 bg-transparent hover:bg-ag-elevated/90 border border-transparent hover:border-ag-border transition cursor-pointer"
    >
      <div className="flex items-start justify-between gap-1.5">
        <div className="flex items-start gap-2 min-w-0 flex-1" onClick={() => onConnect(server)}>
          <span
            className={`mt-1.5 w-2 h-2 rounded-full shrink-0 ${
              isLive ? 'bg-emerald-400 shadow-[0_0_8px_rgba(52,211,153,0.8)]' : 'bg-ag-subtle/60'
            }`}
            style={!isLive && server.color ? { backgroundColor: server.color } : undefined}
          />
          <div className="min-w-0 flex-1">
            <div className="text-[13px] font-medium text-ag-text truncate leading-snug">{server.name}</div>
            <div className="text-[11px] font-mono text-ag-muted truncate mt-0.5">
              {server.username}@{server.host}:{server.port}
            </div>
            {(hasProxy || hasJump || hasKey) && (
              <div className="flex flex-wrap items-center gap-1 mt-1.5">
                {hasKey && (
                  <span className="inline-flex items-center gap-0.5 text-[10px] px-1.5 py-0.2 rounded bg-indigo-500/15 text-indigo-300 border border-indigo-500/20">
                    <KeyRound className="w-2.5 h-2.5" />
                    Key
                  </span>
                )}
                {hasProxy && (
                  <span className="inline-flex items-center gap-0.5 text-[10px] px-1.5 py-0.2 rounded bg-sky-500/15 text-sky-300 border border-sky-500/20">
                    <Globe className="w-2.5 h-2.5" />
                    {server.proxy.type.toUpperCase()}
                  </span>
                )}
                {hasJump && (
                  <span className="inline-flex items-center gap-0.5 text-[10px] px-1.5 py-0.2 rounded bg-amber-500/15 text-amber-300 border border-amber-500/20">
                    <Waypoints className="w-2.5 h-2.5" />
                    跳板
                  </span>
                )}
              </div>
            )}
          </div>
        </div>

        <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition shrink-0">
          <button
            onClick={(e) => {
              e.stopPropagation()
              onEdit(server)
            }}
            title="编辑服务器"
            className="p-1 rounded hover:bg-ag-hover text-ag-muted hover:text-ag-text"
          >
            <Edit3 className="w-3.5 h-3.5" />
          </button>
          <button
            onClick={(e) => {
              e.stopPropagation()
              onDelete(server)
            }}
            title="删除服务器"
            className="p-1 rounded hover:bg-rose-500/20 text-ag-muted hover:text-rose-400"
          >
            <Trash2 className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>
    </div>
  )
}

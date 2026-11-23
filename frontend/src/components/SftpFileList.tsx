import React from 'react'
import { Download, Edit2, File, FileArchive, FileCode2, Folder, Trash2 } from 'lucide-react'
import type { SFTPFileEntry } from '../types'
import { formatBytes } from '../utils/autocomplete'

interface Props {
  entries: SFTPFileEntry[]
  onOpenDir: (path: string) => void
  onDownload: (entry: SFTPFileEntry) => void
  onRename: (entry: SFTPFileEntry) => void
  onDelete: (entry: SFTPFileEntry) => void
}

function fileIcon(entry: SFTPFileEntry) {
  if (entry.isDir) return <Folder className="w-4 h-4 text-sky-400 shrink-0" />
  const lower = entry.name.toLowerCase()
  if (/\.(zip|tar|gz|tgz|bz2|xz|7z|rar)$/.test(lower)) return <FileArchive className="w-4 h-4 text-amber-400 shrink-0" />
  if (/\.(sh|py|go|ts|tsx|js|json|yaml|yml|toml|conf|ini|env|md|sql|dockerfile)$/i.test(lower)) {
    return <FileCode2 className="w-4 h-4 text-indigo-300 shrink-0" />
  }
  return <File className="w-4 h-4 text-ag-muted shrink-0" />
}

export const SftpFileList: React.FC<Props> = ({ entries, onOpenDir, onDownload, onRename, onDelete }) => (
  <div className="divide-y divide-ag-border/40">
    {entries.map((entry) => (
      <div
        key={entry.path}
        onDoubleClick={() => (entry.isDir ? onOpenDir(entry.path) : onDownload(entry))}
        className="group px-3 py-1.5 hover:bg-ag-elevated/85 flex items-center justify-between gap-2 text-xs transition cursor-pointer"
      >
        <div
          className="flex items-center gap-2 min-w-0 flex-1"
          onClick={() => {
            if (entry.isDir) onOpenDir(entry.path)
          }}
        >
          {fileIcon(entry)}
          <div className="min-w-0 flex-1">
            <div className="text-ag-text truncate font-medium">{entry.name}</div>
            <div className="text-[10px] font-mono text-ag-subtle flex items-center gap-2">
              <span>{entry.isDir ? '目录' : formatBytes(entry.size)}</span>
              <span>{entry.mode}</span>
            </div>
          </div>
        </div>

        <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition shrink-0">
          {!entry.isDir && (
            <button
              onClick={(e) => {
                e.stopPropagation()
                onDownload(entry)
              }}
              title="下载到本地"
              className="p-1 rounded hover:bg-sky-500/20 text-ag-muted hover:text-sky-300"
            >
              <Download className="w-3.5 h-3.5" />
            </button>
          )}
          <button
            onClick={(e) => {
              e.stopPropagation()
              onRename(entry)
            }}
            title="重命名"
            className="p-1 rounded hover:bg-ag-hover text-ag-muted hover:text-ag-text"
          >
            <Edit2 className="w-3.5 h-3.5" />
          </button>
          <button
            onClick={(e) => {
              e.stopPropagation()
              onDelete(entry)
            }}
            title="删除"
            className="p-1 rounded hover:bg-rose-500/20 text-ag-muted hover:text-rose-400"
          >
            <Trash2 className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>
    ))}
  </div>
)

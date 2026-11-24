import React from 'react'
import { Download, Edit2, File, FileArchive, FileCode2, Folder, Loader2, ShieldCheck, Trash2 } from 'lucide-react'
import type { SFTPFileEntry } from '../types'
import { formatBytes } from '../utils/autocomplete'

interface Props {
  entries: SFTPFileEntry[]
  busyPath: string | null
  onOpenDir: (path: string) => void
  onDownload: (entry: SFTPFileEntry) => void
  onExtract: (entry: SFTPFileEntry) => void
  onAlignPerms: (entry: SFTPFileEntry) => void
  onRename: (entry: SFTPFileEntry) => void
  onDelete: (entry: SFTPFileEntry) => void
}

export function isArchiveFile(name: string): boolean {
  return /\.(zip|tar\.gz|tgz|tar\.bz2|tar\.xz|tar|7z|gz)$/i.test(name)
}

function fileIcon(entry: SFTPFileEntry) {
  if (entry.isDir) return <Folder className="w-4 h-4 text-sky-400 shrink-0" />
  const lower = entry.name.toLowerCase()
  if (isArchiveFile(lower) || lower.endsWith('.rar')) return <FileArchive className="w-4 h-4 text-amber-400 shrink-0" />
  if (/\.(sh|py|go|ts|tsx|js|json|yaml|yml|toml|conf|ini|env|md|sql|dockerfile)$/i.test(lower)) {
    return <FileCode2 className="w-4 h-4 text-indigo-300 shrink-0" />
  }
  return <File className="w-4 h-4 text-ag-muted shrink-0" />
}

export const SftpFileList: React.FC<Props> = ({
  entries,
  busyPath,
  onOpenDir,
  onDownload,
  onExtract,
  onAlignPerms,
  onRename,
  onDelete,
}) => (
  <div className="divide-y divide-ag-border/40">
    {entries.map((entry) => {
      const archive = !entry.isDir && isArchiveFile(entry.name)
      const isBusy = busyPath === entry.path

      return (
        <div
          key={entry.path}
          onDoubleClick={() => {
            if (isBusy) return
            if (entry.isDir) onOpenDir(entry.path)
            else if (archive) onExtract(entry)
            else onDownload(entry)
          }}
          title={archive ? '双击智能解压（悬停可下载）' : entry.isDir ? '单击或双击进入目录' : '双击下载到本地'}
          className="group px-2.5 py-1.5 hover:bg-ag-elevated/85 flex items-center justify-between gap-2 text-xs transition cursor-pointer"
        >
          <div
            className="flex items-center gap-2 min-w-0 flex-1"
            onClick={() => {
              if (entry.isDir) onOpenDir(entry.path)
            }}
          >
            {isBusy ? <Loader2 className="w-4 h-4 text-amber-400 animate-spin shrink-0" /> : fileIcon(entry)}
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-1.5 min-w-0">
                <span className="text-ag-text truncate font-medium">{entry.name}</span>
                {archive && (
                  <span className="text-[9px] px-1 py-0.2 rounded bg-amber-500/15 text-amber-300 border border-amber-500/25 shrink-0">
                    {isBusy ? '解压中' : '双击解压'}
                  </span>
                )}
              </div>
              <div className="text-[10px] font-mono text-ag-subtle truncate">{entry.mode}</div>
            </div>
          </div>

          <div className="flex items-center gap-1.5 shrink-0">
            <span className="font-mono text-[11px] tabular-nums text-ag-muted bg-[#0d1017]/80 border border-ag-border/60 rounded px-1.5 py-0.5 min-w-[58px] text-right">
              {entry.isDir ? '目录' : formatBytes(entry.size)}
            </span>

            <div className="hidden group-hover:flex items-center gap-0.5 shrink-0">
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  onAlignPerms(entry)
                }}
                disabled={isBusy}
                title="同步同目录权限"
                className="p-1 rounded hover:bg-emerald-500/20 text-ag-muted hover:text-emerald-300 disabled:opacity-40"
              >
                <ShieldCheck className="w-3.5 h-3.5" />
              </button>
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
        </div>
      )
    })}
  </div>
)

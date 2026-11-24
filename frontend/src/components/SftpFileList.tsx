import React from 'react'
import { Archive, Download, Edit2, File, FileArchive, FileCode2, FileEdit, Folder, FolderDown, Loader2, ShieldCheck, Trash2 } from 'lucide-react'
import type { SFTPFileEntry } from '../types'
import { formatBytes } from '../utils/autocomplete'

interface Props {
  entries: SFTPFileEntry[]
  busyPath: string | null
  onOpenDir: (path: string) => void
  onEditFile: (entry: SFTPFileEntry) => void
  onDownload: (entry: SFTPFileEntry) => void
  onDownloadFolder: (entry: SFTPFileEntry) => void
  onCompress: (entry: SFTPFileEntry) => void
  onExtract: (entry: SFTPFileEntry) => void
  onAlignPerms: (entry: SFTPFileEntry) => void
  onRename: (entry: SFTPFileEntry) => void
  onDelete: (entry: SFTPFileEntry) => void
}

export function isArchiveFile(name: string): boolean {
  return /\.(zip|tar\.gz|tgz|tar\.bz2|tar\.xz|tar|7z|gz)$/i.test(name)
}

export function isEditableTextFile(name: string): boolean {
  const lower = name.toLowerCase()
  if (/^(dockerfile|makefile|caddyfile|vagrantfile|readme|license|\.env(\..*)?|\.gitignore|\.bashrc|\.zshrc|\.profile)$/.test(lower)) {
    return true
  }
  return /\.(txt|md|log|conf|cfg|ini|env|json|yaml|yml|toml|xml|sh|bash|zsh|py|go|rs|ts|tsx|js|jsx|css|scss|html|sql|service|timer|socket|nginx|php|rb|java|c|h|cpp|properties|csv)$/i.test(lower)
}

function fileIcon(entry: SFTPFileEntry) {
  if (entry.isDir) return <Folder className="w-4 h-4 text-sky-400 shrink-0" />
  const lower = entry.name.toLowerCase()
  if (isArchiveFile(lower) || lower.endsWith('.rar')) return <FileArchive className="w-4 h-4 text-amber-400 shrink-0" />
  if (isEditableTextFile(lower)) return <FileCode2 className="w-4 h-4 text-indigo-300 shrink-0" />
  return <File className="w-4 h-4 text-ag-muted shrink-0" />
}

const actBtn = 'p-1 rounded text-ag-muted disabled:opacity-40'

export const SftpFileList: React.FC<Props> = ({
  entries, busyPath, onOpenDir, onEditFile, onDownload, onDownloadFolder, onCompress, onExtract, onAlignPerms, onRename, onDelete,
}) => (
  <div className="divide-y divide-ag-border/40">
    {entries.map((entry) => {
      const archive = !entry.isDir && isArchiveFile(entry.name)
      const editable = !entry.isDir && !archive && isEditableTextFile(entry.name)
      const isBusy = busyPath === entry.path

      return (
        <div
          key={entry.path}
          onDoubleClick={() => {
            if (isBusy) return
            if (entry.isDir) onOpenDir(entry.path)
            else if (archive) onExtract(entry)
            else if (editable) onEditFile(entry)
            else onDownload(entry)
          }}
          title={archive ? '双击智能解压' : editable ? '双击在线编辑（Ctrl+S 回传）' : entry.isDir ? '单击进入目录（悬停可打包下载/压缩）' : '双击下载到本地'}
          className="group px-2.5 py-1.5 hover:bg-ag-elevated/85 flex items-center justify-between gap-2 text-xs transition cursor-pointer"
        >
          <div className="flex items-center gap-2 min-w-0 flex-1" onClick={() => { if (!isBusy && entry.isDir) onOpenDir(entry.path) }}>
            {isBusy ? <Loader2 className="w-4 h-4 text-amber-400 animate-spin shrink-0" /> : fileIcon(entry)}
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-1.5 min-w-0">
                <span className="text-ag-text truncate font-medium">{entry.name}</span>
                {archive && (
                  <span className="text-[9px] px-1 py-0.2 rounded bg-amber-500/15 text-amber-300 border border-amber-500/25 shrink-0">
                    {isBusy ? '处理中' : '双击解压'}
                  </span>
                )}
              </div>
              <div className="text-[10px] font-mono text-ag-subtle truncate">{entry.mode}</div>
            </div>
          </div>

          <div className="flex items-center gap-1.5 shrink-0" onClick={(e) => e.stopPropagation()} onDoubleClick={(e) => e.stopPropagation()}>
            <span className="font-mono text-[11px] tabular-nums text-ag-muted bg-[#0d1017]/80 border border-ag-border/60 rounded px-1.5 py-0.5 min-w-[58px] text-right group-hover:hidden">
              {entry.isDir ? '目录' : formatBytes(entry.size)}
            </span>

            <div className="hidden group-hover:flex items-center gap-0.5 shrink-0">
              {!entry.isDir && (
                <button onClick={() => onEditFile(entry)} disabled={isBusy} title="在线编辑" className={`${actBtn} hover:bg-indigo-500/20 hover:text-indigo-300`}>
                  <FileEdit className="w-3.5 h-3.5" />
                </button>
              )}
              {entry.isDir ? (
                <button onClick={() => onDownloadFolder(entry)} disabled={isBusy} title="流式打包下载整个文件夹" className={`${actBtn} hover:bg-sky-500/20 hover:text-sky-300`}>
                  <FolderDown className="w-3.5 h-3.5" />
                </button>
              ) : (
                <button onClick={() => onDownload(entry)} disabled={isBusy} title="下载到本地" className={`${actBtn} hover:bg-sky-500/20 hover:text-sky-300`}>
                  <Download className="w-3.5 h-3.5" />
                </button>
              )}
              <button onClick={() => onCompress(entry)} disabled={isBusy} title="远程压缩 (.tar.gz / .zip)" className={`${actBtn} hover:bg-amber-500/20 hover:text-amber-300`}>
                <Archive className="w-3.5 h-3.5" />
              </button>
              <button onClick={() => onAlignPerms(entry)} disabled={isBusy} title="同步同目录权限" className={`${actBtn} hover:bg-emerald-500/20 hover:text-emerald-300`}>
                <ShieldCheck className="w-3.5 h-3.5" />
              </button>
              <button onClick={() => onRename(entry)} disabled={isBusy} title="重命名" className={`${actBtn} hover:bg-ag-hover hover:text-ag-text`}>
                <Edit2 className="w-3.5 h-3.5" />
              </button>
              <button onClick={() => onDelete(entry)} disabled={isBusy} title="删除" className={`${actBtn} hover:bg-rose-500/20 hover:text-rose-400`}>
                <Trash2 className="w-3.5 h-3.5" />
              </button>
            </div>
          </div>
        </div>
      )
    })}
  </div>
)

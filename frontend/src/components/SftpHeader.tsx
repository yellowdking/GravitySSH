import React from 'react'
import { ArrowUp, Eye, EyeOff, Folder, FolderPlus, Home, RefreshCw, Terminal, Upload } from 'lucide-react'

interface Props {
  sftpReady: boolean
  loading: boolean
  currentPath: string
  pathInput: string
  showHidden: boolean
  onPathInputChange: (v: string) => void
  onPathInputFocusChange?: (focused: boolean) => void
  onSubmitPath: () => void
  onGoHome: () => void
  onGoUp: () => void
  onUpload: () => void
  onMkdir: () => void
  onToggleHidden: () => void
  onRefresh: () => void
  onCdInTerminal: () => void
}

export const SftpHeader: React.FC<Props> = ({
  sftpReady,
  loading,
  currentPath,
  pathInput,
  showHidden,
  onPathInputChange,
  onPathInputFocusChange,
  onSubmitPath,
  onGoHome,
  onGoUp,
  onUpload,
  onMkdir,
  onToggleHidden,
  onRefresh,
  onCdInTerminal,
}) => (
  <>
    <div className="h-9 px-2.5 border-b border-ag-border bg-[#0f1219] flex items-center justify-between gap-2 min-w-0">
      <div className="flex items-center gap-1.5 text-xs font-medium text-sky-300 min-w-0 truncate">
        <Folder className="w-3.5 h-3.5 shrink-0" />
        <span className="truncate">SFTP 文件</span>
        {sftpReady && (
          <span className="text-[10px] px-1.5 py-0.5 rounded bg-emerald-500/15 text-emerald-300 border border-emerald-500/20 shrink-0">
            就绪
          </span>
        )}
      </div>
      <div className="flex items-center gap-0.5 shrink-0">
        <button onClick={onUpload} disabled={!sftpReady} className="inline-flex items-center gap-1 px-2 py-1 rounded-md bg-sky-500/15 hover:bg-sky-500/25 text-sky-300 border border-sky-500/30 text-[11px] font-medium transition disabled:opacity-40">
          <Upload className="w-3 h-3" />上传
        </button>
        <button onClick={onMkdir} disabled={!sftpReady} title="新建文件夹" className="p-1.5 rounded-md hover:bg-ag-hover text-ag-muted hover:text-ag-text disabled:opacity-40"><FolderPlus className="w-3.5 h-3.5" /></button>
        <button onClick={onToggleHidden} title={showHidden ? '隐藏点文件' : '显示隐藏文件'} className="p-1.5 rounded-md hover:bg-ag-hover text-ag-muted hover:text-ag-text">
          {showHidden ? <Eye className="w-3.5 h-3.5 text-indigo-300" /> : <EyeOff className="w-3.5 h-3.5" />}
        </button>
        <button onClick={onRefresh} disabled={!sftpReady} title="刷新" className="p-1.5 rounded-md hover:bg-ag-hover text-ag-muted hover:text-ag-text disabled:opacity-40">
          <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
        </button>
      </div>
    </div>

    <div className="p-2 border-b border-ag-border flex items-center gap-1 bg-[#0d1017] min-w-0">
      <button onClick={onGoHome} disabled={!sftpReady} title="主目录" className="p-1.5 rounded hover:bg-ag-hover text-ag-muted hover:text-ag-text shrink-0"><Home className="w-3.5 h-3.5" /></button>
      <button onClick={onGoUp} disabled={!sftpReady || currentPath === '/'} title="上级目录" className="p-1.5 rounded hover:bg-ag-hover text-ag-muted hover:text-ag-text disabled:opacity-35 shrink-0"><ArrowUp className="w-3.5 h-3.5" /></button>
      <form className="flex-1 min-w-0" onSubmit={(e) => { e.preventDefault(); onSubmitPath() }}>
        <input
          value={pathInput}
          onChange={(e) => onPathInputChange(e.target.value)}
          onFocus={() => onPathInputFocusChange?.(true)}
          onBlur={() => onPathInputFocusChange?.(false)}
          className="w-full rounded-md bg-ag-panel border border-ag-border px-2 py-1 text-xs font-mono text-ag-text focus:outline-none focus:border-sky-500/60"
        />
      </form>
      <button onClick={onCdInTerminal} disabled={!sftpReady} title="在终端 cd 到此目录" className="p-1.5 rounded hover:bg-ag-hover text-ag-muted hover:text-indigo-300 shrink-0"><Terminal className="w-3.5 h-3.5" /></button>
    </div>
  </>
)

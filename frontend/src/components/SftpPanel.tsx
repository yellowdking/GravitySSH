import React from 'react'
import { Loader2 } from 'lucide-react'
import type { SessionInfo } from '../types'
import { useSftpState } from '../hooks/useSftpState'
import { SftpHeader } from './SftpHeader'
import { SftpFileList } from './SftpFileList'
import { SftpTransferBar } from './SftpTransferBar'
import { SftpEditorModal } from './SftpEditorModal'

interface SftpPanelProps {
  session: SessionInfo
  active: boolean
  width: number
  followTerminal: boolean
  onCdInTerminal: (remotePath: string) => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const SftpPanel: React.FC<SftpPanelProps> = ({ session, active, width, followTerminal, onCdInTerminal, onToast }) => {
  const sftp = useSftpState(session, active, followTerminal, onToast)

  return (
    <div style={{ width }} className="shrink-0 bg-ag-sidebar border-l border-ag-border flex flex-col h-full min-w-0 select-none">
      <SftpHeader
        sftpReady={session.sftpReady}
        loading={sftp.loading}
        currentPath={sftp.currentPath}
        pathInput={sftp.pathInput}
        showHidden={sftp.showHidden}
        onPathInputChange={sftp.setPathInput}
        onSubmitPath={() => void sftp.loadDir(sftp.pathInput)}
        onGoHome={() => void sftp.loadDir(session.homeDir || '~')}
        onGoUp={() => void sftp.loadDir(sftp.parentPath)}
        onUpload={() => void sftp.handleUpload()}
        onMkdir={() => void sftp.handleMkdir()}
        onToggleHidden={() => sftp.setShowHidden((v) => !v)}
        onRefresh={() => void sftp.loadDir(sftp.currentPath)}
        onCdInTerminal={() => onCdInTerminal(sftp.currentPath)}
      />

      <div className="flex-1 overflow-y-auto">
        {!session.sftpReady ? (
          <div className="h-full flex flex-col items-center justify-center p-6 text-center text-ag-muted text-xs gap-2">
            {session.status === 'connecting' || session.status === 'reconnecting' ? (
              <><Loader2 className="w-5 h-5 animate-spin text-indigo-400" /><span>正在建立 SSH 与自动 SFTP 通道...</span></>
            ) : (
              <span>等待 SSH 连接就绪后自动加载远程文件</span>
            )}
          </div>
        ) : sftp.error ? (
          <div className="p-4 text-xs text-rose-300 bg-rose-500/10 m-2 rounded-xl border border-rose-500/25">{sftp.error}</div>
        ) : sftp.visibleEntries.length === 0 ? (
          <div className="py-12 text-center text-xs text-ag-subtle">当前目录为空（可将本地文件拖入窗口直接上传）</div>
        ) : (
          <SftpFileList
            entries={sftp.visibleEntries}
            busyPath={sftp.busyPath}
            onOpenDir={(p) => void sftp.loadDir(p)}
            onEditFile={(e) => void sftp.handleEditFile(e)}
            onDownload={(e) => void sftp.handleDownload(e)}
            onDownloadFolder={(e) => void sftp.handleDownloadFolder(e)}
            onCompress={(e) => void sftp.handleCompress(e)}
            onExtract={(e) => void sftp.handleExtract(e)}
            onAlignPerms={(e) => void sftp.handleAlignPerms(e)}
            onRename={(e) => void sftp.handleRename(e)}
            onDelete={(e) => void sftp.handleDelete(e)}
          />
        )}
      </div>

      <SftpTransferBar transfers={sftp.transfers} />

      <div className="px-3 py-1.5 border-t border-ag-border/70 bg-[#0d1017] text-[10px] text-ag-subtle flex items-center justify-between gap-2">
        <span className="truncate">共 {sftp.visibleEntries.length} 项 · 双击编辑/解压/下载</span>
        <span className="shrink-0">拖入直接上传</span>
      </div>

      <SftpEditorModal
        open={!!sftp.editingFile}
        remotePath={sftp.editingFile?.path || ''}
        initialContent={sftp.editingFile?.content || ''}
        saving={sftp.savingFile}
        onSave={sftp.handleSaveFile}
        onClose={() => sftp.setEditingFile(null)}
      />
    </div>
  )
}

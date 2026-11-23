import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { Loader2 } from 'lucide-react'
import type { SessionInfo, SFTPFileEntry, TransferProgress } from '../types'
import { api } from '../wailsApi'
import { SftpHeader } from './SftpHeader'
import { SftpFileList } from './SftpFileList'
import { SftpTransferBar } from './SftpTransferBar'

interface SftpPanelProps {
  session: SessionInfo
  active: boolean
  onCdInTerminal: (remotePath: string) => void
  onToast: (msg: string, type?: 'info' | 'error') => void
}

export const SftpPanel: React.FC<SftpPanelProps> = ({ session, active, onCdInTerminal, onToast }) => {
  const [currentPath, setCurrentPath] = useState(session.homeDir || '/')
  const [pathInput, setPathInput] = useState(session.homeDir || '/')
  const [parentPath, setParentPath] = useState('/')
  const [entries, setEntries] = useState<SFTPFileEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [showHidden, setShowHidden] = useState(false)
  const [error, setError] = useState('')
  const [transfers, setTransfers] = useState<Record<string, TransferProgress>>({})

  const loadDir = useCallback(
    async (targetPath: string) => {
      if (!session.sftpReady) return
      setLoading(true); setError('')
      try {
        const res = await api.sftpList(session.sessionId, targetPath)
        setCurrentPath(res.path); setPathInput(res.path); setParentPath(res.parent); setEntries(res.entries || [])
      } catch (err) { setError(String(err)) } finally { setLoading(false) }
    },
    [session.sessionId, session.sftpReady]
  )

  useEffect(() => { if (session.sftpReady) void loadDir(session.homeDir || '') }, [session.sftpReady, session.homeDir, loadDir])

  useEffect(() => {
    return api.onEvent<TransferProgress>('sftp:progress', (prog) => {
      if (prog.sessionId !== session.sessionId) return
      setTransfers((prev) => ({ ...prev, [prog.transferId]: prog }))
      if (prog.status === 'done') {
        if (prog.direction === 'upload') void loadDir(currentPath)
        setTimeout(() => { setTransfers((p) => { const n = { ...p }; delete n[prog.transferId]; return n }) }, 3500)
      }
    })
  }, [session.sessionId, currentPath, loadDir])

  useEffect(() => {
    if (!active || !session.sftpReady) return
    return api.onFileDrop(async (paths) => {
      try {
        const count = await api.sftpUploadFiles(session.sessionId, paths, currentPath)
        if (count > 0) { onToast(`已通过拖拽上传 ${count} 个文件到 ${currentPath}`); void loadDir(currentPath) }
      } catch (err) { onToast(`拖拽上传失败: ${err}`, 'error') }
    })
  }, [active, session.sftpReady, session.sessionId, currentPath, loadDir, onToast])

  const visibleEntries = useMemo(() => (showHidden ? entries : entries.filter((e) => !e.name.startsWith('.'))), [entries, showHidden])

  const handleUpload = async () => {
    try {
      const count = await api.sftpUploadDialog(session.sessionId, currentPath)
      if (count > 0) { onToast(`已成功上传 ${count} 个文件`); void loadDir(currentPath) }
    } catch (err) { onToast(`上传失败: ${err}`, 'error') }
  }

  const handleDownload = async (entry: SFTPFileEntry) => {
    try { const saved = await api.sftpDownloadDialog(session.sessionId, entry.path); if (saved) onToast(`已下载 ${entry.name} → ${saved}`) }
    catch (err) { onToast(`下载失败: ${err}`, 'error') }
  }

  const handleMkdir = async () => {
    const name = window.prompt('请输入新建文件夹名称：')
    if (!name?.trim()) return
    const full = currentPath === '/' ? `/${name.trim()}` : `${currentPath}/${name.trim()}`
    try { await api.sftpMkdir(session.sessionId, full); onToast(`已创建目录 ${name.trim()}`); void loadDir(currentPath) }
    catch (err) { onToast(`创建目录失败: ${err}`, 'error') }
  }

  const handleRename = async (entry: SFTPFileEntry) => {
    const nextName = window.prompt('重命名为：', entry.name)
    if (!nextName?.trim() || nextName.trim() === entry.name) return
    const base = currentPath === '/' ? '' : currentPath
    try { await api.sftpRename(session.sessionId, entry.path, `${base}/${nextName.trim()}`); onToast(`已重命名为 ${nextName.trim()}`); void loadDir(currentPath) }
    catch (err) { onToast(`重命名失败: ${err}`, 'error') }
  }

  const handleDelete = async (entry: SFTPFileEntry) => {
    const tip = entry.isDir ? `目录 "${entry.name}" 及其所有内容` : `文件 "${entry.name}"`
    if (!window.confirm(`确定要永久删除远程 ${tip} 吗？`)) return
    try { await api.sftpDelete(session.sessionId, entry.path); onToast(`已删除 ${entry.name}`); void loadDir(currentPath) }
    catch (err) { onToast(`删除失败: ${err}`, 'error') }
  }

  return (
    <div className="w-[370px] shrink-0 bg-ag-sidebar border-l border-ag-border flex flex-col h-full select-none">
      <SftpHeader
        sftpReady={session.sftpReady}
        loading={loading}
        currentPath={currentPath}
        pathInput={pathInput}
        showHidden={showHidden}
        onPathInputChange={setPathInput}
        onSubmitPath={() => void loadDir(pathInput)}
        onGoHome={() => void loadDir(session.homeDir || '~')}
        onGoUp={() => void loadDir(parentPath)}
        onUpload={() => void handleUpload()}
        onMkdir={() => void handleMkdir()}
        onToggleHidden={() => setShowHidden((v) => !v)}
        onRefresh={() => void loadDir(currentPath)}
        onCdInTerminal={() => onCdInTerminal(currentPath)}
      />

      <div className="flex-1 overflow-y-auto">
        {!session.sftpReady ? (
          <div className="h-full flex flex-col items-center justify-center p-6 text-center text-ag-muted text-xs gap-2">
            {session.status === 'connecting' ? <><Loader2 className="w-5 h-5 animate-spin text-indigo-400" /><span>正在建立 SSH 与自动 SFTP 通道...</span></> : <span>等待 SSH 连接就绪后自动加载远程文件</span>}
          </div>
        ) : error ? (
          <div className="p-4 text-xs text-rose-300 bg-rose-500/10 m-2 rounded-xl border border-rose-500/25">{error}</div>
        ) : visibleEntries.length === 0 ? (
          <div className="py-12 text-center text-xs text-ag-subtle">当前目录为空（可将本地文件拖入窗口直接上传）</div>
        ) : (
          <SftpFileList entries={visibleEntries} onOpenDir={(p) => void loadDir(p)} onDownload={(e) => void handleDownload(e)} onRename={(e) => void handleRename(e)} onDelete={(e) => void handleDelete(e)} />
        )}
      </div>

      <SftpTransferBar transfers={Object.values(transfers)} />

      <div className="px-3 py-1.5 border-t border-ag-border/70 bg-[#0d1017] text-[10px] text-ag-subtle flex items-center justify-between">
        <span>共 {visibleEntries.length} 项 · 双击文件下载</span>
        <span>支持拖入文件直接上传</span>
      </div>
    </div>
  )
}

import { useCallback, useEffect, useMemo, useState } from 'react'
import type { SessionInfo, SFTPFileEntry, TransferProgress } from '../types'
import { api } from '../wailsApi'

export function useSftpState(
  session: SessionInfo,
  active: boolean,
  onToast: (msg: string, type?: 'info' | 'error') => void
) {
  const [currentPath, setCurrentPath] = useState(session.homeDir || '/')
  const [pathInput, setPathInput] = useState(session.homeDir || '/')
  const [parentPath, setParentPath] = useState('/')
  const [entries, setEntries] = useState<SFTPFileEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [busyPath, setBusyPath] = useState<string | null>(null)
  const [showHidden, setShowHidden] = useState(false)
  const [error, setError] = useState('')
  const [transfers, setTransfers] = useState<Record<string, TransferProgress>>({})

  const loadDir = useCallback(
    async (targetPath: string) => {
      if (!session.sftpReady) return
      setLoading(true)
      setError('')
      try {
        const res = await api.sftpList(session.sessionId, targetPath)
        setCurrentPath(res.path)
        setPathInput(res.path)
        setParentPath(res.parent)
        setEntries(res.entries || [])
        void api.setSessionCwd(session.sessionId, res.path)
      } catch (err) {
        setError(String(err))
      } finally {
        setLoading(false)
      }
    },
    [session.sessionId, session.sftpReady]
  )

  useEffect(() => {
    if (session.sftpReady) void loadDir(session.homeDir || '')
  }, [session.sftpReady, session.homeDir, loadDir])

  useEffect(() => {
    return api.onEvent<TransferProgress>('sftp:progress', (prog) => {
      if (prog.sessionId !== session.sessionId) return
      setTransfers((prev) => ({ ...prev, [prog.transferId]: prog }))
      if (prog.status === 'done') {
        if (prog.direction === 'upload') void loadDir(currentPath)
        setTimeout(() => {
          setTransfers((p) => { const n = { ...p }; delete n[prog.transferId]; return n })
        }, 3500)
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

  const visibleEntries = useMemo(
    () => (showHidden ? entries : entries.filter((e) => !e.name.startsWith('.'))),
    [entries, showHidden]
  )

  const handleUpload = async () => {
    try {
      const count = await api.sftpUploadDialog(session.sessionId, currentPath)
      if (count > 0) { onToast(`已成功上传 ${count} 个文件`); void loadDir(currentPath) }
    } catch (err) { onToast(`上传失败: ${err}`, 'error') }
  }

  const handleDownload = async (entry: SFTPFileEntry) => {
    try {
      const saved = await api.sftpDownloadDialog(session.sessionId, entry.path)
      if (saved) onToast(`已下载 ${entry.name} → ${saved}`)
    } catch (err) { onToast(`下载失败: ${err}`, 'error') }
  }

  const handleAlignPerms = async (entry: SFTPFileEntry) => {
    setBusyPath(entry.path)
    try {
      const mode = await api.sftpAlignPermissions(session.sessionId, entry.path)
      onToast(`已同步 ${entry.name} 权限为 ${mode}`)
      void loadDir(currentPath)
    } catch (err) {
      onToast(`同步权限失败: ${err}`, 'error')
    } finally {
      setBusyPath(null)
    }
  }

  const handleExtract = async (entry: SFTPFileEntry) => {
    setBusyPath(entry.path)
    onToast(`正在智能解压 ${entry.name}... 如缺少 unzip 将自动安装`)
    try {
      const msg = await api.sftpExtractArchive(session.sessionId, entry.path)
      onToast(msg || `已解压 ${entry.name}`)
      void loadDir(currentPath)
    } catch (err) {
      onToast(`解压失败: ${err}`, 'error')
    } finally {
      setBusyPath(null)
    }
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

  return {
    currentPath, pathInput, setPathInput, parentPath, visibleEntries, loading, busyPath,
    showHidden, setShowHidden, error, transfers: Object.values(transfers), loadDir,
    handleUpload, handleDownload, handleAlignPerms, handleExtract, handleMkdir, handleRename, handleDelete,
  }
}

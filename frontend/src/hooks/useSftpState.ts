import { useCallback, useEffect, useMemo, useState } from 'react'
import type { SessionInfo, SFTPFileEntry, TransferProgress } from '../types'
import { api } from '../wailsApi'
import { createSftpActions } from './sftpActions'

export function useSftpState(
  session: SessionInfo,
  active: boolean,
  followTerminal: boolean,
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
  const [editingFile, setEditingFile] = useState<{ path: string; content: string } | null>(null)
  const [savingFile, setSavingFile] = useState(false)

  const loadDir = useCallback(
    async (targetPath: string, syncCwd = true) => {
      if (!session.sftpReady) return
      setLoading(true)
      setError('')
      try {
        const res = await api.sftpList(session.sessionId, targetPath)
        setCurrentPath(res.path)
        setPathInput(res.path)
        setParentPath(res.parent)
        setEntries(res.entries || [])
        if (syncCwd) void api.setSessionCwd(session.sessionId, res.path)
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
    if (!followTerminal || !session.sftpReady) return
    return api.onEvent<{ sessionId: string; path: string }>('session:cwd', (ev) => {
      if (ev.sessionId === session.sessionId && ev.path && ev.path !== currentPath) {
        void loadDir(ev.path, false)
      }
    })
  }, [followTerminal, session.sftpReady, session.sessionId, currentPath, loadDir])

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

  const actions = createSftpActions({
    sessionId: session.sessionId,
    currentPath,
    editingFile,
    setBusyPath,
    setEditingFile,
    setSavingFile,
    loadDir,
    onToast,
  })

  return {
    currentPath, pathInput, setPathInput, parentPath, visibleEntries, loading, busyPath,
    showHidden, setShowHidden, error, transfers: Object.values(transfers),
    editingFile, setEditingFile, savingFile, loadDir, ...actions,
  }
}

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
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
  const [busyPaths, setBusyPaths] = useState<Set<string>>(() => new Set())
  const [showHidden, setShowHidden] = useState(false)
  const [error, setError] = useState('')
  const [transfers, setTransfers] = useState<Record<string, TransferProgress>>({})
  const [editingFile, setEditingFile] = useState<{ path: string; content: string } | null>(null)
  const [savingFile, setSavingFile] = useState(false)

  const currentPathRef = useRef(currentPath)
  currentPathRef.current = currentPath
  const loadSeqRef = useRef(0)

  const addBusyPath = useCallback((p: string) => {
    setBusyPaths((prev) => {
      if (prev.has(p)) return prev
      const next = new Set(prev)
      next.add(p)
      return next
    })
  }, [])

  const removeBusyPath = useCallback((p: string) => {
    setBusyPaths((prev) => {
      if (!prev.has(p)) return prev
      const next = new Set(prev)
      next.delete(p)
      return next
    })
  }, [])

  const removeEntryLocally = useCallback((p: string) => {
    setEntries((prev) => prev.filter((e) => e.path !== p))
  }, [])

  const loadDir = useCallback(
    async (targetPath: string, syncCwd = true) => {
      if (!session.sftpReady) return
      const seq = ++loadSeqRef.current
      setLoading(true)
      setError('')
      try {
        const res = await api.sftpList(session.sessionId, targetPath)
        if (seq !== loadSeqRef.current) return
        currentPathRef.current = res.path
        setCurrentPath(res.path)
        setPathInput(res.path)
        setParentPath(res.parent)
        setEntries(res.entries || [])
        if (syncCwd) void api.setSessionCwd(session.sessionId, res.path)
      } catch (err) {
        if (seq === loadSeqRef.current) setError(String(err))
      } finally {
        if (seq === loadSeqRef.current) setLoading(false)
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
      if (ev.sessionId === session.sessionId && ev.path && ev.path !== currentPathRef.current) {
        void loadDir(ev.path, false)
      }
    })
  }, [followTerminal, session.sftpReady, session.sessionId, loadDir])

  useEffect(() => {
    return api.onEvent<TransferProgress>('sftp:progress', (prog) => {
      if (prog.sessionId !== session.sessionId) return
      setTransfers((prev) => ({ ...prev, [prog.transferId]: prog }))
      if (prog.status === 'done') {
        if (prog.direction === 'upload') void loadDir(currentPathRef.current, false)
        setTimeout(() => {
          setTransfers((p) => { const n = { ...p }; delete n[prog.transferId]; return n })
        }, 3500)
      }
    })
  }, [session.sessionId, loadDir])

  useEffect(() => {
    if (!active || !session.sftpReady) return
    return api.onFileDrop(async (paths) => {
      try {
        const dest = currentPathRef.current
        const count = await api.sftpUploadFiles(session.sessionId, paths, dest)
        if (count > 0) { onToast(`已通过拖拽上传 ${count} 个文件到 ${dest}`); void loadDir(dest, false) }
      } catch (err) { onToast(`拖拽上传失败: ${err}`, 'error') }
    })
  }, [active, session.sftpReady, session.sessionId, loadDir, onToast])

  const visibleEntries = useMemo(
    () => (showHidden ? entries : entries.filter((e) => !e.name.startsWith('.'))),
    [entries, showHidden]
  )

  const actions = createSftpActions({
    sessionId: session.sessionId,
    getCurrentPath: () => currentPathRef.current,
    editingFile,
    addBusyPath,
    removeBusyPath,
    removeEntryLocally,
    setEditingFile,
    setSavingFile,
    loadDir,
    onToast,
  })

  return {
    currentPath, pathInput, setPathInput, parentPath, visibleEntries, loading, busyPaths,
    showHidden, setShowHidden, error, transfers: Object.values(transfers),
    editingFile, setEditingFile, savingFile, loadDir, ...actions,
  }
}

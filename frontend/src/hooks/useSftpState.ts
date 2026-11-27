import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { SessionInfo, SFTPFileEntry, TransferProgress } from '../types'
import { api } from '../wailsApi'
import { errMsg } from '../utils/errors'
import { useActionDialogs } from './useActionDialogs'
import { createSftpActions } from './sftpActions'

export const MAX_ERROR_TRANSFERS = 8
export const MAX_DONE_TRANSFERS = 6

export function pruneTransferMap(
  map: Record<string, TransferProgress>,
  maxErrors = MAX_ERROR_TRANSFERS,
  maxDone = MAX_DONE_TRANSFERS
): { next: Record<string, TransferProgress>; evictedIds: string[] } {
  const entries = Object.entries(map)
  const errorIds: string[] = []
  const doneIds: string[] = []
  for (const [id, tr] of entries) {
    if (tr.status === 'error') errorIds.push(id)
    else if (tr.status === 'done') doneIds.push(id)
  }
  const evict = new Set<string>()
  if (errorIds.length > maxErrors) {
    for (let i = 0; i < errorIds.length - maxErrors; i++) evict.add(errorIds[i])
  }
  if (doneIds.length > maxDone) {
    for (let i = 0; i < doneIds.length - maxDone; i++) evict.add(doneIds[i])
  }
  if (evict.size === 0) return { next: map, evictedIds: [] }
  const next: Record<string, TransferProgress> = {}
  for (const [id, tr] of entries) {
    if (!evict.has(id)) next[id] = tr
  }
  return { next, evictedIds: Array.from(evict) }
}

export function markRunningTransfersInterrupted(
  map: Record<string, TransferProgress>,
  reason = '连接已断开，传输已中断'
): Record<string, TransferProgress> {
  let changed = false
  const next: Record<string, TransferProgress> = {}
  for (const [id, tr] of Object.entries(map)) {
    if (tr.status === 'running') {
      changed = true
      next[id] = { ...tr, status: 'error', error: tr.error || reason }
    } else {
      next[id] = tr
    }
  }
  if (!changed) return map
  return pruneTransferMap(next).next
}

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
  const [editingFile, setEditingFile] = useState<{ token: number; path: string; content: string } | null>(null)
  const [savingToken, setSavingToken] = useState<number | null>(null)

  const currentPathRef = useRef(currentPath)
  currentPathRef.current = currentPath
  const editingFileRef = useRef(editingFile)
  editingFileRef.current = editingFile
  const editorTokenRef = useRef(0)
  const savingTokenRef = useRef<number | null>(null)
  const pathFocusedRef = useRef(false)
  const pathDirtyRef = useRef(false)
  const loadSeqRef = useRef(0)
  const dismissTimersRef = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map())
  const { confirm, prompt, dialogNode } = useActionDialogs()

  const setActiveEditingFile = useCallback((file: { path: string; content: string } | null) => {
    const nextToken = ++editorTokenRef.current
    const next = file ? { token: nextToken, path: file.path, content: file.content } : null
    editingFileRef.current = next
    setEditingFile(next)
  }, [])

  const updateEditingFileIfCurrent = useCallback((token: number, file: { path: string; content: string }) => {
    if (editorTokenRef.current !== token || !editingFileRef.current || editingFileRef.current.path !== file.path) {
      return
    }
    const next = { token, path: file.path, content: file.content }
    editingFileRef.current = next
    setEditingFile(next)
  }, [])

  const beginSaving = useCallback((token: number) => {
    if (savingTokenRef.current === token) return false
    savingTokenRef.current = token
    setSavingToken(token)
    return true
  }, [])

  const endSaving = useCallback((token: number) => {
    if (savingTokenRef.current === token) {
      savingTokenRef.current = null
      setSavingToken((prev) => (prev === token ? null : prev))
    }
  }, [])

  const handlePathInputChange = useCallback((next: string) => {
    pathDirtyRef.current = next !== currentPathRef.current
    setPathInput(next)
  }, [])

  const handlePathInputFocusChange = useCallback((focused: boolean) => {
    pathFocusedRef.current = focused
    if (!focused && !pathDirtyRef.current) {
      setPathInput(currentPathRef.current)
    }
  }, [])

  const dismissTransfer = useCallback((transferId: string) => {
    const existing = dismissTimersRef.current.get(transferId)
    if (existing !== undefined) {
      clearTimeout(existing)
      dismissTimersRef.current.delete(transferId)
    }
    setTransfers((prev) => {
      if (!(transferId in prev)) return prev
      const next = { ...prev }
      delete next[transferId]
      return next
    })
  }, [])

  const clearFinishedTransfers = useCallback(() => {
    setTransfers((prev) => {
      let changed = false
      const next: Record<string, TransferProgress> = {}
      for (const [id, tr] of Object.entries(prev)) {
        if (tr.status === 'done' || tr.status === 'error') {
          changed = true
          const existing = dismissTimersRef.current.get(id)
          if (existing !== undefined) {
            clearTimeout(existing)
            dismissTimersRef.current.delete(id)
          }
        } else {
          next[id] = tr
        }
      }
      return changed ? next : prev
    })
  }, [])

  useEffect(() => {
    const timers = dismissTimersRef.current
    return () => {
      for (const timer of timers.values()) clearTimeout(timer)
      timers.clear()
    }
  }, [])

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
    async (targetPath: string, syncCwd = true, forceSyncInput = syncCwd) => {
      if (!session.sftpReady) return
      const seq = ++loadSeqRef.current
      setLoading(true)
      setError('')
      try {
        const res = await api.sftpList(session.sessionId, targetPath)
        if (seq !== loadSeqRef.current) return
        currentPathRef.current = res.path
        setCurrentPath(res.path)
        if (forceSyncInput) {
          pathDirtyRef.current = false
          setPathInput(res.path)
        } else if (!pathFocusedRef.current && !pathDirtyRef.current) {
          setPathInput(res.path)
        }
        setParentPath(res.parent)
        setEntries(res.entries || [])
        if (syncCwd) void api.setSessionCwd(session.sessionId, res.path).catch(() => {})
      } catch (err) {
        if (seq === loadSeqRef.current) setError(errMsg(err))
      } finally {
        if (seq === loadSeqRef.current) setLoading(false)
      }
    },
    [session.sessionId, session.sftpReady]
  )

  useEffect(() => {
    if (session.sftpReady) void loadDir(session.homeDir || '', true, true)
  }, [session.sftpReady, session.homeDir, loadDir])

  useEffect(() => {
    if (!followTerminal || !session.sftpReady) return
    return api.onEvent<{ sessionId: string; path: string }>('session:cwd', (ev) => {
      if (ev.sessionId === session.sessionId && ev.path && ev.path !== currentPathRef.current) {
        void loadDir(ev.path, false, false)
      }
    })
  }, [followTerminal, session.sftpReady, session.sessionId, loadDir])

  useEffect(() => {
    if (session.status === 'connected') return
    setTransfers((prev) => markRunningTransfersInterrupted(prev))
  }, [session.status])

  useEffect(() => {
    return api.onEvent<TransferProgress>('sftp:progress', (prog) => {
      if (prog.sessionId !== session.sessionId) return
      setTransfers((prev) => {
        const updated = { ...prev }
        delete updated[prog.transferId]
        updated[prog.transferId] = prog
        const { next, evictedIds } = pruneTransferMap(updated)
        for (const id of evictedIds) {
          const t = dismissTimersRef.current.get(id)
          if (t !== undefined) {
            clearTimeout(t)
            dismissTimersRef.current.delete(id)
          }
        }
        return next
      })
      if (prog.status === 'done') {
        if (prog.direction === 'upload') void loadDir(currentPathRef.current, false, false)
        const existing = dismissTimersRef.current.get(prog.transferId)
        if (existing !== undefined) clearTimeout(existing)
        const timer = setTimeout(() => {
          dismissTimersRef.current.delete(prog.transferId)
          setTransfers((p) => {
            if (!(prog.transferId in p)) return p
            const n = { ...p }
            delete n[prog.transferId]
            return n
          })
        }, 3500)
        dismissTimersRef.current.set(prog.transferId, timer)
      } else if (prog.status === 'error') {
        const existing = dismissTimersRef.current.get(prog.transferId)
        if (existing !== undefined) {
          clearTimeout(existing)
          dismissTimersRef.current.delete(prog.transferId)
        }
      }
    })
  }, [session.sessionId, loadDir])

  useEffect(() => {
    if (!active || !session.sftpReady) return
    return api.onFileDrop(async (paths) => {
      if (document.querySelector('[role="dialog"]')) return
      try {
        const dest = currentPathRef.current
        const count = await api.sftpUploadFiles(session.sessionId, paths, dest)
        if (count > 0) {
          onToast(`已通过拖拽上传 ${count} 个文件到 ${dest}`)
          if (currentPathRef.current === dest) void loadDir(dest, false, false)
        }
      } catch (err) { onToast(`拖拽上传失败: ${errMsg(err)}`, 'error') }
    })
  }, [active, session.sftpReady, session.sessionId, loadDir, onToast])

  const visibleEntries = useMemo(
    () => (showHidden ? entries : entries.filter((e) => !e.name.startsWith('.'))),
    [entries, showHidden]
  )

  const transferList = useMemo(() => Object.values(transfers), [transfers])
  const savingFile = editingFile !== null && savingToken === editingFile.token

  const actions = useMemo(
    () =>
      createSftpActions({
        sessionId: session.sessionId,
        getCurrentPath: () => currentPathRef.current,
        getEditingFile: () => editingFileRef.current,
        addBusyPath,
        removeBusyPath,
        removeEntryLocally,
        setEditingFile: setActiveEditingFile,
        updateEditingFileIfCurrent,
        beginSaving,
        endSaving,
        loadDir: (p, syncCwd) => loadDir(p, syncCwd, false),
        confirm,
        prompt,
        onToast,
      }),
    [session.sessionId, addBusyPath, removeBusyPath, removeEntryLocally, setActiveEditingFile, updateEditingFileIfCurrent, beginSaving, endSaving, loadDir, confirm, prompt, onToast]
  )

  return {
    currentPath, pathInput, setPathInput: handlePathInputChange, onPathInputFocusChange: handlePathInputFocusChange,
    parentPath, visibleEntries, loading, busyPaths,
    showHidden, setShowHidden, error, transfers: transferList, dismissTransfer, clearFinishedTransfers,
    editingFile, setEditingFile: setActiveEditingFile, savingFile, loadDir, dialogNode, ...actions,
  }
}

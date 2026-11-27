import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { SessionInfo, SFTPFileEntry, TransferProgress } from '../types'
import { api } from '../wailsApi'
import { errMsg } from '../utils/errors'
import { useActionDialogs } from './useActionDialogs'
import { createSftpActions } from './sftpActions'

export const MAX_ERROR_TRANSFERS = 8
export const MAX_DONE_TRANSFERS = 6

export function normalizeSftpPath(rawPath: string, homeDir = ''): string {
  const trimmed = rawPath.trim()
  if (!trimmed || trimmed === '~') {
    const h = homeDir.trim()
    if (!h || h === '/') return '/'
    return h.replace(/\/+$/, '') || '/'
  }
  if (trimmed === '/') return '/'
  return trimmed.replace(/\/+$/, '') || '/'
}

export function shouldSkipDuplicateSftpList(
  targetPath: string,
  pendingPath: string | null,
  homeDir = '',
  force = false
): boolean {
  if (force || pendingPath === null) return false
  return normalizeSftpPath(targetPath, homeDir) === normalizeSftpPath(pendingPath, homeDir)
}

export function shouldIgnoreCwdSync(
  eventPath: string,
  currentPath: string,
  pendingPath: string | null,
  homeDir = ''
): boolean {
  if (!eventPath.trim()) return true
  const normEv = normalizeSftpPath(eventPath, homeDir)
  if (normEv === normalizeSftpPath(currentPath, homeDir)) return true
  if (pendingPath !== null && normEv === normalizeSftpPath(pendingPath, homeDir)) return true
  return false
}

export interface UploadRefreshCoalescer {
  schedule: (destPath?: string) => void
  cancel: () => void
}

export function createUploadRefreshCoalescer(
  delayMs: number,
  onRefresh: (destPath?: string) => void
): UploadRefreshCoalescer {
  let timer: ReturnType<typeof setTimeout> | null = null
  let pendingDest: string | undefined

  const cancel = () => {
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
    pendingDest = undefined
  }

  const schedule = (destPath?: string) => {
    if (destPath !== undefined) pendingDest = destPath
    if (timer !== null) clearTimeout(timer)
    timer = setTimeout(() => {
      const target = pendingDest
      timer = null
      pendingDest = undefined
      onRefresh(target)
    }, delayMs)
  }

  return { schedule, cancel }
}

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
  const [editingFile, setEditingFile] = useState<{ token: number; path: string; content: string } | null>(null)
  const [savingToken, setSavingToken] = useState<number | null>(null)

  const currentPathRef = useRef(currentPath)
  currentPathRef.current = currentPath
  const pathInputRef = useRef(pathInput)
  pathInputRef.current = pathInput
  const parentPathRef = useRef(parentPath)
  parentPathRef.current = parentPath
  const homeDirRef = useRef(session.homeDir || '')
  homeDirRef.current = session.homeDir || ''
  const pendingPathRef = useRef<string | null>(null)
  const editingFileRef = useRef(editingFile)
  editingFileRef.current = editingFile
  const editorTokenRef = useRef(0)
  const savingTokenRef = useRef<number | null>(null)
  const pathFocusedRef = useRef(false)
  const pathDirtyRef = useRef(false)
  const loadSeqRef = useRef(0)
  const uploadCoalescerRef = useRef<UploadRefreshCoalescer | null>(null)
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
    async (targetPath: string, syncCwd = true, forceSyncInput = syncCwd, force = false) => {
      if (!session.sftpReady) return
      const home = homeDirRef.current
      if (shouldSkipDuplicateSftpList(targetPath, pendingPathRef.current, home, force)) {
        return
      }
      uploadCoalescerRef.current?.cancel()
      const normTarget = normalizeSftpPath(targetPath, home)
      pendingPathRef.current = normTarget
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
        if (seq === loadSeqRef.current) {
          pendingPathRef.current = null
          setLoading(false)
        }
      }
    },
    [session.sessionId, session.sftpReady]
  )

  useEffect(() => {
    const coalescer = createUploadRefreshCoalescer(150, (dest) => {
      const cur = currentPathRef.current
      if (dest !== undefined && normalizeSftpPath(dest, homeDirRef.current) !== normalizeSftpPath(cur, homeDirRef.current)) {
        return
      }
      void loadDir(cur, false, false, true)
    })
    uploadCoalescerRef.current = coalescer
    return () => {
      coalescer.cancel()
      if (uploadCoalescerRef.current === coalescer) uploadCoalescerRef.current = null
    }
  }, [loadDir])

  const scheduleUploadRefresh = useCallback((destPath?: string) => {
    uploadCoalescerRef.current?.schedule(destPath)
  }, [])

  const handleUploadProgressDone = useCallback(() => {
    uploadCoalescerRef.current?.schedule(currentPathRef.current)
  }, [])

  useEffect(() => {
    if (session.sftpReady) void loadDir(session.homeDir || '', true, true, false)
  }, [session.sftpReady, session.homeDir, loadDir])

  useEffect(() => {
    if (!followTerminal || !session.sftpReady) return
    return api.onEvent<{ sessionId: string; path: string }>('session:cwd', (ev) => {
      if (ev.sessionId !== session.sessionId || !ev.path) return
      if (shouldIgnoreCwdSync(ev.path, currentPathRef.current, pendingPathRef.current, homeDirRef.current)) {
        return
      }
      void loadDir(ev.path, false, false, false)
    })
  }, [followTerminal, session.sftpReady, session.sessionId, loadDir])

  useEffect(() => {
    if (!active || !session.sftpReady) return
    return api.onFileDrop(async (paths) => {
      if (document.querySelector('[role="dialog"]')) return
      try {
        const dest = currentPathRef.current
        const count = await api.sftpUploadFiles(session.sessionId, paths, dest)
        if (count > 0) {
          onToast(`已通过拖拽上传 ${count} 个文件到 ${dest}`)
          if (currentPathRef.current === dest) scheduleUploadRefresh(dest)
        }
      } catch (err) { onToast(`拖拽上传失败: ${errMsg(err)}`, 'error') }
    })
  }, [active, session.sftpReady, session.sessionId, scheduleUploadRefresh, onToast])

  const visibleEntries = useMemo(
    () => (showHidden ? entries : entries.filter((e) => !e.name.startsWith('.'))),
    [entries, showHidden]
  )

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
        loadDir: (p, syncCwd, force = true) => loadDir(p, syncCwd, false, force),
        scheduleUploadRefresh,
        confirm,
        prompt,
        onToast,
      }),
    [
      session.sessionId,
      addBusyPath,
      removeBusyPath,
      removeEntryLocally,
      setActiveEditingFile,
      updateEditingFileIfCurrent,
      beginSaving,
      endSaving,
      loadDir,
      scheduleUploadRefresh,
      confirm,
      prompt,
      onToast,
    ]
  )

  const handleSubmitPath = useCallback(() => { void loadDir(pathInputRef.current, true, true, true) }, [loadDir])
  const handleGoHome = useCallback(() => { void loadDir(homeDirRef.current || '~', true, true, true) }, [loadDir])
  const handleGoUp = useCallback(() => { void loadDir(parentPathRef.current, true, true, true) }, [loadDir])
  const handleToggleHidden = useCallback(() => setShowHidden((v) => !v), [])
  const handleRefresh = useCallback(() => { void loadDir(currentPathRef.current, true, true, true) }, [loadDir])
  const handleOpenDir = useCallback((p: string) => { void loadDir(p, true, true, false) }, [loadDir])
  const handleCloseEditor = useCallback(() => setActiveEditingFile(null), [setActiveEditingFile])

  return {
    currentPath,
    pathInput,
    setPathInput: handlePathInputChange,
    onPathInputFocusChange: handlePathInputFocusChange,
    parentPath,
    visibleEntries,
    loading,
    busyPaths,
    showHidden,
    error,
    editingFile,
    savingFile,
    loadDir,
    handleUploadProgressDone,
    handleSubmitPath,
    handleGoHome,
    handleGoUp,
    handleToggleHidden,
    handleRefresh,
    handleOpenDir,
    handleCloseEditor,
    dialogNode,
    ...actions,
  }
}

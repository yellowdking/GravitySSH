import { useCallback, useEffect, useRef, useState } from 'react'
import type { SyncFlowSession, SyncFlowState, SyncFlowStatus } from '../syncTypes'
import { errMsg } from '../utils/errors'
import { api } from '../wailsApi'

const POLL_INTERVAL_MS = 1200

export function isActiveFlowState(state?: SyncFlowState | string): boolean {
  return (
    state === 'waiting_peer' ||
    state === 'awaiting_sas_input' ||
    state === 'awaiting_sas_confirm' ||
    state === 'transferring'
  )
}

interface UseSyncFlowOptions {
  onCompleted?: (status: SyncFlowStatus) => void
  onToast?: (msg: string, type?: 'info' | 'error') => void
}

export function useSyncFlow(options?: UseSyncFlowOptions) {
  const [session, setSession] = useState<SyncFlowSession | null>(null)
  const [status, setStatus] = useState<SyncFlowStatus | null>(null)
  const [starting, setStarting] = useState(false)
  const [submittingSas, setSubmittingSas] = useState(false)
  const [confirmingSas, setConfirmingSas] = useState(false)
  const [cancelling, setCancelling] = useState(false)
  const [error, setError] = useState('')

  const mountedRef = useRef(true)
  const sessionRef = useRef<SyncFlowSession | null>(null)
  sessionRef.current = session
  const statusRef = useRef<SyncFlowStatus | null>(null)
  statusRef.current = status
  const completedFiredForRef = useRef<string | null>(null)
  const onCompletedRef = useRef(options?.onCompleted)
  onCompletedRef.current = options?.onCompleted
  const onToastRef = useRef(options?.onToast)
  onToastRef.current = options?.onToast

  const applyIncomingStatus = useCallback((sid: string, incoming: SyncFlowStatus) => {
    if (!mountedRef.current || sessionRef.current?.sessionId !== sid) return
    setStatus((prev) => {
      const merged: SyncFlowStatus = {
        ...prev,
        ...incoming,
        sessionId: incoming.sessionId || sid,
        pairUri: incoming.pairUri || prev?.pairUri || sessionRef.current?.pairUri,
        roomCode: incoming.roomCode || prev?.roomCode || sessionRef.current?.roomCode,
        expiresAt: incoming.expiresAt || prev?.expiresAt || sessionRef.current?.expiresAt,
      }
      statusRef.current = merged
      return merged
    })
    if (incoming.state === 'completed' && completedFiredForRef.current !== sid) {
      completedFiredForRef.current = sid
      onCompletedRef.current?.(incoming)
    }
  }, [])

  // Subscribe to Wails 'sync:flow_status' events AND poll every 1.2s while flow is active
  const activeSessionId = session?.sessionId ?? ''
  const activeState = status?.state

  useEffect(() => {
    if (!activeSessionId) return
    const unsub = api.onEvent<SyncFlowStatus>('sync:flow_status', (ev) => {
      if (!ev) return
      if (ev.sessionId && ev.sessionId !== activeSessionId) return
      applyIncomingStatus(activeSessionId, ev)
    })
    return () => {
      unsub()
    }
  }, [activeSessionId, applyIncomingStatus])

  useEffect(() => {
    if (!activeSessionId || !isActiveFlowState(activeState)) return
    let cancelled = false
    let inFlight = false

    const pollOnce = async () => {
      if (cancelled || inFlight || sessionRef.current?.sessionId !== activeSessionId) return
      inFlight = true
      try {
        const next = await api.getSyncFlowStatus(activeSessionId)
        if (!cancelled) applyIncomingStatus(activeSessionId, next)
      } catch {
        // Keep waiting on transient IPC/poll hiccups while Wails events are also subscribed
      } finally {
        inFlight = false
      }
    }

    void pollOnce()
    const timer = setInterval(() => {
      void pollOnce()
    }, POLL_INTERVAL_MS)
    return () => {
      cancelled = true
      clearInterval(timer)
    }
  }, [activeSessionId, activeState, applyIncomingStatus])

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      const curSess = sessionRef.current
      const curStatus = statusRef.current
      if (curSess && isActiveFlowState(curStatus?.state ?? curSess.state)) {
        void api.cancelSyncFlow(curSess.sessionId).catch(() => {})
      }
      if (curStatus?.state === 'ready_preview' && curStatus.preview?.previewId) {
        void api.clearSyncPreview(curStatus.preview.previewId).catch(() => {})
      }
    }
  }, [])

  const adoptSession = useCallback((next: SyncFlowSession) => {
    const prevSess = sessionRef.current
    const prevSt = statusRef.current
    if (prevSess && prevSess.sessionId !== next.sessionId && isActiveFlowState(prevSt?.state ?? prevSess.state)) {
      void api.cancelSyncFlow(prevSess.sessionId).catch(() => {})
    }
    completedFiredForRef.current = null
    const initState = (next.state || 'waiting_peer') as SyncFlowState
    const initStatus: SyncFlowStatus = {
      sessionId: next.sessionId,
      purpose: next.purpose,
      role: next.role,
      mode: next.mode,
      state: initState,
      pairUri: next.pairUri,
      roomCode: next.roomCode,
      expiresAt: next.expiresAt,
    }
    sessionRef.current = next
    statusRef.current = initStatus
    setError('')
    setSession(next)
    setStatus(initStatus)
  }, [])

  const startFlow = useCallback(
    async (launcher: () => Promise<SyncFlowSession>): Promise<SyncFlowSession | null> => {
      setStarting(true)
      setError('')
      const prevSess = sessionRef.current
      const prevSt = statusRef.current
      try {
        if (prevSess && isActiveFlowState(prevSt?.state ?? prevSess.state)) {
          await api.cancelSyncFlow(prevSess.sessionId).catch(() => {})
        }
        const next = await launcher()
        if (!mountedRef.current) {
          void api.cancelSyncFlow(next.sessionId).catch(() => {})
          return null
        }
        adoptSession(next)
        return next
      } catch (err) {
        const msg = errMsg(err)
        if (mountedRef.current) {
          setError(msg)
          onToastRef.current?.(msg, 'error')
        }
        return null
      } finally {
        if (mountedRef.current) setStarting(false)
      }
    },
    [adoptSession]
  )

  const submitSasInput = useCallback(async (rawCode: string): Promise<boolean> => {
    const cur = sessionRef.current
    if (!cur) return false
    const digits = rawCode.replace(/\D/g, '')
    if (digits.length !== 6) {
      onToastRef.current?.('请输入完整的 6 位数字安全码', 'error')
      return false
    }
    setSubmittingSas(true)
    setError('')
    try {
      await api.submitSyncSasInput(cur.sessionId, digits)
      const latest = await api.getSyncFlowStatus(cur.sessionId).catch(() => null)
      if (latest && mountedRef.current) applyIncomingStatus(cur.sessionId, latest)
      return true
    } catch (err) {
      const msg = errMsg(err)
      if (mountedRef.current) {
        setError(msg)
        onToastRef.current?.(msg, 'error')
      }
      const latest = await api.getSyncFlowStatus(cur.sessionId).catch(() => null)
      if (latest && mountedRef.current) applyIncomingStatus(cur.sessionId, latest)
      return false
    } finally {
      if (mountedRef.current) setSubmittingSas(false)
    }
  }, [applyIncomingStatus])

  const confirmSas = useCallback(async (approved: boolean): Promise<boolean> => {
    const cur = sessionRef.current
    if (!cur) return false
    setConfirmingSas(true)
    setError('')
    try {
      await api.confirmSyncSas(cur.sessionId, approved)
      const latest = await api.getSyncFlowStatus(cur.sessionId).catch(() => null)
      if (latest && mountedRef.current) applyIncomingStatus(cur.sessionId, latest)
      return true
    } catch (err) {
      const msg = errMsg(err)
      if (mountedRef.current) {
        setError(msg)
        onToastRef.current?.(msg, 'error')
      }
      return false
    } finally {
      if (mountedRef.current) setConfirmingSas(false)
    }
  }, [applyIncomingStatus])

  const cancelFlow = useCallback(async () => {
    const cur = sessionRef.current
    if (!cur) return
    setCancelling(true)
    try {
      await api.cancelSyncFlow(cur.sessionId).catch(() => {})
      if (!mountedRef.current) return
      sessionRef.current = null
      statusRef.current = null
      setSession(null)
      setStatus(null)
      setError('')
    } finally {
      if (mountedRef.current) setCancelling(false)
    }
  }, [])

  const resetFlow = useCallback((clearPreview = false) => {
    const curSt = statusRef.current
    if (clearPreview && curSt?.preview?.previewId) {
      void api.clearSyncPreview(curSt.preview.previewId).catch(() => {})
    }
    sessionRef.current = null
    statusRef.current = null
    setSession(null)
    setStatus(null)
    setError('')
  }, [])

  return {
    session,
    status,
    starting,
    submittingSas,
    confirmingSas,
    cancelling,
    error,
    setError,
    isActive: isActiveFlowState(status?.state),
    adoptSession,
    startFlow,
    submitSasInput,
    confirmSas,
    cancelFlow,
    resetFlow,
  }
}

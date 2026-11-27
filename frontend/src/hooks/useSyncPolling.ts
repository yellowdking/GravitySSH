import { useEffect, useRef } from 'react'

export interface SyncPollingControllerOptions {
  intervalMs?: number
  poll: () => Promise<boolean>
  isHidden?: () => boolean
  subscribeVisibility?: (cb: () => void) => () => void
}

export function startSyncPollingLoop({
  intervalMs = 1500,
  poll,
  isHidden = () => typeof document !== 'undefined' && document.hidden,
  subscribeVisibility = (cb) => {
    if (typeof document === 'undefined') return () => {}
    document.addEventListener('visibilitychange', cb)
    return () => document.removeEventListener('visibilitychange', cb)
  },
}: SyncPollingControllerOptions): () => void {
  let cancelled = false
  let stopped = false
  let inFlight = false
  let timer: ReturnType<typeof setTimeout> | null = null

  const clearTimer = () => {
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
  }

  const schedule = (delay: number) => {
    clearTimer()
    if (cancelled || stopped || isHidden()) return
    timer = setTimeout(() => {
      timer = null
      void runOnce()
    }, delay)
  }

  const runOnce = async () => {
    if (cancelled || stopped || isHidden() || inFlight) return
    inFlight = true
    let shouldContinue = true
    try {
      shouldContinue = await poll()
    } catch {
      shouldContinue = true
    } finally {
      inFlight = false
    }
    if (!cancelled) {
      if (!shouldContinue) {
        stopped = true
        clearTimer()
      } else if (!isHidden()) {
        schedule(intervalMs)
      }
    }
  }

  const onVisibility = () => {
    if (isHidden()) {
      clearTimer()
    } else if (!cancelled && !stopped && !inFlight) {
      schedule(150)
    }
  }

  const unsubscribeVisibility = subscribeVisibility(onVisibility)
  if (!isHidden()) schedule(intervalMs)

  return () => {
    cancelled = true
    clearTimer()
    unsubscribeVisibility()
  }
}

interface UseSyncPollingOptions {
  enabled: boolean
  intervalMs?: number
  resetKey?: string | number | null
  poll: () => Promise<boolean>
}

export function useSyncPolling({ enabled, intervalMs = 1500, resetKey, poll }: UseSyncPollingOptions) {
  const pollRef = useRef(poll)
  pollRef.current = poll

  useEffect(() => {
    if (!enabled) return
    return startSyncPollingLoop({
      intervalMs,
      poll: () => pollRef.current(),
    })
  }, [enabled, intervalMs, resetKey])
}

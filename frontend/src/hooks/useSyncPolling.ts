import { useEffect, useRef } from 'react'

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
      if (cancelled || stopped || document.hidden) return
      timer = setTimeout(() => {
        timer = null
        void runOnce()
      }, delay)
    }

    const runOnce = async () => {
      if (cancelled || stopped || document.hidden || inFlight) return
      inFlight = true
      let shouldContinue = true
      try {
        shouldContinue = await pollRef.current()
      } catch {
        shouldContinue = true
      } finally {
        inFlight = false
      }
      if (!cancelled) {
        if (!shouldContinue) {
          stopped = true
          clearTimer()
        } else if (!document.hidden) {
          schedule(intervalMs)
        }
      }
    }

    const onVisibility = () => {
      if (document.hidden) {
        clearTimer()
      } else if (!cancelled && !stopped && !inFlight) {
        schedule(150)
      }
    }

    document.addEventListener('visibilitychange', onVisibility)
    if (!document.hidden) schedule(intervalMs)

    return () => {
      cancelled = true
      clearTimer()
      document.removeEventListener('visibilitychange', onVisibility)
    }
  }, [enabled, intervalMs, resetKey])
}

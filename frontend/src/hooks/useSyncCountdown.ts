import { useEffect, useState } from 'react'
import { parseSyncTimestampMs } from '../utils/syncUri'

export function fmtSec(n: number): string {
  const clamped = Math.max(0, n)
  return `${String(Math.floor(clamped / 60)).padStart(2, '0')}:${String(clamped % 60).padStart(2, '0')}`
}

function calcLeft(expiresAt: number | string | null | undefined): number {
  const expiresMs = parseSyncTimestampMs(expiresAt)
  if (!Number.isFinite(expiresMs)) return 0
  return Math.max(0, Math.floor((expiresMs - Date.now()) / 1000))
}

export function useSyncCountdown(expiresAt: number | string | null | undefined): number {
  const key = expiresAt === null || expiresAt === undefined ? '' : String(expiresAt)
  const [state, setState] = useState(() => ({ key, left: calcLeft(expiresAt) }))

  useEffect(() => {
    if (!key) {
      setState({ key: '', left: 0 })
      return
    }
    const expiresMs = parseSyncTimestampMs(expiresAt)
    if (!Number.isFinite(expiresMs)) {
      setState({ key, left: 0 })
      return
    }

    let id: ReturnType<typeof setInterval> | null = null
    const tick = () => {
      const rem = Math.max(0, Math.floor((expiresMs - Date.now()) / 1000))
      setState({ key, left: rem })
      if (rem <= 0 && id !== null) {
        clearInterval(id)
        id = null
      }
      return rem
    }

    const startIfNeeded = () => {
      if (id !== null || document.hidden) return
      if (tick() > 0) id = setInterval(tick, 1000)
    }

    const onVisibility = () => {
      if (document.hidden) {
        if (id !== null) {
          clearInterval(id)
          id = null
        }
      } else {
        startIfNeeded()
      }
    }

    document.addEventListener('visibilitychange', onVisibility)
    if (tick() > 0 && !document.hidden) {
      id = setInterval(tick, 1000)
    }
    return () => {
      document.removeEventListener('visibilitychange', onVisibility)
      if (id !== null) clearInterval(id)
    }
  }, [key, expiresAt])

  return state.key === key ? state.left : calcLeft(expiresAt)
}

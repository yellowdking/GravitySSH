import { useEffect, useState } from 'react'

export function fmtSec(n: number): string {
  const clamped = Math.max(0, n)
  return `${String(Math.floor(clamped / 60)).padStart(2, '0')}:${String(clamped % 60).padStart(2, '0')}`
}

function calcLeft(expiresAt: string | null | undefined): number {
  if (!expiresAt) return 0
  const expiresMs = new Date(expiresAt).getTime()
  if (!Number.isFinite(expiresMs)) return 0
  return Math.max(0, Math.floor((expiresMs - Date.now()) / 1000))
}

export function useSyncCountdown(expiresAt: string | null | undefined): number {
  const key = expiresAt ?? ''
  const [state, setState] = useState(() => ({ key, left: calcLeft(expiresAt) }))

  useEffect(() => {
    if (!expiresAt) {
      setState({ key: '', left: 0 })
      return
    }
    const expiresMs = new Date(expiresAt).getTime()
    if (!Number.isFinite(expiresMs)) {
      setState({ key: expiresAt, left: 0 })
      return
    }

    let id: ReturnType<typeof setInterval> | null = null
    const tick = () => {
      const rem = Math.max(0, Math.floor((expiresMs - Date.now()) / 1000))
      setState({ key: expiresAt, left: rem })
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
  }, [expiresAt])

  return state.key === key ? state.left : calcLeft(expiresAt)
}

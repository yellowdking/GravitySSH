import { useCallback, useEffect, useRef, useState } from 'react'
import { copyToClipboard } from '../utils/errors'

export function useCopyFeedback(durationMs = 1800) {
  const [copied, setCopied] = useState(false)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    return () => {
      if (timerRef.current !== null) {
        clearTimeout(timerRef.current)
        timerRef.current = null
      }
    }
  }, [])

  const reset = useCallback(() => {
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current)
      timerRef.current = null
    }
    setCopied(false)
  }, [])

  const copy = useCallback(async (text: string): Promise<boolean> => {
    const ok = await copyToClipboard(text)
    if (!ok) return false
    setCopied(true)
    if (timerRef.current !== null) clearTimeout(timerRef.current)
    timerRef.current = setTimeout(() => {
      timerRef.current = null
      setCopied(false)
    }, durationMs)
    return true
  }, [durationMs])

  return { copied, copy, reset }
}

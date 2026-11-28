import type React from 'react'

export function errMsg(err: unknown): string {
  const msg = err instanceof Error ? err.message : String(err)
  return msg.replace(/^Error:\s*/i, '')
}

export function isMacPlatform(ua?: string): boolean {
  const source =
    ua ??
    (typeof navigator !== 'undefined'
      ? `${navigator.platform || ''} ${navigator.userAgent || ''}`
      : '')
  return /Mac|Macintosh|MacIntel|MacPPC|Mac68K/i.test(source) && !/iPhone|iPad|iPod/i.test(source)
}

export async function copyToClipboard(text: string): Promise<boolean> {
  const rt = typeof window !== 'undefined' ? window.runtime : undefined
  if (typeof rt?.ClipboardSetText === 'function') {
    try {
      const ok = await rt.ClipboardSetText(text)
      if (ok !== false) return true
    } catch {
      // Fall through to navigator.clipboard
    }
  }
  try {
    if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
    return false
  } catch {
    return false
  }
}

export async function readFromClipboard(): Promise<string | null> {
  const rt = typeof window !== 'undefined' ? window.runtime : undefined
  if (typeof rt?.ClipboardGetText === 'function') {
    try {
      const text = await rt.ClipboardGetText()
      if (typeof text === 'string') return text
    } catch {
      // Fall through to navigator.clipboard
    }
  }
  try {
    if (typeof navigator !== 'undefined' && navigator.clipboard?.readText) {
      return await navigator.clipboard.readText()
    }
    return null
  } catch {
    return null
  }
}

export function isAllowedExternalUrl(uri: string): boolean {
  try {
    const parsed = new URL(uri)
    return parsed.protocol === 'http:' || parsed.protocol === 'https:'
  } catch {
    return false
  }
}

export function insertTabIndent(
  e: React.KeyboardEvent<HTMLTextAreaElement>,
  value: string,
  setValue: (next: string) => void
): boolean {
  if (e.nativeEvent.isComposing || e.keyCode === 229) return false
  if (e.key !== 'Tab' || e.shiftKey || e.ctrlKey || e.altKey || e.metaKey) return false
  e.preventDefault()
  const target = e.currentTarget
  const start = target.selectionStart
  const end = target.selectionEnd
  setValue(value.slice(0, start) + '  ' + value.slice(end))
  requestAnimationFrame(() => {
    target.selectionStart = target.selectionEnd = start + 2
  })
  return true
}

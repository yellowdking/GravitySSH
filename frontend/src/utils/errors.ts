import type React from 'react'

export function errMsg(err: unknown): string {
  const msg = err instanceof Error ? err.message : String(err)
  return msg.replace(/^Error:\s*/i, '')
}

export async function copyToClipboard(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    return false
  }
}

export async function readFromClipboard(): Promise<string | null> {
  try {
    return await navigator.clipboard.readText()
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

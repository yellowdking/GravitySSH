export function computeCompletionInsert(currentLine: string, fullCommand: string): {
  backspaces: number
  insertText: string
} {
  const trimmed = currentLine.trimStart()
  if (!trimmed) {
    return { backspaces: 0, insertText: fullCommand }
  }
  if (fullCommand.startsWith(trimmed)) {
    return { backspaces: 0, insertText: fullCommand.slice(trimmed.length) }
  }
  if (fullCommand.toLowerCase().startsWith(trimmed.toLowerCase())) {
    return { backspaces: trimmed.length, insertText: fullCommand }
  }
  return { backspaces: trimmed.length, insertText: fullCommand }
}

export function applyTerminalInputToLine(currentLine: string, rawData: string): string {
  if (rawData.startsWith('\x1b')) {
    if (rawData === '\x1b[A' || rawData === '\x1b[B') {
      return ''
    }
    return currentLine
  }

  let line = currentLine
  for (let i = 0; i < rawData.length; i++) {
    const ch = rawData[i]
    const code = rawData.charCodeAt(i)
    if (ch === '\r' || ch === '\n' || code === 0x03 || code === 0x04 || code === 0x15) {
      line = ''
    } else if (code === 0x7f || code === 0x08) {
      line = line.slice(0, -1)
    } else if (code === 0x17) {
      line = line.replace(/\S+\s*$/, '')
    } else if (code >= 0x20) {
      line += ch
    }
  }
  return line
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let val = bytes / 1024
  let idx = 0
  while (val >= 1024 && idx < units.length - 1) {
    val /= 1024
    idx++
  }
  return `${val.toFixed(val >= 100 ? 0 : 1)} ${units[idx]}`
}

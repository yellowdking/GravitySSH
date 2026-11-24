/**
 * Computes the exact backspaces and remaining text to write into the terminal when the user
 * selects an autocomplete suggestion, avoiding duplicate prefixes for both full commands
 * and directory/file path completions (e.g. `cd doc` -> `cd docker-compose/`, `nano /etc/ng` -> `nano /etc/nginx/`).
 */
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

  // Find longest exact common prefix so shared command/path prefixes are never duplicated or re-typed.
  let common = 0
  const maxLen = Math.min(trimmed.length, fullCommand.length)
  while (common < maxLen && trimmed[common] === fullCommand[common]) {
    common++
  }

  if (common > 0) {
    return {
      backspaces: trimmed.length - common,
      insertText: fullCommand.slice(common),
    }
  }

  // Substring or case-mismatch from start (e.g. "compose" -> "docker compose up -d")
  return { backspaces: trimmed.length, insertText: fullCommand }
}

/**
 * Lightweight client-side input buffer mirror for zero-latency autocomplete triggering.
 */
export function applyTerminalInputToLine(currentLine: string, rawData: string): string {
  // Ignore escape sequences (arrows, function keys, etc.)
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
      // Ctrl+W delete previous word
      line = line.replace(/\S+\s*$/, '')
    } else if (code >= 0x20) {
      line += ch
    }
  }
  return line
}

export function sanitizePasteText(raw: string): string {
  if (!raw) return ''
  return raw
    .replace(/\x1b\[200~/g, '')
    .replace(/\x1b\[201~/g, '')
    .replace(/\r\n/g, '\n')
    .replace(/\r/g, '\n')
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

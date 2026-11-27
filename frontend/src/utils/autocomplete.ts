/**
 * Computes exact backspaces and suffix text to write into the terminal when selecting
 * an autocomplete suggestion, using Unicode code-point case-sensitive LCP so CJK/emoji
 * filenames and chained commands never over-backspace.
 */
export function computeCompletionInsert(currentLine: string, targetLine: string): {
  backspaces: number
  insertText: string
} {
  const hasLeadingWs = currentLine.length > 0 && /^\s/.test(currentLine)
  const base = hasLeadingWs && !/^\s/.test(targetLine) ? currentLine.trimStart() : currentLine
  if (!base.trim()) {
    return { backspaces: 0, insertText: targetLine }
  }

  const cur = [...base]
  const tgt = [...targetLine]
  let common = 0
  const maxLen = Math.min(cur.length, tgt.length)
  while (common < maxLen && cur[common] === tgt[common]) {
    common++
  }

  return {
    backspaces: cur.length - common,
    insertText: tgt.slice(common).join(''),
  }
}

/**
 * Extracts the active matched token prefix from the current input line for cyan
 * prefix highlighting in AutocompletePopup.
 */
export function getActiveTokenPrefix(line: string, source: string): string {
  const trimmed = line.trimStart()
  if (!trimmed) return ''
  if (source === 'history' || source === 'history_local') {
    const seg = trimmed.split(/(?:&&|\|\||\|&|[;|])/).pop() ?? trimmed
    return seg.trimStart()
  }
  if (/\s$/.test(line)) return ''
  const parts = trimmed.split(/(?<!\\)\s+/)
  let tok = (parts[parts.length - 1] || '').replace(/\\ /g, ' ')
  if (tok.startsWith('"') || tok.startsWith("'")) tok = tok.slice(1)
  if (source === 'dir' || source === 'file') {
    const slash = tok.lastIndexOf('/')
    if (slash >= 0) tok = tok.slice(slash + 1)
  }
  return tok
}

const RESET_ESCAPES = new Set([
  '\x1b[A', '\x1b[B', '\x1b[C', '\x1b[D',
  '\x1b[H', '\x1b[F', '\x1b[1~', '\x1b[4~',
  '\x1bOA', '\x1bOB', '\x1bOC', '\x1bOD', '\x1bOH', '\x1bOF',
])

/**
 * Lightweight client-side input buffer mirror for zero-latency autocomplete triggering.
 */
export function applyTerminalInputToLine(currentLine: string, rawData: string): string {
  let data = rawData
  if (data.startsWith('\x1b[200~')) {
    data = data.slice('\x1b[200~'.length)
    if (data.endsWith('\x1b[201~')) {
      data = data.slice(0, -'\x1b[201~'.length)
    }
    data = data.replace(/\x1b\[20[01]~/g, '')
    if (data.includes('\x1b') || data.includes('\r') || data.includes('\n')) {
      return ''
    }
  } else if (data.startsWith('\x1b')) {
    return RESET_ESCAPES.has(data) ? '' : currentLine
  }

  let line = currentLine
  for (const ch of data) {
    const code = ch.charCodeAt(0)
    if (ch === '\r' || ch === '\n' || ch === '\t' || code === 0x03 || code === 0x04 || code === 0x15) {
      line = ''
    } else if (code === 0x7f || code === 0x08) {
      line = [...line].slice(0, -1).join('')
    } else if (code === 0x17) {
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

const ANSI_TOKEN_RE = /(\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_])/g

// Matches an incomplete trailing ANSI CSI, OSC, or bare ESC sequence at the end of a chunk.
const INCOMPLETE_TRAILING_ANSI_RE = /(?:\x1b\[[0-9;?]*[ -/]*|\x1b\][^\x07\x1b]*(?:\x1b)?|\x1b)$/

const HIGHLIGHT_RE =
  /(\b(?:error|fatal|panic|failed|failure|exception|critical|denied)\b|\b(?:warn|warning|deprecated|timeout|retry)\b|\b(?:ok|success|succeeded|passed|healthy)\b|active \(running\)|\b(?:(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\.){3}(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\b)/gi

const RED = '\x1b[38;5;203m'
const AMBER = '\x1b[38;5;221m'
const GREEN = '\x1b[38;5;114m'
const CYAN = '\x1b[38;5;117m'
const FG_RESET = '\x1b[39m'

const MAX_HIGHLIGHT_CHUNK_CHARS = 16384
const MAX_PENDING_ANSI_CHARS = 256

function pickColor(match: string): string {
  const lower = match.toLowerCase()
  if (/^\d+\.\d+\.\d+\.\d+$/.test(lower)) return CYAN
  if (
    lower === 'active (running)' ||
    lower === 'ok' ||
    lower === 'success' ||
    lower === 'succeeded' ||
    lower === 'passed' ||
    lower === 'healthy'
  ) {
    return GREEN
  }
  if (lower === 'warn' || lower === 'warning' || lower === 'deprecated' || lower === 'timeout' || lower === 'retry') {
    return AMBER
  }
  return RED
}

function updateSgrColorState(seq: string, currentInColor: boolean): boolean {
  if (!seq.startsWith('\x1b[') || !seq.endsWith('m')) return currentInColor
  const body = seq.slice(2, -1)
  if (!body) return false // \x1b[m resets all attributes
  if (!/^[0-9;]*$/.test(body)) return currentInColor

  const codes = body.split(';').map((s) => (s === '' ? 0 : Number(s)))
  let inColor = currentInColor
  for (let i = 0; i < codes.length; i++) {
    const c = codes[i]
    if (c === 0 || c === 39) {
      inColor = false
    } else if ((c >= 30 && c <= 37) || (c >= 90 && c <= 97)) {
      inColor = true
    } else if (c === 38) {
      inColor = true
      // Skip extended foreground color payload: 38;5;n or 38;2;r;g;b
      if (codes[i + 1] === 5) i += 2
      else if (codes[i + 1] === 2) i += 4
    } else if (c === 48) {
      // Skip extended background color payload so its components aren't misread as SGR codes
      if (codes[i + 1] === 5) i += 2
      else if (codes[i + 1] === 2) i += 4
    }
  }
  return inColor
}

export interface LogHighlighter {
  highlight: (chunk: string) => string
  reset: () => string
}

/**
 * Stateful ANSI/SGR log keyword highlighter that preserves incomplete trailing
 * escape sequences and foreground color state across streaming terminal chunks.
 */
export function createLogHighlighter(): LogHighlighter {
  let pending = ''
  let inColor = false

  return {
    highlight(chunk: string): string {
      if (!chunk) return ''
      const combined = pending ? pending + chunk : chunk
      pending = ''

      if (combined.length > MAX_HIGHLIGHT_CHUNK_CHARS) {
        // Large burst output: bypass keyword highlighting to avoid UI stalls.
        // Scan only complete ANSI tokens to keep color state reasonably accurate.
        ANSI_TOKEN_RE.lastIndex = 0
        let m: RegExpExecArray | null
        while ((m = ANSI_TOKEN_RE.exec(combined)) !== null) {
          inColor = updateSgrColorState(m[0], inColor)
        }
        return combined
      }

      let body = combined
      const tailMatch = INCOMPLETE_TRAILING_ANSI_RE.exec(combined)
      if (tailMatch && tailMatch[0].length <= MAX_PENDING_ANSI_CHARS) {
        pending = tailMatch[0]
        body = combined.slice(0, tailMatch.index)
        if (!body) return ''
      }

      const hasEsc = body.indexOf('\x1b') !== -1
      const hasCandidate = /[efpwosad0-9]/i.test(body)
      if (!hasEsc) {
        if (inColor || !hasCandidate) return body
        return body.replace(HIGHLIGHT_RE, (m) => `${pickColor(m)}${m}${FG_RESET}`)
      }

      const parts = body.split(ANSI_TOKEN_RE)
      let changed = false

      for (let i = 0; i < parts.length; i++) {
        const part = parts[i]
        if (!part) continue
        if (part.charCodeAt(0) === 0x1b) {
          inColor = updateSgrColorState(part, inColor)
          continue
        }
        if (inColor || !hasCandidate) continue

        const replaced = part.replace(HIGHLIGHT_RE, (m) => {
          changed = true
          return `${pickColor(m)}${m}${FG_RESET}`
        })
        parts[i] = replaced
      }

      return changed ? parts.join('') : body
    },

    reset(): string {
      const tail = pending
      pending = ''
      inColor = false
      return tail
    },
  }
}

/**
 * Applies soft ANSI coloring to uncolored log keywords (errors, warnings, ok, IPv4)
 * while preserving all existing ANSI escape sequences and skipping already-colored text.
 */
export function highlightLogText(text: string): string {
  if (!text) return text
  const hl = createLogHighlighter()
  return hl.highlight(text) + hl.reset()
}

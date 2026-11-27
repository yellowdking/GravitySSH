const HIGHLIGHT_RE =
  /(\b(?:error|fatal|panic|failed|failure|exception|critical|denied)\b|\b(?:warn|warning|deprecated|timeout|retry)\b|\b(?:ok|success|succeeded|passed|healthy)\b|active \(running\)|\b(?:(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\.){3}(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\b)/gi

const RED = '\x1b[38;5;203m'
const AMBER = '\x1b[38;5;221m'
const GREEN = '\x1b[38;5;114m'
const CYAN = '\x1b[38;5;117m'
const FG_RESET = '\x1b[39m'

const MAX_HIGHLIGHT_CHUNK_CHARS = 16384
const MAX_PENDING_ANSI_CHARS = 256

type StringMode = 'none' | 'osc' | 'st'

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
  if (!/^[0-9;:]*$/.test(body)) return currentInColor

  const parts = body.split(';')
  let inColor = currentInColor
  for (let i = 0; i < parts.length; i++) {
    const part = parts[i]
    if (part.includes(':')) {
      // ISO-8613-3 colon-delimited subparameters (e.g. 38:5:203, 38:2::r:g:b, 48:5:236, 4:3)
      const sub = part.split(':')
      const c = sub[0] === '' ? 0 : Number(sub[0])
      if (c === 0 || c === 39) inColor = false
      else if ((c >= 30 && c <= 37) || (c >= 90 && c <= 97) || c === 38) inColor = true
      continue
    }
    const c = part === '' ? 0 : Number(part)
    if (c === 0 || c === 39) {
      inColor = false
    } else if ((c >= 30 && c <= 37) || (c >= 90 && c <= 97)) {
      inColor = true
    } else if (c === 38) {
      inColor = true
      // Skip extended foreground color payload: 38;5;n or 38;2;r;g;b
      const next = parts[i + 1]
      if (next === '5') i += 2
      else if (next === '2') i += 4
    } else if (c === 48 || c === 58) {
      // Skip extended background / underline color payload so its components aren't misread as SGR codes
      const next = parts[i + 1]
      if (next === '5') i += 2
      else if (next === '2') i += 4
    }
  }
  return inColor
}

function isCsiParamByte(code: number): boolean {
  // ECMA-48 CSI parameter bytes: 0x30–0x3F ([0-9:;<=>?])
  return code >= 0x30 && code <= 0x3f
}

function isIntermediateByte(code: number): boolean {
  // ECMA-48 intermediate bytes: 0x20–0x2F ([ -/])
  return code >= 0x20 && code <= 0x2f
}

function isCsiFinalByte(code: number): boolean {
  // ECMA-48 CSI final bytes: 0x40–0x7E ([@-~])
  return code >= 0x40 && code <= 0x7e
}

export interface LogHighlighter {
  highlight: (chunk: string) => string
  reset: () => string
}

/**
 * Stateful ANSI/SGR log keyword highlighter that preserves incomplete trailing
 * escape sequences, control strings (OSC/DCS/APC/PM/SOS), and foreground color state
 * across streaming terminal chunks.
 */
export function createLogHighlighter(): LogHighlighter {
  let pending = ''
  let inColor = false
  let stringMode: StringMode = 'none'

  function scanChunk(text: string, allowHighlight: boolean): string {
    const len = text.length
    const hasCandidate = allowHighlight && /[efpwosad0-9]/i.test(text)

    // Fast path: plain text with no escape sequences and not inside a control string.
    if (stringMode === 'none' && text.indexOf('\x1b') === -1) {
      if (!allowHighlight || inColor || !hasCandidate) return text
      return text.replace(HIGHLIGHT_RE, (m) => `${pickColor(m)}${m}${FG_RESET}`)
    }

    const out: string[] = []
    let pos = 0
    let changed = false

    while (pos < len) {
      if (stringMode !== 'none') {
        const allowBel = stringMode === 'osc'
        let end = -1
        let trailEsc = false
        for (let i = pos; i < len; i++) {
          const code = text.charCodeAt(i)
          if (code === 0x18 || code === 0x1a || (allowBel && code === 0x07)) {
            end = i + 1
            break
          }
          if (code === 0x1b) {
            if (i + 1 >= len) {
              trailEsc = true
              break
            }
            if (text.charCodeAt(i + 1) === 0x5c) {
              // ST (\x1b\\) terminates OSC/DCS/APC/PM/SOS
              end = i + 2
              break
            }
          }
        }
        if (end !== -1) {
          if (allowHighlight) out.push(text.slice(pos, end))
          stringMode = 'none'
          pos = end
          continue
        }
        if (trailEsc && allowHighlight) {
          if (len - 1 > pos) out.push(text.slice(pos, len - 1))
          pending = '\x1b'
          changed = true
          break
        }
        if (allowHighlight) out.push(text.slice(pos))
        break
      }

      const escIdx = text.indexOf('\x1b', pos)
      const segEnd = escIdx === -1 ? len : escIdx
      if (segEnd > pos && allowHighlight) {
        const seg = text.slice(pos, segEnd)
        if (!inColor && hasCandidate) {
          const replaced = seg.replace(HIGHLIGHT_RE, (m) => {
            changed = true
            return `${pickColor(m)}${m}${FG_RESET}`
          })
          out.push(replaced)
        } else {
          out.push(seg)
        }
      }
      if (escIdx === -1) break

      if (escIdx + 1 >= len) {
        if (allowHighlight) {
          pending = '\x1b'
          changed = true
        }
        break
      }

      const ch1 = text.charCodeAt(escIdx + 1)
      if (ch1 === 0x5b) {
        // CSI: \x1b[ [0-9:;<=>?]* [ -/]* [@-~]
        let j = escIdx + 2
        while (j < len && isCsiParamByte(text.charCodeAt(j))) j++
        while (j < len && isIntermediateByte(text.charCodeAt(j))) j++
        if (j >= len) {
          if (allowHighlight && len - escIdx <= MAX_PENDING_ANSI_CHARS) {
            pending = text.slice(escIdx)
            changed = true
          } else if (allowHighlight) {
            out.push(text.slice(escIdx))
          }
          break
        }
        if (isCsiFinalByte(text.charCodeAt(j))) {
          const seq = text.slice(escIdx, j + 1)
          inColor = updateSgrColorState(seq, inColor)
          if (allowHighlight) out.push(seq)
          pos = j + 1
        } else {
          // Malformed CSI byte: emit up to the invalid byte and continue scanning from it.
          if (allowHighlight) out.push(text.slice(escIdx, j))
          pos = j
        }
        continue
      }

      if (ch1 === 0x5d) {
        // OSC introducer: \x1b]
        if (allowHighlight) out.push('\x1b]')
        stringMode = 'osc'
        pos = escIdx + 2
        continue
      }

      if (ch1 === 0x50 || ch1 === 0x58 || ch1 === 0x5e || ch1 === 0x5f) {
        // DCS (\x1bP), SOS (\x1bX), PM (\x1b^), APC (\x1b_)
        if (allowHighlight) out.push(text.slice(escIdx, escIdx + 2))
        stringMode = 'st'
        pos = escIdx + 2
        continue
      }

      if (isIntermediateByte(ch1)) {
        // nF escape sequence (e.g. \x1b(B, \x1b#8): \x1b [ -/]+ [0-~]
        let j = escIdx + 2
        while (j < len && isIntermediateByte(text.charCodeAt(j))) j++
        if (j >= len) {
          if (allowHighlight && len - escIdx <= MAX_PENDING_ANSI_CHARS) {
            pending = text.slice(escIdx)
            changed = true
          } else if (allowHighlight) {
            out.push(text.slice(escIdx))
          }
          break
        }
        const finalCode = text.charCodeAt(j)
        if (finalCode >= 0x30 && finalCode <= 0x7e) {
          if (allowHighlight) out.push(text.slice(escIdx, j + 1))
          pos = j + 1
        } else {
          if (allowHighlight) out.push(text.slice(escIdx, j))
          pos = j
        }
        continue
      }

      if (ch1 >= 0x30 && ch1 <= 0x7e) {
        // 2-byte ESC sequence (excluding '[', ']', 'P', 'X', '^', '_' handled above)
        if (allowHighlight) out.push(text.slice(escIdx, escIdx + 2))
        pos = escIdx + 2
        continue
      }

      // Bare ESC followed by a C0 control byte
      if (allowHighlight) out.push('\x1b')
      pos = escIdx + 1
    }

    if (!allowHighlight) return text
    return changed ? out.join('') : text
  }

  return {
    highlight(chunk: string): string {
      if (!chunk) return ''
      const combined = pending ? pending + chunk : chunk
      pending = ''

      if (combined.length > MAX_HIGHLIGHT_CHUNK_CHARS) {
        // Large burst output: bypass keyword highlighting to avoid UI stalls,
        // while keeping SGR color & control-string parser state accurate.
        return scanChunk(combined, false)
      }

      return scanChunk(combined, true)
    },

    reset(): string {
      const tail = pending
      pending = ''
      inColor = false
      stringMode = 'none'
      return tail
    },
  }
}

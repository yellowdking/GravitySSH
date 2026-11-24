const ANSI_TOKEN_RE = /(\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_])/g

const HIGHLIGHT_RE =
  /(\b(?:error|fatal|panic|failed|failure|exception|critical|denied)\b|\b(?:warn|warning|deprecated|timeout|retry)\b|\b(?:ok|success|succeeded|passed|healthy)\b|active \(running\)|\b(?:(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\.){3}(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\b)/gi

const RED = '\x1b[38;5;203m'
const AMBER = '\x1b[38;5;221m'
const GREEN = '\x1b[38;5;114m'
const CYAN = '\x1b[38;5;117m'
const RESET = '\x1b[0m'

function pickColor(match: string): string {
  const lower = match.toLowerCase()
  if (/^\d+\.\d+\.\d+\.\d+$/.test(lower)) return CYAN
  if (lower === 'active (running)' || lower === 'ok' || lower === 'success' || lower === 'succeeded' || lower === 'passed' || lower === 'healthy') {
    return GREEN
  }
  if (lower === 'warn' || lower === 'warning' || lower === 'deprecated' || lower === 'timeout' || lower === 'retry') {
    return AMBER
  }
  return RED
}

function isSgrReset(seq: string): boolean {
  return seq === '\x1b[0m' || seq === '\x1b[m' || seq === '\x1b[39m' || seq === '\x1b[0;39m'
}

function isSgrColorSet(seq: string): boolean {
  if (!seq.startsWith('\x1b[') || !seq.endsWith('m')) return false
  return !isSgrReset(seq)
}

/**
 * Applies soft ANSI coloring to uncolored log keywords (errors, warnings, ok, IPv4)
 * while preserving all existing ANSI escape sequences and skipping already-colored text.
 */
export function highlightLogText(text: string): string {
  if (!text) return text
  // Fast exit if no candidate characters exist
  if (!/[efpwosad0-9]/i.test(text)) return text

  const parts = text.split(ANSI_TOKEN_RE)
  let inColor = false
  let changed = false

  for (let i = 0; i < parts.length; i++) {
    const part = parts[i]
    if (!part) continue
    if (part.charCodeAt(0) === 0x1b) {
      if (isSgrReset(part)) inColor = false
      else if (isSgrColorSet(part)) inColor = true
      continue
    }
    if (inColor) continue

    const replaced = part.replace(HIGHLIGHT_RE, (m) => {
      changed = true
      return `${pickColor(m)}${m}${RESET}`
    })
    parts[i] = replaced
  }

  return changed ? parts.join('') : text
}

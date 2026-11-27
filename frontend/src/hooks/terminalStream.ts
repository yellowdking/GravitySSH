export interface TerminalActions {
  pasteText: (text: string) => void
  resetInputLine: () => void
}

interface BufferedStream {
  chunks: string[]
  chars: number
}

const MAX_BUFFER_CHARS = 262144
const MAX_BUFFERED_SESSIONS = 32
const STRICT_REPLAY_TTL_MS = 500

const listeners = new Map<string, (data: string) => void>()
const earlyBuffers = new Map<string, BufferedStream>()
const discardedSessions = new Set<string>()

function enqueueEarly(sessionId: string, data: string) {
  let buf = earlyBuffers.get(sessionId)
  if (!buf) {
    if (earlyBuffers.size >= MAX_BUFFERED_SESSIONS) {
      const oldest = earlyBuffers.keys().next().value
      if (oldest !== undefined) earlyBuffers.delete(oldest)
    }
    buf = { chunks: [], chars: 0 }
    earlyBuffers.set(sessionId, buf)
  }
  buf.chunks.push(data)
  buf.chars += data.length
  while (buf.chars > MAX_BUFFER_CHARS && buf.chunks.length > 1) {
    const dropped = buf.chunks.shift()!
    buf.chars -= dropped.length
  }
}

export function pushTerminalData(sessionId: string, data: string) {
  if (!sessionId || !data || discardedSessions.has(sessionId)) return
  const listener = listeners.get(sessionId)
  if (listener) {
    listener(data)
    return
  }
  enqueueEarly(sessionId, data)
}

export function attachTerminalListener(
  sessionId: string,
  onData: (data: string) => void
): () => void {
  discardedSessions.delete(sessionId)

  const buffered = earlyBuffers.get(sessionId)
  earlyBuffers.delete(sessionId)

  // Preserve drained initial chunks briefly so React 18 StrictMode double-mount in dev
  // does not lose early connection output before live chunks arrive.
  let strictReplay: string[] | null = buffered && buffered.chunks.length > 0 ? [...buffered.chunks] : null
  const replayTimer = strictReplay
    ? setTimeout(() => {
        strictReplay = null
      }, STRICT_REPLAY_TTL_MS)
    : null

  const wrapped = (data: string) => {
    strictReplay = null
    onData(data)
  }

  listeners.set(sessionId, wrapped)

  if (buffered) {
    for (const chunk of buffered.chunks) {
      onData(chunk)
    }
  }

  return () => {
    if (replayTimer !== null) clearTimeout(replayTimer)
    if (listeners.get(sessionId) === wrapped) {
      listeners.delete(sessionId)
    }
    if (strictReplay && strictReplay.length > 0 && !discardedSessions.has(sessionId) && !listeners.has(sessionId)) {
      for (const chunk of strictReplay) {
        enqueueEarly(sessionId, chunk)
      }
    }
  }
}

export function discardTerminalBuffer(sessionId: string) {
  if (!sessionId) return
  discardedSessions.add(sessionId)
  if (discardedSessions.size > 200) {
    const oldest = discardedSessions.values().next().value
    if (oldest !== undefined) discardedSessions.delete(oldest)
  }
  earlyBuffers.delete(sessionId)
  listeners.delete(sessionId)
}

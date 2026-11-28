import { useCallback, useEffect, useRef, type MutableRefObject } from 'react'
import type { Terminal } from '@xterm/xterm'
import type { FitAddon } from '@xterm/addon-fit'
import type { SearchAddon } from '@xterm/addon-search'
import type { AppSettings, CompletionItem } from '../types'
import { api } from '../wailsApi'
import { applyTerminalInputToLine, sanitizePasteText } from '../utils/autocomplete'
import { copyToClipboard } from '../utils/errors'
import { inspectPasteSafety } from '../utils/pasteGuard'
import { createLogHighlighter } from '../utils/keywordHighlight'
import { base64ToUint8Array, DEFAULT_TERMINAL_FONT } from '../components/terminalTheme'
import { attachTerminalImeGuard as bindIme, attachTerminalInputHandlers as bindInput, createConfiguredTerminal as initTerm, syncTerminalImeAttrs as syncIme } from './terminalHandlers'
import { attachTerminalListener } from './terminalStream'

function isTerminalFocusBlocked(termTextarea?: HTMLTextAreaElement | null): boolean {
  if (document.querySelector('[role="dialog"]')) return true
  const activeEl = document.activeElement
  if (
    activeEl &&
    activeEl !== document.body &&
    activeEl !== termTextarea &&
    (activeEl.tagName === 'INPUT' ||
      activeEl.tagName === 'TEXTAREA' ||
      activeEl.tagName === 'SELECT' ||
      (activeEl as HTMLElement).isContentEditable)
  ) {
    return true
  }
  return false
}

interface UseTerminalOptions {
  sessionId: string
  status: string
  active: boolean
  showSftp: boolean
  rightDrawer: string
  settings: AppSettings
  suggestionsRef: MutableRefObject<CompletionItem[]>
  inputLineRef: MutableRefObject<string>
  onRequestSuggestions: (l: string) => void
  onClearSuggestions: () => void
  onResetAutocomplete: () => void
  onMoveSelection: (d: number) => void
  onApplySelected: () => void
  onOpenSearch: () => void
  onPasteGuard: (t: string, bracketedPasteActive: boolean) => void
}

export function useTerminalInstance(opts: UseTerminalOptions) {
  const { sessionId, status, active, showSftp, rightDrawer, settings } = opts
  const containerRef = useRef<HTMLDivElement | null>(null)
  const termRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const searchAddonRef = useRef<SearchAddon | null>(null)
  const imeRef = useRef<((f?: boolean) => void) | null>(null)
  const lastSizeRef = useRef<{ cols: number; rows: number } | null>(null)
  const sRef = useRef(settings)
  sRef.current = settings
  const cRef = useRef(opts)
  cRef.current = opts

  const enqueueWrite = useCallback((d: string) => {
    if (d) void api.writeTerminal(sessionId, d).catch(() => {})
  }, [sessionId])

  const sendResize = useCallback((cols: number, rows: number) => {
    if (cols <= 0 || rows <= 0) return
    const prev = lastSizeRef.current
    if (prev && prev.cols === cols && prev.rows === rows) return
    lastSizeRef.current = { cols, rows }
    void api.resizeTerminal(sessionId, cols, rows).catch(() => {})
  }, [sessionId])

  const doPaste = useCallback((raw: string, guarded: boolean) => {
    const t = termRef.current
    const s = sanitizePasteText(raw)
    if (!t || !s) return
    if (guarded && sRef.current.pasteGuardEnabled) {
      const bracketed = t.modes.bracketedPasteMode
      const c = inspectPasteSafety(s, bracketed)
      if (c.needsConfirm) {
        cRef.current.onPasteGuard(s, bracketed)
        return
      }
    }
    t.paste(s)
    if (cRef.current.active && !isTerminalFocusBlocked(t.textarea)) {
      t.focus()
    } else {
      requestAnimationFrame(() => {
        const curTerm = termRef.current
        if (curTerm && cRef.current.active && !isTerminalFocusBlocked(curTerm.textarea)) {
          curTerm.focus()
        }
      })
    }
  }, [])
  const commitPaste = useCallback((r: string) => doPaste(r, false), [doPaste])
  const pasteText = useCallback((r: string) => doPaste(r, true), [doPaste])

  const syncSize = useCallback(() => {
    const f = fitRef.current
    const t = termRef.current
    const el = containerRef.current
    if (!f || !t || !el || el.clientWidth === 0 || el.clientHeight === 0) return
    try {
      const proposed = f.proposeDimensions()
      if (proposed && proposed.cols === t.cols && proposed.rows === t.rows) {
        sendResize(t.cols, t.rows)
        return
      }
      f.fit()
      sendResize(t.cols, t.rows)
    } catch {
      /**/
    }
  }, [sendResize])

  useEffect(() => {
    const t = termRef.current
    if (t) {
      t.options.fontSize = settings.terminalFontSize
      t.options.fontFamily = settings.terminalFontFamily || DEFAULT_TERMINAL_FONT
      if (cRef.current.active) syncSize()
    }
  }, [settings.terminalFontSize, settings.terminalFontFamily, syncSize])

  useEffect(() => {
    syncIme(termRef.current, settings.autoSwitchEnglishIme)
    if (active && settings.autoSwitchEnglishIme && !isTerminalFocusBlocked(termRef.current?.textarea)) {
      imeRef.current?.(true)
    }
  }, [settings.autoSwitchEnglishIme, active])

  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    lastSizeRef.current = null
    const { term, fit, searchAddon } = initTerm(el, sRef.current)
    termRef.current = term
    fitRef.current = fit
    searchAddonRef.current = searchAddon
    sendResize(term.cols, term.rows)

    let dec = new TextDecoder('utf-8')
    let decActive = false
    const hl = createLogHighlighter()
    let wasHighlighting = false
    let inAltScreen = term.buffer.active.type === 'alternate'
    let pendingWrites = 0
    let escTail = ''
    const ALT_SCREEN_RE = /\x1b\[\?(?:[0-9]+;)*(?:47|1047|1049)(?:;[0-9]+)*([hl])/g
    const INCOMPLETE_ALT_TAIL_RE = /\x1b(?:\[(?:\?[0-9;]{0,64})?)?$/

    const queuedWrite = (payload: string | Uint8Array) => {
      if (typeof payload === 'string' ? payload.length === 0 : payload.byteLength === 0) return
      pendingWrites++
      term.write(payload, () => {
        pendingWrites = Math.max(0, pendingWrites - 1)
        if (pendingWrites === 0) {
          inAltScreen = term.buffer.active.type === 'alternate'
        }
      })
    }

    const flushHighlighter = () => {
      if (!wasHighlighting) return
      wasHighlighting = false
      const hlTail = hl.reset()
      if (hlTail) queuedWrite(hlTail)
    }

    const flushPipelineForRawMode = () => {
      // Chronological order: hl.pending (oldest) -> escTail -> trailing UTF-8 decoder bytes
      if (wasHighlighting) {
        wasHighlighting = false
        const hlTail = hl.reset()
        if (hlTail) queuedWrite(hlTail)
      }
      if (escTail) {
        const tail = escTail
        escTail = ''
        queuedWrite(tail)
      }
      if (decActive) {
        const decodedTail = dec.decode()
        decActive = false
        dec = new TextDecoder('utf-8')
        if (decodedTail) queuedWrite(decodedTail)
      }
    }

    const onBufferChange = term.buffer.onBufferChange((buf) => {
      const isAlt = buf.type === 'alternate'
      if (pendingWrites === 0) {
        inAltScreen = isAlt
      }
      if (isAlt) {
        cRef.current.inputLineRef.current = ''
        cRef.current.onResetAutocomplete()
      }
    })

    const onCopy = (e: ClipboardEvent) => {
      const hasSel = typeof term.hasSelection === 'function' ? term.hasSelection() : Boolean(term.getSelection())
      if (!hasSel) return
      const sel = term.getSelection()
      if (!sel) return
      if (e.clipboardData) {
        e.clipboardData.setData('text/plain', sel)
        e.preventDefault()
        e.stopImmediatePropagation()
      } else {
        e.preventDefault()
        void copyToClipboard(sel)
      }
    }
    const onPaste = (e: ClipboardEvent) => {
      const t = e.clipboardData?.getData('text/plain')
      if (t) {
        e.preventDefault()
        e.stopImmediatePropagation()
        pasteText(t)
      }
    }
    el.addEventListener('copy', onCopy, true)
    el.addEventListener('paste', onPaste, true)
    bindInput({
      term,
      enqueueWrite,
      pasteText,
      suggestionsRef: cRef.current.suggestionsRef,
      onOpenSearch: () => cRef.current.onOpenSearch(),
      onMoveSelection: (d) => cRef.current.onMoveSelection(d),
      onApplySelected: () => cRef.current.onApplySelected(),
      onClearSuggestions: () => cRef.current.onClearSuggestions(),
    })
    const ime = bindIme(el, term, () => cRef.current.active, () => sRef.current.autoSwitchEnglishIme)
    imeRef.current = ime.trigger
    const onData = term.onData((d) => {
      enqueueWrite(d)
      const c = cRef.current
      if (term.buffer.active.type === 'alternate') {
        c.inputLineRef.current = ''
        c.onResetAutocomplete()
        return
      }
      const n = applyTerminalInputToLine(c.inputLineRef.current, d)
      c.inputLineRef.current = n
      if (sRef.current.autocompleteEnabled && n.trimStart()) c.onRequestSuggestions(n)
      else c.onClearSuggestions()
    })
    const onResize = term.onResize(({ cols, rows }) => {
      sendResize(cols, rows)
    })
    const unsub = attachTerminalListener(sessionId, (data) => {
      if (!data) return
      const b = base64ToUint8Array(data)
      if (!sRef.current.keywordHighlightEnabled) {
        flushPipelineForRawMode()
        queuedWrite(b)
        return
      }
      if (inAltScreen && !escTail && b.indexOf(0x1b) === -1) {
        decActive = true
        const text = dec.decode(b, { stream: true })
        if (text) queuedWrite(text)
        return
      }

      decActive = true
      let chunk = escTail + dec.decode(b, { stream: true })
      escTail = ''
      const tailMatch = INCOMPLETE_ALT_TAIL_RE.exec(chunk)
      if (tailMatch) {
        escTail = tailMatch[0]
        chunk = chunk.slice(0, tailMatch.index)
      }
      if (!chunk) return

      ALT_SCREEN_RE.lastIndex = 0
      let cursor = 0
      let match: RegExpExecArray | null
      while ((match = ALT_SCREEN_RE.exec(chunk)) !== null) {
        const seg = chunk.slice(cursor, match.index)
        if (seg) {
          if (inAltScreen) {
            queuedWrite(seg)
          } else {
            wasHighlighting = true
            const out = hl.highlight(seg)
            if (out) queuedWrite(out)
          }
        }
        if (match[1] === 'h') {
          if (!inAltScreen) flushHighlighter()
          inAltScreen = true
          cRef.current.inputLineRef.current = ''
          cRef.current.onResetAutocomplete()
        } else {
          inAltScreen = false
        }
        queuedWrite(match[0])
        cursor = match.index + match[0].length
      }

      const rest = chunk.slice(cursor)
      if (rest) {
        if (inAltScreen) {
          queuedWrite(rest)
        } else {
          wasHighlighting = true
          const out = hl.highlight(rest)
          if (out) queuedWrite(out)
        }
      }
    })
    let roT = 0
    const ro = new ResizeObserver(() => {
      if (!cRef.current.active) return
      clearTimeout(roT)
      roT = window.setTimeout(() => {
        if (!cRef.current.active || el.clientWidth === 0 || el.clientHeight === 0) return
        try {
          const proposed = fit.proposeDimensions()
          if (proposed && proposed.cols === term.cols && proposed.rows === term.rows) return
          fit.fit()
        } catch {
          /**/
        }
      }, 80)
    })
    ro.observe(el)
    return () => {
      clearTimeout(roT)
      el.removeEventListener('copy', onCopy, true)
      el.removeEventListener('paste', onPaste, true)
      ime.detach()
      imeRef.current = null
      ro.disconnect()
      unsub()
      onBufferChange.dispose()
      onData.dispose()
      onResize.dispose()
      term.dispose()
      termRef.current = null
      fitRef.current = null
      searchAddonRef.current = null
      lastSizeRef.current = null
    }
  }, [sessionId, enqueueWrite, pasteText, sendResize])

  useEffect(() => {
    if (status !== 'connected') return
    if (active) syncSize()
    const t = setTimeout(() => {
      if (cRef.current.active) syncSize()
      if (active && !isTerminalFocusBlocked(termRef.current?.textarea)) {
        termRef.current?.focus()
        imeRef.current?.(true)
      }
    }, 120)
    return () => clearTimeout(t)
  }, [status, active, syncSize])

  useEffect(() => {
    if (!active) return
    const rafId = requestAnimationFrame(() => {
      if (cRef.current.active) syncSize()
    })
    const id = setTimeout(() => {
      if (!cRef.current.active) return
      syncSize()
      if (!isTerminalFocusBlocked(termRef.current?.textarea)) {
        termRef.current?.focus()
        imeRef.current?.(true)
      }
    }, 40)
    return () => {
      cancelAnimationFrame(rafId)
      clearTimeout(id)
    }
  }, [active, showSftp, rightDrawer, settings.monitorBarEnabled, syncSize])

  return { containerRef, termRef, searchAddonRef, pasteText, commitPaste }
}

import { useCallback, useEffect, useRef, type MutableRefObject } from 'react'
import type { Terminal } from '@xterm/xterm'
import type { FitAddon } from '@xterm/addon-fit'
import type { SearchAddon } from '@xterm/addon-search'
import type { AppSettings, CompletionItem } from '../types'
import { api } from '../wailsApi'
import { applyTerminalInputToLine, sanitizePasteText } from '../utils/autocomplete'
import { inspectPasteSafety, type PasteInspection } from '../utils/pasteGuard'
import { createLogHighlighter } from '../utils/keywordHighlight'
import { base64ToUint8Array, DEFAULT_TERMINAL_FONT } from '../components/terminalTheme'
import { attachTerminalImeGuard as bindIme, attachTerminalInputHandlers as bindInput, createConfiguredTerminal as initTerm, syncTerminalImeAttrs as syncIme } from './terminalHandlers'

interface UseTerminalOptions {
  sessionId: string
  status: string
  active: boolean
  showSftp: boolean
  rightDrawer: string
  settings: AppSettings
  suggestionsRef: MutableRefObject<CompletionItem[]>
  selectedIdxRef: MutableRefObject<number>
  inputLineRef: MutableRefObject<string>
  onRequestSuggestions: (l: string) => void
  onClearSuggestions: () => void
  onMoveSelection: (d: number) => void
  onApplySelected: () => void
  onOpenSearch: () => void
  onPasteGuard: (t: string, i: PasteInspection, bracketedPasteActive: boolean) => void
}

export function useTerminalInstance(opts: UseTerminalOptions) {
  const { sessionId, status, active, showSftp, rightDrawer, settings } = opts
  const containerRef = useRef<HTMLDivElement | null>(null)
  const termRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const searchAddonRef = useRef<SearchAddon | null>(null)
  const imeRef = useRef<((f?: boolean) => void) | null>(null)
  const wq = useRef(Promise.resolve())
  const sRef = useRef(settings)
  sRef.current = settings
  const cRef = useRef(opts)
  cRef.current = opts

  const enqueueWrite = useCallback((d: string) => {
    if (d) wq.current = wq.current.then(() => api.writeTerminal(sessionId, d)).catch(() => {})
  }, [sessionId])

  const doPaste = useCallback((raw: string, guarded: boolean) => {
    const t = termRef.current
    const s = sanitizePasteText(raw)
    if (!t || !s) return
    if (guarded && sRef.current.pasteGuardEnabled) {
      const bracketed = t.modes.bracketedPasteMode
      const c = inspectPasteSafety(s, bracketed)
      if (c.needsConfirm) {
        cRef.current.onPasteGuard(s, c, bracketed)
        return
      }
    }
    t.paste(s)
    t.focus()
  }, [])
  const commitPaste = useCallback((r: string) => doPaste(r, false), [doPaste])
  const pasteText = useCallback((r: string) => doPaste(r, true), [doPaste])

  const syncSize = useCallback(() => {
    const f = fitRef.current
    const t = termRef.current
    if (f && t) {
      try {
        f.fit()
        if (t.cols > 0 && t.rows > 0) void api.resizeTerminal(sessionId, t.cols, t.rows)
      } catch {
        /**/
      }
    }
  }, [sessionId])

  useEffect(() => {
    const t = termRef.current
    if (t) {
      t.options.fontSize = settings.terminalFontSize
      t.options.fontFamily = settings.terminalFontFamily || DEFAULT_TERMINAL_FONT
      syncSize()
    }
  }, [settings.terminalFontSize, settings.terminalFontFamily, syncSize])

  useEffect(() => {
    syncIme(termRef.current, settings.autoSwitchEnglishIme)
    if (active && settings.autoSwitchEnglishIme) imeRef.current?.(true)
  }, [settings.autoSwitchEnglishIme, active])

  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const { term, fit, searchAddon } = initTerm(el, sRef.current)
    termRef.current = term
    fitRef.current = fit
    searchAddonRef.current = searchAddon
    if (term.cols > 0 && term.rows > 0) void api.resizeTerminal(sessionId, term.cols, term.rows)

    let dec = new TextDecoder('utf-8')
    const hl = createLogHighlighter()
    let wasHighlighting = false

    const flushHighlightPipeline = () => {
      if (!wasHighlighting) return
      wasHighlighting = false
      const decodedTail = dec.decode()
      const hlOut = (decodedTail ? hl.highlight(decodedTail) : '') + hl.reset()
      if (hlOut) term.write(hlOut)
      dec = new TextDecoder('utf-8')
    }

    const onPaste = (e: ClipboardEvent) => {
      const t = e.clipboardData?.getData('text/plain')
      if (t) {
        e.preventDefault()
        e.stopImmediatePropagation()
        pasteText(t)
      }
    }
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
        c.onClearSuggestions()
        return
      }
      const n = applyTerminalInputToLine(c.inputLineRef.current, d)
      c.inputLineRef.current = n
      if (sRef.current.autocompleteEnabled && n.trimStart()) c.onRequestSuggestions(n)
      else c.onClearSuggestions()
    })
    const onResize = term.onResize(({ cols, rows }) => {
      void api.resizeTerminal(sessionId, cols, rows)
    })
    const unsub = api.onEvent<{ sessionId: string; data: string }>('terminal:data', (p) => {
      if (p.sessionId !== sessionId || !p.data) return
      const b = base64ToUint8Array(p.data)
      if (sRef.current.keywordHighlightEnabled && term.buffer.active.type === 'normal') {
        wasHighlighting = true
        const s = dec.decode(b, { stream: true })
        if (s) {
          const out = hl.highlight(s)
          if (out) term.write(out)
        }
      } else {
        flushHighlightPipeline()
        term.write(b)
      }
    })
    let roT = 0
    const ro = new ResizeObserver(() => {
      clearTimeout(roT)
      roT = window.setTimeout(() => {
        try {
          fit.fit()
        } catch {
          /**/
        }
      }, 24)
    })
    ro.observe(el)
    return () => {
      clearTimeout(roT)
      el.removeEventListener('paste', onPaste, true)
      ime.detach()
      imeRef.current = null
      ro.disconnect()
      unsub()
      onData.dispose()
      onResize.dispose()
      term.dispose()
      termRef.current = null
      fitRef.current = null
      searchAddonRef.current = null
    }
  }, [sessionId, enqueueWrite, pasteText])

  useEffect(() => {
    if (status !== 'connected') return
    syncSize()
    const t = setTimeout(() => {
      syncSize()
      if (active) {
        termRef.current?.focus()
        imeRef.current?.(true)
      }
    }, 120)
    return () => clearTimeout(t)
  }, [status, active, syncSize])

  useEffect(() => {
    if (!active) return
    const id = setTimeout(() => {
      syncSize()
      termRef.current?.focus()
      imeRef.current?.(true)
    }, 40)
    return () => clearTimeout(id)
  }, [active, showSftp, rightDrawer, settings.monitorBarEnabled, syncSize])

  return { containerRef, termRef, searchAddonRef, pasteText, commitPaste }
}

import { useCallback, useEffect, useRef } from 'react'
import type { Terminal } from '@xterm/xterm'
import type { FitAddon } from '@xterm/addon-fit'
import type { SearchAddon } from '@xterm/addon-search'
import type { AppSettings, CompletionItem } from '../types'
import { api } from '../wailsApi'
import { applyTerminalInputToLine, sanitizePasteText } from '../utils/autocomplete'
import { inspectPasteSafety, type PasteInspection } from '../utils/pasteGuard'
import { highlightLogText } from '../utils/keywordHighlight'
import { base64ToUint8Array, DEFAULT_TERMINAL_FONT } from '../components/terminalTheme'
import { attachTerminalInputHandlers, createConfiguredTerminal } from './terminalHandlers'

interface UseTerminalOptions {
  sessionId: string
  status: string
  active: boolean
  showSftp: boolean
  rightDrawer: string
  settings: AppSettings
  suggestionsRef: React.MutableRefObject<CompletionItem[]>
  selectedIdxRef: React.MutableRefObject<number>
  inputLineRef: React.MutableRefObject<string>
  onRequestSuggestions: (line: string) => void
  onClearSuggestions: () => void
  onMoveSelection: (delta: number) => void
  onApplySelected: () => void
  onOpenSearch: () => void
  onPasteGuard: (text: string, inspection: PasteInspection) => void
}

export function useTerminalInstance(opts: UseTerminalOptions) {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const termRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const searchAddonRef = useRef<SearchAddon | null>(null)
  const writeQueueRef = useRef<Promise<void>>(Promise.resolve())
  const settingsRef = useRef(opts.settings); settingsRef.current = opts.settings
  const cbRef = useRef(opts); cbRef.current = opts

  const enqueueWrite = useCallback((data: string) => {
    if (!data) return
    writeQueueRef.current = writeQueueRef.current.then(() => api.writeTerminal(opts.sessionId, data)).catch(() => {})
  }, [opts.sessionId])

  const commitPaste = useCallback((raw: string) => {
    const term = termRef.current
    const cleaned = sanitizePasteText(raw)
    if (!term || !cleaned) return
    term.paste(cleaned)
    term.focus()
  }, [])

  const pasteText = useCallback((raw: string) => {
    const term = termRef.current
    const cleaned = sanitizePasteText(raw)
    if (!term || !cleaned) return
    if (settingsRef.current.pasteGuardEnabled) {
      const check = inspectPasteSafety(cleaned, term.modes.bracketedPasteMode)
      if (check.needsConfirm) { cbRef.current.onPasteGuard(cleaned, check); return }
    }
    term.paste(cleaned)
    term.focus()
  }, [])

  const syncSize = useCallback(() => {
    const fit = fitRef.current, term = termRef.current
    if (!fit || !term) return
    try {
      fit.fit()
      if (term.cols > 0 && term.rows > 0) void api.resizeTerminal(opts.sessionId, term.cols, term.rows)
    } catch { /* ignore */ }
  }, [opts.sessionId])

  useEffect(() => {
    if (!termRef.current) return
    termRef.current.options.fontSize = opts.settings.terminalFontSize
    termRef.current.options.fontFamily = opts.settings.terminalFontFamily || DEFAULT_TERMINAL_FONT
    syncSize()
  }, [opts.settings.terminalFontSize, opts.settings.terminalFontFamily, syncSize])

  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const { term, fit, searchAddon } = createConfiguredTerminal(el, settingsRef.current)
    termRef.current = term; fitRef.current = fit; searchAddonRef.current = searchAddon
    if (term.cols > 0 && term.rows > 0) void api.resizeTerminal(opts.sessionId, term.cols, term.rows)

    const decoder = new TextDecoder('utf-8')
    const onNativePaste = (ev: ClipboardEvent) => {
      const text = ev.clipboardData?.getData('text/plain')
      if (!text) return
      ev.preventDefault(); ev.stopImmediatePropagation()
      pasteText(text)
    }
    el.addEventListener('paste', onNativePaste, true)

    attachTerminalInputHandlers({
      term, enqueueWrite, pasteText,
      suggestionsRef: cbRef.current.suggestionsRef,
      onOpenSearch: () => cbRef.current.onOpenSearch(),
      onMoveSelection: (d) => cbRef.current.onMoveSelection(d),
      onApplySelected: () => cbRef.current.onApplySelected(),
      onClearSuggestions: () => cbRef.current.onClearSuggestions(),
    })

    const onData = term.onData((data) => {
      enqueueWrite(data)
      if (term.buffer.active.type === 'alternate') { cbRef.current.inputLineRef.current = ''; cbRef.current.onClearSuggestions(); return }
      const nextLine = applyTerminalInputToLine(cbRef.current.inputLineRef.current, data)
      cbRef.current.inputLineRef.current = nextLine
      if (settingsRef.current.autocompleteEnabled && nextLine.trimStart().length > 0) cbRef.current.onRequestSuggestions(nextLine)
      else cbRef.current.onClearSuggestions()
    })
    const onResize = term.onResize(({ cols, rows }) => { void api.resizeTerminal(opts.sessionId, cols, rows) })
    const unsubData = api.onEvent<{ sessionId: string; data: string }>('terminal:data', (p) => {
      if (p.sessionId !== opts.sessionId || !p.data) return
      const bytes = base64ToUint8Array(p.data)
      if (settingsRef.current.keywordHighlightEnabled && term.buffer.active.type === 'normal') {
        const text = decoder.decode(bytes, { stream: true })
        if (text) term.write(highlightLogText(text))
      } else {
        term.write(bytes)
      }
    })

    let roTimer: ReturnType<typeof setTimeout> | null = null
    const ro = new ResizeObserver(() => {
      if (roTimer) clearTimeout(roTimer)
      roTimer = setTimeout(() => { try { fit.fit() } catch { /* ignore */ } }, 24)
    })
    ro.observe(el)
    return () => {
      if (roTimer) clearTimeout(roTimer)
      el.removeEventListener('paste', onNativePaste, true)
      ro.disconnect(); unsubData(); onData.dispose(); onResize.dispose(); term.dispose()
      termRef.current = null; fitRef.current = null
    }
  }, [opts.sessionId, enqueueWrite, pasteText])

  useEffect(() => {
    if (opts.status !== 'connected') return
    syncSize()
    const t = setTimeout(() => { syncSize(); if (opts.active) termRef.current?.focus() }, 120)
    return () => clearTimeout(t)
  }, [opts.status, opts.active, syncSize])

  useEffect(() => {
    if (!opts.active) return
    const id = setTimeout(() => { syncSize(); termRef.current?.focus() }, 40)
    return () => clearTimeout(id)
  }, [opts.active, opts.showSftp, opts.rightDrawer, opts.settings.monitorBarEnabled, syncSize])

  return { containerRef, termRef, searchAddonRef, pasteText, commitPaste }
}

import { useCallback, useEffect, useRef } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { SearchAddon } from '@xterm/addon-search'
import { Unicode11Addon } from '@xterm/addon-unicode11'
import type { AppSettings, CompletionItem } from '../types'
import { api } from '../wailsApi'
import { applyTerminalInputToLine, sanitizePasteText } from '../utils/autocomplete'
import { ANTIGRAVITY_TERM_THEME, base64ToUint8Array, DEFAULT_TERMINAL_FONT } from '../components/terminalTheme'

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

  const pasteText = useCallback((raw: string) => {
    const term = termRef.current
    const cleaned = sanitizePasteText(raw)
    if (!term || !cleaned) return
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
    const term = new Terminal({
      cursorBlink: true, cursorStyle: 'bar', cursorWidth: 2,
      fontFamily: settingsRef.current.terminalFontFamily || DEFAULT_TERMINAL_FONT,
      fontSize: settingsRef.current.terminalFontSize,
      lineHeight: 1.15, letterSpacing: 0, convertEol: false, windowsMode: false,
      scrollSensitivity: 0.34, scrollback: 8000, allowProposedApi: true, theme: ANTIGRAVITY_TERM_THEME,
    })
    const fit = new FitAddon(), links = new WebLinksAddon(), searchAddon = new SearchAddon(), unicode11 = new Unicode11Addon()
    term.loadAddon(fit); term.loadAddon(links); term.loadAddon(searchAddon); term.loadAddon(unicode11)
    term.unicode.activeVersion = '11'
    term.open(el); fit.fit()
    termRef.current = term; fitRef.current = fit; searchAddonRef.current = searchAddon
    if (term.cols > 0 && term.rows > 0) void api.resizeTerminal(opts.sessionId, term.cols, term.rows)

    const onNativePaste = (ev: ClipboardEvent) => {
      const text = ev.clipboardData?.getData('text/plain')
      if (!text) return
      ev.preventDefault(); ev.stopImmediatePropagation()
      const cleaned = sanitizePasteText(text)
      if (cleaned) term.paste(cleaned)
    }
    el.addEventListener('paste', onNativePaste, true)

    term.attachCustomWheelEventHandler((ev: WheelEvent) => {
      if (ev.deltaY === 0) return true
      if (term.buffer.active.type === 'normal') { ev.preventDefault(); term.scrollLines(ev.deltaY > 0 ? 1 : -1); return false }
      if (term.modes.mouseTrackingMode === 'none') { ev.preventDefault(); enqueueWrite(ev.deltaY > 0 ? '\x1b[B' : '\x1b[A'); return false }
      return true
    })

    term.attachCustomKeyEventHandler((ev: KeyboardEvent) => {
      if (ev.type !== 'keydown') return true
      const k = ev.key.toLowerCase()
      if ((ev.ctrlKey || ev.metaKey) && k === 'f') { ev.preventDefault(); cbRef.current.onOpenSearch(); return false }
      if (ev.ctrlKey && ev.shiftKey && k === 'c') { ev.preventDefault(); const s = term.getSelection(); if (s) void navigator.clipboard.writeText(s); return false }
      if (ev.ctrlKey && ev.shiftKey && k === 'v') { ev.preventDefault(); void navigator.clipboard.readText().then((t) => { const c = sanitizePasteText(t); if (c) term.paste(c) }); return false }
      if (term.buffer.active.type === 'normal' && cbRef.current.suggestionsRef.current.length > 0) {
        if (ev.key === 'ArrowDown') { ev.preventDefault(); cbRef.current.onMoveSelection(1); return false }
        if (ev.key === 'ArrowUp') { ev.preventDefault(); cbRef.current.onMoveSelection(-1); return false }
        if (ev.key === 'Tab') { ev.preventDefault(); cbRef.current.onApplySelected(); return false }
        if (ev.key === 'Escape') { ev.preventDefault(); cbRef.current.onClearSuggestions(); return false }
      }
      return true
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
      if (p.sessionId === opts.sessionId && p.data) term.write(base64ToUint8Array(p.data))
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
  }, [opts.sessionId, enqueueWrite])

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
  }, [opts.active, opts.showSftp, opts.rightDrawer, syncSize])

  return { containerRef, termRef, searchAddonRef, pasteText }
}

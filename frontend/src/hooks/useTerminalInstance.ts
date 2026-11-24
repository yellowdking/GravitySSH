import { useCallback, useEffect, useRef } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { SearchAddon } from '@xterm/addon-search'
import { Unicode11Addon } from '@xterm/addon-unicode11'
import type { AppSettings, CompletionItem } from '../types'
import { api } from '../wailsApi'
import { applyTerminalInputToLine } from '../utils/autocomplete'
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
  const settingsRef = useRef(opts.settings)
  settingsRef.current = opts.settings

  const cbRef = useRef(opts)
  cbRef.current = opts

  const syncSize = useCallback(() => {
    const fit = fitRef.current
    const term = termRef.current
    if (!fit || !term) return
    try {
      fit.fit()
      if (term.cols > 0 && term.rows > 0) {
        void api.resizeTerminal(opts.sessionId, term.cols, term.rows)
      }
    } catch { /* ignore resize race */ }
  }, [opts.sessionId])

  useEffect(() => {
    if (termRef.current) {
      termRef.current.options.fontSize = opts.settings.terminalFontSize
      termRef.current.options.fontFamily = opts.settings.terminalFontFamily || DEFAULT_TERMINAL_FONT
      syncSize()
    }
  }, [opts.settings.terminalFontSize, opts.settings.terminalFontFamily, syncSize])

  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const term = new Terminal({
      cursorBlink: true,
      cursorStyle: 'bar',
      cursorWidth: 2,
      fontFamily: settingsRef.current.terminalFontFamily || DEFAULT_TERMINAL_FONT,
      fontSize: settingsRef.current.terminalFontSize,
      lineHeight: 1.15,
      letterSpacing: 0,
      convertEol: false,
      windowsMode: false,
      scrollSensitivity: 0.34,
      scrollback: 8000,
      allowProposedApi: true,
      theme: ANTIGRAVITY_TERM_THEME,
    })
    const fit = new FitAddon()
    const links = new WebLinksAddon()
    const searchAddon = new SearchAddon()
    const unicode11 = new Unicode11Addon()
    term.loadAddon(fit)
    term.loadAddon(links)
    term.loadAddon(searchAddon)
    term.loadAddon(unicode11)
    term.unicode.activeVersion = '11'
    term.open(el)
    fit.fit()
    termRef.current = term; fitRef.current = fit; searchAddonRef.current = searchAddon
    if (term.cols > 0 && term.rows > 0) void api.resizeTerminal(opts.sessionId, term.cols, term.rows)

    term.attachCustomWheelEventHandler((ev: WheelEvent) => {
      if (ev.deltaY === 0) return true
      if (term.buffer.active.type === 'normal') {
        ev.preventDefault()
        term.scrollLines(ev.deltaY > 0 ? 1 : -1)
        return false
      }
      if (term.modes.mouseTrackingMode === 'none') {
        ev.preventDefault()
        void api.writeTerminal(opts.sessionId, ev.deltaY > 0 ? '\x1b[B' : '\x1b[A')
        return false
      }
      return true
    })

    term.attachCustomKeyEventHandler((ev: KeyboardEvent) => {
      if (ev.type !== 'keydown') return true
      if ((ev.ctrlKey || ev.metaKey) && ev.key.toLowerCase() === 'f') { ev.preventDefault(); cbRef.current.onOpenSearch(); return false }
      if (ev.ctrlKey && ev.shiftKey && ev.key.toLowerCase() === 'c') { const sel = term.getSelection(); if (sel) void navigator.clipboard.writeText(sel); return false }
      if (ev.ctrlKey && ev.shiftKey && ev.key.toLowerCase() === 'v') { void navigator.clipboard.readText().then((t) => { if (t) void api.writeTerminal(opts.sessionId, t) }); return false }
      if (cbRef.current.suggestionsRef.current.length > 0) {
        if (ev.key === 'ArrowDown') { ev.preventDefault(); cbRef.current.onMoveSelection(1); return false }
        if (ev.key === 'ArrowUp') { ev.preventDefault(); cbRef.current.onMoveSelection(-1); return false }
        if (ev.key === 'Tab') { ev.preventDefault(); cbRef.current.onApplySelected(); return false }
        if (ev.key === 'Escape') { ev.preventDefault(); cbRef.current.onClearSuggestions(); return false }
      }
      return true
    })

    const onData = term.onData((data) => {
      void api.writeTerminal(opts.sessionId, data)
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
      ro.disconnect(); unsubData(); onData.dispose(); onResize.dispose(); term.dispose()
      termRef.current = null; fitRef.current = null
    }
  }, [opts.sessionId])

  useEffect(() => {
    if (opts.status !== 'connected') return
    syncSize()
    const t = setTimeout(() => {
      syncSize()
      if (opts.active) termRef.current?.focus()
    }, 120)
    return () => clearTimeout(t)
  }, [opts.status, opts.active, syncSize])

  useEffect(() => {
    if (!opts.active) return
    const id = setTimeout(() => { syncSize(); termRef.current?.focus() }, 40)
    return () => clearTimeout(id)
  }, [opts.active, opts.showSftp, opts.rightDrawer, syncSize])

  return { containerRef, termRef, searchAddonRef }
}

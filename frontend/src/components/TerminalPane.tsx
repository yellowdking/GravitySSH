import React, { useCallback, useEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { SearchAddon } from '@xterm/addon-search'
import type { AppSettings, CompletionItem, SessionInfo } from '../types'
import { api } from '../wailsApi'
import { applyTerminalInputToLine, computeCompletionInsert } from '../utils/autocomplete'
import { TerminalToolbar } from './TerminalToolbar'
import { TerminalSearchBar } from './TerminalSearchBar'
import { AutocompletePopup } from './AutocompletePopup'
import { ANTIGRAVITY_TERM_THEME, base64ToUint8Array } from './terminalTheme'

interface TerminalPaneProps {
  session: SessionInfo
  active: boolean
  settings: AppSettings
  showSftp: boolean
  rightDrawer: 'none' | 'history' | 'shortcuts'
  onToggleSftp: () => void
  onToggleRightDrawer: (mode: 'history' | 'shortcuts') => void
  onToggleAutocomplete: () => void
  onReconnect: (serverId: string) => void
}

export const TerminalPane: React.FC<TerminalPaneProps> = ({
  session, active, settings, showSftp, rightDrawer, onToggleSftp, onToggleRightDrawer, onToggleAutocomplete, onReconnect,
}) => {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const termRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const searchAddonRef = useRef<SearchAddon | null>(null)
  const inputLineRef = useRef<string>('')
  const [suggestions, setSuggestions] = useState<CompletionItem[]>([])
  const [selectedIdx, setSelectedIdx] = useState(0)
  const [showSearch, setShowSearch] = useState(false)
  const [searchTerm, setSearchTerm] = useState('')

  const settingsRef = useRef(settings); settingsRef.current = settings
  const suggestionsRef = useRef(suggestions); suggestionsRef.current = suggestions
  const selectedIdxRef = useRef(selectedIdx); selectedIdxRef.current = selectedIdx

  const requestSuggestions = useCallback(async (line: string) => {
    if (!settingsRef.current.autocompleteEnabled || line.trimStart().length < 1) { setSuggestions([]); return }
    try { setSuggestions((await api.suggestCommands(line, session.serverId)) || []); setSelectedIdx(0) }
    catch { setSuggestions([]) }
  }, [session.serverId])

  const applySuggestion = useCallback(async (item: CompletionItem) => {
    const { backspaces, insertText } = computeCompletionInsert(inputLineRef.current, item.command)
    const payload = '\x7f'.repeat(backspaces) + insertText
    if (payload) await api.writeTerminal(session.sessionId, payload)
    inputLineRef.current = item.command
    setSuggestions([])
    termRef.current?.focus()
  }, [session.sessionId])

  useEffect(() => { if (!settings.autocompleteEnabled) setSuggestions([]) }, [settings.autocompleteEnabled])

  useEffect(() => {
    if (termRef.current) {
      termRef.current.options.fontSize = settings.terminalFontSize
      termRef.current.options.fontFamily = settings.terminalFontFamily
      fitRef.current?.fit()
    }
  }, [settings.terminalFontSize, settings.terminalFontFamily])

  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const term = new Terminal({
      cursorBlink: true, cursorStyle: 'bar', cursorWidth: 2,
      fontFamily: settingsRef.current.terminalFontFamily, fontSize: settingsRef.current.terminalFontSize,
      lineHeight: 1.25, scrollback: 8000, allowProposedApi: true, theme: ANTIGRAVITY_TERM_THEME,
    })
    const fit = new FitAddon(); const links = new WebLinksAddon(); const searchAddon = new SearchAddon()
    term.loadAddon(fit); term.loadAddon(links); term.loadAddon(searchAddon)
    term.open(el); fit.fit()
    termRef.current = term; fitRef.current = fit; searchAddonRef.current = searchAddon

    term.attachCustomKeyEventHandler((ev: KeyboardEvent) => {
      if (ev.type !== 'keydown') return true
      if ((ev.ctrlKey || ev.metaKey) && ev.key.toLowerCase() === 'f') { ev.preventDefault(); setShowSearch(true); return false }
      if (ev.ctrlKey && ev.shiftKey && ev.key.toLowerCase() === 'c') { const sel = term.getSelection(); if (sel) void navigator.clipboard.writeText(sel); return false }
      if (ev.ctrlKey && ev.shiftKey && ev.key.toLowerCase() === 'v') { void navigator.clipboard.readText().then((t) => { if (t) void api.writeTerminal(session.sessionId, t) }); return false }
      const list = suggestionsRef.current
      if (list.length > 0) {
        if (ev.key === 'ArrowDown') { ev.preventDefault(); setSelectedIdx((p) => (p + 1) % list.length); return false }
        if (ev.key === 'ArrowUp') { ev.preventDefault(); setSelectedIdx((p) => (p - 1 + list.length) % list.length); return false }
        if (ev.key === 'Tab') { ev.preventDefault(); const chosen = list[selectedIdxRef.current] || list[0]; if (chosen) void applySuggestion(chosen); return false }
        if (ev.key === 'Escape') { ev.preventDefault(); setSuggestions([]); return false }
      }
      return true
    })

    const onData = term.onData((data) => {
      void api.writeTerminal(session.sessionId, data)
      const nextLine = applyTerminalInputToLine(inputLineRef.current, data)
      inputLineRef.current = nextLine
      if (settingsRef.current.autocompleteEnabled && nextLine.trimStart().length > 0) void requestSuggestions(nextLine)
      else setSuggestions([])
    })
    const onResize = term.onResize(({ cols, rows }) => { void api.resizeTerminal(session.sessionId, cols, rows) })
    const unsubData = api.onEvent<{ sessionId: string; data: string }>('terminal:data', (p) => {
      if (p.sessionId === session.sessionId && p.data) term.write(base64ToUint8Array(p.data))
    })
    const ro = new ResizeObserver(() => { try { fit.fit() } catch { /* ignore */ } })
    ro.observe(el)
    return () => { ro.disconnect(); unsubData(); onData.dispose(); onResize.dispose(); term.dispose(); termRef.current = null; fitRef.current = null }
  }, [session.sessionId, applySuggestion, requestSuggestions])

  useEffect(() => {
    if (active && fitRef.current && termRef.current) {
      const id = setTimeout(() => { fitRef.current?.fit(); termRef.current?.focus() }, 40)
      return () => clearTimeout(id)
    }
  }, [active, showSftp, rightDrawer])

  return (
    <div className="flex flex-col h-full min-w-0 flex-1 bg-ag-bg relative">
      <TerminalToolbar session={session} autocompleteEnabled={settings.autocompleteEnabled} showSearch={showSearch} showSftp={showSftp} rightDrawer={rightDrawer} onReconnect={onReconnect} onToggleAutocomplete={onToggleAutocomplete} onToggleSearch={() => setShowSearch((v) => !v)} onToggleSftp={onToggleSftp} onToggleRightDrawer={onToggleRightDrawer} />
      {showSearch && <TerminalSearchBar value={searchTerm} onChange={(v) => { setSearchTerm(v); searchAddonRef.current?.findNext(v) }} onPrev={() => searchAddonRef.current?.findPrevious(searchTerm)} onNext={() => searchAddonRef.current?.findNext(searchTerm)} onClose={() => setShowSearch(false)} />}
      <div ref={containerRef} className="flex-1 min-h-0 w-full relative overflow-hidden" onContextMenu={(e) => { e.preventDefault(); const sel = termRef.current?.getSelection(); if (sel) { void navigator.clipboard.writeText(sel); termRef.current?.clearSelection() } else { void navigator.clipboard.readText().then((t) => { if (t) void api.writeTerminal(session.sessionId, t) }) } }} />
      {settings.autocompleteEnabled && <AutocompletePopup suggestions={suggestions} selectedIdx={selectedIdx} onSelectIndex={setSelectedIdx} onApply={(item) => void applySuggestion(item)} />}
    </div>
  )
}

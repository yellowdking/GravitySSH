import React, { Suspense, lazy, useCallback, useEffect, useRef, useState } from 'react'
import { AlertCircle } from 'lucide-react'
import type { AppSettings, SessionInfo } from '../types'
import type { PasteInspection } from '../utils/pasteGuard'
import type { TerminalActions } from '../hooks/terminalStream'
import { useAutocomplete } from '../hooks/useAutocomplete'
import { useTerminalInstance } from '../hooks/useTerminalInstance'
import { copyToClipboard, readFromClipboard } from '../utils/errors'
import { TerminalToolbar } from './TerminalToolbar'
import { TerminalSearchBar } from './TerminalSearchBar'
import { AutocompletePopup } from './AutocompletePopup'
import { MonitorBar } from './MonitorBar'

const PasteGuardModal = lazy(() => import('./PasteGuardModal').then((m) => ({ default: m.PasteGuardModal })))

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
  onRegisterTerminal?: (sessionId: string, actions: TerminalActions | null) => void
}

interface PendingPaste {
  text: string
  inspection: PasteInspection
  bracketedPasteActive: boolean
}

export const TerminalPane: React.FC<TerminalPaneProps> = React.memo(({
  session, active, settings, showSftp, rightDrawer, onToggleSftp, onToggleRightDrawer, onToggleAutocomplete, onReconnect, onRegisterTerminal,
}) => {
  const inputLineRef = useRef('')
  const focusTermRef = useRef<() => void>(() => {})
  const searchInputRef = useRef<HTMLInputElement | null>(null)
  const [showSearch, setShowSearch] = useState(false)
  const [searchTerm, setSearchTerm] = useState('')
  const [pendingPaste, setPendingPaste] = useState<PendingPaste | null>(null)

  const ac = useAutocomplete({
    enabled: settings.autocompleteEnabled,
    serverId: session.serverId,
    sessionId: session.sessionId,
    inputLineRef,
    onFocusTerminal: () => focusTermRef.current(),
  })

  const resetInputLine = useCallback(() => {
    inputLineRef.current = ''
    ac.clearSuggestions(false)
  }, [ac.clearSuggestions])

  const openSearch = useCallback(() => {
    setShowSearch(true)
    requestAnimationFrame(() => {
      const input = searchInputRef.current
      if (input) {
        input.focus()
        input.select()
      }
    })
  }, [])

  const { containerRef, termRef, searchAddonRef, pasteText, commitPaste } = useTerminalInstance({
    sessionId: session.sessionId,
    status: session.status,
    active,
    showSftp,
    rightDrawer,
    settings,
    suggestionsRef: ac.suggestionsRef,
    selectedIdxRef: ac.selectedIdxRef,
    inputLineRef,
    onRequestSuggestions: (line) => ac.requestSuggestions(line),
    onClearSuggestions: () => ac.clearSuggestions(true),
    onResetAutocomplete: resetInputLine,
    onMoveSelection: ac.moveSelection,
    onApplySelected: ac.applySelected,
    onOpenSearch: openSearch,
    onPasteGuard: (text, inspection, bracketedPasteActive) =>
      setPendingPaste({ text, inspection, bracketedPasteActive }),
  })
  focusTermRef.current = () => termRef.current?.focus()

  useEffect(() => {
    if (!onRegisterTerminal) return
    onRegisterTerminal(session.sessionId, { pasteText, resetInputLine })
    return () => {
      onRegisterTerminal(session.sessionId, null)
    }
  }, [onRegisterTerminal, session.sessionId, pasteText, resetInputLine])

  const closeSearch = useCallback(() => {
    setShowSearch(false)
    searchAddonRef.current?.clearDecorations()
    termRef.current?.focus()
  }, [searchAddonRef, termRef])

  const toggleSearch = useCallback(() => {
    if (showSearch) {
      closeSearch()
    } else {
      openSearch()
    }
  }, [showSearch, closeSearch, openSearch])

  const handleSearchChange = useCallback(
    (v: string) => {
      setSearchTerm(v)
      if (!v) {
        searchAddonRef.current?.clearDecorations()
        return
      }
      searchAddonRef.current?.findNext(v, { incremental: true })
    },
    [searchAddonRef]
  )

  return (
    <div className="flex flex-col h-full min-w-0 flex-1 bg-ag-bg relative">
      <TerminalToolbar
        session={session}
        autocompleteEnabled={settings.autocompleteEnabled}
        showSearch={showSearch}
        showSftp={showSftp}
        rightDrawer={rightDrawer}
        onReconnect={onReconnect}
        onToggleAutocomplete={onToggleAutocomplete}
        onToggleSearch={toggleSearch}
        onToggleSftp={onToggleSftp}
        onToggleRightDrawer={onToggleRightDrawer}
      />
      {session.status === 'error' && session.error && (
        <div
          role="alert"
          className="px-3 py-2 border-b border-rose-500/30 bg-rose-950/50 text-rose-200 text-xs flex items-center justify-between gap-2 shrink-0"
        >
          <div className="flex items-center gap-2 min-w-0">
            <AlertCircle className="w-3.5 h-3.5 text-rose-400 shrink-0" />
            <span className="truncate font-mono" title={session.error}>
              连接失败：{session.error}
            </span>
          </div>
          <button
            type="button"
            onClick={() => onReconnect(session.serverId)}
            className="shrink-0 px-2 py-0.5 rounded bg-rose-500/20 hover:bg-rose-500/30 border border-rose-500/35 text-rose-100 text-[11px] transition"
          >
            重试连接
          </button>
        </div>
      )}
      {showSearch && (
        <TerminalSearchBar
          value={searchTerm}
          inputRef={searchInputRef}
          onChange={handleSearchChange}
          onPrev={() => { if (searchTerm) searchAddonRef.current?.findPrevious(searchTerm) }}
          onNext={() => { if (searchTerm) searchAddonRef.current?.findNext(searchTerm) }}
          onClose={closeSearch}
        />
      )}
      <div className="flex-1 min-h-0 w-full relative overflow-hidden">
        <div
          ref={containerRef}
          className="w-full h-full"
          onContextMenu={(e) => {
            e.preventDefault()
            const sel = termRef.current?.getSelection()
            if (sel) {
              void copyToClipboard(sel)
              termRef.current?.clearSelection()
            } else {
              void readFromClipboard().then((t) => { if (t) pasteText(t) })
            }
          }}
        />
        {settings.autocompleteEnabled && (
          <AutocompletePopup
            suggestions={ac.suggestions}
            selectedIdx={ac.selectedIdx}
            inputLine={ac.queryLine}
            onSelectIndex={ac.setSelectedIdx}
            onApply={(item) => void ac.applySuggestion(item)}
          />
        )}
      </div>
      <MonitorBar sessionId={session.sessionId} status={session.status} active={active} enabled={settings.monitorBarEnabled} />
      {pendingPaste && (
        <Suspense fallback={null}>
          <PasteGuardModal
            open
            initialText={pendingPaste.text}
            bracketedPasteActive={pendingPaste.bracketedPasteActive}
            onConfirm={(finalText) => { setPendingPaste(null); commitPaste(finalText) }}
            onCancel={() => { setPendingPaste(null); termRef.current?.focus() }}
          />
        </Suspense>
      )}
    </div>
  )
})

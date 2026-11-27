import React, { Suspense, lazy, useCallback, useRef, useState } from 'react'
import type { AppSettings, SessionInfo } from '../types'
import type { PasteInspection } from '../utils/pasteGuard'
import { useAutocomplete } from '../hooks/useAutocomplete'
import { useTerminalInstance } from '../hooks/useTerminalInstance'
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
}

interface PendingPaste {
  text: string
  inspection: PasteInspection
  bracketedPasteActive: boolean
}

export const TerminalPane: React.FC<TerminalPaneProps> = React.memo(({
  session, active, settings, showSftp, rightDrawer, onToggleSftp, onToggleRightDrawer, onToggleAutocomplete, onReconnect,
}) => {
  const inputLineRef = useRef('')
  const focusTermRef = useRef<() => void>(() => {})
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
    onMoveSelection: ac.moveSelection,
    onApplySelected: ac.applySelected,
    onOpenSearch: () => setShowSearch(true),
    onPasteGuard: (text, inspection, bracketedPasteActive) =>
      setPendingPaste({ text, inspection, bracketedPasteActive }),
  })
  focusTermRef.current = () => termRef.current?.focus()

  const closeSearch = useCallback(() => {
    setShowSearch(false)
    searchAddonRef.current?.clearDecorations()
    termRef.current?.focus()
  }, [searchAddonRef, termRef])

  const toggleSearch = useCallback(() => {
    setShowSearch((prev) => {
      if (prev) {
        searchAddonRef.current?.clearDecorations()
        termRef.current?.focus()
      }
      return !prev
    })
  }, [searchAddonRef, termRef])

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
      {showSearch && (
        <TerminalSearchBar
          value={searchTerm}
          onChange={(v) => { setSearchTerm(v); searchAddonRef.current?.findNext(v) }}
          onPrev={() => searchAddonRef.current?.findPrevious(searchTerm)}
          onNext={() => searchAddonRef.current?.findNext(searchTerm)}
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
              void navigator.clipboard.writeText(sel)
              termRef.current?.clearSelection()
            } else {
              void navigator.clipboard.readText().then((t) => { if (t) pasteText(t) })
            }
          }}
        />
        {settings.autocompleteEnabled && (
          <AutocompletePopup
            suggestions={ac.suggestions}
            selectedIdx={ac.selectedIdx}
            inputLine={inputLineRef.current}
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

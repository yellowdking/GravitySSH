import React, { useRef, useState } from 'react'
import type { AppSettings, SessionInfo } from '../types'
import type { PasteInspection } from '../utils/pasteGuard'
import { getActiveTokenPrefix } from '../utils/autocomplete'
import { useAutocomplete } from '../hooks/useAutocomplete'
import { useTerminalInstance } from '../hooks/useTerminalInstance'
import { TerminalToolbar } from './TerminalToolbar'
import { TerminalSearchBar } from './TerminalSearchBar'
import { AutocompletePopup } from './AutocompletePopup'
import { MonitorBar } from './MonitorBar'
import { PasteGuardModal } from './PasteGuardModal'

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
  const inputLineRef = useRef('')
  const focusTermRef = useRef<() => void>(() => {})
  const [showSearch, setShowSearch] = useState(false)
  const [searchTerm, setSearchTerm] = useState('')
  const [pendingPaste, setPendingPaste] = useState<{ text: string; inspection: PasteInspection } | null>(null)

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
    onPasteGuard: (text, inspection) => setPendingPaste({ text, inspection }),
  })
  focusTermRef.current = () => termRef.current?.focus()

  const activePrefix = getActiveTokenPrefix(inputLineRef.current, ac.suggestions[0]?.source || '')

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
        onToggleSearch={() => setShowSearch((v) => !v)}
        onToggleSftp={onToggleSftp}
        onToggleRightDrawer={onToggleRightDrawer}
      />
      {showSearch && (
        <TerminalSearchBar
          value={searchTerm}
          onChange={(v) => { setSearchTerm(v); searchAddonRef.current?.findNext(v) }}
          onPrev={() => searchAddonRef.current?.findPrevious(searchTerm)}
          onNext={() => searchAddonRef.current?.findNext(searchTerm)}
          onClose={() => setShowSearch(false)}
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
            activePrefix={activePrefix}
            inputLine={inputLineRef.current}
            onSelectIndex={ac.setSelectedIdx}
            onApply={(item) => void ac.applySuggestion(item)}
          />
        )}
      </div>
      <MonitorBar sessionId={session.sessionId} status={session.status} active={active} enabled={settings.monitorBarEnabled} />
      <PasteGuardModal
        open={!!pendingPaste}
        initialText={pendingPaste?.text || ''}
        warnings={pendingPaste?.inspection.warnings || []}
        hasHiddenChars={!!pendingPaste?.inspection.hasHiddenChars}
        onConfirm={(finalText) => { setPendingPaste(null); commitPaste(finalText) }}
        onCancel={() => { setPendingPaste(null); termRef.current?.focus() }}
      />
    </div>
  )
}

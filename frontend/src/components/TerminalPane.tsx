import React, { useCallback, useEffect, useRef, useState } from 'react'
import type { AppSettings, CompletionItem, SessionInfo } from '../types'
import type { PasteInspection } from '../utils/pasteGuard'
import { api } from '../wailsApi'
import { computeCompletionInsert } from '../utils/autocomplete'
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
  const inputLineRef = useRef<string>('')
  const reqSeqRef = useRef(0)
  const [suggestions, setSuggestions] = useState<CompletionItem[]>([])
  const [selectedIdx, setSelectedIdx] = useState(0)
  const [showSearch, setShowSearch] = useState(false)
  const [searchTerm, setSearchTerm] = useState('')
  const [pendingPaste, setPendingPaste] = useState<{ text: string; inspection: PasteInspection } | null>(null)

  const suggestionsRef = useRef(suggestions); suggestionsRef.current = suggestions
  const selectedIdxRef = useRef(selectedIdx); selectedIdxRef.current = selectedIdx

  const requestSuggestions = useCallback(async (line: string) => {
    if (!settings.autocompleteEnabled || line.trimStart().length < 1) { setSuggestions([]); return }
    const seq = ++reqSeqRef.current
    try {
      const list = await api.suggestCommandsForSession(line, session.serverId, session.sessionId)
      if (seq === reqSeqRef.current) { setSuggestions(list || []); setSelectedIdx(0) }
    } catch {
      if (seq === reqSeqRef.current) setSuggestions([])
    }
  }, [settings.autocompleteEnabled, session.serverId, session.sessionId])

  useEffect(() => { if (!settings.autocompleteEnabled) setSuggestions([]) }, [settings.autocompleteEnabled])

  const { containerRef, termRef, searchAddonRef, pasteText, commitPaste } = useTerminalInstance({
    sessionId: session.sessionId,
    status: session.status,
    active,
    showSftp,
    rightDrawer,
    settings,
    suggestionsRef,
    selectedIdxRef,
    inputLineRef,
    onRequestSuggestions: (line) => { void requestSuggestions(line) },
    onClearSuggestions: () => setSuggestions([]),
    onMoveSelection: (delta) => setSelectedIdx((p) => {
      const len = suggestionsRef.current.length
      return len > 0 ? (p + delta + len) % len : 0
    }),
    onApplySelected: () => {
      const chosen = suggestionsRef.current[selectedIdxRef.current] || suggestionsRef.current[0]
      if (chosen) void applySuggestion(chosen)
    },
    onOpenSearch: () => setShowSearch(true),
    onPasteGuard: (text, inspection) => setPendingPaste({ text, inspection }),
  })

  const applySuggestion = useCallback(async (item: CompletionItem) => {
    const { backspaces, insertText } = computeCompletionInsert(inputLineRef.current, item.command)
    const payload = '\x7f'.repeat(backspaces) + insertText
    if (payload) await api.writeTerminal(session.sessionId, payload)
    inputLineRef.current = item.command
    setSuggestions([])
    termRef.current?.focus()
  }, [session.sessionId, termRef])

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
            suggestions={suggestions}
            selectedIdx={selectedIdx}
            onSelectIndex={setSelectedIdx}
            onApply={(item) => void applySuggestion(item)}
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

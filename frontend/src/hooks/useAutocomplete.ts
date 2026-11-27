import { useCallback, useEffect, useRef, useState, type MutableRefObject } from 'react'
import type { CompletionItem } from '../types'
import { api } from '../wailsApi'
import { computeCompletionInsert } from '../utils/autocomplete'

interface Options {
  enabled: boolean
  serverId: string
  sessionId: string
  inputLineRef: MutableRefObject<string>
  onFocusTerminal: () => void
}

export function useAutocomplete({ enabled, serverId, sessionId, inputLineRef, onFocusTerminal }: Options) {
  const dismissedLineRef = useRef<string | null>(null)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const reqSeqRef = useRef(0)
  const applyingRef = useRef(false)
  const focusRef = useRef(onFocusTerminal); focusRef.current = onFocusTerminal
  const [suggestions, setSuggestions] = useState<CompletionItem[]>([])
  const [selectedIdx, setSelectedIdx] = useState(0)

  const suggestionsRef = useRef(suggestions); suggestionsRef.current = suggestions
  const selectedIdxRef = useRef(selectedIdx); selectedIdxRef.current = selectedIdx

  const cancelDebounce = useCallback(() => {
    if (debounceRef.current) { clearTimeout(debounceRef.current); debounceRef.current = null }
  }, [])

  const clearSuggestions = useCallback((dismissCurrent = false) => {
    cancelDebounce()
    reqSeqRef.current++
    if (dismissCurrent && inputLineRef.current.trimStart()) {
      dismissedLineRef.current = inputLineRef.current
    } else if (!inputLineRef.current.trimStart()) {
      dismissedLineRef.current = null
    }
    suggestionsRef.current = []
    setSuggestions([])
  }, [cancelDebounce, inputLineRef])

  const requestSuggestions = useCallback((line: string, immediate = false) => {
    cancelDebounce()
    if (!enabled || !line.trimStart()) {
      reqSeqRef.current++
      dismissedLineRef.current = null
      suggestionsRef.current = []
      setSuggestions([])
      return
    }
    if (!immediate && dismissedLineRef.current === line) return
    dismissedLineRef.current = null

    const run = async () => {
      const seq = ++reqSeqRef.current
      try {
        const list = await api.suggestCommandsForSession(line, serverId, sessionId)
        if (seq === reqSeqRef.current) {
          const next = (list || []).slice(0, 5)
          suggestionsRef.current = next
          setSuggestions(next)
          setSelectedIdx(0)
        }
      } catch {
        if (seq === reqSeqRef.current) {
          suggestionsRef.current = []
          setSuggestions([])
        }
      }
    }

    const delay = immediate || line.endsWith(' ') || line.endsWith('/') ? 0 : 45
    if (delay === 0) void run()
    else debounceRef.current = setTimeout(() => { debounceRef.current = null; void run() }, delay)
  }, [cancelDebounce, enabled, serverId, sessionId])

  useEffect(() => { if (!enabled) clearSuggestions(false) }, [enabled, clearSuggestions])
  useEffect(() => () => {
    reqSeqRef.current++
    cancelDebounce()
    suggestionsRef.current = []
  }, [cancelDebounce])

  const moveSelection = useCallback((delta: number) => {
    setSelectedIdx((p) => {
      const len = suggestionsRef.current.length
      return len > 0 ? (p + delta + len) % len : 0
    })
  }, [])

  const applySuggestion = useCallback(async (item: CompletionItem) => {
    if (applyingRef.current) return
    applyingRef.current = true
    const target = item.insertText || item.command
    const prevLine = inputLineRef.current
    const { backspaces, insertText } = computeCompletionInsert(prevLine, target)
    const payload = '\x7f'.repeat(backspaces) + insertText
    inputLineRef.current = target
    dismissedLineRef.current = null
    clearSuggestions(false)
    try {
      if (payload) await api.writeTerminal(sessionId, payload)
      focusRef.current()
      if (inputLineRef.current === target && (target.endsWith('/') || target.endsWith(' '))) {
        requestSuggestions(target, true)
      }
    } catch {
      if (inputLineRef.current === target) {
        inputLineRef.current = prevLine
      }
      clearSuggestions(false)
    } finally {
      applyingRef.current = false
    }
  }, [sessionId, inputLineRef, requestSuggestions, clearSuggestions])

  const applySelected = useCallback(() => {
    const chosen = suggestionsRef.current[selectedIdxRef.current] || suggestionsRef.current[0]
    if (chosen) void applySuggestion(chosen)
  }, [applySuggestion])

  return {
    suggestions, selectedIdx, setSelectedIdx, suggestionsRef, selectedIdxRef,
    requestSuggestions, clearSuggestions, moveSelection, applySuggestion, applySelected,
  }
}

import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { SearchAddon } from '@xterm/addon-search'
import { Unicode11Addon } from '@xterm/addon-unicode11'
import type { AppSettings, CompletionItem } from '../types'
import { api } from '../wailsApi'
import { copyToClipboard, isAllowedExternalUrl, readFromClipboard } from '../utils/errors'
import { ANTIGRAVITY_TERM_THEME, DEFAULT_TERMINAL_FONT } from '../components/terminalTheme'

function openTerminalExternalUrl(uri: string) {
  if (!isAllowedExternalUrl(uri)) return
  void api.openExternalURL(uri).catch(() => {})
}

export function createConfiguredTerminal(el: HTMLDivElement, settings: AppSettings) {
  const term = new Terminal({
    cursorBlink: true,
    cursorStyle: 'bar',
    cursorWidth: 2,
    fontFamily: settings.terminalFontFamily || DEFAULT_TERMINAL_FONT,
    fontSize: settings.terminalFontSize,
    fontWeight: '400',
    fontWeightBold: '600',
    customGlyphs: true,
    lineHeight: 1.15,
    letterSpacing: 0,
    convertEol: false,
    windowsMode: false,
    scrollSensitivity: 0.34,
    scrollback: 8000,
    allowProposedApi: true,
    linkHandler: {
      activate: (_ev, uri) => {
        openTerminalExternalUrl(uri)
      },
    },
    theme: ANTIGRAVITY_TERM_THEME,
  })
  const fit = new FitAddon()
  const searchAddon = new SearchAddon()
  term.loadAddon(fit)
  term.loadAddon(new WebLinksAddon((_ev, uri) => { openTerminalExternalUrl(uri) }))
  term.loadAddon(searchAddon)
  term.loadAddon(new Unicode11Addon())
  term.unicode.activeVersion = '11'
  term.open(el)
  fit.fit()
  return { term, fit, searchAddon }
}

interface HandlerDeps {
  term: Terminal
  enqueueWrite: (data: string) => void
  pasteText: (raw: string) => void
  suggestionsRef: React.MutableRefObject<CompletionItem[]>
  onOpenSearch: () => void
  onMoveSelection: (delta: number) => void
  onApplySelected: () => void
  onClearSuggestions: () => void
}

export function attachTerminalInputHandlers(d: HandlerDeps) {
  d.term.attachCustomWheelEventHandler((ev: WheelEvent) => {
    if (ev.deltaY === 0) return true
    if (d.term.buffer.active.type === 'normal') {
      ev.preventDefault()
      d.term.scrollLines(ev.deltaY > 0 ? 1 : -1)
      return false
    }
    if (d.term.modes.mouseTrackingMode === 'none') {
      ev.preventDefault()
      d.enqueueWrite(ev.deltaY > 0 ? '\x1b[B' : '\x1b[A')
      return false
    }
    return true
  })

  d.term.attachCustomKeyEventHandler((ev: KeyboardEvent) => {
    if (ev.type !== 'keydown') return true
    if (ev.isComposing || ev.keyCode === 229) return true
    const k = ev.key.toLowerCase()
    if ((ev.ctrlKey || ev.metaKey) && k === 'f') { ev.preventDefault(); d.onOpenSearch(); return false }
    if (ev.ctrlKey && ev.shiftKey && k === 'c') {
      ev.preventDefault()
      const s = d.term.getSelection()
      if (s) void copyToClipboard(s)
      return false
    }
    if (ev.ctrlKey && ev.shiftKey && k === 'v') {
      ev.preventDefault()
      void readFromClipboard().then((t) => { if (t) d.pasteText(t) })
      return false
    }
    if (d.term.buffer.active.type === 'normal' && d.suggestionsRef.current.length > 0) {
      if (ev.key === 'ArrowDown') { ev.preventDefault(); d.onMoveSelection(1); return false }
      if (ev.key === 'ArrowUp') { ev.preventDefault(); d.onMoveSelection(-1); return false }
      if (ev.key === 'Tab') { ev.preventDefault(); d.onApplySelected(); return false }
      if (ev.key === 'Escape') { ev.preventDefault(); d.onClearSuggestions(); return false }
    }
    return true
  })
}

export function syncTerminalImeAttrs(term: Terminal | null, enabled: boolean) {
  const ta = term?.textarea
  if (!ta) return
  if (enabled) {
    ta.setAttribute('inputmode', 'url')
    ta.setAttribute('lang', 'en')
    ta.setAttribute('autocapitalize', 'off')
    ta.setAttribute('spellcheck', 'false')
  } else {
    ta.removeAttribute('inputmode')
    ta.removeAttribute('lang')
  }
}

interface TerminalImeGuard {
  trigger: (force?: boolean) => void
  detach: () => void
}

export function attachTerminalImeGuard(
  el: HTMLElement,
  term: Terminal,
  isActive: () => boolean,
  isEnabled: () => boolean
): TerminalImeGuard {
  const ta = term.textarea
  syncTerminalImeAttrs(term, isEnabled())

  let lastTrigger = 0
  let retryTimer: ReturnType<typeof setTimeout> | null = null
  let isComposing = false
  let refocusing = false

  const clearRetry = () => {
    if (retryTimer) { clearTimeout(retryTimer); retryTimer = null }
  }

  const trigger = (force = false) => {
    if (!isActive() || !isEnabled() || isComposing) return
    const now = Date.now()
    if (!force && now - lastTrigger < 100) return
    lastTrigger = now
    void api.switchTerminalIme().catch(() => {})
    clearRetry()
    retryTimer = setTimeout(() => {
      retryTimer = null
      if (isActive() && isEnabled() && !isComposing) void api.switchTerminalIme().catch(() => {})
    }, 80)
  }

  const isOtherInput = () => {
    if (document.querySelector('[role="dialog"]')) return true
    const a = document.activeElement
    return Boolean(a && a !== ta && (a.tagName === 'INPUT' || a.tagName === 'TEXTAREA' || a.tagName === 'SELECT' || (a as HTMLElement).isContentEditable))
  }

  const onFocus = () => { if (!refocusing) trigger() }
  const onCompStart = () => { isComposing = true }
  const onCompEnd = () => { isComposing = false }

  const onPointerDown = () => {
    if (!isActive() || !isEnabled() || isComposing) return
    if (!ta || document.activeElement !== ta) trigger(true)
  }

  const onWindowFocus = () => {
    if (!isActive() || !isEnabled() || isComposing || isOtherInput()) return
    if (ta && document.activeElement === ta && !refocusing) {
      refocusing = true
      try { ta.blur(); ta.focus({ preventScroll: true }) } finally { refocusing = false }
    }
    trigger(true)
  }

  const onVis = () => { if (!document.hidden) onWindowFocus() }

  ta?.addEventListener('focus', onFocus)
  ta?.addEventListener('compositionstart', onCompStart)
  ta?.addEventListener('compositionend', onCompEnd)
  el.addEventListener('pointerdown', onPointerDown, true)
  window.addEventListener('focus', onWindowFocus)
  document.addEventListener('visibilitychange', onVis)

  return {
    trigger,
    detach: () => {
      clearRetry()
      ta?.removeEventListener('focus', onFocus)
      ta?.removeEventListener('compositionstart', onCompStart)
      ta?.removeEventListener('compositionend', onCompEnd)
      el.removeEventListener('pointerdown', onPointerDown, true)
      window.removeEventListener('focus', onWindowFocus)
      document.removeEventListener('visibilitychange', onVis)
    },
  }
}
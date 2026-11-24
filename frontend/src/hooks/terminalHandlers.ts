import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { SearchAddon } from '@xterm/addon-search'
import { Unicode11Addon } from '@xterm/addon-unicode11'
import type { AppSettings, CompletionItem } from '../types'
import { api } from '../wailsApi'
import { ANTIGRAVITY_TERM_THEME, DEFAULT_TERMINAL_FONT } from '../components/terminalTheme'

export function createConfiguredTerminal(el: HTMLDivElement, settings: AppSettings) {
  const term = new Terminal({
    cursorBlink: true,
    cursorStyle: 'bar',
    cursorWidth: 2,
    fontFamily: settings.terminalFontFamily || DEFAULT_TERMINAL_FONT,
    fontSize: settings.terminalFontSize,
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
    const k = ev.key.toLowerCase()
    if ((ev.ctrlKey || ev.metaKey) && k === 'f') { ev.preventDefault(); d.onOpenSearch(); return false }
    if (ev.ctrlKey && ev.shiftKey && k === 'c') {
      ev.preventDefault()
      const s = d.term.getSelection()
      if (s) void navigator.clipboard.writeText(s)
      return false
    }
    if (ev.ctrlKey && ev.shiftKey && k === 'v') {
      ev.preventDefault()
      void navigator.clipboard.readText().then((t) => { if (t) d.pasteText(t) })
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

export function attachTerminalImeGuard(
  term: Terminal,
  isActive: () => boolean,
  isEnabled: () => boolean
): () => void {
  let lastTrigger = 0
  const trigger = () => {
    if (!isActive() || !isEnabled()) return
    const now = Date.now()
    if (now - lastTrigger < 120) return
    lastTrigger = now
    void api.switchTerminalIme().catch(() => {})
  }

  const onWindowFocus = () => {
    const activeEl = document.activeElement
    const isOtherInput =
      activeEl &&
      activeEl !== term.textarea &&
      (activeEl.tagName === 'INPUT' || activeEl.tagName === 'TEXTAREA' || (activeEl as HTMLElement).isContentEditable)
    if (!isOtherInput) trigger()
  }

  const ta = term.textarea
  ta?.addEventListener('focus', trigger)
  window.addEventListener('focus', onWindowFocus)
  return () => {
    ta?.removeEventListener('focus', trigger)
    window.removeEventListener('focus', onWindowFocus)
  }
}

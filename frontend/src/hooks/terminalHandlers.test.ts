import { describe, expect, it, vi } from 'vitest'

vi.mock('@xterm/xterm', () => ({ Terminal: vi.fn() }))
vi.mock('@xterm/addon-fit', () => ({ FitAddon: vi.fn() }))
vi.mock('@xterm/addon-web-links', () => ({ WebLinksAddon: vi.fn() }))
vi.mock('@xterm/addon-search', () => ({ SearchAddon: vi.fn() }))
vi.mock('@xterm/addon-unicode11', () => ({ Unicode11Addon: vi.fn() }))

import type { Terminal } from '@xterm/xterm'
import type { CompletionItem } from '../types'
import { attachTerminalInputHandlers, syncTerminalImeAttrs } from './terminalHandlers'

function createMockTerminal(options?: {
  bufferType?: 'normal' | 'alternate'
  mouseTrackingMode?: string
  selection?: string
}) {
  let wheelHandler: ((ev: WheelEvent) => boolean) | null = null
  let keyHandler: ((ev: KeyboardEvent) => boolean) | null = null
  const scrollLines = vi.fn()
  const attrs = new Map<string, string>()

  const textarea = {
    setAttribute: vi.fn((k: string, v: string) => {
      attrs.set(k, v)
    }),
    removeAttribute: vi.fn((k: string) => {
      attrs.delete(k)
    }),
    getAttribute: (k: string) => attrs.get(k) ?? null,
  }

  const term = {
    buffer: { active: { type: options?.bufferType ?? 'normal' } },
    modes: { mouseTrackingMode: options?.mouseTrackingMode ?? 'none' },
    textarea,
    scrollLines,
    getSelection: vi.fn(() => options?.selection ?? ''),
    attachCustomWheelEventHandler: vi.fn((fn: (ev: WheelEvent) => boolean) => {
      wheelHandler = fn
    }),
    attachCustomKeyEventHandler: vi.fn((fn: (ev: KeyboardEvent) => boolean) => {
      keyHandler = fn
    }),
  } as unknown as Terminal

  return {
    term,
    attrs,
    scrollLines,
    dispatchWheel: (deltaY: number) => {
      const preventDefault = vi.fn()
      const res = wheelHandler!({ deltaY, preventDefault } as unknown as WheelEvent)
      return { res, preventDefault }
    },
    dispatchKey: (overrides: Partial<KeyboardEvent> & { key: string }) => {
      const preventDefault = vi.fn()
      const ev = {
        type: 'keydown',
        isComposing: false,
        keyCode: 0,
        ctrlKey: false,
        metaKey: false,
        shiftKey: false,
        preventDefault,
        ...overrides,
      } as unknown as KeyboardEvent
      const res = keyHandler!(ev)
      return { res, preventDefault }
    },
  }
}

describe('terminalHandlers', () => {
  it('passes through IME composition key events even when autocomplete suggestions are open', () => {
    const mock = createMockTerminal({ bufferType: 'normal' })
    const onMoveSelection = vi.fn()
    const onApplySelected = vi.fn()
    const onClearSuggestions = vi.fn()
    const suggestionsRef = {
      current: [{ command: 'ls -la', description: '', category: '', source: 'history', insertText: 'ls -la', score: 1 }] as CompletionItem[],
    }

    attachTerminalInputHandlers({
      term: mock.term,
      enqueueWrite: vi.fn(),
      pasteText: vi.fn(),
      suggestionsRef,
      onOpenSearch: vi.fn(),
      onMoveSelection,
      onApplySelected,
      onClearSuggestions,
    })

    expect(mock.dispatchKey({ key: 'ArrowDown', isComposing: true }).res).toBe(true)
    expect(mock.dispatchKey({ key: 'Tab', keyCode: 229 }).res).toBe(true)
    expect(mock.dispatchKey({ key: 'Escape', isComposing: true }).res).toBe(true)
    expect(onMoveSelection).not.toHaveBeenCalled()
    expect(onApplySelected).not.toHaveBeenCalled()
    expect(onClearSuggestions).not.toHaveBeenCalled()
  })

  it('intercepts Ctrl+F and autocomplete navigation only in normal buffer', () => {
    const mock = createMockTerminal({ bufferType: 'normal' })
    const onOpenSearch = vi.fn()
    const onMoveSelection = vi.fn()
    const onApplySelected = vi.fn()
    const onClearSuggestions = vi.fn()
    const suggestionsRef = {
      current: [{ command: 'git status', description: '', category: '', source: 'builtin', insertText: 'git status', score: 10 }] as CompletionItem[],
    }

    attachTerminalInputHandlers({
      term: mock.term,
      enqueueWrite: vi.fn(),
      pasteText: vi.fn(),
      suggestionsRef,
      onOpenSearch,
      onMoveSelection,
      onApplySelected,
      onClearSuggestions,
    })

    const searchRes = mock.dispatchKey({ key: 'f', ctrlKey: true })
    expect(searchRes.res).toBe(false)
    expect(searchRes.preventDefault).toHaveBeenCalledTimes(1)
    expect(onOpenSearch).toHaveBeenCalledTimes(1)

    expect(mock.dispatchKey({ key: 'ArrowDown' }).res).toBe(false)
    expect(onMoveSelection).toHaveBeenCalledWith(1)

    expect(mock.dispatchKey({ key: 'ArrowUp' }).res).toBe(false)
    expect(onMoveSelection).toHaveBeenCalledWith(-1)

    expect(mock.dispatchKey({ key: 'Tab' }).res).toBe(false)
    expect(onApplySelected).toHaveBeenCalledTimes(1)

    expect(mock.dispatchKey({ key: 'Escape' }).res).toBe(false)
    expect(onClearSuggestions).toHaveBeenCalledTimes(1)

    // In alternate screen, autocomplete keys must pass through to TUI app
    ;(mock.term.buffer.active as { type: string }).type = 'alternate'
    expect(mock.dispatchKey({ key: 'ArrowDown' }).res).toBe(true)
    expect(mock.dispatchKey({ key: 'Tab' }).res).toBe(true)
    expect(onMoveSelection).toHaveBeenCalledTimes(2)
    expect(onApplySelected).toHaveBeenCalledTimes(1)
  })

  it('handles wheel scrolling in normal, alternate-no-mouse, and mouse-tracking modes', () => {
    const mock = createMockTerminal({ bufferType: 'normal', mouseTrackingMode: 'none' })
    const enqueueWrite = vi.fn()
    attachTerminalInputHandlers({
      term: mock.term,
      enqueueWrite,
      pasteText: vi.fn(),
      suggestionsRef: { current: [] },
      onOpenSearch: vi.fn(),
      onMoveSelection: vi.fn(),
      onApplySelected: vi.fn(),
      onClearSuggestions: vi.fn(),
    })

    const normalDown = mock.dispatchWheel(100)
    expect(normalDown.res).toBe(false)
    expect(mock.scrollLines).toHaveBeenCalledWith(1)

    ;(mock.term.buffer.active as { type: string }).type = 'alternate'
    const altUp = mock.dispatchWheel(-100)
    expect(altUp.res).toBe(false)
    expect(enqueueWrite).toHaveBeenCalledWith('\x1b[A')

    ;(mock.term.modes as { mouseTrackingMode: string }).mouseTrackingMode = 'any'
    const altMouse = mock.dispatchWheel(100)
    expect(altMouse.res).toBe(true)
  })

  it('toggles terminal textarea IME attributes cleanly', () => {
    const mock = createMockTerminal()
    syncTerminalImeAttrs(mock.term, true)
    expect(mock.attrs.get('inputmode')).toBe('url')
    expect(mock.attrs.get('lang')).toBe('en')
    expect(mock.attrs.get('spellcheck')).toBe('false')

    syncTerminalImeAttrs(mock.term, false)
    expect(mock.attrs.has('inputmode')).toBe(false)
    expect(mock.attrs.has('lang')).toBe(false)
  })
})
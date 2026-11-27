import { describe, expect, it, vi } from 'vitest'
import type React from 'react'
import { insertTabIndent, isAllowedExternalUrl } from './errors'

function makeKeyEvent(overrides: Partial<{
  key: string
  shiftKey: boolean
  ctrlKey: boolean
  altKey: boolean
  metaKey: boolean
  keyCode: number
  isComposing: boolean
}> = {}) {
  const preventDefault = vi.fn()
  const ev = {
    key: overrides.key ?? 'Tab',
    shiftKey: overrides.shiftKey ?? false,
    ctrlKey: overrides.ctrlKey ?? false,
    altKey: overrides.altKey ?? false,
    metaKey: overrides.metaKey ?? false,
    keyCode: overrides.keyCode ?? 9,
    nativeEvent: { isComposing: overrides.isComposing ?? false },
    preventDefault,
    currentTarget: { selectionStart: 2, selectionEnd: 2 } as HTMLTextAreaElement,
  } as unknown as React.KeyboardEvent<HTMLTextAreaElement>
  return { ev, preventDefault }
}

describe('insertTabIndent', () => {
  it('inserts two spaces on plain Tab', () => {
    const origRaf = globalThis.requestAnimationFrame
    globalThis.requestAnimationFrame = ((cb: FrameRequestCallback) => {
      cb(0)
      return 0
    }) as typeof requestAnimationFrame
    try {
      const setValue = vi.fn()
      const { ev, preventDefault } = makeKeyEvent()
      expect(insertTabIndent(ev, 'abcd', setValue)).toBe(true)
      expect(preventDefault).toHaveBeenCalledTimes(1)
      expect(setValue).toHaveBeenCalledWith('ab  cd')
    } finally {
      globalThis.requestAnimationFrame = origRaf
    }
  })

  it('does not trap Shift+Tab or modified Tab keys', () => {
    const setValue = vi.fn()
    for (const mod of [{ shiftKey: true }, { ctrlKey: true }, { altKey: true }, { metaKey: true }]) {
      const { ev, preventDefault } = makeKeyEvent(mod)
      expect(insertTabIndent(ev, 'abcd', setValue)).toBe(false)
      expect(preventDefault).not.toHaveBeenCalled()
    }
    expect(setValue).not.toHaveBeenCalled()
  })

  it('ignores Tab during IME composition', () => {
    const setValue = vi.fn()
    const comp1 = makeKeyEvent({ isComposing: true })
    expect(insertTabIndent(comp1.ev, 'abcd', setValue)).toBe(false)
    expect(comp1.preventDefault).not.toHaveBeenCalled()

    const comp2 = makeKeyEvent({ keyCode: 229 })
    expect(insertTabIndent(comp2.ev, 'abcd', setValue)).toBe(false)
    expect(comp2.preventDefault).not.toHaveBeenCalled()
    expect(setValue).not.toHaveBeenCalled()
  })
})

describe('isAllowedExternalUrl', () => {
  it('allows only http and https URLs', () => {
    expect(isAllowedExternalUrl('https://github.com/yellowdking/GravitySSH')).toBe(true)
    expect(isAllowedExternalUrl('http://127.0.0.1:8080/health')).toBe(true)
    expect(isAllowedExternalUrl('javascript:alert(1)')).toBe(false)
    expect(isAllowedExternalUrl('file:///C:/Windows/win.ini')).toBe(false)
    expect(isAllowedExternalUrl('data:text/html,hi')).toBe(false)
    expect(isAllowedExternalUrl('not-a-url')).toBe(false)
  })
})

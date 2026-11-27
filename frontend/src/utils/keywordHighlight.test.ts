import { describe, expect, it } from 'vitest'
import { createLogHighlighter } from './keywordHighlight'

function runHighlight(text: string): string {
  const hl = createLogHighlighter()
  return hl.highlight(text) + hl.reset()
}

describe('keywordHighlight', () => {
  it('highlights error, warning, ok, and IPv4 with foreground reset (39m)', () => {
    const out = runHighlight('ERROR: failed to connect 10.0.1.5, warning retry -> OK')
    expect(out).toContain('\x1b[38;5;203mERROR\x1b[39m')
    expect(out).toContain('\x1b[38;5;117m10.0.1.5\x1b[39m')
    expect(out).toContain('\x1b[38;5;221mwarning\x1b[39m')
    expect(out).toContain('\x1b[38;5;114mOK\x1b[39m')
  })

  it('does not overwrite text already colored by remote program', () => {
    const colored = '\x1b[32mERROR already green\x1b[0m plain ERROR'
    const out = runHighlight(colored)
    expect(out.startsWith('\x1b[32mERROR already green\x1b[0m plain \x1b[38;5;203mERROR\x1b[39m')).toBe(true)
  })

  it('carries incomplete ANSI escape sequences and active color state across chunks', () => {
    const hl = createLogHighlighter()
    const c1 = hl.highlight('prefix \x1b[3')
    const c2 = hl.highlight('2mERROR inside green')
    const c3 = hl.highlight('\x1b[0m plain ERROR')
    expect(c1).toBe('prefix ')
    expect(c2).toBe('\x1b[32mERROR inside green')
    expect(c3).toBe('\x1b[0m plain \x1b[38;5;203mERROR\x1b[39m')
  })

  it('skips highlighting on chunks larger than 16 KB', () => {
    const hl = createLogHighlighter()
    const big = 'ERROR '.repeat(3500) // > 16 KB
    expect(hl.highlight(big)).toBe(big)
  })
})

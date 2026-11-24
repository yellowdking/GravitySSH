import { describe, expect, it } from 'vitest'
import { highlightLogText } from './keywordHighlight'

describe('keywordHighlight', () => {
  it('highlights error, warning, ok, and IPv4 in uncolored log output', () => {
    const out = highlightLogText('ERROR: failed to connect 10.0.1.5, warning retry -> OK')
    expect(out).toContain('\x1b[38;5;203mERROR\x1b[0m')
    expect(out).toContain('\x1b[38;5;117m10.0.1.5\x1b[0m')
    expect(out).toContain('\x1b[38;5;221mwarning\x1b[0m')
    expect(out).toContain('\x1b[38;5;114mOK\x1b[0m')
  })

  it('does not overwrite text already colored by remote program', () => {
    const colored = '\x1b[32mERROR already green\x1b[0m plain ERROR'
    const out = highlightLogText(colored)
    expect(out.startsWith('\x1b[32mERROR already green\x1b[0m plain \x1b[38;5;203mERROR\x1b[0m')).toBe(true)
  })
})

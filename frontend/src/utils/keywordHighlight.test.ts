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

  it('preserves ISO-8613-3 colon-SGR and private CSI parameter bytes without overwriting colors', () => {
    const colonSgr = 'plain \x1b[38:5:203mERROR already red\x1b[39m plain ERROR'
    const out1 = runHighlight(colonSgr)
    expect(out1).toBe('plain \x1b[38:5:203mERROR already red\x1b[39m plain \x1b[38;5;203mERROR\x1b[39m')

    const colonRgb = '\x1b[38:2::255:128:0mwarning colored\x1b[0m ok'
    const out2 = runHighlight(colonRgb)
    expect(out2).toBe('\x1b[38:2::255:128:0mwarning colored\x1b[0m \x1b[38;5;114mok\x1b[39m')

    // Private CSI with > param byte must be skipped intact without breaking following text
    const privCsi = 'start \x1b[>4;2mERROR'
    const out3 = runHighlight(privCsi)
    expect(out3).toBe('start \x1b[>4;2m\x1b[38;5;203mERROR\x1b[39m')
  })

  it('never injects SGR highlights into OSC, DCS, APC, PM, or SOS control strings across chunks or >256 bytes', () => {
    // Split OSC title containing keyword ERROR
    const hlOsc = createLogHighlighter()
    const o1 = hlOsc.highlight('ok \x1b]0;build ERROR ')
    const o2 = hlOsc.highlight('failed ' + 'x'.repeat(300) + '\x1b')
    const o3 = hlOsc.highlight('\\ done ERROR')
    expect(o1 + o2 + o3).toBe(
      '\x1b[38;5;114mok\x1b[39m \x1b]0;build ERROR failed ' + 'x'.repeat(300) + '\x1b\\ done \x1b[38;5;203mERROR\x1b[39m'
    )

    // DCS tmux passthrough, APC, PM, SOS payloads containing keywords
    const dcs = runHighlight('ok \x1bPtmux;\x1b\x1b[31mERROR\x1b\\ warn')
    expect(dcs).toBe('\x1b[38;5;114mok\x1b[39m \x1bPtmux;\x1b\x1b[31mERROR\x1b\\ \x1b[38;5;221mwarn\x1b[39m')

    const apc = runHighlight('\x1b_GERROR=1,OK=2\x1b\\OK')
    expect(apc).toBe('\x1b_GERROR=1,OK=2\x1b\\\x1b[38;5;114mOK\x1b[39m')

    const pm = runHighlight('\x1b^ERROR secret\x1b\\failed')
    expect(pm).toBe('\x1b^ERROR secret\x1b\\\x1b[38;5;203mfailed\x1b[39m')

    const sos = runHighlight('\x1bXWARNING 10.0.0.1\x1b\\OK')
    expect(sos).toBe('\x1bXWARNING 10.0.0.1\x1b\\\x1b[38;5;114mOK\x1b[39m')
  })
})
